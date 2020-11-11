package run

import (
	"context"
	"fmt"
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
	applicationsMutex       sync.Mutex
	gitUtil                 *git.Util
	stop                    chan bool
	waitGroup               *sync.WaitGroup
}

// Start runs a continuous loop with two tickers for queueing runs.
// One ticker queues a new run every X seconds, where X is the value from $FULL_RUN_INTERVAL_SECONDS.
// The other ticker queues a new run upon every new Git commit, checking the repo every Y seconds where Y is the value from $POLL_INTERVAL_SECONDS.
func (s *Scheduler) Start() {
	if s.waitGroup != nil {
		return
	}
	s.stop = make(chan bool)
	s.waitGroup = &sync.WaitGroup{}
	s.gitUtil = &git.Util{RepoPath: s.RepoPath}
	s.applications = make(map[string]*kubeapplierv1alpha1.Application)

	go s.updateApplicationsLoop()
	go s.gitPollingLoop()
}

func (s *Scheduler) Stop() {
	close(s.stop)
	s.waitGroup.Wait()
	s.waitGroup = nil
}

func (s *Scheduler) updateApplicationsLoop() {
	ticker := time.NewTicker(s.ApplicationPollInterval)
	defer ticker.Stop()
	s.waitGroup.Add(1)
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
			for _, app := range apps {
				if _, ok := s.applications[app.Namespace]; ok {
					// TODO: check polling interval has changed and update accordingly
					s.applications[app.Namespace] = &app
				} else {
					// TODO: setup new loops
				}
				// TODO: cancel deleted
			}
			// TODO: setup all the scheduled run tickers
			s.applicationsMutex.Unlock()
		case <-s.stop:
			return
		}
	}
}

func (s *Scheduler) gitPollingLoop() {
	ticker := time.NewTicker(s.GitPollInterval)
	defer ticker.Stop()
	s.waitGroup.Add(1)
	defer s.waitGroup.Done()
	for {
		select {
		case <-ticker.C:
			newHeadHash, err := s.gitUtil.HeadHashForPaths(".")
			if err != nil {
				log.Logger.Warn(fmt.Sprintf("Could not get HEAD hash: %v", err))
				break
			}
			s.applicationsMutex.Lock()
			// TODO: this needs to be updated or we might apply again and again
			for i := range s.applications {
				if s.applications[i].Status.LastRun != nil &&
					s.applications[i].Status.LastRun.Info.Commit != newHeadHash {
					s.enqueue(PollingRun, s.applications[i])
				}
			}
			s.applicationsMutex.Unlock()
		case <-s.stop:
			return
		}
	}
}

func (s *Scheduler) newApplicationLoop(app *kubeapplierv1alpha1.Application) func() {
	ticker := time.NewTicker(time.Duration(app.Spec.RunInterval) * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.enqueue(ScheduledRun, app)
			}
		}
	}()
	// TODO: does this cause it to queue one last time when we cancel?
	return ticker.Stop
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
