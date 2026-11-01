package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

type cachePruneDomain struct {
	name, table, referenced string
	store                   interface {
		CacheEmbedding(context.Context, string, embeddings.Embedding) error
		EmbeddingByHash(context.Context, string) (embeddings.Embedding, bool, error)
		PruneEmbeddingCache(context.Context) (int64, error)
	}
	editAway func(t *testing.T)
	// Previous schema, before the cache recorded formats.
	meta, migrationDomain string
	previousVersion       int
	reopen                func() (interface{ Close() error }, error)
}

func cachePruneDomains(t *testing.T) (*sql.DB, []cachePruneDomain) {
	db, notes, code := sharedDomainFixture(t)
	ctx := t.Context()
	return db, []cachePruneDomain{
		{name: "notes", table: tableEmbeddingCache, referenced: "note-paid", store: notes, editAway: func(t *testing.T) {
			require.NoError(t, notes.UpsertNoteChunks(ctx, "n1", []embeddings.ChunkInput{{Index: 0, Hash: "note-edited"}}, []embeddings.Embedding{{0, 0, 1, 0}}))
		}, meta: tableIndexMeta, migrationDomain: string(migration.DomainNoteEmbeddings), previousVersion: 7, reopen: func() (interface{ Close() error }, error) {
			meta := embeddings.IndexMetadata{Provider: "test-notes", Model: "note-paid", Dimensions: 4}
			return OpenNotes(ctx, Borrowed(db), Options{Access: WriteIndex, Dimensions: 4, Metadata: &meta})
		}},
		{name: "code", table: codeTableEmbeddingCache, referenced: "code-paid", store: code, editAway: func(t *testing.T) {
			require.NoError(t, code.UpsertItemChunks(ctx, "a1", []codeindex.ChunkInput{{Index: 0, Granularity: "symbol", Hash: "code-edited"}}, []string{"func Source() { edited() }"}, []embeddings.Embedding{{0, 0, 1, 0}}))
		}, meta: codeTableIndexMeta, migrationDomain: string(migration.DomainCodeEmbeddings), previousVersion: 12, reopen: func() (interface{ Close() error }, error) {
			meta := embeddings.IndexMetadata{Provider: "test-code", Model: "code-paid", Dimensions: 4}
			return OpenCode(ctx, Borrowed(db), Options{Access: WriteIndex, Dimensions: 4, Metadata: &meta})
		}},
	}
}

func setCacheFormat(t *testing.T, db *sql.DB, table, hash, format string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `UPDATE `+table+` SET format = ? WHERE content_hash = ?`, format, hash)
	require.NoError(t, err)
}

func cacheFormatOf(t *testing.T, db *sql.DB, table, hash string) string {
	t.Helper()
	var format string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT format FROM `+table+` WHERE content_hash = ?`, hash).Scan(&format))
	return format
}

func TestPruneEmbeddingCacheRemovesOnlyUnreferencedEarlierFormats(t *testing.T) {
	db, domains := cachePruneDomains(t)
	ctx := t.Context()
	for _, d := range domains {
		t.Run(d.name, func(t *testing.T) {
			cached := func(hash string) bool {
				_, ok, err := d.store.EmbeddingByHash(ctx, hash)
				require.NoError(t, err)
				return ok
			}
			for _, hash := range []string{"earlier", "revert-history", "unknown"} {
				require.NoError(t, d.store.CacheEmbedding(ctx, hash, embeddings.Embedding{0, 0, 0, 1}))
			}
			setCacheFormat(t, db, d.table, "earlier", "earlier-format")
			setCacheFormat(t, db, d.table, "unknown", "")
			setCacheFormat(t, db, d.table, d.referenced, "earlier-format")

			pruned, err := d.store.PruneEmbeddingCache(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 2, pruned)
			require.False(t, cached("earlier"), "an unreferenced earlier-format vector is pruned")
			require.True(t, cached("revert-history"), "an unreferenced current-format vector keeps revert reuse")
			require.Equal(t, cacheFormat(d.table), cacheFormatOf(t, db, d.table, "revert-history"), "every cache write records the current format")
			require.False(t, cached("unknown"), "an unreferenced vector from before formats were recorded is reclaimed")
			require.True(t, cached(d.referenced), "a referenced vector is kept")
			require.Equal(t, cacheFormat(d.table), cacheFormatOf(t, db, d.table, d.referenced), "a referenced vector adopts the current format")

			d.editAway(t)
			_, err = d.store.PruneEmbeddingCache(ctx)
			require.NoError(t, err)
			require.True(t, cached(d.referenced), "a promoted vector survives being edited away, so a revert reuses it")
		})
	}
}

// Caches written before the format column existed keep every paid vector
// through the upgrade, with an unknown format.
func TestEmbeddingCacheFormatUpgradeKeepsVectors(t *testing.T) {
	db, domains := cachePruneDomains(t)
	ctx := t.Context()
	for _, d := range domains {
		t.Run(d.name, func(t *testing.T) {
			require.NoError(t, d.store.CacheEmbedding(ctx, "paid-before-upgrade", embeddings.Embedding{0, 0, 0, 1}))
			_, err := db.ExecContext(ctx, `ALTER TABLE `+d.table+` DROP COLUMN format`)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `UPDATE `+d.meta+` SET schema_version = ?`, d.previousVersion)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `UPDATE rzm_migration_state SET version = ? WHERE domain = ?`, d.previousVersion, d.migrationDomain)
			require.NoError(t, err)

			reopened, err := d.reopen()
			require.NoError(t, err)
			t.Cleanup(func() { _ = reopened.Close() })

			for _, hash := range []string{"paid-before-upgrade", d.referenced} {
				vec, ok, err := d.store.EmbeddingByHash(ctx, hash)
				require.NoError(t, err)
				require.True(t, ok, hash)
				require.NotEmpty(t, vec)
				require.Equal(t, "", cacheFormatOf(t, db, d.table, hash))
			}
		})
	}
}
