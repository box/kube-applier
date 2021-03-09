// Package git provides methods for manipulating and querying git repositories
// on disk.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
)

var (
	gitExecutablePath string
)

func init() {
	gitExecutablePath = exec.Command("git").String()
}

// RepositoryConfig defines a remote git repository.
type RepositoryConfig struct {
	Remote   string
	Branch   string
	Revision string
	Depth    int
}

// SyncOptions encapsulates options about how a Repository should be fetched
// from the remote.
type SyncOptions struct {
	GitSSHKeyPath        string
	GitSSHKnownHostsPath string
	Interval             time.Duration
}

// gitSSHCommand returns the environment variable to be used for configuring
// git over ssh.
func (so SyncOptions) gitSSHCommand() string {
	sshKeyPath := so.GitSSHKeyPath
	if sshKeyPath == "" {
		sshKeyPath = "/dev/null"
	}
	knownHostsOptions := "-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no"
	if so.GitSSHKeyPath != "" && so.GitSSHKnownHostsPath != "" {
		knownHostsOptions = fmt.Sprintf("-o UserKnownHostsFile=%s", so.GitSSHKnownHostsPath)
	}
	return fmt.Sprintf(`GIT_SSH_COMMAND=ssh -q -F none -o IdentitiesOnly=yes -o IdentityFile=%s %s`, sshKeyPath, knownHostsOptions)
}

// Repository defines a remote git repository that should be synced regularly
// and is the source of truth for a cluster. Changes in this repository trigger
// GitPolling type runs for namespaces. The implementation borrows heavily from
// git-sync.
type Repository struct {
	lock             sync.Mutex
	path             string
	repositoryConfig RepositoryConfig
	running          bool
	stop, stopped    chan bool
	syncOptions      SyncOptions
}

// NewRepository initialises a Repository struct.
func NewRepository(path string, repositoryConfig RepositoryConfig, syncOptions SyncOptions) (*Repository, error) {
	if path == "" {
		return nil, fmt.Errorf("cannot create Repository with empty local path")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("Repository path must be absolute")
	}
	if repositoryConfig.Remote == "" {
		return nil, fmt.Errorf("cannot create Repository with empty remote")
	}
	if repositoryConfig.Depth < 0 {
		return nil, fmt.Errorf("Repository depth cannot be negative")
	}
	if repositoryConfig.Branch == "" {
		log.Logger("repository").Info("Defaulting repository branch to 'master'")
		repositoryConfig.Branch = "master"
	}
	if repositoryConfig.Revision == "" {
		log.Logger("repository").Info("Defaulting repository revision to 'HEAD'")
		repositoryConfig.Revision = "HEAD"
	}
	if syncOptions.Interval == 0 {
		log.Logger("repository").Info("Defaulting Interval to 30 seconds")
		syncOptions.Interval = time.Second * 30
	}
	return &Repository{
		path:             path,
		repositoryConfig: repositoryConfig,
		syncOptions:      syncOptions,
		lock:             sync.Mutex{},
	}, nil
}

// StartSync begins syncing from the remote git repository. The provided context
// is only used for the initial sync operation which is performed synchronously.
func (r *Repository) StartSync(ctx context.Context) error {
	if r.running {
		return fmt.Errorf("sync has already been started")
	}
	r.stop = make(chan bool)
	r.running = true
	log.Logger("repository").Info("waiting for the repository to complete initial sync")
	// The first sync is done outside of the syncLoop (and a seperate timeout if
	// the provided context has a deadline). The first clone might take longer
	// than usual depending on the size of the repository. Additionally it
	// runs in the foreground which simplifies startup since kube-applier
	// requires a repository clone to exist before starting up properly.
	if err := r.sync(ctx); err != nil {
		return err
	}
	go r.syncLoop()
	return nil
}

func (r *Repository) syncLoop() {
	r.stopped = make(chan bool)
	defer close(r.stopped)
	ticker := time.NewTicker(r.syncOptions.Interval)
	defer ticker.Stop()
	log.Logger("repository").Info("started repository sync loop", "interval", r.syncOptions.Interval)
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), r.syncOptions.Interval-time.Second)
			err := r.sync(ctx)
			if err != nil {
				log.Logger("repository").Error("could not sync git repository", "error", err)
			}
			metrics.RecordGitSync(err == nil)
			cancel()
		case <-r.stop:
			return
		}
	}
}

// StopSync stops the syncing process.
func (r *Repository) StopSync() {
	if !r.running {
		log.Logger("repository").Info("Sync has not been started, will not do anything")
		return
	}
	close(r.stop)
	<-r.stopped
	r.running = false
}

func (r *Repository) runGitCommand(ctx context.Context, environment []string, cwd string, args ...string) (string, error) {
	cmdStr := gitExecutablePath + " " + strings.Join(args, " ")
	log.Logger("repository").Debug("running command", "cwd", cwd, "cmd", cmdStr)

	cmd := exec.CommandContext(ctx, gitExecutablePath, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	outbuf := bytes.NewBuffer(nil)
	errbuf := bytes.NewBuffer(nil)
	cmd.Stdout = outbuf
	cmd.Stderr = errbuf
	cmd.Env = append(os.Environ(), r.syncOptions.gitSSHCommand())
	if len(environment) > 0 {
		cmd.Env = append(cmd.Env, environment...)
	}

	err := cmd.Run()
	stdout := outbuf.String()
	stderr := errbuf.String()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("Run(%s): %w: { stdout: %q, stderr: %q }", cmdStr, ctx.Err(), stdout, stderr)
	}
	if err != nil {
		return "", fmt.Errorf("Run(%s): %w: { stdout: %q, stderr: %q }", cmdStr, err, stdout, stderr)
	}
	log.Logger("repository").Debug("command result", "stdout", stdout, "stderr", stderr)

	return stdout, nil
}

