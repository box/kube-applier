package run

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/kubectl"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

const (
	defaultBatchApplierWorkerCount = 2
)

// BatchApplierInterface allows for mocking out the functionality of BatchApplier when testing the full process of an apply run.
type BatchApplierInterface interface {
	Apply(string, []kubeapplierv1alpha1.Application, *ApplyOptions)
}

// BatchApplier makes apply calls for a batch of files, and updates metrics based on the results of each call.
type BatchApplier struct {
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
func (a *BatchApplier) Apply(rootPath string, appList []kubeapplierv1alpha1.Application, options *ApplyOptions) {
	if len(appList) == 0 {
		return
	}

	if a.WorkerCount == 0 {
		a.WorkerCount = defaultBatchApplierWorkerCount
	}

	wg := sync.WaitGroup{}
	mutex := sync.Mutex{}

	apps := make(chan *kubeapplierv1alpha1.Application, len(appList))

	for i := 0; i < a.WorkerCount; i++ {
		wg.Add(1)
		go func(root string, apps <-chan *kubeapplierv1alpha1.Application, opts *ApplyOptions) {
			defer wg.Done()
			for app := range apps {
				a.apply(root, app, opts)

				mutex.Lock()
				if app.Status.LastRun.Success {
					log.Logger.Info(fmt.Sprintf("%v\n%v", app.Status.LastRun.Command, app.Status.LastRun.Output))
				} else {
					log.Logger.Warn(fmt.Sprintf("%v\n%v", app.Status.LastRun.Command, app.Status.LastRun.ErrorMessage))
				}
				a.Metrics.UpdateNamespaceSuccess(app.Namespace, app.Status.LastRun.Success)
				mutex.Unlock()
			}
		}(rootPath, apps, options)
	}

	for i := range appList {
		apps <- &appList[i]
	}

	close(apps)
	wg.Wait()

	sort.Slice(appList, func(i, j int) bool {
		return appList[i].Spec.RepositoryPath < appList[j].Spec.RepositoryPath
	})
}

func (a *BatchApplier) apply(rootPath string, app *kubeapplierv1alpha1.Application, options *ApplyOptions) {
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
		pruneBlacklist := uniqueStrings(append(a.PruneBlacklist, app.Spec.PruneBlacklist...))
		for _, b := range pruneBlacklist {
			for i, w := range pruneWhitelist {
				if b == w {
					pruneWhitelist = append(pruneWhitelist[:i], pruneWhitelist[i+1:]...)
					break
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

	app.Status.LastRun = &kubeapplierv1alpha1.ApplicationStatusRun{
		Command:      cmd,
		Output:       output,
		ErrorMessage: "",
		Finished:     metav1.NewTime(finish),
		Started:      metav1.NewTime(start),
	}
	if err != nil {
		app.Status.LastRun.ErrorMessage = err.Error()
	} else {
		app.Status.LastRun.Success = true
	}
}

func uniqueStrings(in []string) []string {
	m := make(map[string]bool)
	for _, i := range in {
		m[i] = true
	}
	out := make([]string, len(m))
	i := 0
	for v := range m {
		out[i] = v
		i++
	}
	return out
}
