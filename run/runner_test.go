package run

import (
	"fmt"
	"github.com/box/kube-applier/applylist"
	"github.com/box/kube-applier/git"
	"github.com/box/kube-applier/sysutil"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type testCase struct {
	runResults     <-chan Result
	runMetrics     <-chan Result
	errors         <-chan error
	expectedResult Result
	expectedErr    error
}

func TestRunnerStartFullLoop(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	clock := sysutil.NewMockClockInterface(mockCtrl)
	repo := git.NewMockGitUtilInterface(mockCtrl)
	batchApplier := NewMockBatchApplierInterface(mockCtrl)
	factory := applylist.NewMockFactoryInterface(mockCtrl)

	errors := make(chan error)
	quickRunQueue := make(chan string, 1)
	fullRunQueue := make(chan bool, 1)
	runResults := make(chan Result, 5)
	runMetrics := make(chan Result, 5)
	runCount := make(chan int)
	r := Runner{batchApplier, factory, repo, clock, "", "", quickRunQueue, fullRunQueue, runResults, runMetrics, errors, runCount}

	go r.StartRunCounter()
	go r.StartFullLoop()

	// Empty apply list and blacklist, empty successes and failures
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().ListAllFiles().Times(1).Return([]string{}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{}).Times(1).Return([]string{}, []string{}, []string{}, nil),
		repo.EXPECT().CommitLog("hash").Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply(0, []string{}).Times(1).Return([]ApplyAttempt{}, []ApplyAttempt{}),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
	)
	expectedResult := Result{
		0,
		FullRun,
		time.Time{},
		time.Time{},
		"hash",
		"log",
		[]string{},
		[]string{},
		[]ApplyAttempt{},
		[]ApplyAttempt{},
		"",
	}
	fullRunQueue <- true
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, nil})

	// Apply list and blacklist, empty successes and failures
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().ListAllFiles().Times(1).Return([]string{"file1", "file2", "file3"}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{"file1", "file2", "file3"}).Times(1).Return([]string{"file1", "file2", "file3"}, []string{"black1", "black2"}, []string{}, nil),
		repo.EXPECT().CommitLog("hash").Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply(1, []string{"file1", "file2", "file3"}).Times(1).Return([]ApplyAttempt{}, []ApplyAttempt{}),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
	)
	expectedResult = Result{
		1,
		FullRun,
		time.Time{},
		time.Time{},
		"hash",
		"log",
		[]string{"black1", "black2"},
		[]string{},
		[]ApplyAttempt{},
		[]ApplyAttempt{},
		"",
	}
	fullRunQueue <- true
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, nil})

	// Apply list and blacklist, successes and failures
	successes := []ApplyAttempt{
		{"file1", "apply1", "cmd1", ""},
		{"file2", "apply2", "cmd2", ""},
		{"file4", "apply3", "cmd3", ""},
	}
	failures := []ApplyAttempt{
		{"file3", "apply3", "cmd3", "error3"},
		{"file5", "apply5", "cmd5", "error5"},
	}
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().ListAllFiles().Times(1).Return([]string{"file1", "file2", "file3", "file4", "file5"}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{"file1", "file2", "file3", "file4", "file5"}).Times(1).Return([]string{"file1", "file2", "file3", "file4", "file5"}, []string{"black1", "black2"}, []string{}, nil),
		repo.EXPECT().CommitLog("hash").Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply(2, []string{"file1", "file2", "file3", "file4", "file5"}).Times(1).Return(successes, failures),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
	)
	expectedResult = Result{
		2,
		FullRun,
		time.Time{},
		time.Time{},
		"hash",
		"log",
		[]string{"black1", "black2"},
		[]string{},
		successes,
		failures,
		"",
	}
	fullRunQueue <- true
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, nil})

	// Apply list, blacklist and whitelist , successes and failures
	successes = []ApplyAttempt{
		{"file1", "apply1", "cmd1", ""},
		{"file2", "apply2", "cmd2", ""},
		{"file4", "apply3", "cmd3", ""},
	}
	failures = []ApplyAttempt{
		{"file3", "apply3", "cmd3", "error3"},
		{"file5", "apply5", "cmd5", "error5"},
	}
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().ListAllFiles().Times(1).Return([]string{"file1", "file2", "file3", "file4", "file5"}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{"file1", "file2", "file3", "file4", "file5"}).Times(1).Return([]string{"file1", "file2", "file3", "file4", "file5"}, []string{"black1", "black2"}, []string{"file1", "file2", "file3", "file4", "file5"}, nil),
		repo.EXPECT().CommitLog("hash").Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply(3, []string{"file1", "file2", "file3", "file4", "file5"}).Times(1).Return(successes, failures),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
	)
	expectedResult = Result{
		3,
		FullRun,
		time.Time{},
		time.Time{},
		"hash",
		"log",
		[]string{"black1", "black2"},
		[]string{"file1", "file2", "file3", "file4", "file5"},
		successes,
		failures,
		"",
	}
	fullRunQueue <- true
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, nil})

	// HeadHash() error
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("", fmt.Errorf("hash error")),
	)
	fullRunQueue <- true
	waitAndAssert(t, testCase{runResults, runMetrics, errors, Result{}, fmt.Errorf("hash error")})

	// Need to restart, error shuts down goroutine
	go r.StartFullLoop()

	// ListAllFiles() error
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().ListAllFiles().Times(1).Return(nil, fmt.Errorf("list error")),
	)
	fullRunQueue <- true
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, fmt.Errorf("list error")})

	// Need to restart, error shuts down goroutine
	go r.StartFullLoop()

	// Create() error
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().ListAllFiles().Times(1).Return([]string{}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{}).Times(1).Return(nil, nil, nil, fmt.Errorf("create error")),
	)
	fullRunQueue <- true
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, fmt.Errorf("create error")})

	// Need to restart, error shuts down goroutine
	go r.StartFullLoop()

	// CommitLog() error
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().ListAllFiles().Times(1).Return([]string{}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{}).Times(1).Return([]string{}, []string{}, []string{}, nil),
		repo.EXPECT().CommitLog("hash").Times(1).Return("", fmt.Errorf("log error")),
	)
	fullRunQueue <- true
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, fmt.Errorf("log error")})
}

