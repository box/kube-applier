package applylist

import (
	"github.com/utilitywarehouse/kube-applier/sysutil"
	"path/filepath"
	"sort"
)

// FactoryInterface allows for mocking out the functionality of Factory when testing the full process of an apply run.
type FactoryInterface interface {
	Create() ([]string, []string, error)
}

// Factory handles constructing the list of files to apply and the blacklist.
type Factory struct {
	RepoPath      string
	BlacklistPath string
	FileSystem    sysutil.FileSystemInterface
}

// Create returns two alphabetically sorted lists: the list of files to apply, and the blacklist of files to skip.
func (f *Factory) Create() ([]string, []string, error) {
	blacklist, err := f.createBlacklist()
	if err != nil {
		return nil, nil, err
	}
	applyList, err := f.createApplyList(blacklist)
	if err != nil {
		return nil, nil, err
	}
	return applyList, blacklist, nil
}

// createBlacklist reads lines from the blacklist file, converts the relative paths to full paths, and returns a sorted list of full paths.
func (f *Factory) createBlacklist() ([]string, error) {
	if f.BlacklistPath == "" {
		return []string{}, nil
	}
	rawBlacklist, err := f.FileSystem.ReadLines(f.BlacklistPath)
	if err != nil {
		return nil, err
	}
	blacklist := prependToEachPath(f.RepoPath, rawBlacklist)
	sort.Strings(blacklist)
	return blacklist, nil
}

// createApplyList gets all files within the repo directory and returns a filtered and sorted list of full paths.
func (f *Factory) createApplyList(blacklist []string) ([]string, error) {
	rawApplyList, err := f.FileSystem.ListAllFiles(f.RepoPath)
	if err != nil {
		return nil, err
	}
	applyList := filter(rawApplyList, blacklist)
	sort.Strings(applyList)
	return applyList, nil
}

// shouldApplyPath returns true if file path should be applied, false otherwise.
// Conditions for skipping the file path are:
// 1. File path is not a .json or .yaml file
// 2. File path is listed in the blacklist
func shouldApplyPath(path string, blacklistMap map[string]struct{}) bool {
	_, inBlacklist := blacklistMap[path]
	ext := filepath.Ext(path)
	return !inBlacklist && (ext == ".json" || ext == ".yaml")
}

// filter iterates through the list of all files in the repo and filters it down to a list of those that should be applied.
func filter(rawApplyList, blacklist []string) []string {
	blacklistMap := stringSliceToMap(blacklist)

	applyList := []string{}
	for _, filePath := range rawApplyList {
		if shouldApplyPath(filePath, blacklistMap) {
			applyList = append(applyList, filePath)
		}
	}
	return applyList
}
