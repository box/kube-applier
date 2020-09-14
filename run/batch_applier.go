package run

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	"github.com/utilitywarehouse/kube-applier/kube"
	"github.com/utilitywarehouse/kube-applier/kubectl"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
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
}

// BatchApplierInterface allows for mocking out the functionality of BatchApplier when testing the full process of an apply run.
type BatchApplierInterface interface {
	Apply([]string, *ApplyOptions) ([]ApplyAttempt, []ApplyAttempt)
}

// BatchApplier makes apply calls for a batch of files, and updates metrics based on the results of each call.
type BatchApplier struct {
	KubeClient     kube.ClientInterface
	KubectlClient  kubectl.ClientInterface
	Metrics        metrics.PrometheusInterface
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
func (a *BatchApplier) Apply(applyList []string, options *ApplyOptions) ([]ApplyAttempt, []ApplyAttempt) {
	if a.WorkerCount == 0 {
		a.WorkerCount = defaultBatchApplierWorkerCount
	}
	successes := []ApplyAttempt{}
	failures := []ApplyAttempt{}
	wg := sync.WaitGroup{}
	mutex := sync.Mutex{}

	paths := make(chan string, len(applyList))

	for i := 0; i < a.WorkerCount; i++ {
		wg.Add(1)
		go func(paths <-chan string) {
			defer wg.Done()
			for path := range paths {
				appliedFile, success := a.apply(path, options)
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
		}(paths)
	}

	for _, path := range applyList {
		paths <- path
	}

	close(paths)
	wg.Wait()

	sortApplyAttemptSlice(applyList, successes)
	sortApplyAttemptSlice(applyList, failures)

	return successes, failures
}

func (a *BatchApplier) apply(path string, options *ApplyOptions) (*ApplyAttempt, bool) {
	log.Logger.Info(fmt.Sprintf("Applying dir %v", path))
	ns := filepath.Base(path)
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

	var kustomize bool
	if _, err := os.Stat(path + "/kustomization.yaml"); err == nil {
		kustomize = true
	} else if _, err := os.Stat(path + "/kustomization.yml"); err == nil {
		kustomize = true
	} else if _, err := os.Stat(path + "/Kustomization"); err == nil {
		kustomize = true
	}

	dryRunStrategy := "none"
	if a.DryRun || dryRun {
		dryRunStrategy = "server"
	}

	var cmd, output string
	cmd, output, err = a.KubectlClient.Apply(path, ns, dryRunStrategy, kustomize, pruneWhitelist)
	appliedFile := ApplyAttempt{path, cmd, output, ""}
	if err != nil {
		appliedFile.ErrorMessage = err.Error()
	}

	return &appliedFile, err == nil
}

func sortApplyAttemptSlice(paths []string, attempts []ApplyAttempt) {
	sort.Slice(attempts, func(i, j int) bool {
		indexI := -1
		indexJ := -1
		found := 0
		for k, v := range paths {
			if attempts[i].FilePath == v {
				indexI = k
				found++
			}
			if attempts[j].FilePath == v {
				indexJ = k
				found++
			}
			if found == 2 {
				break
			}
		}
		return indexI < indexJ
	})
}
