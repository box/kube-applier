package run

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/utilitywarehouse/kube-applier/kube"
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
	Apply([]string) ([]ApplyAttempt, []ApplyAttempt)
}

// BatchApplier makes apply calls for a batch of files, and updates metrics based on the results of each call.
type BatchApplier struct {
	KubeClient  kube.ClientInterface
	Metrics     metrics.PrometheusInterface
	DryRun      bool
	Prune       bool
	StrictApply bool
}

// Apply takes a list of files and attempts an apply command on each.
// It returns two lists of ApplyAttempts - one for files that succeeded, and one for files that failed.
func (a *BatchApplier) Apply(applyList []string) ([]ApplyAttempt, []ApplyAttempt) {
	successes := []ApplyAttempt{}
	failures := []ApplyAttempt{}
	for _, path := range applyList {
		log.Logger.Info(fmt.Sprintf("Applying dir %v", path))
		ns := filepath.Base(path)
		s, err := a.KubeClient.GetNamespaceStatus(ns)
		if err != nil {
			log.Logger.Error("Error while getting namespace status, defaulting to off", "error", err)
		}
		var disabled bool
		switch s {
		case kube.On:
			disabled = false
		case kube.Off:
			continue
		case kube.DryRun:
			disabled = true
		default:
			continue
		}

		var kustomize bool
		if _, err := os.Stat(path + "/kustomization.yaml"); err == nil {
			kustomize = true
		}

		var cmd, output string
		cmd, output, err = a.KubeClient.Apply(path, ns, a.DryRun || disabled, a.Prune, a.StrictApply, kustomize)
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
