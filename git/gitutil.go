package git

import (
	"fmt"
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
func (g *Util) HeadHashForPaths(args ...string) (string, error) {
	cmd := []string{"log", "--pretty=format:%h", "-n", "1", "--"}
	cmd = append(cmd, args...)
	hash, err := runGitCmd(g.RepoPath, cmd...)
	return strings.Trim(hash, "\n"), err
}

// HeadCommitLogForPaths returns the log of the current HEAD commit, including a list
// of the files that were modified for the filtered directories
func (g *Util) HeadCommitLogForPaths(args ...string) (string, error) {
	cmd := []string{"log", "-1", "--name-status", "--"}
	cmd = append(cmd, args...)
	log, err := runGitCmd(g.RepoPath, cmd...)
	return log, err
}

// CommitLog returns the log of the provided commit, including a list of the
// files that were modified for the filtered directories
func (g *Util) CommitLog(commit string) (string, error) {
	cmd := []string{"log", "-1", "--name-status", commit}
	cmd = append(cmd)
	log, err := runGitCmd(g.RepoPath, cmd...)
	return log, err
}

// HasChangesForPath returns true if changes have been committed since the
// commit hash provided, under the specified path.
func (g *Util) HasChangesForPath(path, sinceHash string) (bool, error) {
	cmd := []string{"diff", "--quiet", sinceHash, "HEAD", "--", path}
	_, err := runGitCmd(g.RepoPath, cmd...)
	if _, ok := err.(*exec.ExitError); ok {
		return true, nil
	}
	return false, err
}

// SplitPath returns the absolute root path of the git repository, as well as
// the relative subpath, based on the RepoPath attribute.
func (g *Util) SplitPath() (string, string, error) {
	root, err := runGitCmd(g.RepoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	sub, err := runGitCmd(g.RepoPath, "rev-parse", "--show-prefix")
	if err != nil {
		return "", "", err
	}
	return strings.Trim(root, "\n"), strings.Trim(sub, "\n"), nil
}

// CloneRepository clones a shallow copy of local repository to a new location
// on disk and only checkouts the specified path.
func CloneRepository(src, dst, path string) error {
	// git clone --no-checkout src dst
	if _, err := runGitCmd("/", "clone", "--no-checkout", src, dst); err != nil {
		return err
	}

	// git checkout HEAD -- ./path
	_, err := runGitCmd(dst, "checkout", "HEAD", "--", fmt.Sprintf("./%s", path))
	return err
}

func runGitCmd(dir string, args ...string) (string, error) {
	var cmd *exec.Cmd
	cmd = exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Error running command %v: %v: %s", strings.Join(cmd.Args, " "), err, output)
	}
	return string(output), nil
}
