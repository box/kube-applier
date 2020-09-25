package run

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/kube"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

// Request defines an apply run request
type Request struct {
	Type Type
	Args interface{}
}

// Type defines what kind of apply run is performed.
type Type int

func (t Type) String() string {
	switch t {
	case ScheduledFullRun:
		return "Scheduled full run"
	case ForcedFullRun:
		return "Forced full run"
	case PartialRun:
		return "Git polling partial run"
	case FailedOnlyRun:
		return "Failed-only run"
	case SingleDirectoryRun:
		return "Single directory run"
	default:
		return "Unknown run type"
	}
}

const (
	// ScheduledFullRun indicates a scheduled, full apply run across all
	// directories.
	ScheduledFullRun Type = iota
	// ForcedFullRun indicates a forced (triggered on the UI), full apply run
	// across all directories.
	ForcedFullRun
	// PartialRun indicates a partial apply run, considering only directories
	// which have changed in the git repository since the last successful apply
	// run.
	PartialRun
	// FailedOnlyRun indicates a partial apply run, considering only directories
	// which failed to apply in the last run.
	FailedOnlyRun
	// SingleDirectoryRun indicates a partial apply run for a single directory.
	SingleDirectoryRun
)

// Runner manages the full process of an apply run, including getting the appropriate files, running apply commands on them, and handling the results.
type Runner struct {
	RepoPath        string
	RepoPathFilters []string
	BatchApplier    BatchApplierInterface
	Clock           sysutil.ClockInterface
	Metrics         metrics.PrometheusInterface
	KubeClient      kube.ClientInterface
	DiffURLFormat   string
	RunQueue        <-chan Request
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
		if newRun != nil {
			r.RunResults <- *newRun
		}
	}
}

// Run executes the requested apply run, and returns a Result with data about
// the completed run (or nil if the run failed to complete).
func (r *Runner) run(t Request) (*Result, error) {
	start := r.Clock.Now()
	log.Logger.Info("Started apply run", "start-time", start)

	gitUtil, cleanupTemp, err := r.copyRepository()
	if err != nil {
		return nil, err
	}
	defer cleanupTemp()

	var dirs []string
	if t.Type == FailedOnlyRun {
		dirs = r.lastRunFailures
	} else {
		d, err := sysutil.ListDirs(gitUtil.RepoPath)
		if err != nil {
			return nil, err
		}
		d = r.pruneDirs(d)

		if t.Type == PartialRun {
			d = r.pruneUnchangedDirs(gitUtil, d)
		} else if t.Type == SingleDirectoryRun {
			valid := false
			for _, v := range d {
				if v == t.Args.(string) {
					d = []string{v}
					valid = true
					break
				}
			}
			if !valid {
				log.Logger.Error(fmt.Sprintf("Invalid path '%s' requested, ignoring", t.Args.(string)))
				return nil, nil
			}
		}
		dirs = d
	}

	hash, err := gitUtil.HeadHashForPaths(r.RepoPathFilters...)
	if err != nil {
		return nil, err
	}
	commitLog, err := gitUtil.HeadCommitLogForPaths(r.RepoPathFilters...)
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
	successes, failures := r.BatchApplier.Apply(gitUtil.RepoPath, dirs, applyOptions)

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

	runInfo := Info{
		Start:         start,
		Finish:        finish,
		CommitHash:    hash,
		FullCommit:    commitLog,
		DiffURLFormat: r.DiffURLFormat,
		Type:          t.Type,
	}
	for i := range successes {
		successes[i].Run = runInfo
	}
	for i := range failures {
		failures[i].Run = runInfo
	}
	newRun := Result{
		LastRun:   runInfo,
		RootPath:  r.RepoPath,
		Successes: successes,
		Failures:  failures,
	}
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
			matched, err := filepath.Match(repoPathFilter, dir)
			if err != nil {
				log.Logger.Error(err.Error())
			} else if matched {
				prunedDirs = append(prunedDirs, dir)
			}
		}
	}

	return prunedDirs
}

func (r *Runner) pruneUnchangedDirs(gitUtil *git.Util, dirs []string) []string {
	var prunedDirs []string
	for _, dir := range dirs {
		path := path.Join(gitUtil.RepoPath, dir)
		if r.lastAppliedHash[dir] != "" {
			changed, err := gitUtil.HasChangesForPath(path, r.lastAppliedHash[dir])
			if err != nil {
				log.Logger.Warn(fmt.Sprintf("Could not check dir '%s' for changes, forcing apply: %v", path, err))
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

func (r *Runner) copyRepository() (*git.Util, func(), error) {
	root, sub, err := (&git.Util{RepoPath: r.RepoPath}).SplitPath()
	if err != nil {
		return nil, nil, err
	}
	tmpDir, err := ioutil.TempDir("", fmt.Sprintf("run-%d-", r.Clock.Now().Unix()))
	if err != nil {
		return nil, nil, err
	}
	err = sysutil.CopyDir(root, tmpDir)
	if err != nil {
		return nil, nil, err
	}
	return &git.Util{RepoPath: path.Join(tmpDir, sub)}, func() { os.RemoveAll(tmpDir) }, nil
}
