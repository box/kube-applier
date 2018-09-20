package run

import (
	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

// Runner manages the full process of an apply run, including getting the appropriate files, running apply commands on them, and handling the results.
type Runner struct {
	RepoPath      string
	BatchApplier  BatchApplierInterface
	GitUtil       git.UtilInterface
	Clock         sysutil.ClockInterface
	Metrics       metrics.PrometheusInterface
	DiffURLFormat string
	RunQueue      <-chan bool
	RunResults    chan<- Result
	Errors        chan<- error
}

// Start runs a continuous loop that starts a new run when a request comes into the queue channel.
func (r *Runner) Start() {
	for range r.RunQueue {
		newRun, err := r.run()
		if err != nil {
			r.Errors <- err
			return
		}
		r.RunResults <- *newRun
	}
}

// Run performs a full apply run, and returns a Result with data about the completed run (or nil if the run failed to complete).
func (r *Runner) run() (*Result, error) {

	start := r.Clock.Now()
	log.Logger.Info("Started apply run", "start-time", start)

	dirs, err := sysutil.ListDirs(r.RepoPath)
	if err != nil {
		return nil, err
	}
	hash, err := r.GitUtil.HeadHash()
	if err != nil {
		return nil, err
	}
	commitLog, err := r.GitUtil.HeadCommitLog()
	if err != nil {
		return nil, err
	}

	successes, failures := r.BatchApplier.Apply(dirs)

	finish := r.Clock.Now()

	log.Logger.Info("Finished apply run", "stop-time", finish)

	success := len(failures) == 0

	results := make(map[string]string)
	for _, s := range successes {
		results[s.FilePath] = s.Output
	}
	r.Metrics.UpdateResultSummary(results)

	r.Metrics.UpdateRunLatency(r.Clock.Since(start).Seconds(), success)

	newRun := Result{start, finish, hash, commitLog, successes, failures, r.DiffURLFormat}
	return &newRun, nil
}
