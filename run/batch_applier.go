package run

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/client"
	"github.com/utilitywarehouse/kube-applier/kubectl"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

const (
	defaultBatchApplierWorkerCount = 2
)

// ApplyAttempt stores the data from an attempt at applying a single file.
type ApplyAttempt struct {
	Application  kubeapplierv1alpha1.Application
	Command      string
	Output       string
	ErrorMessage string
	Run          Info
	Start        time.Time
	Finish       time.Time
}

// FormattedStart returns the Start time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (a ApplyAttempt) FormattedStart() string {
	return a.Start.Truncate(time.Second).String()
}

// FormattedFinish returns the Finish time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (a ApplyAttempt) FormattedFinish() string {
	return a.Finish.Truncate(time.Second).String()
}

// Latency returns the latency for the apply task in seconds, truncated to 3
// decimal places.
func (a ApplyAttempt) Latency() string {
	return fmt.Sprintf("%.3f sec", a.Finish.Sub(a.Start).Seconds())
}

// BatchApplierInterface allows for mocking out the functionality of BatchApplier when testing the full process of an apply run.
type BatchApplierInterface interface {
	Apply(string, []kubeapplierv1alpha1.Application, *ApplyOptions) ([]ApplyAttempt, []ApplyAttempt)
}

// BatchApplier makes apply calls for a batch of files, and updates metrics based on the results of each call.
type BatchApplier struct {
	KubeClient     client.ClientInterface
	KubectlClient  kubectl.ClientInterface
	Metrics        metrics.PrometheusInterface
	Clock          sysutil.ClockInterface
	DryRun         bool
	PruneBlacklist []string
	WorkerCount    int
}

// ApplyOptions contains global configuration for Apply
type ApplyOptions struct {
	ClusterResources    []string
	NamespacedResources []string
}

// Apply takes a list of files and attempts an apply command on each.
// It returns two lists of ApplyAttempts - one for files that succeeded, and one for files that failed.
func (a *BatchApplier) Apply(rootPath string, appList []kubeapplierv1alpha1.Application, options *ApplyOptions) ([]ApplyAttempt, []ApplyAttempt) {
	successes := []ApplyAttempt{}
	failures := []ApplyAttempt{}

	if len(appList) == 0 {
		return successes, failures
	}

	if a.WorkerCount == 0 {
		a.WorkerCount = defaultBatchApplierWorkerCount
	}

	wg := sync.WaitGroup{}
	mutex := sync.Mutex{}

	apps := make(chan kubeapplierv1alpha1.Application, len(appList))

	for i := 0; i < a.WorkerCount; i++ {
		wg.Add(1)
		go func(root string, apps <-chan kubeapplierv1alpha1.Application, opts *ApplyOptions) {
			defer wg.Done()
			for app := range apps {
				appliedFile, success := a.apply(root, app, opts)
				if appliedFile == nil {
					continue
				}

				mutex.Lock()
				if success {
					successes = append(successes, *appliedFile)
					log.Logger.Info(fmt.Sprintf("%v\n%v", appliedFile.Command, appliedFile.Output))
				} else {
					failures = append(failures, *appliedFile)
					log.Logger.Warn(fmt.Sprintf("%v\n%v", appliedFile.Command, appliedFile.ErrorMessage))
				}
				a.Metrics.UpdateNamespaceSuccess(app.Namespace, success)
				mutex.Unlock()
			}
		}(rootPath, apps, options)
	}

	for _, app := range appList {
		apps <- app
	}

	close(apps)
	wg.Wait()

	sortApplyAttemptSlice(successes)
	sortApplyAttemptSlice(failures)

	return successes, failures
}

func (a *BatchApplier) apply(rootPath string, app kubeapplierv1alpha1.Application, options *ApplyOptions) (*ApplyAttempt, bool) {
	start := a.Clock.Now()
	path := filepath.Join(rootPath, app.Spec.RepositoryPath)
	log.Logger.Info(fmt.Sprintf("Applying dir %v", path))

	var pruneWhitelist []string
	if app.Spec.Prune {
		pruneWhitelist = append(pruneWhitelist, options.NamespacedResources...)

		if app.Spec.PruneClusterResources {
			pruneWhitelist = append(pruneWhitelist, options.ClusterResources...)
		}

		// Trim blacklisted items out of the whitelist
		for _, b := range a.PruneBlacklist {
			for i, w := range pruneWhitelist {
				if b == w {
					pruneWhitelist = append(pruneWhitelist[:i], pruneWhitelist[i+1:]...)
				}
			}
		}
	}

	dryRunStrategy := "none"
	if a.DryRun || app.Spec.DryRun {
		dryRunStrategy = "server"
	}

	cmd, output, err := a.KubectlClient.Apply(path, kubectl.ApplyFlags{
		Namespace:      app.Namespace,
		DryRunStrategy: dryRunStrategy,
		PruneWhitelist: pruneWhitelist,
		ServerSide:     app.Spec.ServerSideApply,
	})
	finish := a.Clock.Now()

	appliedFile := ApplyAttempt{
		Application:  app,
		Command:      cmd,
		Output:       output,
		ErrorMessage: "",
		Start:        start,
		Finish:       finish,
	}
	if err != nil {
		appliedFile.ErrorMessage = err.Error()
	}
	return &appliedFile, err == nil
}

func sortApplyAttemptSlice(attempts []ApplyAttempt) {
	sort.Slice(attempts, func(i, j int) bool {
		return attempts[i].Application.Spec.RepositoryPath < attempts[j].Application.Spec.RepositoryPath
	})
}
