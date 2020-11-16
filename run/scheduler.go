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
	Errors                  chan<- error
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

func (s *Scheduler) updateApplicationsLoop() {
	ticker := time.NewTicker(s.ApplicationPollInterval)
	defer ticker.Stop()
	defer s.waitGroup.Done()
	for {
		select {
		case <-ticker.C:
			apps, err := s.KubeClient.ListApplications(context.TODO())
			if err != nil {
				log.Logger.Error("Could not list Applications: %v", err)
				break
			}
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
				log.Logger.Warn(fmt.Sprintf("Could not get HEAD hash: %v", err))
				break
			}
			if hash == s.gitLastQueuedHash {
				break
			}
			s.applicationsMutex.Lock()
			for i := range s.applications {
				if s.applications[i].Status.LastRun != nil &&
					s.applications[i].Status.LastRun.Info.Commit != hash {
					s.enqueue(PollingRun, s.applications[i])
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
		ticker := time.NewTicker(time.Duration(app.Spec.RunInterval) * time.Second)
		defer ticker.Stop()
		defer close(stopped)
		if app.Status.LastRun == nil || time.Since(app.Status.LastRun.Started.Time) > time.Duration(app.Spec.RunInterval)*time.Second {
			s.enqueue(ScheduledRun, app)
		}
		for {
			select {
			case <-ticker.C:
				s.enqueue(ScheduledRun, app)
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

// enqueue attempts to add a run to the queue, logging the result of the request.
func (s *Scheduler) enqueue(t Type, app *kubeapplierv1alpha1.Application) {
	// TODO: how big of a channel buffer do we need here to avoid locking?
	// we should not ever drop requests
	select {
	case s.RunQueue <- Request{Type: t, Application: app}:
		log.Logger.Debug(fmt.Sprintf("%s queued for %s/%s", t, app.Namespace, app.Name))
	default:
		log.Logger.Info("Run queue is already full")
	}
}
