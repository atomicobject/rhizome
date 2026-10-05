package unifiedsearch

import (
	"context"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	codeembsql "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	noteembsql "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/indexgeneration"
)

var ErrIndexGenerationUnavailable = indexgeneration.ErrUnavailable

// IndexGeneration is a composite identity for every persisted source used by
// unified search. NoteMetadataState hashes change when indexed note ownership,
// content, or provider regions change. The embedding stores publish visible
// generations only after their write batches commit. Scope and indexer version
// cover code eligibility and index semantics. This uses existing metadata and
// adds no persistence schema.
func IndexGeneration(ctx context.Context, intel *semdb.Store, notes *noteembsql.Store, code *codeembsql.Store) (string, error) {
	return indexgeneration.Current(ctx, intel, notes, code)
}