// localHash returns the locally known hash for the configured Revision.
func (r *Repository) localHash(ctx context.Context) (string, error) {
	output, err := r.runGitCommand(ctx, nil, r.path, "rev-parse", r.repositoryConfig.Revision)
	if err != nil {
		return "", err
	}
	return strings.Trim(string(output), "\n"), nil
}

// localHashForPath returns the hash of the configured revision for the
// specified path.
func (r *Repository) localHashForPath(ctx context.Context, path string) (string, error) {
	output, err := r.runGitCommand(ctx, nil, r.path, "log", "--pretty=format:%h", "-n", "1", "--", path)
	if err != nil {
		return "", err
	}
	return strings.Trim(string(output), "\n"), nil
}

// remoteHash returns the upstream hash for the ref that corresponds to the
// configured Revision.
func (r *Repository) remoteHash(ctx context.Context) (string, error) {
	// Build a ref string, depending on whether the user asked to track HEAD or
	// a tag.
	ref := ""
	if r.repositoryConfig.Revision == "HEAD" {
		ref = "refs/heads/" + r.repositoryConfig.Branch
	} else {
		ref = "refs/tags/" + r.repositoryConfig.Revision
	}

	output, err := r.runGitCommand(ctx, nil, r.path, "ls-remote", "-q", "origin", ref)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(output), "\t")
	return parts[0], nil
}

func (r *Repository) sync(ctx context.Context) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	gitRepoPath := filepath.Join(r.path, ".git")
	_, err := os.Stat(gitRepoPath)
	switch {
	case os.IsNotExist(err):
		// First time. Just clone it and get the hash.
		err = r.cloneRemote(ctx)
		if err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("error checking if repo exists %q: %v", gitRepoPath, err)
	default:
		// Not the first time. Figure out if the ref has changed.
		local, err := r.localHash(ctx)
		if err != nil {
			return err
		}
		remote, err := r.remoteHash(ctx)
		if err != nil {
			return err
		}
		if local == remote {
			log.Logger("repository").Info("no update required", "rev", r.repositoryConfig.Revision, "local", local, "remote", remote)
			return nil
		}
		log.Logger("repository").Info("update required", "rev", r.repositoryConfig.Revision, "local", local, "remote", remote)
	}

	log.Logger("repository").Info("syncing git", "branch", r.repositoryConfig.Branch, "rev", r.repositoryConfig.Revision)
	args := []string{"fetch", "-f", "--tags"}
	if r.repositoryConfig.Depth != 0 {
		args = append(args, "--depth", strconv.Itoa(r.repositoryConfig.Depth))
	}
	args = append(args, "origin", r.repositoryConfig.Branch)
	// Update from the remote.
	if _, err := r.runGitCommand(ctx, nil, r.path, args...); err != nil {
		return err
	}
	// GC clone
	if _, err := r.runGitCommand(ctx, nil, r.path, "gc", "--prune=all"); err != nil {
		return err
	}
	return nil
}

func (r *Repository) cloneRemote(ctx context.Context) error {
	args := []string{"clone", "--no-checkout", "-b", r.repositoryConfig.Branch}
	if r.repositoryConfig.Depth != 0 {
		args = append(args, "--depth", strconv.Itoa(r.repositoryConfig.Depth))
	}
	args = append(args, r.repositoryConfig.Remote, r.path)
	log.Logger("repository").Info("cloning repo", "origin", r.repositoryConfig.Remote, "path", r.path)

	_, err := r.runGitCommand(ctx, nil, "", args...)
	if err != nil {
		if strings.Contains(err.Error(), "already exists and is not an empty directory") {
			// Maybe a previous run crashed?  Git won't use this dir.
			log.Logger("repository").Info("git root exists and is not empty (previous crash?), cleaning up", "path", r.path)
			err := os.RemoveAll(r.path)
			if err != nil {
				return err
			}
			_, err = r.runGitCommand(ctx, nil, "", args...)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

// CloneLocal creates a clone of the existing repository to a new location on
// disk and only checkouts the specified subpath. On success, it returns the
// hash of the new repository clone's HEAD.
func (r *Repository) CloneLocal(ctx context.Context, environment []string, dst, subpath string) (string, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	hash, err := r.localHashForPath(ctx, subpath)
	if err != nil {
		return "", err
	}

	// git clone --no-checkout src dst
	if _, err := r.runGitCommand(ctx, nil, "", "clone", "--no-checkout", r.path, dst); err != nil {
		return "", err
	}

	// git checkout HEAD -- ./path
	if _, err := r.runGitCommand(ctx, environment, dst, "checkout", r.repositoryConfig.Revision, "--", subpath); err != nil {
		return "", err
	}
	return hash, nil
}

// HashForPath returns the hash of the configured revision for the specified
// path.
func (r *Repository) HashForPath(ctx context.Context, path string) (string, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.localHashForPath(ctx, path)
}

// HasChangesForPath returns true if there are changes that have been committed
// since the commit hash provided, under the specified path.
func (r *Repository) HasChangesForPath(ctx context.Context, path, sinceHash string) (bool, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	cmd := []string{"diff", "--quiet", sinceHash, r.repositoryConfig.Revision, "--", path}
	_, err := r.runGitCommand(ctx, nil, r.path, cmd...)
	if err == nil {
		return false, nil
	}
	var e *exec.ExitError
	if errors.As(err, &e) && e.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}
