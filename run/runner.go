package run

import (
	"fmt"
	"path"
	"path/filepath"

	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/kube"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

// Type defines what kind of apply run is performed.
type Type int

func (t Type) String() string {
	switch t {
	case FullRun:
		return "Full run"
	case PartialRun:
		return "Partial run"
	case FailedRun:
		return "Failed-only run"
	default:
		return "Unknown run type"
	}
}

const (
	// FullRun indicates a full apply run across all directories.
	FullRun Type = iota
	// PartialRun indicates a partial apply run, considering only directories
	// which have changed in the git repository since the last successful apply
	// run.
	PartialRun
	// FailedRun indicates a partial apply run, considering only directories
	// which failed to apply in the last run.
	FailedRun
)

// Runner manages the full process of an apply run, including getting the appropriate files, running apply commands on them, and handling the results.
type Runner struct {
	RepoPath        string
	RepoPathFilters []string
	BatchApplier    BatchApplierInterface
	GitUtil         git.UtilInterface
	Clock           sysutil.ClockInterface
	Metrics         metrics.PrometheusInterface
	KubeClient      kube.ClientInterface
	DiffURLFormat   string
	RunQueue        <-chan Type
	RunResults      chan<- Result
	Errors          chan<- error
	lastAppliedHash map[string]string
	lastRunFailures []string
}

// Start runs a continuous loop that starts a new run when a request comes into the queue channel.
func (r *Runner) Start() {
	if r.lastAppliedHash == nil {
		r.lastAppliedHash = make(map[string]string)
	}
	if r.lastRunFailures == nil {
		r.lastRunFailures = make([]string, 0)
	}
	for t := range r.RunQueue {
		newRun, err := r.run(t)
		if err != nil {
			r.Errors <- err
			return
		}
		r.RunResults <- *newRun
	}
}

// Run performs an apply run of the specified type, and returns a Result with
// data about the completed run (or nil if the run failed to complete).
func (r *Runner) run(t Type) (*Result, error) {

	start := r.Clock.Now()
	log.Logger.Info("Started apply run", "start-time", start)

	var dirs []string
	if t == FailedRun {
		dirs = r.lastRunFailures
	} else {
		d, err := sysutil.ListDirs(r.RepoPath)
		if err != nil {
			return nil, err
		}
		d = r.pruneDirs(d)
		if t == PartialRun {
			d = r.pruneUnchangedDirs(d)
		}
		dirs = d
	}

	hash, err := r.GitUtil.HeadHashForPaths(r.RepoPathFilters...)
	if err != nil {
		return nil, err
	}
	commitLog, err := r.GitUtil.HeadCommitLogForPaths(r.RepoPathFilters...)
	if err != nil {
		return nil, err
	}

	clusterResources, namespacedResources, err := r.KubeClient.PrunableResourceGVKs()
	if err != nil {
		return nil, err
	}
	applyOptions := &ApplyOptions{
		ClusterResources:    clusterResources,
		NamespacedResources: namespacedResources,
	}

	log.Logger.Debug(fmt.Sprintf("applying dirs: %v", dirs))
	successes, failures := r.BatchApplier.Apply(dirs, applyOptions)

	finish := r.Clock.Now()

	log.Logger.Info("Finished apply run", "stop-time", finish)

	success := len(failures) == 0

	results := make(map[string]string)
	for _, s := range successes {
		results[s.FilePath] = s.Output
	}
	r.Metrics.UpdateResultSummary(results)

	r.Metrics.UpdateRunLatency(r.Clock.Since(start).Seconds(), success)
	r.Metrics.UpdateLastRunTimestamp(finish)

	newRun := Result{start, finish, hash, commitLog, successes, failures, r.DiffURLFormat, t}
	for _, s := range successes {
		r.lastAppliedHash[s.FilePath] = hash
	}
	r.lastRunFailures = make([]string, len(failures))
	for i, f := range failures {
		r.lastRunFailures[i] = f.FilePath
	}
	return &newRun, nil
}

func (r *Runner) pruneDirs(dirs []string) []string {
	if len(r.RepoPathFilters) == 0 {
		return dirs
	}

	var prunedDirs []string
	for _, dir := range dirs {
		for _, repoPathFilter := range r.RepoPathFilters {
			matched, err := filepath.Match(path.Join(r.RepoPath, repoPathFilter), dir)
			if err != nil {
				log.Logger.Error(err.Error())
			} else if matched {
				prunedDirs = append(prunedDirs, dir)
			}
		}
	}

	return prunedDirs
}

func (r *Runner) pruneUnchangedDirs(dirs []string) []string {
	var prunedDirs []string
	for _, dir := range dirs {
		if r.lastAppliedHash[dir] != "" {
			changed, err := r.GitUtil.HasChangesForPath(dir, r.lastAppliedHash[dir])
			if err != nil {
				log.Logger.Warn(fmt.Sprintf("Could not check dir '%s' for changes, forcing apply: %v", dir, err))
				changed = true
			}
			if !changed {
				continue
			}
		} else {
			log.Logger.Info(fmt.Sprintf("No previous apply recorded for '%s', forcing apply", dir))
		}
		prunedDirs = append(prunedDirs, dir)
	}
	return prunedDirs
}
