package run

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/client"
	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

// Type defines what kind of apply run is performed.
type Type int

func typeFromString(s string) Type {
	for i, v := range typeToString {
		if s == v {
			return Type(i)
		}
	}
	return -1
}

func (t Type) String() string {
	if int(t) >= len(typeToString) || int(t) < 0 {
		return "Unknown run type"
	}
	return typeToString[int(t)]
}

var typeToString = []string{
	"Scheduled run",   // ScheduledRun
	"Forced run",      // ForcedRun
	"Git polling run", // PollingRun
	"Failed run",      // FailedRun
}

const (
	// ScheduledRun indicates a scheduled, regular apply run.
	ScheduledRun Type = iota
	// ForcedRun indicates a forced (triggered on the UI) apply run.
	ForcedRun
	// PollingRun indicated a run triggered by changes in the git repository.
	PollingRun
	// FailedRun indicates an apply run, scheduled after a previous failure.
	FailedRun
)

// Scheduler handles queueing apply runs.
type Scheduler struct {
	ApplicationPollInterval time.Duration
	Clock                   sysutil.ClockInterface
	GitPollInterval         time.Duration
	KubeClient              *client.Client
	RepoPath                string
	RunQueue                chan<- Request
	applications            map[string]*kubeapplierv1alpha1.Application
	applicationSchedulers   map[string]func()
	applicationsMutex       sync.Mutex
	gitUtil                 *git.Util
	gitLastQueuedHash       string
	stop                    chan bool
	waitGroup               *sync.WaitGroup
}

// Start runs two loops: one that keeps track of Applications on apiserver and
// maintains loops for applying namespaces on a schedule, and one that watches
// the git repository for changes and queues runs for applications that are
// affected by commits.
func (s *Scheduler) Start() {
	if s.waitGroup != nil {
		return
	}
	s.stop = make(chan bool)
	s.waitGroup = &sync.WaitGroup{}
	s.gitUtil = &git.Util{RepoPath: s.RepoPath}
	s.applications = make(map[string]*kubeapplierv1alpha1.Application)
	s.applicationSchedulers = make(map[string]func())

	s.waitGroup.Add(1)
	go s.updateApplicationsLoop()
	s.waitGroup.Add(1)
	go s.gitPollingLoop()
}

// Stop gracefully shuts down the Scheduler.
func (s *Scheduler) Stop() {
	close(s.stop)
	s.waitGroup.Wait()
	s.waitGroup = nil
	s.applicationsMutex.Lock()
	for _, cancel := range s.applicationSchedulers {
		cancel()
	}
	s.applicationSchedulers = nil
	s.applications = nil
	s.applicationsMutex.Unlock()
}

func (s *Scheduler) updateApplications() {
	apps, err := s.KubeClient.ListApplications(context.TODO())
	if err != nil {
		log.Logger.Error("Scheduler could not list Applications", "error", err)
		return
	}
	metrics.ReconcileFromApplicationList(apps)
	metrics.UpdateResultSummary(apps)
	s.applicationsMutex.Lock()
	for i := range apps {
		app := &apps[i]
		if v, ok := s.applications[app.Namespace]; ok {
			if !reflect.DeepEqual(v.Spec, app.Spec) {
				s.applicationSchedulers[app.Namespace]()
				s.applicationSchedulers[app.Namespace] = s.newApplicationLoop(app)
				s.applications[app.Namespace] = app
			}
		} else {
			s.applicationSchedulers[app.Namespace] = s.newApplicationLoop(app)
			s.applications[app.Namespace] = app
		}
	}
	for ns := range s.applications {
		found := false
		for _, app := range apps {
			if ns == app.Namespace {
				found = true
				break
			}
		}
		if !found {
			s.applicationSchedulers[ns]()
			delete(s.applicationSchedulers, ns)
			delete(s.applications, ns)
		}
	}
	s.applicationsMutex.Unlock()
}

func (s *Scheduler) updateApplicationsLoop() {
	ticker := time.NewTicker(s.ApplicationPollInterval)
	defer ticker.Stop()
	defer s.waitGroup.Done()
	s.updateApplications()
	for {
		select {
		case <-ticker.C:
			s.updateApplications()
		case <-s.stop:
			return
		}
	}
}

func (s *Scheduler) gitPollingLoop() {
	ticker := time.NewTicker(s.GitPollInterval)
	defer ticker.Stop()
	defer s.waitGroup.Done()
	for {
		select {
		case <-ticker.C:
			hash, err := s.gitUtil.HeadHashForPaths(".")
			if err != nil {
				log.Logger.Warn("Scheduler git polling could not get HEAD hash", "error", err)
				break
			}
			if hash == s.gitLastQueuedHash {
				break
			}
			s.applicationsMutex.Lock()
			for i := range s.applications {
				// If LastRun is nil, we don't trigger the Polling run at all
				// and instead rely on the Scheduled run to kickstart things.
				if s.applications[i].Status.LastRun != nil && s.applications[i].Status.LastRun.Commit != hash {
					sinceHash := s.applications[i].Status.LastRun.Commit
					path := s.applications[i].Spec.RepositoryPath
					appId := fmt.Sprintf("%s/%s", s.applications[i].Namespace, s.applications[i].Name)
					changed, err := s.gitUtil.HasChangesForPath(path, sinceHash)
					if err != nil {
						log.Logger.Warn("Could not check path for changes, skipping polling run", "app", appId, "path", path, "since", sinceHash, "error", err)
						continue
					}
					if !changed {
						continue
					}
					Enqueue(s.RunQueue, PollingRun, s.applications[i])
				}
			}
			s.gitLastQueuedHash = hash
			s.applicationsMutex.Unlock()
		case <-s.stop:
			return
		}
	}
}

func (s *Scheduler) newApplicationLoop(app *kubeapplierv1alpha1.Application) func() {
	stop := make(chan bool)
	stopped := make(chan bool)
	go func() {
		defer close(stopped)

		// Immediately trigger if there is no previous run recorded, otherwise
		// wait for the proper amount of time in order to maintain the period.
		// If it's been too long, it will still trigger immediately since the
		// wait duration is going to be negative.
		if app.Status.LastRun == nil {
			Enqueue(s.RunQueue, ScheduledRun, app)
		} else {
			runAt := app.Status.LastRun.Started.Add(time.Duration(app.Spec.RunInterval) * time.Second)
			select {
			case <-time.After(runAt.Sub(s.Clock.Now())):
				Enqueue(s.RunQueue, ScheduledRun, app)
			case <-stop:
				return
			}
		}
		ticker := time.NewTicker(time.Duration(app.Spec.RunInterval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				Enqueue(s.RunQueue, ScheduledRun, app)
			case <-stop:
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-stopped
	}
}
