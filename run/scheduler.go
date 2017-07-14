package run

import (
	"github.com/box/kube-applier/git"
	"log"
	"time"
)

// Scheduler handles queueing apply runs at a given time interval and upon every new Git commit.
type Scheduler struct {
	GitUtil        git.GitUtilInterface
	PollTicker     <-chan time.Time
	FullRunTicker  <-chan time.Time
	QuickRunQueue  chan string
	FullRunQueue   chan<- bool
	Errors         chan<- error
	LastCommitHash string
}

// Start runs a continuous loop with two tickers for queueing runs.
// One ticker queues a new run every X seconds, where X is the value from $FULL_RUN_INTERVAL_SECONDS.
// The other ticker queues a new run upon every new Git commit, checking the repo every Y seconds where Y is the value from $POLL_INTERVAL_SECONDS.
func (s *Scheduler) Start() {
	hash, err := s.GitUtil.HeadHash()
	if err != nil {
		s.Errors <- err
		return
	}
	s.LastCommitHash = hash

	log.Print("Queueing initial full run.")
	s.enqueueFull()

	for {
		select {
		case <-s.PollTicker:
			s.poll()
			if err != nil {
				s.Errors <- err
			}
		case <-s.FullRunTicker:
			log.Printf("Full run interval reached, queueing full run.")
			s.enqueueFull()
		}
	}
}

// poll checks the repository and queues a quick run (marked with HEAD hash) if there are new commits.
// Any existing queued quick run is dequeued and replaced with the newer hash.
func (s *Scheduler) poll() error {
	newCommitHash, err := s.GitUtil.HeadHash()
	if err != nil {
		return err
	}

	// Compare the current hash with the stored hash and queue a quick run if they are different.
	if newCommitHash != s.LastCommitHash {
		log.Printf("New HEAD hash is %v (previously %v).", newCommitHash, s.LastCommitHash)

		// Pop queue first in case there is a quick run queued with an older hash.
		select {
		case oldHash := <-s.QuickRunQueue:
			log.Printf("Removed quick run queued with hash %v.", oldHash)
		default:
		}
		s.QuickRunQueue <- newCommitHash
		log.Printf("Queued quick run with hash %v.", newCommitHash)
	}
	s.LastCommitHash = newCommitHash
	return nil
}

// enqueueFull pushes a run request to the full run queue.
func (s *Scheduler) enqueueFull() {
	select {
	case s.FullRunQueue <- true:
		log.Print("Queued full run.")
	default:
		log.Print("Full run queue already full.")
	}
}
