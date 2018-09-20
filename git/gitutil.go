package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// UtilInterface allows for mocking out the functionality of GitUtil when testing the full process of an apply run.
type UtilInterface interface {
	HeadHash() (string, error)
	HeadCommitLog() (string, error)
}

// Util allows for fetching information about a Git repository using Git CLI commands.
type Util struct {
	RepoPath string
}

// HeadHash returns the hash of the current HEAD commit.
func (g *Util) HeadHash() (string, error) {
	hash, err := runGitCmd(g.RepoPath, "rev-parse", "HEAD")
	return strings.TrimSuffix(hash, "\n"), err
}

// HeadCommitLog returns the log of the current HEAD commit, including a list of the files that were modified.
func (g *Util) HeadCommitLog() (string, error) {
	log, err := runGitCmd(g.RepoPath, "log", "-1", "--name-status")
	return log, err
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
