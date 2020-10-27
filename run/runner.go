package run

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/client"
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
	RepoPath      string
	BatchApplier  BatchApplierInterface
	Clock         sysutil.ClockInterface
	Metrics       metrics.PrometheusInterface
	KubeClient    kube.ClientInterface
	DiffURLFormat string
	RunQueue      <-chan Request
	RunResults    chan<- Result
	Errors        chan<- error
	client        *client.Client
}

// Start runs a continuous loop that starts a new run when a request comes into the queue channel.
func (r *Runner) Start() {
	c, err := client.New()
	if err != nil {
		r.Errors <- err
		return
	}
	r.client = c
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

	apps, err := r.client.ListApplications(context.TODO())
	if err != nil {
		log.Logger.Error("Could not list Applications: %v", err)
	}

	var appList []kubeapplierv1alpha1.Application
	if t.Type == ScheduledFullRun || t.Type == ForcedFullRun {
		appList = apps
	} else if t.Type == FailedOnlyRun {
		for _, a := range apps {
			if a.Status.LastRun != nil && !a.Status.LastRun.Success {
				appList = append(appList, a)
			}
		}
	} else if t.Type == PartialRun {
		appList = r.pruneUnchangedDirs(gitUtil, apps)
	} else if t.Type == SingleDirectoryRun {
		valid := false
		for _, app := range apps {
			if app.Spec.RepositoryPath == t.Args.(string) {
				appList = []kubeapplierv1alpha1.Application{app}
				valid = true
				break
			}
		}
		if !valid {
			log.Logger.Error(fmt.Sprintf("Invalid path '%s' requested, ignoring", t.Args.(string)))
			return nil, nil
		}
	} else {
		log.Logger.Error(fmt.Sprintf("Run type '%s' is not properly handled", t.Type))
	}

	hash, err := gitUtil.HeadHashForPaths(".")
	if err != nil {
		return nil, err
	}
	commitLog, err := gitUtil.HeadCommitLogForPaths(".")
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

	dirs := make([]string, len(appList))
	for i, app := range appList {
		dirs[i] = app.Spec.RepositoryPath
	}
	log.Logger.Debug(fmt.Sprintf("applying dirs: %v", dirs))
	successes, failures := r.BatchApplier.Apply(gitUtil.RepoPath, appList, applyOptions)

	finish := r.Clock.Now()

	log.Logger.Info("Finished apply run", "stop-time", finish)

	success := len(failures) == 0

	results := make(map[string]string)
	for _, s := range successes {
		results[s.Application.Spec.RepositoryPath] = s.Output
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
		successes[i].Application.Status.LastRun = &kubeapplierv1alpha1.ApplicationStatusRun{
			Commit:   runInfo.CommitHash,
			Finished: metav1.NewTime(successes[i].Finish),
			Started:  metav1.NewTime(successes[i].Start),
			Success:  true,
			Type:     runInfo.Type.String(),
		}
		if err := r.client.UpdateApplicationStatus(context.TODO(), &successes[i].Application); err != nil {
			log.Logger.Warn(fmt.Sprintf("Could not update Application run info: %v\n", err))
		}
	}
	for i := range failures {
		failures[i].Run = runInfo
		failures[i].Application.Status.LastRun = &kubeapplierv1alpha1.ApplicationStatusRun{
			Commit:   runInfo.CommitHash,
			Finished: metav1.NewTime(failures[i].Finish),
			Started:  metav1.NewTime(failures[i].Start),
			Success:  false,
			Type:     runInfo.Type.String(),
		}
		if err := r.client.UpdateApplicationStatus(context.TODO(), &failures[i].Application); err != nil {
			log.Logger.Warn(fmt.Sprintf("Could not update Application run info: %v\n", err))
		}
	}
	newRun := Result{
		LastRun:   runInfo,
		RootPath:  r.RepoPath,
		Successes: successes,
		Failures:  failures,
	}
	return &newRun, nil
}

func (r *Runner) pruneUnchangedDirs(gitUtil *git.Util, apps []kubeapplierv1alpha1.Application) []kubeapplierv1alpha1.Application {
	var prunedApps []kubeapplierv1alpha1.Application
	for _, app := range apps {
		path := path.Join(gitUtil.RepoPath, app.Spec.RepositoryPath)
		if app.Status.LastRun != nil && app.Status.LastRun.Commit != "" {
			changed, err := gitUtil.HasChangesForPath(path, app.Status.LastRun.Commit)
			if err != nil {
				log.Logger.Warn(fmt.Sprintf("Could not check dir '%s' for changes, forcing apply: %v", path, err))
				changed = true
			}
			if !changed {
				continue
			}
		} else {
			log.Logger.Info(fmt.Sprintf("No previous apply recorded for Application '%s/%s', forcing apply", app.Namespace, app.Name))
		}
		prunedApps = append(prunedApps, app)
	}
	return prunedApps
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
	if err := git.CloneRepository(root, tmpDir); err != nil {
		return nil, nil, err
	}
	return &git.Util{RepoPath: path.Join(tmpDir, sub)}, func() { os.RemoveAll(tmpDir) }, nil
}
