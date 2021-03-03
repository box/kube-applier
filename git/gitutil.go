// Package git provides methods for manipulating and querying git repositories
// on disk.
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Util allows for fetching information about a Git repository using Git CLI
// commands.
type Util struct {
	RepoPath string
}

// HeadHashForPaths returns the hash of the current HEAD commit for the
// filtered directories
func (g *Util) HeadHashForPaths(ctx context.Context, args ...string) (string, error) {
	cmd := []string{"log", "--pretty=format:%h", "-n", "1", "--"}
	cmd = append(cmd, args...)
	hash, err := runGitCmd(ctx, nil, g.RepoPath, cmd...)
	return strings.Trim(hash, "\n"), err
}

// HasChangesForPath returns true if changes have been committed since the
// commit hash provided, under the specified path.
func (g *Util) HasChangesForPath(ctx context.Context, path, sinceHash string) (bool, error) {
	cmd := []string{"diff", "--quiet", sinceHash, "HEAD", "--", path}
	_, err := runGitCmd(ctx, nil, g.RepoPath, cmd...)
	if err == nil {
		return false, nil
	}
	var e *exec.ExitError
	if errors.As(err, &e) && e.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

// SplitPath returns the absolute root path of the git repository, as well as
// the relative subpath, based on the RepoPath attribute.
func (g *Util) SplitPath(ctx context.Context) (string, string, error) {
	root, err := runGitCmd(ctx, nil, g.RepoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	sub, err := runGitCmd(ctx, nil, g.RepoPath, "rev-parse", "--show-prefix")
	if err != nil {
		return "", "", err
	}
	return strings.Trim(root, "\n"), strings.Trim(sub, "\n"), nil
}

// CloneRepository clones a shallow copy of local repository to a new location
// on disk and only checkouts the specified path.
func CloneRepository(ctx context.Context, src, dst, path string, environment []string) error {
	// git clone --no-checkout src dst
	if _, err := runGitCmd(ctx, nil, "/", "clone", "--no-checkout", src, dst); err != nil {
		return err
	}

	// git checkout HEAD -- ./path
	_, err := runGitCmd(ctx, environment, dst, "checkout", "HEAD", "--", fmt.Sprintf("./%s", path))
	return err
}

func runGitCmd(ctx context.Context, environment []string, dir string, args ...string) (string, error) {
	var cmd *exec.Cmd
	cmd = exec.CommandContext(ctx, "git", args...)
	if len(environment) > 0 {
		cmd.Env = append(os.Environ(), environment...)
	}
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Error running command '%v': %w. Output: %s", strings.Join(cmd.Args, " "), err, output)
	}
	return string(output), nil
}
