package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/utilitywarehouse/kube-applier/kube"
	"github.com/utilitywarehouse/kube-applier/kubectl"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
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
}

// ApplyOptions contains global configuration for Apply
type ApplyOptions struct {
	ClusterResources    []string
	NamespacedResources []string
}

// Apply takes a list of files and attempts an apply command on each.
// It returns two lists of ApplyAttempts - one for files that succeeded, and one for files that failed.
func (a *BatchApplier) Apply(applyList []string, options *ApplyOptions) ([]ApplyAttempt, []ApplyAttempt) {
	successes := []ApplyAttempt{}
	failures := []ApplyAttempt{}

	for _, path := range applyList {
		log.Logger.Info(fmt.Sprintf("Applying dir %v", path))
		ns := filepath.Base(path)
		kaa, err := a.KubeClient.NamespaceAnnotations(ns)
		if err != nil {
			log.Logger.Error("Error while getting namespace annotations, defaulting to kube-applier.io/enabled=false", "error", err)
			continue
		}

		enabled, err := strconv.ParseBool(kaa.Enabled)
		if err != nil {
			log.Logger.Info("Could not get value for kube-applier.io/enabled", "error", err)
			continue
		} else if !enabled {
			log.Logger.Info("Skipping namespace", "kube-applier.io/enabled", enabled)
			continue
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

		var cmd, output string
		cmd, output, err = a.KubectlClient.Apply(path, ns, a.DryRun || dryRun, kustomize, pruneWhitelist)
		success := (err == nil)
		appliedFile := ApplyAttempt{path, cmd, output, ""}
		if success {
			successes = append(successes, appliedFile)
			log.Logger.Info(fmt.Sprintf("%v\n%v", cmd, output))
		} else {
			appliedFile.ErrorMessage = err.Error()
			failures = append(failures, appliedFile)
			log.Logger.Warn(fmt.Sprintf("%v\n%v\n%v", cmd, output, appliedFile.ErrorMessage))
		}

		a.Metrics.UpdateNamespaceSuccess(path, success)

	}
	return successes, failures
}
