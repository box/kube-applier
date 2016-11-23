package applylist

import "path"

// prependToEachPath prepends the specified prefix path to the base path, using path.Join to handle slashes.
func prependToEachPath(prefix string, paths []string) []string {
	result := []string{}
	for _, basePath := range paths {
		result = append(result, path.Join(prefix, basePath))
	}
	return result
}

// stringSliceToMap creates a map with the slice's strings as keys and empty structs as values.
// The map is intended to be used for easy lookup across the set of strings.
func stringSliceToMap(strings []string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, s := range strings {
		m[s] = struct{}{}
	}
	return m
}
