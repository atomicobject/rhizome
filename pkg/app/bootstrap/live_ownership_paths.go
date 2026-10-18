package bootstrap

// Path and snapshot helpers shared by the ownership reconciliation batch.

import (
	"github.com/atomicobject/rhizome/pkg/vault/cache"
)

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
