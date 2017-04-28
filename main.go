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

func main() {
	repoPath := sysutil.GetRequiredEnvString("REPO_PATH")
	listenPort := sysutil.GetRequiredEnvInt("LISTEN_PORT")
	server := sysutil.GetEnvStringOrDefault("SERVER", "")
	blacklistPath := sysutil.GetEnvStringOrDefault("BLACKLIST_PATH", "")

	// A file that contains a list of files to consider for application.
	// If the env var is not defined or if the file is empty act like a no-op and
	// all files will be considered.
	whitelistPath := sysutil.GetEnvStringOrDefault("WHITELIST_PATH", "")
	diffURLFormat := sysutil.GetEnvStringOrDefault("DIFF_URL_FORMAT", "")
	pollInterval := time.Duration(sysutil.GetEnvIntOrDefault("POLL_INTERVAL_SECONDS", defaultPollIntervalSeconds)) * time.Second
	fullRunInterval := time.Duration(sysutil.GetEnvIntOrDefault("FULL_RUN_INTERVAL_SECONDS", defaultFullRunIntervalSeconds)) * time.Second

	if diffURLFormat != "" && !strings.Contains(diffURLFormat, "%s") {
		log.Fatalf("Invalid DIFF_URL_FORMAT, must contain %q: %v", "%s", diffURLFormat)
	}

	metrics := &metrics.Prometheus{}
	metrics.Init()

	clock := &sysutil.Clock{}

	if err := sysutil.WaitForDir(repoPath, clock, waitForRepoInterval); err != nil {
		log.Fatal(err)
	}

	kubeClient := &kube.Client{Server: server}
	kubeClient.Configure()

	batchApplier := &run.BatchApplier{kubeClient, metrics}
	gitUtil := &git.GitUtil{repoPath}
	fileSystem := &sysutil.FileSystem{}
	listFactory := &applylist.Factory{repoPath, blacklistPath, whitelistPath, fileSystem}

	// Webserver and scheduler send run requests to runQueue channel, runner receives the requests and initiates runs.
	// Only 1 pending request may sit in the queue at a time.
	runQueue := make(chan bool, 1)

	// Runner sends run results to runResults channel, webserver receives the results and displays them.
	// Limit of 5 is arbitrary - there is significant delay between sends, and receives are handled near instantaneously.
	runResults := make(chan run.Result, 5)

	// Runner, webserver, and scheduler all send fatal errors to errors channel, and main() exits upon receiving an error.
	// No limit needed, as a single fatal error will exit the program anyway.
	errors := make(chan error)

	runner := &run.Runner{
		batchApplier,
		listFactory,
		gitUtil,
		clock,
		metrics,
		diffURLFormat,
		runQueue,
		runResults,
		errors,
	}
	scheduler := &run.Scheduler{gitUtil, pollInterval, fullRunInterval, runQueue, errors}
	webserver := &webserver.WebServer{listenPort, clock, metrics.GetHandler(), runQueue, runResults, errors}

	go scheduler.Start()
	go runner.Start()
	go webserver.Start()

	for err := range errors {
		log.Fatal(err)
	}

}
