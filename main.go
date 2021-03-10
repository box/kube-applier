package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/utilitywarehouse/kube-applier/client"
	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/kubectl"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/run"
	"github.com/utilitywarehouse/kube-applier/sysutil"
	"github.com/utilitywarehouse/kube-applier/webserver"
)

var (
	repoRemote            = os.Getenv("REPO_REMOTE")
	repoBranch            = os.Getenv("REPO_BRANCH")
	repoRevision          = os.Getenv("REPO_REVISION")
	repoDepth             = os.Getenv("REPO_DEPTH")
	repoDest              = os.Getenv("REPO_DEST")
	repoGitSSHKeyPath     = os.Getenv("REPO_GIT_SSH_KEY_PATH")
	repoGitKnownHostsPath = os.Getenv("REPO_GIT_KNOWN_HOSTS_PATH")
	repoSyncInterval      = os.Getenv("REPO_SYNC_INTERVAL_SECONDS")
	repoPath              = os.Getenv("REPO_PATH")
	repoTimeout           = os.Getenv("REPO_TIMEOUT_SECONDS")
	listenPort            = os.Getenv("LISTEN_PORT")
	gitPollWait           = os.Getenv("GIT_POLL_WAIT_SECONDS")
	waybillPollInterval   = os.Getenv("WAYBILL_POLL_INTERVAL_SECONDS")
	statusUpdateInterval  = os.Getenv("STATUS_UPDATE_INTERVAL_SECONDS")
	dryRun                = os.Getenv("DRY_RUN")
	logLevel              = os.Getenv("LOG_LEVEL")
	pruneBlacklist        = os.Getenv("PRUNE_BLACKLIST")
	diffURLFormat         = os.Getenv("DIFF_URL_FORMAT")
	workerCount           = os.Getenv("WORKER_COUNT")

	runnerWorkerCount int
)

func validate() {
	if repoDepth == "" {
		repoDepth = "0"
	} else {
		_, err := strconv.Atoi(repoDepth)
		if err != nil {
			fmt.Println("REPO_DEPTH must be an int")
			os.Exit(1)
		}
	}

	if repoDest == "" {
		repoDest = "/src"
	}

	if repoSyncInterval == "" {
		repoSyncInterval = "0"
	} else {
		_, err := strconv.Atoi(repoSyncInterval)
		if err != nil {
			fmt.Println("REPO_SYNC_INTERVAL must be an int")
			os.Exit(1)
		}
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

	if gitPollWait == "" {
		gitPollWait = "10"
	} else {
		_, err := strconv.Atoi(gitPollWait)
		if err != nil {
			fmt.Println("GIT_POLL_WAIT_SECONDS must be an int")
			os.Exit(1)
		}
	}

	if waybillPollInterval == "" {
		waybillPollInterval = "60"
	} else {
		_, err := strconv.Atoi(waybillPollInterval)
		if err != nil {
			fmt.Println("WAYBILL_POLL_INTERVAL_SECONDS must be an int")
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

	log.SetLevel(logLevel)

	clock := &sysutil.Clock{}

	rd, _ := strconv.Atoi(repoDepth)
	rsi, _ := strconv.Atoi(repoSyncInterval)
	repo, err := git.NewRepository(
		repoDest,
		git.RepositoryConfig{
			Remote:   repoRemote,
			Branch:   repoBranch,
			Revision: repoRevision,
			Depth:    rd,
		},
		git.SyncOptions{
			GitSSHKeyPath:        repoGitSSHKeyPath,
			GitSSHKnownHostsPath: repoGitKnownHostsPath,
			Interval:             time.Duration(rsi) * time.Second,
		},
	)
	if err != nil {
		log.Logger("kube-applier").Error("could not create git repository", "error", err)
		os.Exit(1)
	}
	rt, _ := strconv.Atoi(repoTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(rt)*time.Second)
	if err := repo.StartSync(ctx); err != nil {
		log.Logger("kube-applier").Error("could not sync git repository", "error", err)
		os.Exit(1)
	}
	cancel()

	kubeClient, err := client.New()
	if err != nil {
		log.Logger("kube-applier").Error("error creating kubernetes API client", "error", err)
		os.Exit(1)
	}

	kubectlClient := &kubectl.Client{}

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
		PruneBlacklist: pruneBlacklistSlice,
		Repository:     repo,
		RepoPath:       repoPath,
		WorkerCount:    runnerWorkerCount,
	}

	runQueue := runner.Start()

	gpw, _ := strconv.Atoi(gitPollWait)
	wpi, _ := strconv.Atoi(waybillPollInterval)
	scheduler := &run.Scheduler{
		Clock:               clock,
		GitPollWait:         time.Duration(gpw) * time.Second,
		KubeClient:          kubeClient,
		Repository:          repo,
		RepoPath:            repoPath,
		RunQueue:            runQueue,
		WaybillPollInterval: time.Duration(wpi) * time.Second,
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
		log.Logger("kube-applier").Error(fmt.Sprintf("Cannot start webserver: %v", err))
		os.Exit(1)
	}

	ctx = signals.SetupSignalHandler()
	<-ctx.Done()
	log.Logger("kube-applier").Info("Interrupted, shutting down...")
	if err := webserver.Shutdown(); err != nil {
		log.Logger("kube-applier").Error(fmt.Sprintf("Cannot shutdown webserver: %v", err))
	}
	repo.StopSync()
	scheduler.Stop()
	runner.Stop()
}
