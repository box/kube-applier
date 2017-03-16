package run

import (
	"fmt"
	"github.com/box/kube-applier/applylist"
	"github.com/box/kube-applier/git"
	"github.com/box/kube-applier/metrics"
	"github.com/box/kube-applier/sysutil"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type runnerTestCase struct {
	batchApplier BatchApplierInterface
	factory      applylist.FactoryInterface
	repo         git.GitUtilInterface
	clock        sysutil.ClockInterface
	metrics      metrics.PrometheusInterface

	expectedResult *Result
	expectedErr    error
}

func TestRunnerRun(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	clock := sysutil.NewMockClockInterface(mockCtrl)
	repo := git.NewMockGitUtilInterface(mockCtrl)
	batchApplier := NewMockBatchApplierInterface(mockCtrl)
	factory := applylist.NewMockFactoryInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// Empty apply list and blacklist, empty successes and failures
	gomock.InOrder(
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create().Times(1).Return([]string{}, []string{}, nil),
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().HeadCommitLog().Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply([]string{}).Times(1).Return([]ApplyAttempt{}, []ApplyAttempt{}),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		clock.EXPECT().Since(time.Time{}).Times(1).Return(5*time.Second),
		metrics.EXPECT().UpdateRunLatency(5.0, true).Times(1),
	)
	expectedResult := &Result{
		time.Time{},
		time.Time{},
		"hash",
		"log",
		[]string{},
		[]ApplyAttempt{},
		[]ApplyAttempt{},
		"",
	}
	runAndAssert(t, runnerTestCase{batchApplier, factory, repo, clock, metrics, expectedResult, nil})

	// Apply list and blacklist, empty successes and failures
	gomock.InOrder(
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create().Times(1).Return([]string{"file1", "file2", "file3"}, []string{"black1", "black2"}, nil),
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().HeadCommitLog().Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply([]string{"file1", "file2", "file3"}).Times(1).Return([]ApplyAttempt{}, []ApplyAttempt{}),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		clock.EXPECT().Since(time.Time{}).Times(1).Return(5*time.Second),
		metrics.EXPECT().UpdateRunLatency(5.0, true).Times(1),
	)
	expectedResult = &Result{
		time.Time{},
		time.Time{},
		"hash",
		"log",
		[]string{"black1", "black2"},
		[]ApplyAttempt{},
		[]ApplyAttempt{},
		"",
	}
	runAndAssert(t, runnerTestCase{batchApplier, factory, repo, clock, metrics, expectedResult, nil})

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
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create().Times(1).Return([]string{"file1", "file2", "file3", "file4", "file5"}, []string{"black1", "black2"}, nil),
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().HeadCommitLog().Times(1).Return("log", nil),
		batchApplier.EXPECT().Apply([]string{"file1", "file2", "file3", "file4", "file5"}).Times(1).Return(successes, failures),
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		clock.EXPECT().Since(time.Time{}).Times(1).Return(5*time.Second),
		metrics.EXPECT().UpdateRunLatency(5.0, false).Times(1),
	)
	expectedResult = &Result{
		time.Time{},
		time.Time{},
		"hash",
		"log",
		[]string{"black1", "black2"},
		successes,
		failures,
		"",
	}
	runAndAssert(t, runnerTestCase{batchApplier, factory, repo, clock, metrics, expectedResult, nil})

	// factory.Create() error
	gomock.InOrder(
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create().Times(1).Return(nil, nil, fmt.Errorf("list error")),
	)
	runAndAssert(t, runnerTestCase{batchApplier, factory, repo, clock, metrics, nil, fmt.Errorf("list error")})

	// repo.HeadHash() error
	gomock.InOrder(
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create().Times(1).Return([]string{}, []string{}, nil),
		repo.EXPECT().HeadHash().Times(1).Return("", fmt.Errorf("hash error")),
	)
	runAndAssert(t, runnerTestCase{batchApplier, factory, repo, clock, metrics, nil, fmt.Errorf("hash error")})

	// repo.HeadCommitLog() error
	gomock.InOrder(
		clock.EXPECT().Now().Times(1).Return(time.Time{}),
		factory.EXPECT().Create().Times(1).Return([]string{}, []string{}, nil),
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
		repo.EXPECT().HeadCommitLog().Times(1).Return("", fmt.Errorf("log error")),
	)
	runAndAssert(t, runnerTestCase{batchApplier, factory, repo, clock, metrics, nil, fmt.Errorf("log error")})
}

func runAndAssert(t *testing.T, tc runnerTestCase) {
	assert := assert.New(t)
	r := Runner{tc.batchApplier, tc.factory, tc.repo, tc.clock, tc.metrics, "",
		make(chan bool, 1), make(chan Result, 5), make(chan error)}
	result, err := r.run()
	assert.Equal(tc.expectedResult, result)
	assert.Equal(tc.expectedErr, err)
}
