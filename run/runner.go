package run

import (
	"github.com/box/kube-applier/applylist"
	"github.com/box/kube-applier/git"
	"github.com/box/kube-applier/sysutil"
	"log"
)

// Runner manages the full process of an apply run, including getting the appropriate files, running apply commands on them, and handling the results.
type Runner struct {
	BatchApplier  BatchApplierInterface
	ListFactory   applylist.FactoryInterface
	GitUtil       git.GitUtilInterface
	Clock         sysutil.ClockInterface
	DiffURLFormat string
	LastHash      string
	QuickRunQueue <-chan string
	FullRunQueue  <-chan bool
	RunResults    chan<- Result
	RunMetrics    chan<- Result
	Errors        chan<- error
	RunCount      chan int
}

// StartFullLoop runs a continuous loop that starts a new full run through the repo when a request comes into the queue channel.
func (r *Runner) StartFullLoop() {
	for range r.FullRunQueue {
		id := <-r.RunCount
		result, err := r.fullRun(id)
		if err != nil {
			r.Errors <- err
			return
		}
		r.RunResults <- *result
		r.RunMetrics <- *result
	}
}

// StartQuickLoop runs a continuous loop that starts a new quick run (based on a diff) when a request comes into the queue channel.
func (r *Runner) StartQuickLoop() {
	initHash, err := r.GitUtil.HeadHash()
	if err != nil {
		r.Errors <- err
		return
	}
	r.LastHash = initHash
	for hash := range r.QuickRunQueue {
		id := <-r.RunCount
		result, err := r.quickRun(id, hash)
		if err != nil {
			r.Errors <- err
			return
		}
		r.RunResults <- *result
		r.RunMetrics <- *result
	}
}

// StartRunCounter maintains a run count so that runs can be labeled with an ID.
func (r *Runner) StartRunCounter() {
	count := 0
	for {
		// This will block until received.
		r.RunCount <- count
		// When a run receives the current count, update the count.
		count++
	}
}

// fullRun initiates a full apply run, considering all files in the repo as candidates for applying.
// The current HEAD hash and list of all files in the repo are passed to the "run" helper function.
func (r *Runner) fullRun(id int) (*Result, error) {
	hash, err := r.GitUtil.HeadHash()
	if err != nil {
		return nil, err
	}
	rawList, err := r.GitUtil.ListAllFiles()
	if err != nil {
		return nil, err
	}
	log.Printf("RUN %v: Starting full run with hash %v", id, hash)
	result, err := r.run(id, FullRun, rawList, hash)
	log.Printf("RUN %v: Finished full run.", id)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// quickRun initiates a quick apply run, considering only files modified since the last run as candidates for applying.
// The input commit hash is used in a diff to get the list of modified files, which is passed to the "run" helper function.
func (r *Runner) quickRun(id int, hash string) (*Result, error) {
	rawList, err := r.GitUtil.ListDiffFiles(r.LastHash, hash)
	if err != nil {
		return nil, err
	}
	log.Printf("RUN %v: Starting quick run with hash %v.", id, hash)
	result, err := r.run(id, QuickRun, rawList, hash)
	log.Printf("RUN %v: Finished quick run.", id)
	if err != nil {
		return nil, err
	}
	// Only update LastHash as part of quick run.
	// If we updated at end of full run, a long full run might set LastHash back to outdated value.
	r.LastHash = hash
	return result, nil
}

// run takes in a list of candidate files, filters using the whitelist/blacklist, and applies them.
// run returns a Result with info about the run.
func (r *Runner) run(id int, runType RunType, rawList []string, hash string) (*Result, error) {
	start := r.Clock.Now()

	applyList, blacklist, whitelist, err := r.ListFactory.Create(rawList)
	if err != nil {
		return nil, err
	}

	commitLog, err := r.GitUtil.CommitLog(hash)
	if err != nil {
		return nil, err
	}

	successes, failures := r.BatchApplier.Apply(id, applyList)

	finish := r.Clock.Now()

	newRun := &Result{id, runType, start, finish, hash, commitLog, blacklist, whitelist, successes, failures, r.DiffURLFormat}
	return newRun, err
}
