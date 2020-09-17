package run

import (
	"time"

	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/log"
)

// Scheduler handles queueing apply runs at a given time interval and upon every new Git commit.
type Scheduler struct {
	GitUtil         git.UtilInterface
	PollInterval    time.Duration
	FullRunInterval time.Duration
	RepoPathFilters []string
	RunQueue        chan<- Request
	Errors          chan<- error
}

// Start runs a continuous loop with two tickers for queueing runs.
// One ticker queues a new run every X seconds, where X is the value from $FULL_RUN_INTERVAL_SECONDS.
// The other ticker queues a new run upon every new Git commit, checking the repo every Y seconds where Y is the value from $POLL_INTERVAL_SECONDS.
func (s *Scheduler) Start() {
	if s.FullRunInterval != 0 {
		fullRunTicker := time.NewTicker(s.FullRunInterval)
		defer fullRunTicker.Stop()
		fullRunTickerChan := fullRunTicker.C
		go func() {
			for {
				select {
				case <-fullRunTickerChan:
					log.Logger.Info("Full run interval reached, queueing run", "interval", s.FullRunInterval)
					s.enqueue(s.RunQueue, FullRun)
				}
			}
		}()
	}

	pollTicker := time.NewTicker(s.PollInterval)
	defer pollTicker.Stop()
	pollTickerChan := pollTicker.C
	lastCommitHash := ""
	for {
		select {
		case <-pollTickerChan:
			newCommitHash, err := s.GitUtil.HeadHashForPaths(s.RepoPathFilters...)
			if err != nil {
				s.Errors <- err
				return
			}
			if newCommitHash != lastCommitHash {
				log.Logger.Info("Queueing run", "newest-commit", newCommitHash, "last-commit", lastCommitHash)
				s.enqueue(s.RunQueue, PartialRun)
				lastCommitHash = newCommitHash
			}
		}
	}
}

// enqueue attempts to add a run to the queue, logging the result of the request.
func (s *Scheduler) enqueue(runQueue chan<- Request, t Type) {
	select {
	case runQueue <- Request{Type: t}:
		log.Logger.Info("Run queued")
	default:
		log.Logger.Info("Run queue is already full")
	}
}
