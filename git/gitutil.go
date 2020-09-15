package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// UtilInterface allows for mocking out the functionality of GitUtil when
// testing the full process of an apply run.
type UtilInterface interface {
	HeadCommitLogForPaths(args ...string) (string, error)
	HeadHashForPaths(args ...string) (string, error)
	HasChangesForPath(path, sinceHash string) (bool, error)
}

// Util allows for fetching information about a Git repository using Git CLI
// commands.
type Util struct {
	RepoPath string
}

// HeadHashForPaths returns the hash of the current HEAD commit for the
// filtered directories
func (g *Util) HeadHashForPaths(args ...string) (string, error) {
	cmd := []string{"log", "--pretty=format:'%h'", "-n", "1", "--"}
	cmd = append(cmd, args...)
	hash, err := runGitCmd(g.RepoPath, cmd...)
	return strings.Trim(hash, "'\n"), err
}

// HeadCommitLogForPaths returns the log of the current HEAD commit, including a list
// of the files that were modified for the filtered directories
func (g *Util) HeadCommitLogForPaths(args ...string) (string, error) {
	cmd := []string{"log", "-1", "--name-status", "--"}
	cmd = append(cmd, args...)
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
