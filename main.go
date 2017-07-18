package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	cli "github.com/jawher/mow.cli"
	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/kube"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/run"
	"github.com/utilitywarehouse/kube-applier/sysutil"
	"github.com/utilitywarehouse/kube-applier/webserver"
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

	app := cli.App(webserver.AppName, webserver.AppDescription)
	basePath := app.String(cli.StringOpt{
		Name:   "base-path",
		Desc:   "Git repo base path",
		EnvVar: "BASE_PATH",
	})
	namespace := app.String(cli.StringOpt{
		Name:   "namespace",
		Desc:   "Namespace kube-applier operates on",
		EnvVar: "NAMESPACE",
	})
	listenPort := app.Int(cli.IntOpt{
		Name:   "listenport",
		Value:  8080,
		Desc:   "Listen port",
		EnvVar: "LISTEN_PORT",
	})
	server := app.String(cli.StringOpt{
		Name:   "server",
		Value:  "",
		Desc:   "K8s server. Mainly for local testing.",
		EnvVar: "SERVER",
	})
	diffURLFormat := app.String(cli.StringOpt{
		Name:   "diff-url-format",
		Value:  "https://github.com/utilitywarehouse/kubernetes-manifests/commit/%s",
		Desc:   "Github commit diff url",
		EnvVar: "DIFF_URL_FORMAT",
	})
	pollInterval := app.Int(cli.IntOpt{
		Name:   "poll-interval-seconds",
		Value:  5,
		Desc:   "Poll interval",
		EnvVar: "POLL_INTERVAL_SECONDS",
	})
	fullRunInterval := app.Int(cli.IntOpt{
		Name:   "full-run-interval-seconds",
		Value:  60,
		Desc:   "Full run interval",
		EnvVar: "FULL_RUN_INTERVAL_SECONDS",
	})
	dryRun := app.Bool(cli.BoolOpt{
		Name:   "dry-run",
		Value:  false,
		Desc:   "Dry run",
		EnvVar: "DRY_RUN",
	})
	label := app.String(cli.StringOpt{
		Name:   "label",
		Value:  "automaticDeployment",
		Desc:   "K8s label used to enable/disable automatic deployments.",
		EnvVar: "LABEL",
	})

	if *diffURLFormat != "" && !strings.Contains(*diffURLFormat, "%s") {
		log.Fatalf("Invalid DIFF_URL_FORMAT, must contain %q: %v", "%s", *diffURLFormat)
	}

	if *basePath == "" || *namespace == "" {
		log.Fatalf("Must provide base-path and namespace")
	}

	repoPath := filepath.Join(*basePath, *namespace)

	app.Action = func() {
		metrics := &metrics.Prometheus{}
		metrics.Init()

		clock := &sysutil.Clock{}

		if err := sysutil.WaitForDir(repoPath, clock, waitForRepoInterval); err != nil {
			log.Fatal(err)
		}

		kubeClient := &kube.Client{Server: *server, Label: *label}
		kubeClient.Configure()
		batchApplier := &run.BatchApplier{KubeClient: kubeClient, DryRun: *dryRun, Metrics: metrics}
		gitUtil := &git.GitUtil{repoPath}

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
			RepoPath:      repoPath,
			BatchApplier:  batchApplier,
			GitUtil:       gitUtil,
			Clock:         clock,
			Metrics:       metrics,
			DiffURLFormat: *diffURLFormat,
			RunQueue:      runQueue,
			RunResults:    runResults,
			Errors:        errors,
		}
		scheduler := &run.Scheduler{gitUtil, time.Duration(*pollInterval) * time.Second, time.Duration(*fullRunInterval) * time.Second, runQueue, errors}
		webserver := &webserver.WebServer{*listenPort, clock, runQueue, runResults, errors}

		go scheduler.Start()
		go runner.Start()
		go webserver.Start()

		for err := range errors {
			log.Fatal(err)
		}
	}
	app.Run(os.Args)
}
