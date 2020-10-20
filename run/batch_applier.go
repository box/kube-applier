package run

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/utilitywarehouse/kube-applier/kube"
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
	FilePath     string
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
	Apply(string, []string, *ApplyOptions) ([]ApplyAttempt, []ApplyAttempt)
}

// BatchApplier makes apply calls for a batch of files, and updates metrics based on the results of each call.
type BatchApplier struct {
	KubeClient     kube.ClientInterface
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
func (a *BatchApplier) Apply(rootPath string, applyList []string, options *ApplyOptions) ([]ApplyAttempt, []ApplyAttempt) {
	successes := []ApplyAttempt{}
	failures := []ApplyAttempt{}

	if len(applyList) == 0 {
		return successes, failures
	}

	if a.WorkerCount == 0 {
		a.WorkerCount = defaultBatchApplierWorkerCount
	}

	wg := sync.WaitGroup{}
	mutex := sync.Mutex{}

	paths := make(chan string, len(applyList))

	for i := 0; i < a.WorkerCount; i++ {
		wg.Add(1)
		go func(root string, paths <-chan string, opts *ApplyOptions) {
			defer wg.Done()
			for path := range paths {
				appliedFile, success := a.apply(root, path, opts)
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
				a.Metrics.UpdateNamespaceSuccess(path, success)
				mutex.Unlock()
			}
		}(rootPath, paths, options)
	}

	for _, path := range applyList {
		paths <- path
	}

	close(paths)
	wg.Wait()

	sortApplyAttemptSlice(successes)
	sortApplyAttemptSlice(failures)

	return successes, failures
}

func (a *BatchApplier) apply(rootPath, subPath string, options *ApplyOptions) (*ApplyAttempt, bool) {
	start := a.Clock.Now()
	path := filepath.Join(rootPath, subPath)
	ns := subPath
	log.Logger.Info(fmt.Sprintf("Applying dir %v", path))

	kaa, err := a.KubeClient.NamespaceAnnotations(ns)
	if err != nil {
		log.Logger.Error("Error while getting namespace annotations, defaulting to kube-applier.io/enabled=false", "error", err)
		return nil, false
	}

	enabled, err := strconv.ParseBool(kaa.Enabled)
	if err != nil {
		log.Logger.Info("Could not get value for kube-applier.io/enabled", "error", err)
		return nil, false
	} else if !enabled {
		log.Logger.Info("Skipping namespace", "kube-applier.io/enabled", enabled)
		return nil, false
	}

	dryRun, err := strconv.ParseBool(kaa.DryRun)
	if err != nil {
		log.Logger.Info("Could not get value for kube-applier.io/dry-run", "error", err)
		dryRun = false
	}

	prune, err := strconv.ParseBool(kaa.Prune)
	if err != nil {
		log.Logger.Info("Could not get value for kube-applier.io/prune", "error", err)
		prune = true
	}

	serverSide, err := strconv.ParseBool(kaa.ServerSide)
	if err != nil {
		log.Logger.Info("Could not get value for kube-applier.io/server-side", "error", err)
		serverSide = false
	}

	var pruneWhitelist []string
	if prune {
		pruneWhitelist = append(pruneWhitelist, options.NamespacedResources...)

		pruneClusterResources, err := strconv.ParseBool(kaa.PruneClusterResources)
		if err != nil {
			log.Logger.Info("Could not get value for kube-applier.io/prune-cluster-resources", "error", err)
			pruneClusterResources = false
		}
		if pruneClusterResources {
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
	if a.DryRun || dryRun {
		dryRunStrategy = "server"
	}

	cmd, output, err := a.KubectlClient.Apply(path, kubectl.ApplyFlags{
		Namespace:      ns,
		DryRunStrategy: dryRunStrategy,
		PruneWhitelist: pruneWhitelist,
		ServerSide:     serverSide,
	})
	finish := a.Clock.Now()

	appliedFile := ApplyAttempt{
		FilePath:     subPath,
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
		return attempts[i].FilePath < attempts[j].FilePath
	})
}
