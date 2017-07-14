package run

import (
	"fmt"
	"github.com/box/kube-applier/git"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

// TestSchedulerPoll tests the poll() function, which checks the repo and queues a new run if there are new commits.
// We force the result of HeadHash() and then ensure expected behavior (run queued, run not queued, queue overwritten, etc).
func TestSchedulerPoll(t *testing.T) {
	assert := assert.New(t)
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	repo := git.NewMockGitUtilInterface(mockCtrl)
	pollTicker := make(chan time.Time)
	fullRunTicker := make(chan time.Time)
	quickRunQueue := make(chan string, 1)
	fullRunQueue := make(chan bool, 1)
	errors := make(chan error, 1)
	lastCommitHash := ""

	s := &Scheduler{repo, pollTicker, fullRunTicker, quickRunQueue, fullRunQueue, errors, lastCommitHash}

	// Cases for each call to s.poll()
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("hash0", nil),
		repo.EXPECT().HeadHash().Times(2).Return("hash1", nil),
		repo.EXPECT().HeadHash().Times(1).Return("hash2", nil),
		repo.EXPECT().HeadHash().Times(1).Return("hash3", nil),
		repo.EXPECT().HeadHash().Times(1).Return("", fmt.Errorf("git error")),
	)

	// First poll is hash0.
	// Queue run with hash0, receive the queued run and check hash, then check queue is empty.
	err := s.poll()
	assert.Nil(err)
	assert.Equal("hash0", s.LastCommitHash)
	hash := <-quickRunQueue
	assert.Equal("hash0", hash)
	assert.True(checkQuickEmpty(quickRunQueue))

	// Second poll is hash1.
	// Queue run with hash1, receive the queued run and check hash, then check queue is empty.
	err = s.poll()
	assert.Nil(err)
	assert.Equal("hash1", s.LastCommitHash)
	hash = <-quickRunQueue
	assert.Equal("hash1", hash)
	assert.True(checkQuickEmpty(quickRunQueue))

	// Third poll is hash1.
	// No change in hash, so queue should be empty.
	err = s.poll()
	assert.Nil(err)
	assert.Equal("hash1", s.LastCommitHash)
	assert.True(checkQuickEmpty(quickRunQueue))

	// Fourth poll is hash2.
	// Queue run with hash2.
	err = s.poll()
	assert.Nil(err)
	assert.Equal("hash2", s.LastCommitHash)
	assert.False(checkQuickEmpty(quickRunQueue))

	// Fifth poll is hash3.
	// Queue run with hash3 (overwrites hash2), receive the queued run and check hash, then check queue is empty.
	err = s.poll()
	assert.Nil(err)
	assert.Equal("hash3", s.LastCommitHash)
	hash = <-quickRunQueue
	assert.Equal("hash3", hash)
	assert.True(checkQuickEmpty(quickRunQueue))

	// Sixth poll returns error.
	// Check lastCommitHash is still hash3, check queue still empty.
	err = s.poll()
	assert.Equal(fmt.Errorf("git error"), err)
	assert.Equal("hash3", s.LastCommitHash)
	assert.True(checkQuickEmpty(quickRunQueue))
}

// TestSchedulerEnqueueFull tests the enqueueFull() function, which attempts to add a run to the fullRunQueue.
func TestSchedulerEnqueueFull(t *testing.T) {
	assert := assert.New(t)
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	repo := git.NewMockGitUtilInterface(mockCtrl)
	pollTicker := make(chan time.Time)
	fullRunTicker := make(chan time.Time)
	quickRunQueue := make(chan string, 1)
	fullRunQueue := make(chan bool, 1)
	errors := make(chan error, 1)
	lastCommitHash := ""

	s := &Scheduler{repo, pollTicker, fullRunTicker, quickRunQueue, fullRunQueue, errors, lastCommitHash}

	// Check queue is empty, queue full run, check queue is not empty.
	assert.True(checkFullEmpty(fullRunQueue))
	s.enqueueFull()
	assert.False(checkFullEmpty(fullRunQueue))

	// Empty queue and check queue is empty.
	<-fullRunQueue
	assert.True(checkFullEmpty(fullRunQueue))

	// Queue full run, check queue is not empty.
	s.enqueueFull()
	assert.False(checkFullEmpty(fullRunQueue))

	// Queue multiple full runs.
	// There should still only be one run in the queue.
	s.enqueueFull()
	s.enqueueFull()
	s.enqueueFull()

	// Pop one run and check queue is empty.
	<-fullRunQueue
	assert.True(checkFullEmpty(fullRunQueue))

}

// Return true if the queue is empty. If not empty, put the item back and return false.
func checkQuickEmpty(queue chan string) bool {
	empty := false
	select {
	case val := <-queue:
		queue <- val
	default:
		empty = true
	}
	return empty
}

// Return true if the queue is empty. If not empty, put the item back and return false.
func checkFullEmpty(queue chan bool) bool {
	empty := false
	select {
	case val := <-queue:
		queue <- val
	default:
		empty = true
	}
	return empty
}
