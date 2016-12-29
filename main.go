package main

import (
	"github.com/box/kube-applier/applylist"
	"github.com/box/kube-applier/git"
	"github.com/box/kube-applier/kube"
	"github.com/box/kube-applier/metrics"
	"github.com/box/kube-applier/run"
	"github.com/box/kube-applier/sysutil"
	"github.com/box/kube-applier/webserver"
	"log"
	"strings"
	"time"
)

const (
	// Default number of seconds to wait before checking the Git repo for new commits.
	defaultPollIntervalSeconds = 5

	// Default number of seconds to wait in between apply runs (if no new commits to the repo have been made).
	defaultFullRunIntervalSeconds = 5 * 60

	// Number of seconds to wait in between attempts to locate the repo at the specified path.
	// Git-sync atomically places the repo at the specified path once it is finished pulling, so it will not be present immediately.
	waitForRepoInterval = 1 * time.Second
)

// startApplyLoop runs a continuous loop of checking whether an apply run is necessary and performing one if so.
func startApplyLoop(lastRun *run.Result, runChecker run.CheckerInterface, runner run.RunnerInterface, clock sysutil.ClockInterface, pollInterval time.Duration, forceSwitch *bool) {
	for {
		shouldRun, err := runChecker.ShouldRun(lastRun)
		if err != nil {
			log.Fatal(err)
		}
		if shouldRun || *forceSwitch {
			*forceSwitch = false
			newRun, err := runner.Run()
			if err != nil {
				log.Fatal(err)
			}
			*lastRun = *newRun
		}
		clock.Sleep(pollInterval)
	}
}

func main() {
	repoPath := sysutil.GetRequiredEnvString("REPO_PATH")
	listenPort := sysutil.GetRequiredEnvInt("LISTEN_PORT")
	server := sysutil.GetEnvStringOrDefault("SERVER", "")
	blacklistPath := sysutil.GetEnvStringOrDefault("BLACKLIST_PATH", "")
	diffURLFormat := sysutil.GetEnvStringOrDefault("DIFF_URL_FORMAT", "")
	pollInterval := time.Duration(sysutil.GetEnvIntOrDefault("POLL_INTERVAL_SECONDS", defaultPollIntervalSeconds)) * time.Second
	fullRunInterval := time.Duration(sysutil.GetEnvIntOrDefault("FULL_RUN_INTERVAL_SECONDS", defaultFullRunIntervalSeconds)) * time.Second
	lastRun := &run.Result{}

	if diffURLFormat != "" && !strings.Contains(diffURLFormat, "%s") {
		log.Fatalf("Invalid DIFF_URL_FORMAT, must contain %q: %v", "%s", diffURLFormat)
	}

	metrics := &metrics.Prometheus{}
	metrics.Init()

	clock := &sysutil.Clock{}

	if err := sysutil.WaitForDir(repoPath, clock, waitForRepoInterval); err != nil {
		log.Fatal(err)
	}

	kubeClient := &kube.Client{server}
	kubeClient.Configure()

	forceSwitch := false

	batchApplier := &run.BatchApplier{kubeClient, metrics}
	gitUtil := &git.GitUtil{repoPath}
	fileSystem := &sysutil.FileSystem{}
	listFactory := &applylist.Factory{repoPath, blacklistPath, fileSystem}
	runChecker := &run.Checker{gitUtil, clock, fullRunInterval}

	runner := &run.Runner{
		batchApplier,
		listFactory,
		gitUtil,
		clock,
		metrics,
		diffURLFormat,
	}

	go startApplyLoop(lastRun, runChecker, runner, clock, pollInterval, &forceSwitch)

	// If it returns, startWebServer returns a non-nil error from http.ListenAndServe
	err := webserver.StartWebServer(listenPort, lastRun, clock, metrics.GetHandler(), &forceSwitch)
	log.Fatalf("Webserver error: %v", err)
}
