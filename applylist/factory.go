package applylist

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/box/kube-applier/sysutil"
)

// FactoryInterface allows for mocking out the functionality of Factory when testing the full process of an apply run.
type FactoryInterface interface {
	Create([]string) (applyList, blacklist, whitelist []string, err error)
}

// Factory handles constructing the list of files to apply and the blacklist.
type Factory struct {
	RepoPath      string
	DirectoryMode bool
	BlacklistPath string
	WhitelistPath string
	FileSystem    sysutil.FileSystemInterface
}

// Create takes in a preliminary list of candidate files for applying, and filters against the blacklist and whitelist.
// Three alphabetically sorted lists are returned: the final list of files to apply, the blacklist, and the whitelist.
func (f *Factory) Create(rawList []string) (applyList, blacklist, whitelist []string, err error) {
	blacklist, err = f.createBlacklist()
	if err != nil {
		return nil, nil, nil, err
	}
	whitelist, err = f.createWhitelist()
	if err != nil {
		return nil, nil, nil, err
	}
	applyList = f.filter(rawList, blacklist, whitelist)
	sort.Strings(applyList)
	return applyList, blacklist, whitelist, nil
}

// purgeCommentsFromList iterates over the list contents and deletes comment
// lines. A comment is a line whose first non-space character is #
func (f *Factory) purgeCommentsFromList(rawList []string) []string {

	// http://stackoverflow.com/a/20551116/5771861
	i := 0
	for _, l := range rawList {
		// # is the comment line
		if len(l) > 0 && string(l[0]) != "#" {
			rawList[i] = l
			i++
		}
	}
	rv := rawList[:i]
	return rv
}

// createFilelist reads lines from the given file, converts the relative
// paths to full paths, and returns a sorted list of full paths.
func (f *Factory) createFileList(listFilePath string) ([]string, error) {
	if listFilePath == "" {
		return []string{}, nil
	}
	rawList, err := f.FileSystem.ReadLines(listFilePath)
	if err != nil {
		return nil, err
	}

	filteredList := f.purgeCommentsFromList(rawList)

	list := PrependToEachPath(f.RepoPath, filteredList)
	sort.Strings(list)
	return list, nil
}

// createBlacklist reads lines from the blacklist file, converts the relative
// paths to full paths, and returns a sorted list of full paths.
func (f *Factory) createBlacklist() ([]string, error) {
	return f.createFileList(f.BlacklistPath)
}

// createWhitelist reads lines from the whitelist file, converts the relative
// paths to full paths, and returns a sorted list of full paths.
func (f *Factory) createWhitelist() ([]string, error) {
	return f.createFileList(f.WhitelistPath)
}

// isSubPath returns true if the path is equal to a path in listMap, or
// if the path is a sub path of a path in listMap
func isSubPath(path string, listMap map[string]struct{}) bool {
	_, inListMap := listMap[path]
	if inListMap {
		return true
	}

	for p := range listMap {
		rel, err := filepath.Rel(p, path)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

// shouldApplyFilePath returns true if file path should be applied, false otherwise.
// Conditions for skipping the file path are:
// 1. File path is not a .json or .yaml file
// 2. File path is listed in the blacklist
func shouldApplyFilePath(path string, blacklistMap, whitelistMap map[string]struct{}) bool {
	_, inBlacklist := blacklistMap[path]

	// If whitelist is empty, essentially there is no whitelist.
	inWhiteList := len(whitelistMap) == 0
	if !inWhiteList {
		_, inWhiteList = whitelistMap[path]
	}
	ext := filepath.Ext(path)
	return inWhiteList && !inBlacklist && (ext == ".json" || ext == ".yaml")
}

// shouldApplyDirectoryPath returns true if a path to a directory should be applied, false otherwise.
// Conditions for skipping the path are:
// a) File path is listed in the blacklist
// or
// b) File path is subpath of a path in blacklist
func shouldApplyDirectoryPath(path string, blacklistMap, whitelistMap map[string]struct{}) bool {
	inBlacklist := isSubPath(path, blacklistMap)

	// If whitelist is empty, essentially there is no whitelist.
	inWhiteList := len(whitelistMap) == 0
	if !inWhiteList {
		inWhiteList = isSubPath(path, whitelistMap)
	}

	return inWhiteList && !inBlacklist
}

// filter iterates through the list of all files in the repo and filters it
// down to a list of those that should be applied.
func (f *Factory) filter(rawApplyList, blacklist, whitelist []string) []string {
	blacklistMap := stringSliceToMap(blacklist)
	whitelistMap := stringSliceToMap(whitelist)

	applyList := []string{}
	for _, filePath := range rawApplyList {
		if (f.DirectoryMode && shouldApplyDirectoryPath(filePath, blacklistMap, whitelistMap)) ||
			(!f.DirectoryMode && shouldApplyFilePath(filePath, blacklistMap, whitelistMap)) {
			applyList = append(applyList, filePath)
		}
	}
	return applyList
}
