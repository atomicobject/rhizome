package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

// TestValidateOrInitMetadata_SkipsZeroDimRow guards against the bug where a
// provider that learns its dimensions lazily (e.g. Voyage) would persist a
// zero-dim placeholder row. Subsequent calls with the real dim then read the
// 0 and trigger a destructive ResetDomain.
func TestValidateOrInitMetadata_SkipsZeroDimRow(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "lazy-dims.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Pre-embed call: provider hasn't learned dims yet.
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "voyage",
		Model:      "voyage-4-lite",
		Dimensions: 0,
	}))

	_, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.False(t, ok, "no row should be inserted while dimensions are unknown")

	// Provider has now learned dims via its first embedding response.
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "voyage",
		Model:      "voyage-4-lite",
		Dimensions: 1024,
	}))

	got, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1024, got.Dimensions)
}

func TestValidateExistingMetadataIsReadOnlyAndRejectsMismatch(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "existing.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	want := embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}
	require.NoError(t, store.ValidateOrInitMetadata(ctx, want))

	require.NoError(t, ValidateExistingMetadata(ctx, store.db, want))
	err = ValidateExistingMetadata(ctx, store.db, embeddings.IndexMetadata{Provider: "other", Model: "det", Dimensions: 4})
	require.ErrorContains(t, err, "provider mismatch")
	got, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "test", got.Provider)
}

func TestValidateExistingMetadataRejectsPartialMatchingMetadataWithZeroDimensionsWithoutMutation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "legacy-zero-readonly.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	want := embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}
	require.NoError(t, store.ValidateOrInitMetadata(ctx, want))
	_, err = store.db.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET dimensions = 0 WHERE id = 1`)
	require.NoError(t, err)

	err = ValidateExistingMetadata(ctx, store.db, want)
	require.ErrorContains(t, err, "metadata is partially populated")
	var provider, model string
	var dimensions int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT provider, model, dimensions FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&provider, &model, &dimensions))
	require.Equal(t, want.Provider, provider)
	require.Equal(t, want.Model, model)
	require.Zero(t, dimensions)
}

func TestValidateExistingMetadataRejectsFullyUnknownLegacyRowWithoutVectors(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "legacy-placeholder-empty-readonly.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	want := embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}
	require.NoError(t, store.ValidateOrInitMetadata(ctx, want))

	_, err = store.db.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET provider = '', model = '', dimensions = 0 WHERE id = 1`)
	require.NoError(t, err)
	err = ValidateExistingMetadata(ctx, store.db, want)
	require.ErrorContains(t, err, "provider mismatch")
	var provider, model string
	var dimensions int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT provider, model, dimensions FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&provider, &model, &dimensions))
	require.Empty(t, provider)
	require.Empty(t, model)
	require.Zero(t, dimensions)
	_, err = store.db.ExecContext(ctx, `DELETE FROM `+tableIndexMeta+` WHERE id = 1`)
	require.NoError(t, err)
	err = ValidateExistingMetadata(ctx, store.db, want)
	require.ErrorContains(t, err, "provider mismatch")
}

func TestValidateExistingMetadataAcceptsFullyUnknownLegacyRowWithCompatibleVectorsWithoutMutation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "legacy-placeholder-vectors-readonly.db"), 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	want := embeddings.IndexMetadata{Provider: "test", Model: "det", Dimensions: 4}
	require.NoError(t, store.ValidateOrInitMetadata(ctx, want))
	_, err = store.db.ExecContext(ctx, `
		CREATE TABLE intel_chunks (id INTEGER PRIMARY KEY) STRICT;
		CREATE TABLE intel_embeddings (
			chunk_row_id INTEGER PRIMARY KEY,
			chunk_id TEXT NOT NULL UNIQUE,
			norm REAL NOT NULL,
			dimensions INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		) STRICT;
		CREATE VIRTUAL TABLE intel_embeddings_vec_d4
		USING vec0(chunk_id integer primary key, embedding float[4] distance_metric=cosine);
		INSERT INTO intel_chunks (id) VALUES (1);
		INSERT INTO intel_embeddings (chunk_row_id, chunk_id, norm, dimensions, created_at)
		VALUES (1, 'legacy-note-section', 1.0, 4, 1);
		INSERT INTO intel_embeddings_vec_d4 (chunk_id, embedding)
		VALUES (1, X'0000803F000000000000000000000000');
	`)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET provider = '', model = '', dimensions = 0 WHERE id = 1`)
	require.NoError(t, err)
	require.NoError(t, ValidateExistingMetadata(ctx, store.db, want))
	var provider, model string
	var dimensions int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT provider, model, dimensions FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&provider, &model, &dimensions))
	require.Empty(t, provider)
	require.Empty(t, model)
	require.Zero(t, dimensions)

	_, err = store.db.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET provider = '', model = 'det', dimensions = 0 WHERE id = 1`)
	require.NoError(t, err)
	err = ValidateExistingMetadata(ctx, store.db, want)
	require.ErrorContains(t, err, "metadata is partially populated")
	_, err = store.db.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET provider = '', model = '', dimensions = 0 WHERE id = 1`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `DELETE FROM `+tableIndexMeta+` WHERE id = 1`)
	require.NoError(t, err)
	require.NoError(t, ValidateExistingMetadata(ctx, store.db, want))
}

func TestIncrementSyncGenerationCreatesLazyDimensionMetadataRow(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "lazy-sync-generation.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "voyage",
		Model:      "voyage-4-lite",
		Dimensions: 0,
	}))
	gen, err := store.IncrementSyncGeneration(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), gen)

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "voyage",
		Model:      "voyage-4-lite",
		Dimensions: 1024,
	}))
	got, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "voyage", got.Provider)
	require.Equal(t, "voyage-4-lite", got.Model)
	require.Equal(t, 1024, got.Dimensions)
	gotGen, err := store.SyncGeneration(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), gotGen)
}

// TestValidateOrInitMetadata_HealsLegacyZeroDimRow guards against the older
// fleet of databases that already wrote a 0-dim placeholder. The validator
// must heal the row in place rather than wiping the index.
func TestValidateOrInitMetadata_HealsLegacyZeroDimRow(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "legacy-zero.db"), 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Simulate a legacy 0-dim row written by an earlier buggy version.
	require.NoError(t, store.EnsureSchema(ctx))
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, last_sync)
		VALUES (1, 'voyage', 'voyage-4-lite', 0, ?, 1, 0)
	`, currentSchemaVersion)
	require.NoError(t, err)

	// Seed a note + chunk so we can detect a destructive reset.
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "n1",
		Title: "n1",
		Path:  "n1.md",
	}))
	require.NoError(t, store.UpsertNoteChunks(ctx, "n1", []embeddings.ChunkInput{
		embeddings.NewChunkInput(0, "chunk one", "n1", "h1"),
	}, []embeddings.Embedding{{1, 0, 0, 0}}))

	// Validate with known dims — should heal the row, not reset.
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "voyage",
		Model:      "voyage-4-lite",
		Dimensions: 4,
	}))

	got, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 4, got.Dimensions)

	var chunkCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableChunkEmbeddings).Scan(&chunkCount))
	require.Equal(t, 1, chunkCount, "validator should not wipe stored chunks when healing dims=0")
}
