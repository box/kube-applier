package run

import (
	"github.com/box/kube-applier/applylist"
	"github.com/box/kube-applier/git"
	"github.com/box/kube-applier/metrics"
	"github.com/box/kube-applier/sysutil"
	"log"
)

// Runner manages the full process of an apply run, including getting the appropriate files, running apply commands on them, and handling the results.
type Runner struct {
	BatchApplier  BatchApplierInterface
	ListFactory   applylist.FactoryInterface
	GitUtil       git.GitUtilInterface
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
	log.Printf("Started apply run at %v", start)

	applyList, blacklist, whitelist, err := r.ListFactory.Create()
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

	successes, failures := r.BatchApplier.Apply(applyList)

	finish := r.Clock.Now()

	log.Printf("Finished apply run at %v", finish)

	success := len(failures) == 0
	r.Metrics.UpdateRunLatency(r.Clock.Since(start).Seconds(), success)

	newRun := Result{start, finish, hash, commitLog, blacklist, whitelist, successes, failures, r.DiffURLFormat}
	return &newRun, nil
}
