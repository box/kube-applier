package run

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPruneDirsWithFilter(t *testing.T) {
	runner := Runner{
		RepoPathFilters: []string{"run", "webserver", "sys*", "?anifests"},
	}

	dirs := strings.Split(`.git
git
kube
log
Makefile
manifests
metrics
run
static
sysutil
sys-log
templates
webserver
`, "\n")

	prunedDirs := runner.pruneDirs(dirs)
	assert.Len(t, prunedDirs, 5)
}

func TestPruneDirsWithoutFilter(t *testing.T) {
	runner := Runner{
		RepoPathFilters: []string{},
	}

	dirs := strings.Split(`.git
git
kube
log
Makefile
manifests
metrics
run
static
sysutil
sys-log
templates
webserver
`, "\n")

	prunedDirs := runner.pruneDirs(dirs)
	assert.Len(t, prunedDirs, 14)
}
