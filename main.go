package main

import (
	"context"
	"flag"
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
	"github.com/utilitywarehouse/kube-applier/webserver/oidc"
)

var (
	fDiffURLFormat        = flag.String("diff-url-format", getStringEnv("DIFF_URL_FORMAT", ""), "Used to generate commit links in the status page")
	fDryRun               = flag.Bool("dry-run", getBoolEnv("DRY_RUN", false), "Whether kube-applier operates in dry-run mode globally")
	fGitPollWait          = flag.Duration("git-poll-wait", getDurationEnv("GIT_POLL_WAIT", time.Second*5), "How long kube-applier waits before checking for changes in the repository")
	fGitKnownHostsPath    = flag.String("git-ssh-known-hosts-path", getStringEnv("GIT_KNOWN_HOSTS_PATH", ""), "Path to the known hosts file used for fetching the repository")
	fGitSSHKeyPath        = flag.String("git-ssh-key-path", getStringEnv("GIT_SSH_KEY_PATH", ""), "Path to the SSH key file used for fetching the repository")
	fListenPort           = flag.Int("listen-port", getIntEnv("LISTEN_PORT", 8080), "Port that the http server is listening on")
	fLogLevel             = flag.String("log-level", getStringEnv("LOG_LEVEL", "warn"), "Logging level: trace, debug, info, warn, error, off")
	fOidcCallbackURL      = flag.String("oidc-callback-url", getStringEnv("OIDC_CALLBACK_URL", ""), "OIDC callback url should be the root URL where kube-applier is exposed")
	fOidcClientID         = flag.String("oidc-client-id", getStringEnv("OIDC_CLIENT_ID", ""), "Client ID of the OIDC application")
	fOidcClientSecret     = flag.String("oidc-client-secret", getStringEnv("OIDC_CLIENT_SECRET", ""), "Client secret of the OIDC application")
	fOidcIssuer           = flag.String("oidc-issuer", getStringEnv("OIDC_ISSUER", ""), "OIDC issuer URL of the authentication server")
	fPruneBlacklist       = flag.String("prune-blacklist", getStringEnv("PRUNE_BLACKLIST", ""), "Comma-seperated list of resources to add to the global prune blacklist, in the <group>/<version>/<kind> format")
	fRepoBranch           = flag.String("repo-branch", getStringEnv("REPO_BRANCH", "master"), "Branch of the git repository to use")
	fRepoDepth            = flag.Int("repo-depth", getIntEnv("REPO_DEPTH", 0), "Depth of the git repository to fetch. Use zero to ignore")
	fRepoDest             = flag.String("repo-dest", getStringEnv("REPO_DEST", "/src"), "Path under which the the git repository is fetched")
	fRepoPath             = flag.String("repo-path", getStringEnv("REPO_PATH", ""), "Path relative to the repository root that kube-applier operates in")
	fRepoRemote           = flag.String("repo-remote", getStringEnv("REPO_REMOTE", ""), "Remote URL of the git repository that kube-applier uses as a source")
	fRepoRevision         = flag.String("repo-revision", getStringEnv("REPO_REVISION", "HEAD"), "Revision of the git repository to use")
	fRepoSyncInterval     = flag.Duration("repo-sync-interval", getDurationEnv("REPO_SYNC_INTERVAL", time.Second*30), "How often kube-applier will try to sync the local repository clone to the remote")
	fRepoTimeout          = flag.Duration("repo-timeout", getDurationEnv("REPO_TIMEOUT", time.Minute*3), "How long kube-applier will wait for the initial repository sync to complete")
	fStatusUpdateInterval = flag.Duration("status-update-interval", getDurationEnv("STATUS_UPDATE_INTERVAL", time.Minute), "How often the status page updates from the cluster state")
	fWaybillPollInterval  = flag.Duration("waybill-poll-interval", getDurationEnv("WAYBILL_POLL_INTERVAL", time.Minute), "How often kube-applier updates the Waybills it tracks from the cluster")
	fWorkerCount          = flag.Int("worker-count", getIntEnv("WORKER_COUNT", 2), "Number of apply worker goroutines that kube-applier uses")
)

func getStringEnv(name, defaultValue string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return defaultValue
}

