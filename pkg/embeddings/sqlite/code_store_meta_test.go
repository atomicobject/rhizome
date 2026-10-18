package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embeddings"
)

// A first call with dims=0 must NOT persist a placeholder row, and a second
// call with the real dims must heal the meta in place rather than wiping the
// index. Voyage's lazy dimension discovery makes this the common path.
func TestValidateOrInitMetadata_HealsZeroDimensions(t *testing.T) {
	for _, tc := range []struct{ name, initialProvider, initialModel string }{
		{"known provider and model", "test", "text-embedding-3-large"},
		{"missing provider and model", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dbPath := filepath.Join(t.TempDir(), "codeemb.db")

			store, err := OpenCode(context.Background(), File(dbPath), Options{Access: WriteIndex, Dimensions: 0})
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })

			require.NoError(t, store.EnsureSchema(ctx))

			// Pre-embed call: provider hasn't learned dims yet; no row should be written.
			require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
				Provider:      tc.initialProvider,
				Model:         tc.initialModel,
				Dimensions:    0,
				SchemaVersion: codeSchemaVersion,
			}))
			_, ok, err := store.Metadata(ctx)
			require.NoError(t, err)
			require.False(t, ok, "no row should be inserted while dimensions are unknown")

			// Now seed the kind of stub data a real index pass would have written.
			_, err = store.db.ExecContext(ctx, `INSERT INTO intel_code_anchors (anchor_id, path) VALUES ('a1', 'pkg/foo.go')`)
			require.NoError(t, err)
			_, err = store.db.ExecContext(ctx, `INSERT INTO code_items (anchor_row_id, updated_at) SELECT id, strftime('%s','now') FROM intel_code_anchors WHERE anchor_id = 'a1'`)
			require.NoError(t, err)

			// Provider has now learned dims; meta row should be initialized.
			require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
				Provider:      "test",
				Model:         "text-embedding-3-large",
				Dimensions:    8,
				SchemaVersion: codeSchemaVersion,
			}))

			meta, ok, err := store.Metadata(ctx)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, "test", meta.Provider)
			require.Equal(t, "text-embedding-3-large", meta.Model)
			require.Equal(t, 8, meta.Dimensions)

			// Items must survive: dims=0 is a placeholder, not a corruption signal.
			var count int
			require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM code_items`).Scan(&count))
			require.Equal(t, 1, count, "validator must not wipe data when dims transition 0 -> known")
		})
	}
}

func TestCodeValidateExistingMetadataIsReadOnlyAndRejectsMismatch(t *testing.T) {
	ctx := context.Background()
	store, err := OpenCode(context.Background(), File(filepath.Join(t.TempDir(), "existing.db")), Options{Access: WriteIndex, Dimensions: 4})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	want := embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4, FingerprintVersion: 2}
	require.NoError(t, store.ValidateOrInitMetadata(ctx, want))

	require.NoError(t, validateCodeExisting(ctx, store.db, want))
	err = validateCodeExisting(ctx, store.db, embeddings.IndexMetadata{Provider: "test", Model: "other", Dimensions: 4, FingerprintVersion: 3})
	require.ErrorContains(t, err, "model mismatch")
	got, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2, got.FingerprintVersion)
}

func TestValidateExistingMetadataAcceptsUnknownStoredDimensionsWithoutMutation(t *testing.T) {
	ctx := context.Background()
	store, err := OpenCode(context.Background(), File(filepath.Join(t.TempDir(), "legacy-zero-readonly.db")), Options{Access: WriteIndex, Dimensions: 4})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	want := embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4, FingerprintVersion: 2}
	require.NoError(t, store.ValidateOrInitMetadata(ctx, want))
	_, err = store.db.ExecContext(ctx, `UPDATE `+codeTableIndexMeta+` SET dimensions = 0 WHERE id = 1`)
	require.NoError(t, err)

	require.NoError(t, validateCodeExisting(ctx, store.db, want))
	var dimensions int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT dimensions FROM `+codeTableIndexMeta+` WHERE id = 1`).Scan(&dimensions))
	require.Zero(t, dimensions)
}

func TestCodeIncrementSyncGenerationCreatesLazyDimensionMetadataRow(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "codeemb-lazy-sync.db")

	store, err := OpenCode(context.Background(), File(dbPath), Options{Access: WriteIndex, Dimensions: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:      "voyage",
		Model:         "voyage-4-lite",
		Dimensions:    0,
		SchemaVersion: codeSchemaVersion,
	}))
	gen, err := store.IncrementSyncGeneration(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), gen)

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:      "voyage",
		Model:         "voyage-4-lite",
		Dimensions:    1024,
		SchemaVersion: codeSchemaVersion,
	}))
	meta, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "voyage", meta.Provider)
	require.Equal(t, "voyage-4-lite", meta.Model)
	require.Equal(t, 1024, meta.Dimensions)
	gotGen, err := store.SyncGeneration(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), gotGen)
}

// Legacy databases written by the prior (buggy) version may already have a
// 0-dim row plus real embeddings. The validator must heal the row rather
// than nuke the data.
func TestCodeValidateOrInitMetadata_HealsLegacyZeroDimRow(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy-zero.db")

	store, err := OpenCode(context.Background(), File(dbPath), Options{Access: WriteIndex, Dimensions: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))

	// Simulate a legacy 0-dim row written by an earlier buggy version.
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+codeTableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, fingerprint_version)
		VALUES (1, 'voyage', 'voyage-4-lite', 0, ?, 1, 0)
	`, codeSchemaVersion)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `INSERT INTO intel_code_anchors (anchor_id, path) VALUES ('a1', 'pkg/foo.go')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO code_items (anchor_row_id, updated_at) SELECT id, strftime('%s','now') FROM intel_code_anchors WHERE anchor_id = 'a1'`)
	require.NoError(t, err)

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:      "voyage",
		Model:         "voyage-4-lite",
		Dimensions:    1024,
		SchemaVersion: codeSchemaVersion,
	}))

	meta, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1024, meta.Dimensions)

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM code_items`).Scan(&count))
	require.Equal(t, 1, count, "validator must not wipe data when healing a legacy 0-dim row")
}

// A 0-dim first call followed by a known-dim second call must heal the row
// in place. Provider/model values from the second call must be persisted as
// the row's first concrete metadata (no destructive reset).
func TestValidateOrInitMetadata_RebuildsOnMismatch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "codeemb.db")

	store, err := OpenCode(context.Background(), File(dbPath), Options{Access: WriteIndex, Dimensions: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:      "test",
		Model:         "text-embedding-3-large",
		Dimensions:    8,
		SchemaVersion: codeSchemaVersion,
	}))

	// Insert a stub row to confirm the domain gets reset.
	_, err = store.db.ExecContext(ctx, `INSERT INTO intel_code_anchors (anchor_id, path) VALUES ('a1', 'pkg/foo.go')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `INSERT INTO code_items (anchor_row_id, updated_at) SELECT id, strftime('%s','now') FROM intel_code_anchors WHERE anchor_id = 'a1'`)
	require.NoError(t, err)

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:      "test",
		Model:         "text-embedding-3-small",
		Dimensions:    3072,
		SchemaVersion: codeSchemaVersion,
	}))

	meta, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "test", meta.Provider)
	require.Equal(t, "text-embedding-3-small", meta.Model)
	require.Equal(t, 3072, meta.Dimensions)

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM code_items`).Scan(&count))
	require.Zero(t, count, "expected reset to drop stale items")
}
