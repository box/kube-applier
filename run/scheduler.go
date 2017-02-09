package run

import (
	"github.com/box/kube-applier/git"
	"log"
	"time"
)

// Scheduler handles queueing apply runs at a given time interval and upon every new Git commit.
type Scheduler struct {
	GitUtil         git.GitUtilInterface
	PollInterval    time.Duration
	FullRunInterval time.Duration
	RunQueue        chan<- bool
	Errors          chan<- error
}

// Start runs a continuous loop with two tickers for queueing runs.
// One ticker queues a new run every X seconds, where X is the value from $FULL_RUN_INTERVAL_SECONDS.
// The other ticker queues a new run upon every new Git commit, checking the repo every Y seconds where Y is the value from $POLL_INTERVAL_SECONDS.
func (s *Scheduler) Start() {
	pollTicker := time.NewTicker(s.PollInterval).C
	fullRunTicker := time.NewTicker(s.FullRunInterval).C
	lastCommitHash := ""

	for {
		select {
		case <-pollTicker:
			newCommitHash, err := s.GitUtil.HeadHash()
			if err != nil {
				s.Errors <- err
				return
			}
			if newCommitHash != lastCommitHash {
				log.Printf("Most recent commit hash is %v (previously was %v), queueing run.", newCommitHash, lastCommitHash)
				enqueue(s.RunQueue)
				lastCommitHash = newCommitHash
			}
		case <-fullRunTicker:
			log.Printf("Full run interval (%v) reached, queueing run.", s.FullRunInterval)
			enqueue(s.RunQueue)
		}
	}
}

// enqueue attempts to add a run to the queue, logging the result of the request.
func enqueue(runQueue chan<- bool) {
	select {
	case runQueue <- true:
		log.Print("Run queued.")
	default:
		log.Print("Run queue is already full.")
	}
}