func TestRunnerStartQuickLoop(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	assert := assert.New(t)

	clock := sysutil.NewMockClockInterface(mockCtrl)
	repo := git.NewMockGitUtilInterface(mockCtrl)
	batchApplier := NewMockBatchApplierInterface(mockCtrl)
	factory := applylist.NewMockFactoryInterface(mockCtrl)

	errors := make(chan error)
	quickRunQueue := make(chan string, 1)
	fullRunQueue := make(chan bool, 1)
	runResults := make(chan Result, 5)
	runMetrics := make(chan Result, 5)
	runCount := make(chan int)
	r := Runner{batchApplier, factory, repo, clock, "", "", quickRunQueue, fullRunQueue, runResults, runMetrics, errors, runCount}

	go r.StartRunCounter()

	repo.EXPECT().HeadHash().Times(1).Return("initHash", nil)
	go r.StartQuickLoop()

	// Empty apply list and blacklist, empty successes and failures
	gomock.InOrder(
		repo.EXPECT().ListDiffFiles("initHash", "hash0").Times(1).Return([]string{}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{}).Times(1).Return([]string{}, []string{}, []string{}, nil),
		repo.EXPECT().CommitLog("hash0").Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply(0, []string{}).Times(1).Return([]ApplyAttempt{}, []ApplyAttempt{}),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
	)
	expectedResult := Result{
		0,
		QuickRun,
		time.Time{},
		time.Time{},
		"hash0",
		"log",
		[]string{},
		[]string{},
		[]ApplyAttempt{},
		[]ApplyAttempt{},
		"",
	}
	quickRunQueue <- "hash0"
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, nil})
	assert.Equal("hash0", r.LastHash)

	// Apply list and blacklist, empty successes and failures
	gomock.InOrder(
		repo.EXPECT().ListDiffFiles("hash0", "hash1").Times(1).Return([]string{"file1", "file2", "file3"}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{"file1", "file2", "file3"}).Times(1).Return([]string{"file1", "file2", "file3"}, []string{"black1", "black2"}, []string{}, nil),
		repo.EXPECT().CommitLog("hash1").Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply(1, []string{"file1", "file2", "file3"}).Times(1).Return([]ApplyAttempt{}, []ApplyAttempt{}),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
	)
	expectedResult = Result{
		1,
		QuickRun,
		time.Time{},
		time.Time{},
		"hash1",
		"log",
		[]string{"black1", "black2"},
		[]string{},
		[]ApplyAttempt{},
		[]ApplyAttempt{},
		"",
	}
	quickRunQueue <- "hash1"
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, nil})
	assert.Equal("hash1", r.LastHash)

	// Apply list and blacklist, successes and failures
	successes := []ApplyAttempt{
		{"file1", "apply1", "cmd1", ""},
		{"file2", "apply2", "cmd2", ""},
		{"file4", "apply3", "cmd3", ""},
	}
	failures := []ApplyAttempt{
		{"file3", "apply3", "cmd3", "error3"},
		{"file5", "apply5", "cmd5", "error5"},
	}
	gomock.InOrder(
		repo.EXPECT().ListDiffFiles("hash1", "hash2").Times(1).Return([]string{"file1", "file2", "file3", "file4", "file5"}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{"file1", "file2", "file3", "file4", "file5"}).Times(1).Return([]string{"file1", "file2", "file3", "file4", "file5"}, []string{"black1", "black2"}, []string{}, nil),
		repo.EXPECT().CommitLog("hash2").Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply(2, []string{"file1", "file2", "file3", "file4", "file5"}).Times(1).Return(successes, failures),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
	)
	expectedResult = Result{
		2,
		QuickRun,
		time.Time{},
		time.Time{},
		"hash2",
		"log",
		[]string{"black1", "black2"},
		[]string{},
		successes,
		failures,
		"",
	}
	quickRunQueue <- "hash2"
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, nil})
	assert.Equal("hash2", r.LastHash)

	// Apply list, blacklist and whitelist , successes and failures
	successes = []ApplyAttempt{
		{"file1", "apply1", "cmd1", ""},
		{"file2", "apply2", "cmd2", ""},
		{"file4", "apply3", "cmd3", ""},
	}
	failures = []ApplyAttempt{
		{"file3", "apply3", "cmd3", "error3"},
		{"file5", "apply5", "cmd5", "error5"},
	}
	gomock.InOrder(
		repo.EXPECT().ListDiffFiles("hash2", "hash3").Times(1).Return([]string{"file1", "file2", "file3", "file4", "file5"}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{"file1", "file2", "file3", "file4", "file5"}).Times(1).Return([]string{"file1", "file2", "file3", "file4", "file5"}, []string{"black1", "black2"}, []string{"file1", "file2", "file3", "file4", "file5"}, nil),
		repo.EXPECT().CommitLog("hash3").Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply(3, []string{"file1", "file2", "file3", "file4", "file5"}).Times(1).Return(successes, failures),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
	)
	expectedResult = Result{
		3,
		QuickRun,
		time.Time{},
		time.Time{},
		"hash3",
		"log",
		[]string{"black1", "black2"},
		[]string{"file1", "file2", "file3", "file4", "file5"},
		successes,
		failures,
		"",
	}
	quickRunQueue <- "hash3"
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, nil})
	assert.Equal("hash3", r.LastHash)

	// ListDiffFiles() error
	gomock.InOrder(
		repo.EXPECT().ListDiffFiles("hash3", "hash4").Times(1).Return(nil, fmt.Errorf("diff error")),
	)
	quickRunQueue <- "hash4"
	waitAndAssert(t, testCase{runResults, runMetrics, errors, Result{}, fmt.Errorf("diff error")})

	// Need to restart, error shuts down goroutine
	repo.EXPECT().HeadHash().Times(1).Return("hash4", nil)
	go r.StartQuickLoop()

	// Create() error
	gomock.InOrder(
		repo.EXPECT().ListDiffFiles("hash4", "hash5").Times(1).Return([]string{}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{}).Times(1).Return(nil, nil, nil, fmt.Errorf("create error")),
	)
	quickRunQueue <- "hash5"
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, fmt.Errorf("create error")})

	// Need to restart, error shuts down goroutine
	repo.EXPECT().HeadHash().Times(1).Return("hash5", nil)
	go r.StartQuickLoop()

	// CommitLog() error
	gomock.InOrder(
		repo.EXPECT().ListDiffFiles("hash5", "hash6").Times(1).Return([]string{}, nil),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create([]string{}).Times(1).Return([]string{}, []string{}, []string{}, nil),
		repo.EXPECT().CommitLog("hash6").Times(1).Return("", fmt.Errorf("log error")),
	)
	quickRunQueue <- "hash6"
	waitAndAssert(t, testCase{runResults, runMetrics, errors, expectedResult, fmt.Errorf("log error")})
}

func waitAndAssert(t *testing.T, tc testCase) {
	assert := assert.New(t)

	select {
	case result := <-tc.runResults:
		assert.Equal(tc.expectedResult, result)
		metricsResult := <-tc.runMetrics
		assert.Equal(tc.expectedResult, metricsResult)
	case err := <-tc.errors:
		assert.Equal(tc.expectedErr, err)
	}
}
