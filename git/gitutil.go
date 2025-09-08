package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/box/kube-applier/applylist"
)

// GitUtilInterface allows for mocking out the functionality of GitUtil when testing the full process of an apply run.
type GitUtilInterface interface {
	HeadHash() (string, error)
	ListAllFiles() ([]string, error)
	ListAllDirectories() ([]string, error)
	CommitLog(string) (string, error)
	ListDiffFiles(string, string) ([]string, error)
	ListDiffDirectories(string, string) ([]string, error)
}

// GitUtil allows for fetching information about a Git repository using Git CLI commands.
type GitUtil struct {
	RepoPath string
}

// HeadHash returns the hash of the current HEAD commit.
func (g *GitUtil) HeadHash() (string, error) {
	hash, err := runGitCmd(g.RepoPath, "rev-parse", "HEAD")
	return strings.TrimSuffix(hash, "\n"), err
}

// CommitLog returns the log of the specified commit, including a list of the files that were modified.
func (g *GitUtil) CommitLog(hash string) (string, error) {
	log, err := runGitCmd(g.RepoPath, "log", "-1", "--name-status", hash)
	return log, err
}

func getDirectoryPaths(paths []string) []string {
	var directoryPaths = make(map[string]bool)
	// This allows us to apply paths in the order received, as maps are unordered
	var result = []string{}
	for _, path := range paths {
		dirPath := filepath.Dir(path)
		if !directoryPaths[dirPath] {
			directoryPaths[dirPath] = true
			result = append(result, dirPath)
		}
	}
	return result
}

// ListAllFiles returns a list of all files under $REPO_PATH, with paths relative to $REPO_PATH.
func (g *GitUtil) ListAllFiles() ([]string, error) {
	raw, err := runGitCmd(g.RepoPath, "ls-files")
	if err != nil {
		return nil, err
	}
	relativePaths := strings.Split(strings.TrimSpace(raw), "\n")
	fullPaths := applylist.PrependToEachPath(g.RepoPath, relativePaths)
	return fullPaths, nil
}

// ListAllDirectories returns a list of all directories under $REPO_PATH, with paths relative to $REPO_PATH.
func (g *GitUtil) ListAllDirectories() ([]string, error) {
	raw, err := runGitCmd(g.RepoPath, "ls-files")
	if err != nil {
		return nil, err
	}
	relativePaths := getDirectoryPaths(strings.Split(strings.TrimSpace(raw), "\n"))
	fullPaths := applylist.PrependToEachPath(g.RepoPath, relativePaths)
	return fullPaths, nil
}

// ListDiffFiles returns the file names that were added, modified, copied, or renamed.
// Deletes are ignored because kube-applier should not apply files deleted by a commit.
func (g *GitUtil) ListDiffFiles(oldHash, newHash string) ([]string, error) {
	raw, err := runGitCmd(g.RepoPath, "diff", "--diff-filter=AMCR", "--name-only", "--relative", oldHash, newHash)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return []string{}, nil
	}
	relativePaths := strings.Split(strings.TrimSpace(raw), "\n")
	fullPaths := applylist.PrependToEachPath(g.RepoPath, relativePaths)
	return fullPaths, nil
}

// ListDiffDirectories returns the directory names of files that were added, modified, copied, or renamed.
// Deletes are ignored becaue kube-applier should not apply files deleted by a commit.
func (g *GitUtil) ListDiffDirectories(oldHash, newHash string) ([]string, error) {
	raw, err := runGitCmd(g.RepoPath, "diff", "--diff-filter=AMCR", "--name-only", "--relative", oldHash, newHash)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return []string{}, nil
	}
	relativePaths := getDirectoryPaths(strings.Split(strings.TrimSpace(raw), "\n"))
	fullPaths := applylist.PrependToEachPath(g.RepoPath, relativePaths)
	return fullPaths, nil
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
