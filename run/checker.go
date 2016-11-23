package run

import (
	"github.com/box/kube-applier/git"
	"github.com/box/kube-applier/sysutil"
	"time"
)

// CheckerInterface allows for mocking out the functionality of Checker when testing the full process of an apply run.
type CheckerInterface interface {
	ShouldRun(lastRun *Result) (bool, error)
}

// Checker determines whether or not an apply run is necessary at the current moment.
type Checker struct {
	GitUtil         git.GitUtilInterface
	Clock           sysutil.ClockInterface
	FullRunInterval time.Duration
}

// ShouldRun returns true if a run is necessary, false if not. There are two indicators that a run is necessary.
// 1. The HEAD commit hash of the repo has changed since the last run.
// 2. The time passed since the last run is greater than the specified interval for periodic runs.
func (c *Checker) ShouldRun(lastRun *Result) (bool, error) {
	newCommitHash, err := c.GitUtil.HeadHash()
	if err != nil {
		return false, err
	}
	if newCommitHash != lastRun.CommitHash {
		return true, nil
	}
	return (c.Clock.Since(lastRun.Finish) > c.FullRunInterval), nil
}
