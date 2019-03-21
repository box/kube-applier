package run

import (
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestPruneDirsWithFilter(t *testing.T) {
	runner := Runner{
		RepoPath:        "/repo/",
		RepoPathFilters: []string{"run", "webserver"},
	}

	dirs := strings.Split(`/repo/.git
/repo/git
/repo/kube
/repo/log
/repo/Makefile
/repo/manifests
/repo/metrics
/repo/run
/repo/static
/repo/sysutil
/repo/templates
/repo/webserver
`, "\n")

	prunedDirs := runner.pruneDirs(dirs)
	assert.Len(t, prunedDirs,2)
}

func TestPruneDirsWithoutFilter(t *testing.T) {
	runner := Runner{
		RepoPath:        "/repo/",
		RepoPathFilters: []string{},
	}

	dirs := strings.Split(`/repo/.git
/repo/git
/repo/kube
/repo/log
/repo/Makefile
/repo/manifests
/repo/metrics
/repo/run
/repo/static
/repo/sysutil
/repo/templates
/repo/webserver
`, "\n")

	prunedDirs := runner.pruneDirs(dirs)
	assert.Len(t, prunedDirs,13)
}