package applylist

import "path"

// PrependToEachPath prepends the specified prefix path to the base path, using path.Join to handle slashes.
func PrependToEachPath(prefix string, paths []string) []string {
	result := []string{}
	for _, basePath := range paths {
		result = append(result, path.Join(prefix, basePath))
	}
	return result
}
