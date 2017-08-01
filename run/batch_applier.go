package run

import (
	"github.com/box/kube-applier/kube"
	"log"
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
	Apply(int, []string) (successes []ApplyAttempt, failures []ApplyAttempt)
}

// BatchApplier makes apply calls for a batch of files.
type BatchApplier struct {
	KubeClient kube.ClientInterface
}

// Apply takes a list of files and attempts an apply command on each, labeling logs with the run ID.
// It returns two lists of ApplyAttempts - one for files that succeeded, and one for files that failed.
func (a *BatchApplier) Apply(id int, applyList []string) (successes []ApplyAttempt, failures []ApplyAttempt) {
	if err := a.KubeClient.CheckVersion(); err != nil {
		log.Fatal(err)
	}

	successes = []ApplyAttempt{}
	failures = []ApplyAttempt{}
	for _, path := range applyList {
		log.Printf("RUN %v: Applying file %v", id, path)
		cmd, output, err := a.KubeClient.Apply(path)
		success := (err == nil)
		appliedFile := ApplyAttempt{path, cmd, output, ""}
		if success {
			successes = append(successes, appliedFile)
			log.Printf("RUN %v: %v\n%v", id, cmd, output)
		} else {
			appliedFile.ErrorMessage = err.Error()
			failures = append(failures, appliedFile)
			log.Printf("RUN %v: %v\n%v\n%v", id, cmd, output, appliedFile.ErrorMessage)
		}
	}
	return successes, failures
}
