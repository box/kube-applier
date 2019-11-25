package run

import (
    "github.com/box/kube-applier/kube"
    "log"
    "strings"
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

// Find function to search for element in slice
func Find(slice []string, val string) (int, bool) {
    for i, item := range slice {
        if item == val {
            return i, true
        }
    }
    return -1, false
}

// unique function returns unique elements from 2 slices
func unique(slice []string) []string {
    encountered := map[string]int{}
    diff := []string{}

    for _, v := range slice {
        encountered[v] = encountered[v]+1
    }

    for _, v := range slice {
        if encountered[v] == 1 {
        diff = append(diff, v)
        }
    }
    return diff
}

// RemoveAttempt stores the data from an attempt at removing a single resource
type RemoveAttempt struct {
    Resource     string
    Command      string
    Output       string
    ErrorMessage string
}

// BatchRemoverInterface allows for mocking out the functionality of BatchRemover when testing the full process of an delete run.
type BatchRemoverInterface interface {
    Remove(int, string, string, string, []string) (successes []RemoveAttempt, failures []RemoveAttempt)
}

// BatchRemover makes delete calls for a list of resources.
type BatchRemover struct {
    KubeClient kube.ClientInterface
}

// Remove takes a resource type and the rawList from git
// gets a list of deployments that should be running and a list that are currently running
// to ensure only deployments found in git are kept running
func (a *BatchRemover) Remove(id int, resourceType string, repoPath string, autoDelete string, rawList []string) (successes []RemoveAttempt, failures []RemoveAttempt) {
    if err := a.KubeClient.CheckVersion(); err != nil {
        log.Fatal(err)
    }

    // create a list of unique deployments that exist in the repo
    var existingDeployments []string
    for _, s := range rawList {
        s := strings.TrimPrefix(s, repoPath)
        parts := strings.Split(s, "/")
        if len(parts) > 1 {
            _, found := Find(existingDeployments, parts[1])
            if !found {
                existingDeployments = append(existingDeployments, parts[1])
            }
        }
    }

    log.Printf("Found deployments in git repo: %v", existingDeployments)

    successes = []RemoveAttempt{}
    failures = []RemoveAttempt{}

    cmd, runningDeploys, err := a.KubeClient.List(resourceType)
    log.Printf("Ran list command: %v", cmd)
    if err != nil {
        log.Fatal(err)
    }
    runningDeployments := strings.Fields(runningDeploys)
    log.Printf("Found running deployments: %v", runningDeployments)
    if len(runningDeployments) > 0 && autoDelete != "disabled" {
        removeList := unique(append(runningDeployments, existingDeployments...))
        for _, item := range removeList {
            log.Printf("RUN %v: Removing resource %v", id, item)
            cmd, output, err := a.KubeClient.Remove(resourceType, item)
            success := (err == nil)
            removedResource := RemoveAttempt{item, cmd, output, ""}
            if success {
                successes = append(successes, removedResource)
                log.Printf("RUN %v: %v\n%v", id, cmd, output)
            } else {
                removedResource.ErrorMessage = err.Error()
                failures = append(failures, removedResource)
                log.Printf("RUN %v: %v\n%v\n%v", id, cmd, output, removedResource.ErrorMessage)
            }
        }
    }
    return successes, failures
}
