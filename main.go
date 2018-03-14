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

	clock := &sysutil.Clock{}

	if err := sysutil.WaitForDir(repoPath, clock, waitForRepoInterval); err != nil {
		log.Fatal(err)
	}

	kubeClient := &kube.Client{Server: server}
	kubeClient.Configure()

	gitUtil := &git.GitUtil{repoPath}
	fileSystem := &sysutil.FileSystem{}
	listFactory := &applylist.Factory{repoPath, blacklistPath, whitelistPath, fileSystem}

	// Webserver and scheduler send run requests to FullRunQueue channel.
	// Runner receives the requests and initiates full runs.
	// Only 1 pending request may sit in the queue at a time.
	fullRunQueue := make(chan bool, 1)

	// When a new Git commit comes in, scheduler sends the commit hash to QuickRunQueue channel.
	// Runner receives the hash and initiates a quick run, using the hash for a diff.
	// Only 1 pending request may sit in the queue at a time.
	quickRunQueue := make(chan string, 1)

	// Runner sends run results to runResults channel, webserver receives the results and displays them.
	// Limit of 5 is arbitrary - there is significant delay between sends, and receives are handled near instantaneously.
	runResults := make(chan run.Result, 5)

	// Runner sends run results to runMetrics channel, metrics handler receives the results and updates its metrics.
	// Limit of 5 is arbitrary - there is significant delay between sends, and receives are handled hear instantaneously.
	runMetrics := make(chan run.Result, 5)

	// Runner, webserver, and scheduler all send fatal errors to errors channel, and main() exits upon receiving an error.
	// No limit needed, as a single fatal error will exit the program anyway.
	errors := make(chan error)

	// runCount keeps a count of total runs and used as a run ID for logging purposes.
	// Implementing as an unbuffered channel allows for blocking on both sides.
	// The counter will block on incrementing until some run pops the current count.
	// The runner will block on popping the current count until it is updated.
	runCount := make(chan int)

	metrics := &metrics.Prometheus{RunMetrics: runMetrics}
	metrics.Configure()
	batchApplier := &run.BatchApplier{kubeClient}

	pollTicker := time.Tick(pollInterval)
	fullRunTicker := time.Tick(fullRunInterval)

	runner := &run.Runner{
		batchApplier,
		listFactory,
		gitUtil,
		clock,
		diffURLFormat,
		"",
		quickRunQueue,
		fullRunQueue,
		runResults,
		runMetrics,
		errors,
		runCount,
	}
	scheduler := &run.Scheduler{gitUtil, pollTicker, fullRunTicker, quickRunQueue, fullRunQueue, errors, ""}
	webserver := &webserver.WebServer{listenPort, clock, metrics.GetHandler(), fullRunQueue, runResults, errors}

	go metrics.StartMetricsLoop()
	go scheduler.Start()
	go runner.StartRunCounter()
	go runner.StartQuickLoop()
	go runner.StartFullLoop()
	    go webserver.Start()

	for err := range errors {
		log.Fatal(err)
	}

}
