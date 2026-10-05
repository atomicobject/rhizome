package bootstrap

// Path and snapshot helpers shared by the ownership reconciliation batch.

import (
	"sort"

	"github.com/atomicobject/rhizome/pkg/vault/cache"
)

func mergeLivePaths(groups ...[]string) []string {
	set := make(map[string]struct{})
	for _, group := range groups {
		for _, path := range group {
			if path != "" {
				set[path] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(set))
	for path := range set {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func cloneDirtyKinds(input map[string]cache.DirtyKind) map[string]cache.DirtyKind {
	if len(input) == 0 {
		return make(map[string]cache.DirtyKind)
	}
	result := make(map[string]cache.DirtyKind, len(input))
	for path, kind := range input {
		result[path] = kind
	}
	return result
}