func getBoolEnv(name string, defaultValue bool) bool {
	if v, ok := os.LookupEnv(name); ok {
		vv, err := strconv.ParseBool(v)
		if err != nil {
			fmt.Printf("%s must be a boolean, got %v\n", name, v)
			os.Exit(1)
		}
		return vv
	}
	return defaultValue
}

func getIntEnv(name string, defaultValue int) int {
	if v, ok := os.LookupEnv(name); ok {
		vv, err := strconv.Atoi(v)
		if err != nil {
			fmt.Printf("%s must be an integer, got %v\n", name, v)
			os.Exit(1)
		}
		return vv
	}
	return defaultValue
}

func getDurationEnv(name string, defaultValue time.Duration) time.Duration {
	if v, ok := os.LookupEnv(name); ok {
		vv, err := time.ParseDuration(v)
		if err != nil {
			fmt.Printf("%s must be a duration, got %v\n", name, v)
			os.Exit(1)
		}
		return vv
	}
	return defaultValue
}

func main() {
	flag.Parse()

	log.SetLevel(*fLogLevel)

	clock := &sysutil.Clock{}

	var (
		oidcAuthenticator *oidc.Authenticator
		err               error
	)

	if strings.Join([]string{*fOidcIssuer, *fOidcClientID, *fOidcClientSecret, *fOidcCallbackURL}, "") != "" {
		oidcAuthenticator, err = oidc.NewAuthenticator(
			*fOidcIssuer,
			*fOidcClientID,
			*fOidcClientSecret,
			*fOidcCallbackURL,
		)
		if err != nil {
			log.Logger("kube-applier").Error("could not setup oidc authenticator", "error", err)
			os.Exit(1)
		}
		log.Logger("kube-applier").Info("OIDC authentication configured", "issuer", *fOidcIssuer, "clientID", *fOidcClientID)
	}

	repo, err := git.NewRepository(
		*fRepoDest,
		git.RepositoryConfig{
			Remote:   *fRepoRemote,
			Branch:   *fRepoBranch,
			Revision: *fRepoRevision,
			Depth:    *fRepoDepth,
		},
		git.SyncOptions{
			GitSSHKeyPath:        *fGitSSHKeyPath,
			GitSSHKnownHostsPath: *fGitKnownHostsPath,
			Interval:             *fRepoSyncInterval,
		},
	)
	if err != nil {
		log.Logger("kube-applier").Error("could not create git repository", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *fRepoTimeout)
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

	kubectlClient := kubectl.NewClient("", "", "")

	// Kubernetes copies annotations from StatefulSets, Deployments and
	// Daemonsets to the corresponding ControllerRevision, including
	// 'kubectl.kubernetes.io/last-applied-configuration', which will result
	// in kube-applier pruning ControllerRevisions that it shouldn't be
	// managing at all. This makes it unsuitable for pruning and a
	// reasonable default for blacklisting.
	pruneBlacklistSlice := []string{"apps/v1/ControllerRevision"}
	if *fPruneBlacklist != "" {
		pruneBlacklistSlice = append(pruneBlacklistSlice, strings.Split(*fPruneBlacklist, ",")...)
	}

	runner := &run.Runner{
		Clock:          clock,
		DryRun:         *fDryRun,
		KubeClient:     kubeClient,
		KubectlClient:  kubectlClient,
		PruneBlacklist: pruneBlacklistSlice,
		Repository:     repo,
		RepoPath:       *fRepoPath,
		WorkerCount:    *fWorkerCount,
	}

	runQueue := runner.Start()

	scheduler := &run.Scheduler{
		Clock:               clock,
		GitPollWait:         *fGitPollWait,
		KubeClient:          kubeClient,
		Repository:          repo,
		RepoPath:            *fRepoPath,
		RunQueue:            runQueue,
		WaybillPollInterval: *fWaybillPollInterval,
	}
	scheduler.Start()

	webserver := &webserver.WebServer{
		Authenticator:        oidcAuthenticator,
		Clock:                clock,
		DiffURLFormat:        *fDiffURLFormat,
		KubeClient:           kubeClient,
		ListenPort:           *fListenPort,
		RunQueue:             runQueue,
		StatusUpdateInterval: *fStatusUpdateInterval,
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
