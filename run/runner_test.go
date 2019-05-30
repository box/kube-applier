package run

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPruneDirsWithFilter(t *testing.T) {
	runner := Runner{
		RepoPath:        "/repo/",
		RepoPathFilters: []string{"run", "webserver", "sys*", "?anifests"},
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
/repo/sys-log
/repo/templates
/repo/webserver
`, "\n")

	prunedDirs := runner.pruneDirs(dirs)
	assert.Len(t, prunedDirs, 5)
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
/repo/sys-log
/repo/templates
/repo/webserver
`, "\n")

	prunedDirs := runner.pruneDirs(dirs)
	assert.Len(t, prunedDirs, 14)
}
