package unifiedsearch

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// loadRequestCorpus plans one shared catalog for all facets. Precision symbol
// lookups need no corpus scan; broad code requests need only shape counts.
func loadRequestCorpus(ctx context.Context, opts Options, inputs []QueryInput, seeds []string, fallback search.Intent, store *semdb.Store) (*search.CorpusSnapshot, error) {
	metadata := opts.Filters.AllowsType("note") || len(seeds) > 0
	ownership := len(seeds) > 0
	shape := false
	for _, input := range inputs {
		intent := fallback
		if mode := strings.TrimSpace(input.Mode); mode != "" {
			intent = search.Intent(mode)
		}
		shape = shape || search.IsBroadIntent(intent)
		ownership = ownership || search.QueryNeedsPathOwnership(search.QuerySpec{Text: input.Text, Intent: intent})
	}
	if !metadata && !ownership && !shape {
		return nil, nil
	}
	detail := search.CorpusPaths
	if ownership {
		detail = search.CorpusOwnership
	}
	if metadata {
		detail = search.CorpusMetadata
	}
	return search.LoadCorpusSnapshot(ctx, store, detail)
}
