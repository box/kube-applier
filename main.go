package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/kube"
	"github.com/utilitywarehouse/kube-applier/kubectl"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/run"
	"github.com/utilitywarehouse/kube-applier/sysutil"
	"github.com/utilitywarehouse/kube-applier/webserver"
)

const (
	// Number of seconds to wait in between attempts to locate the repo at the specified path.
	// Git-sync atomically places the repo at the specified path once it is finished pulling, so it will not be present immediately.
	waitForRepoInterval = 1 * time.Second
)

var (
	repoPath        = os.Getenv("REPO_PATH")
	repoPathFilters = os.Getenv("REPO_PATH_FILTERS")
	repoTimeout     = os.Getenv("REPO_TIMEOUT_SECONDS")
	listenPort      = os.Getenv("LISTEN_PORT")
	pollInterval    = os.Getenv("POLL_INTERVAL_SECONDS")
	fullRunInterval = os.Getenv("FULL_RUN_INTERVAL_SECONDS")
	dryRun          = os.Getenv("DRY_RUN")
	logLevel        = os.Getenv("LOG_LEVEL")
	pruneBlacklist  = os.Getenv("PRUNE_BLACKLIST")

	// Github commit diff url
	diffURLFormat = os.Getenv("DIFF_URL_FORMAT")
)

func validate() {
	if repoPath == "" {
		fmt.Println("Need to export REPO_PATH")
		os.Exit(1)
	}

	if repoTimeout == "" {
		repoTimeout = "120"
	} else {
		_, err := strconv.Atoi(repoTimeout)
		if err != nil {
			fmt.Println("REPO_TIMEOUT_SECONDS must be an int")
			os.Exit(1)
		}
	}

	if listenPort == "" {
		listenPort = "8080"
	} else {
		_, err := strconv.Atoi(listenPort)
		if err != nil {
			fmt.Println("LISTEN_PORT must be an int")
			os.Exit(1)
		}
	}

	if diffURLFormat != "" && !strings.Contains(diffURLFormat, "%s") {
		fmt.Printf("Invalid DIFF_URL_FORMAT, must contain %q: %v\n", "%s", diffURLFormat)
		os.Exit(1)
	}

	if pollInterval == "" {
		pollInterval = "5"
	} else {
		_, err := strconv.Atoi(pollInterval)
		if err != nil {
			fmt.Println("POLL_INTERVAL_SECONDS must be an int")
			os.Exit(1)
		}
	}

	if fullRunInterval == "" {
		fullRunInterval = "3600"
	} else {
		_, err := strconv.Atoi(fullRunInterval)
		if err != nil {
			fmt.Println("FULL_RUN_INTERVAL_SECONDS must be an int")
			os.Exit(1)
		}
	}

	if dryRun == "" {
		dryRun = "false"
	} else {
		_, err := strconv.ParseBool(dryRun)
		if err != nil {
			fmt.Println("DRY_RUN must be a boolean")
			os.Exit(1)
		}
	}

	// log level [trace|debug|info|warn|error] case insensitive
	if logLevel == "" {
		logLevel = "warn"
	}
}

func main() {
	validate()

	log.InitLogger(logLevel)

	metrics := &metrics.Prometheus{}
	metrics.Init()

	clock := &sysutil.Clock{}

	rt, _ := strconv.Atoi(repoTimeout)
	if err := sysutil.WaitForDir(repoPath, waitForRepoInterval, time.Duration(rt)*time.Second); err != nil {
		log.Logger.Error("problem waiting for repo", "path", repoPath, "error", err)
		os.Exit(1)
	}

	kubeClient, err := kube.New()
	if err != nil {
		log.Logger.Error("error creating kubernetes API client", "error", err)
		os.Exit(1)
	}

	kubectlClient := &kubectl.Client{
		Metrics: metrics,
	}

	// Kubernetes copies annotations from StatefulSets, Deployments and
	// Daemonsets to the corresponding ControllerRevision, including
	// 'kubectl.kubernetes.io/last-applied-configuration', which will result
	// in kube-applier pruning ControllerRevisions that it shouldn't be
	// managing at all. This makes it unsuitable for pruning and a
	// reasonable default for blacklisting.
	pruneBlacklistSlice := []string{"apps/v1/ControllerRevision"}
	if pruneBlacklist != "" {
		pruneBlacklistSlice = append(pruneBlacklistSlice, strings.Split(pruneBlacklist, ",")...)
	}
	dr, _ := strconv.ParseBool(dryRun)
	batchApplier := &run.BatchApplier{
		PruneBlacklist: pruneBlacklistSlice,
		KubeClient:     kubeClient,
		KubectlClient:  kubectlClient,
		DryRun:         dr,
		Metrics:        metrics,
	}

	gitUtil := &git.Util{
		RepoPath: repoPath,
	}

	// Webserver and scheduler send run requests to runQueue channel, runner receives the requests and initiates runs.
	// Only 1 pending request may sit in the queue at a time.
	runQueue := make(chan bool, 1)

	// Runner sends run results to runResults channel, webserver receives the results and displays them.
	// Limit of 5 is arbitrary - there is significant delay between sends, and receives are handled near instantaneously.
	runResults := make(chan run.Result, 5)

	// Runner, webserver, and scheduler all send fatal errors to errors channel, and main() exits upon receiving an error.
	// No limit needed, as a single fatal error will exit the program anyway.
	errors := make(chan error)

	var repoPathFiltersSlice []string
	if repoPathFilters != "" {
		repoPathFiltersSlice = strings.Split(repoPathFilters, ",")
	}

	runner := &run.Runner{
		RepoPath:        repoPath,
		RepoPathFilters: repoPathFiltersSlice,
		BatchApplier:    batchApplier,
		GitUtil:         gitUtil,
		Clock:           clock,
		Metrics:         metrics,
		KubeClient:      kubeClient,
		DiffURLFormat:   diffURLFormat,
		RunQueue:        runQueue,
		RunResults:      runResults,
		Errors:          errors,
	}

	pi, _ := strconv.Atoi(pollInterval)
	fi, _ := strconv.Atoi(fullRunInterval)
	scheduler := &run.Scheduler{
		GitUtil:         gitUtil,
		PollInterval:    time.Duration(pi) * time.Second,
		FullRunInterval: time.Duration(fi) * time.Second,
		RepoPathFilters: repoPathFiltersSlice,
		RunQueue:        runQueue,
		Errors:          errors,
	}

	lp, _ := strconv.Atoi(listenPort)
	webserver := &webserver.WebServer{
		ListenPort: lp,
		Clock:      clock,
		RunQueue:   runQueue,
		RunResults: runResults,
		Errors:     errors,
	}

	go scheduler.Start()
	go runner.Start()
	go webserver.Start()

	err = <-errors
	log.Logger.Error("Fatal error, exiting", "error", err)
	os.Exit(1)
}
