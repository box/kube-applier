package run_test

import (
	"fmt"
	"github.com/box/kube-applier/git"
	"github.com/box/kube-applier/run"
	"github.com/box/kube-applier/sysutil"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type checkerTestCase struct {
	clock          sysutil.ClockInterface
	repo           git.GitUtilInterface
	lastRunHash    string
	expectedOutput bool
	expectedErr    error
}

func TestCheckerShouldRun(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	clock := sysutil.NewMockClockInterface(mockCtrl)
	repo := git.NewMockGitUtilInterface(mockCtrl)

	// Conditions: No previous runs
	// Expected: shouldRun = true
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("hash", nil),
	)
	tc := checkerTestCase{clock, repo, "", true, nil}
	runTestCaseAndAssert(t, tc)

	// Conditions: Hash is same, time since last run > fullRunInterval
	// Expected: shouldRun = true
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("oldHash", nil),
		clock.EXPECT().Since(time.Time{}).Times(1).Return(2*time.Second),
	)
	tc = checkerTestCase{clock, repo, "oldHash", true, nil}
	runTestCaseAndAssert(t, tc)

	// Conditions: Hash is same, time since last run < fullRunInterval
	// Expected: shouldRun = false
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("oldHash", nil),
		clock.EXPECT().Since(time.Time{}).Times(1).Return(0*time.Second),
	)
	tc = checkerTestCase{clock, repo, "oldHash", false, nil}
	runTestCaseAndAssert(t, tc)

	// Conditions: Hash is different
	// Expected: shouldRun = true
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("newHash", nil),
	)
	tc = checkerTestCase{clock, repo, "oldHash", true, nil}
	runTestCaseAndAssert(t, tc)

	// Conditions: Hash error
	// Expected: shouldRun = false, return error
	gomock.InOrder(
		repo.EXPECT().HeadHash().Times(1).Return("", fmt.Errorf("hash error")),
	)
	tc = checkerTestCase{clock, repo, "oldHash", false, fmt.Errorf("hash error")}
	runTestCaseAndAssert(t, tc)
}

func runTestCaseAndAssert(t *testing.T, tc checkerTestCase) {
	assert := assert.New(t)
	c := run.Checker{tc.repo, tc.clock, 1 * time.Second}
	shouldRun, err := c.ShouldRun(&run.Result{CommitHash: tc.lastRunHash, Finish: time.Time{}})
	assert.Equal(tc.expectedOutput, shouldRun)
	assert.Equal(tc.expectedErr, err)
}
