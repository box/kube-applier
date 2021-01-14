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
	Clock               sysutil.ClockInterface
	GitPollInterval     time.Duration
	KubeClient          *client.Client
	RepoPath            string
	RunQueue            chan<- Request
	WaybillPollInterval time.Duration
	waybills            map[string]*kubeapplierv1alpha1.Waybill
	waybillSchedulers   map[string]func()
	waybillsMutex       sync.Mutex
	gitUtil             *git.Util
	gitLastQueuedHash   string
	stop                chan bool
	waitGroup           *sync.WaitGroup
}

// Start runs two loops: one that keeps track of Waybills on apiserver and
// maintains loops for applying namespaces on a schedule, and one that watches
// the git repository for changes and queues runs for waybills that are affected
// by commits.
func (s *Scheduler) Start() {
	if s.waitGroup != nil {
		return
	}
	s.stop = make(chan bool)
	s.waitGroup = &sync.WaitGroup{}
	s.gitUtil = &git.Util{RepoPath: s.RepoPath}
	s.waybills = make(map[string]*kubeapplierv1alpha1.Waybill)
	s.waybillSchedulers = make(map[string]func())

	s.waitGroup.Add(1)
	go s.updateWaybillsLoop()
	s.waitGroup.Add(1)
	go s.gitPollingLoop()
}

// Stop gracefully shuts down the Scheduler.
func (s *Scheduler) Stop() {
	if s.waitGroup == nil {
		log.Logger("scheduler").Debug("already stopped or being stopped")
		return
	}
	close(s.stop)
	s.waitGroup.Wait()
	s.waitGroup = nil
	s.waybillsMutex.Lock()
	for _, cancel := range s.waybillSchedulers {
		cancel()
	}
	s.waybillSchedulers = nil
	s.waybills = nil
	s.waybillsMutex.Unlock()
}

func (s *Scheduler) updateWaybills() {
	waybills, err := s.KubeClient.ListWaybills(context.TODO())
	if err != nil {
		log.Logger("scheduler").Error("Could not list Waybills", "error", err)
		return
	}
	metrics.ReconcileFromWaybillList(waybills)
	metrics.UpdateResultSummary(waybills)
	s.waybillsMutex.Lock()
	for i := range waybills {
		wb := &waybills[i]
		if v, ok := s.waybills[wb.Namespace]; ok {
			if !reflect.DeepEqual(v, wb) {
				s.waybillSchedulers[wb.Namespace]()
				s.waybillSchedulers[wb.Namespace] = s.newWaybillLoop(wb)
				s.waybills[wb.Namespace] = wb
				log.Logger("scheduler").Debug("Waybill changed, updating schedulers", "waybill", fmt.Sprintf("%s/%s", wb.Namespace, wb.Name))
			}
		} else {
			s.waybillSchedulers[wb.Namespace] = s.newWaybillLoop(wb)
			s.waybills[wb.Namespace] = wb
		}
	}
	for ns := range s.waybills {
		found := false
		for _, wb := range waybills {
			if ns == wb.Namespace {
				found = true
				break
			}
		}
		if !found {
			s.waybillSchedulers[ns]()
			delete(s.waybillSchedulers, ns)
			delete(s.waybills, ns)
		}
	}
	s.waybillsMutex.Unlock()
}

func (s *Scheduler) updateWaybillsLoop() {
	ticker := time.NewTicker(s.WaybillPollInterval)
	defer ticker.Stop()
	defer s.waitGroup.Done()
	s.updateWaybills()
	for {
		select {
		case <-ticker.C:
			s.updateWaybills()
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
				log.Logger("scheduler").Warn("Git polling could not get HEAD hash", "error", err)
				break
			}
			// This check prevents the Scheduler from queueing multiple runs for
			// a Waybill; without this check, when a new commit appears it will
			// be eligible for new a run until it finishes the run and its
			// status is updated.
			// Waybills that are not in the Scheduler's cache when a new commit
			// appears will not be retroactively checked against the latest
			// commit when they are acknowledged. This is acceptable, since they
			// will (eventually) trigger a scheduled run.
			s.waybillsMutex.Lock()
			if hash == s.gitLastQueuedHash {
				s.waybillsMutex.Unlock()
				break
			}
			for i := range s.waybills {
				// If LastRun is nil, we don't trigger the Polling run at all
				// and instead rely on the Scheduled run to kickstart things.
				if s.waybills[i].Status.LastRun != nil && s.waybills[i].Status.LastRun.Commit != hash {
					sinceHash := s.waybills[i].Status.LastRun.Commit
					path := s.waybills[i].Spec.RepositoryPath
					if path == "" {
						path = s.waybills[i].Namespace
					}
					wbId := fmt.Sprintf("%s/%s", s.waybills[i].Namespace, s.waybills[i].Name)
					changed, err := s.gitUtil.HasChangesForPath(path, sinceHash)
					if err != nil {
						log.Logger("scheduler").Warn("Could not check path for changes, skipping polling run", "waybill", wbId, "path", path, "since", sinceHash, "error", err)
						continue
					}
					if !changed {
						continue
					}
					Enqueue(s.RunQueue, PollingRun, s.waybills[i])
				}
			}
			s.gitLastQueuedHash = hash
			s.waybillsMutex.Unlock()
		case <-s.stop:
			return
		}
	}
}

func (s *Scheduler) newWaybillLoop(waybill *kubeapplierv1alpha1.Waybill) func() {
	stop := make(chan bool)
	stopped := make(chan bool)
	go func() {
		defer close(stopped)

		// Immediately trigger if there is no previous run recorded, otherwise
		// wait for the proper amount of time in order to maintain the period.
		// If it's been too long, it will still trigger immediately since the
		// wait duration is going to be negative.
		if waybill.Status.LastRun == nil {
			Enqueue(s.RunQueue, ScheduledRun, waybill)
		} else {
			runAt := waybill.Status.LastRun.Started.Add(time.Duration(waybill.Spec.RunInterval) * time.Second)
			select {
			case <-time.After(runAt.Sub(s.Clock.Now())):
				Enqueue(s.RunQueue, ScheduledRun, waybill)
			case <-stop:
				return
			}
		}
		ticker := time.NewTicker(time.Duration(waybill.Spec.RunInterval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				Enqueue(s.RunQueue, ScheduledRun, waybill)
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
