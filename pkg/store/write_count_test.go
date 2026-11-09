package store

import (
	"context"
	"testing"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

// Each write path SPEC-0136 R4 names must advance the counter that the index
// generation reads, including a write that changes nothing.
func TestWriteCountAdvancesForEveryWritePath(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "write-count.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	cases := []struct {
		name  string
		write func() error
	}{
		{"index publish", func() error { return store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{}) }},
		{"watcher write", func() error { return store.UpsertNoteMeta(ctx, "notes/a.md", "h1", "v1", 1) }},
		{"write that changes nothing", func() error { return store.UpsertNoteMeta(ctx, "notes/a.md", "h1", "v1", 1) }},
		{"derived-lane completion", func() error {
			return store.AckDerivedWork(ctx, []anchors.DerivedWork{{Kind: "graph", Path: "notes/a.md"}})
		}},
		{"validation run", func() error { _, err := store.SetValidationRunning(ctx); return err }},
	}
	for _, tc := range cases {
		before := WriteCount()
		require.NoError(t, tc.write(), tc.name)
		require.Greater(t, WriteCount(), before, tc.name)
	}
}

func TestValidationGenerationReadsTheRunCounter(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "validation-generation.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	generation, err := store.ValidationGeneration(ctx)
	require.NoError(t, err)
	require.Zero(t, generation)
	started, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	generation, err = store.ValidationGeneration(ctx)
	require.NoError(t, err)
	require.Equal(t, started, generation)
}
