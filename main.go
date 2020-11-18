package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/utilitywarehouse/kube-applier/client"
	"github.com/utilitywarehouse/kube-applier/kubectl"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/run"
	"github.com/utilitywarehouse/kube-applier/sysutil"
	"github.com/utilitywarehouse/kube-applier/webserver"
)

const (
	// Number of seconds to wait in between attempts to locate the repo at the
	// specified path. Git-sync atomically places the repo at the specified path
	// once it is finished pulling, so it will not be present immediately.
	waitForRepoInterval = 1 * time.Second
)

var (
	repoPath             = os.Getenv("REPO_PATH")
	repoTimeout          = os.Getenv("REPO_TIMEOUT_SECONDS")
	listenPort           = os.Getenv("LISTEN_PORT")
	gitPollInterval      = os.Getenv("GIT_POLL_INTERVAL_SECONDS")
	appPollInterval      = os.Getenv("APP_POLL_INTERVAL_SECONDS")
	statusUpdateInterval = os.Getenv("STATUS_UPDATE_INTERVAL_SECONDS")
	dryRun               = os.Getenv("DRY_RUN")
	logLevel             = os.Getenv("LOG_LEVEL")
	pruneBlacklist       = os.Getenv("PRUNE_BLACKLIST")
	execTimeout          = os.Getenv("EXEC_TIMEOUT")

	// Github commit diff url
	diffURLFormat = os.Getenv("DIFF_URL_FORMAT")
	workerCount   = os.Getenv("WORKER_COUNT")

	runnerWorkerCount int
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

	if gitPollInterval == "" {
		gitPollInterval = "15"
	} else {
		_, err := strconv.Atoi(gitPollInterval)
		if err != nil {
			fmt.Println("GIT_POLL_INTERVAL_SECONDS must be an int")
			os.Exit(1)
		}
	}

	if appPollInterval == "" {
		appPollInterval = "60"
	} else {
		_, err := strconv.Atoi(appPollInterval)
		if err != nil {
			fmt.Println("APP_POLL_INTERVAL_SECONDS must be an int")
			os.Exit(1)
		}
	}

	if statusUpdateInterval == "" {
		statusUpdateInterval = "60"
	} else {
		_, err := strconv.Atoi(statusUpdateInterval)
		if err != nil {
			fmt.Println("STATUS_UPDATE_INTERVAL_SECONDS must be an int")
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

	if execTimeout == "" {
		execTimeout = "3m"
	}

	if workerCount == "" {
		workerCount = "0"
	}
	i, err := strconv.Atoi(workerCount)
	if err != nil {
		fmt.Printf("Cannot parse WORKER_COUNT: %v\n", err)
		os.Exit(1)
	}
	runnerWorkerCount = i
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

	kubeClient, err := client.New()
	if err != nil {
		log.Logger.Error("error creating kubernetes API client", "error", err)
		os.Exit(1)
	}

	execTimeoutDuration, err := time.ParseDuration(execTimeout)
	if err != nil {
		log.Logger.Error("error parsing command exec timeout duration", "timeout", execTimeout, "error", err)
		os.Exit(1)
	}
	kubectlClient := &kubectl.Client{
		Metrics: metrics,
		Timeout: execTimeoutDuration,
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

	runner := &run.Runner{
		Clock:          clock,
		DiffURLFormat:  diffURLFormat,
		DryRun:         dr,
		KubeClient:     kubeClient,
		KubectlClient:  kubectlClient,
		Metrics:        metrics,
		PruneBlacklist: pruneBlacklistSlice,
		RepoPath:       repoPath,
		WorkerCount:    runnerWorkerCount,
	}

	runQueue := runner.Start()

	gpi, _ := strconv.Atoi(gitPollInterval)
	api, _ := strconv.Atoi(appPollInterval)
	scheduler := &run.Scheduler{
		ApplicationPollInterval: time.Duration(api) * time.Second,
		GitPollInterval:         time.Duration(gpi) * time.Second,
		KubeClient:              kubeClient,
		RepoPath:                repoPath,
		RunQueue:                runQueue,
	}
	scheduler.Start()

	sui, _ := strconv.Atoi(statusUpdateInterval)
	lp, _ := strconv.Atoi(listenPort)
	webserver := &webserver.WebServer{
		Clock:                clock,
		DiffURLFormat:        diffURLFormat,
		KubeClient:           kubeClient,
		ListenPort:           lp,
		RunQueue:             runQueue,
		StatusUpdateInterval: time.Duration(sui) * time.Second,
	}
	if err := webserver.Start(); err != nil {
		log.Logger.Error(fmt.Sprintf("Cannot start webserver: %v", err))
		os.Exit(1)
	}

	ctx := signals.SetupSignalHandler()
	<-ctx.Done()
	log.Logger.Info("Interrupted, shutting down...")
	if err := webserver.Shutdown(); err != nil {
		log.Logger.Error(fmt.Sprintf("Cannot shutdown webserver: %v", err))
	}
	scheduler.Stop()
	runner.Stop()
}
