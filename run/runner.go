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
	RunQueue        <-chan bool
	RunResults      chan<- Result
	Errors          chan<- error
	lastRunHash     string
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

	dirs = r.pruneDirs(dirs)
	dirs = r.pruneUnchangedDirs(dirs)

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

	newRun := Result{start, finish, hash, commitLog, successes, failures, r.DiffURLFormat}
	r.lastRunHash = hash
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
	if r.lastRunHash == "" {
		log.Logger.Info("No previous run recorded, applying everything")
		return dirs
	}
	var prunedDirs []string
	for _, dir := range dirs {
		changed, err := r.GitUtil.HasChangesForPath(dir, r.lastRunHash)
		if err != nil {
			log.Logger.Warn(fmt.Sprintf("Could not check dir '%s' for changes, forcing apply", dir))
			changed = true
		}
		if changed {
			prunedDirs = append(prunedDirs, dir)
		}
	}
	return prunedDirs
}
