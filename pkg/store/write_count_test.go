package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
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
		before := WriteSequence()
		counterBefore, err := store.WriteGeneration(ctx)
		require.NoError(t, err)
		require.NoError(t, tc.write(), tc.name)
		after := WriteSequence()
		counterAfter, err := store.WriteGeneration(ctx)
		require.NoError(t, err)
		require.NotEqual(t, before, after, tc.name)
		require.NotEqual(t, counterBefore, counterAfter, tc.name)
		require.False(t, WritesOpen(after), tc.name)
	}
}

// The sequence shows a write as open from before its transaction runs until
// after it commits, so no reader can pair a committed write with the
// sequence value from before it (SPEC-0136 R4).
func TestWriteSequenceIsOpenUntilTheWriteHasCommitted(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "write-sequence.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	before := WriteSequence()
	require.False(t, WritesOpen(before))
	var inside uint64
	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error {
		inside = WriteSequence()
		_, err := tx.ExecContext(ctx, `UPDATE validation_state SET generation = generation`)
		return err
	}))
	after := WriteSequence()
	require.True(t, WritesOpen(inside), "a write is open while its transaction runs")
	require.NotEqual(t, before, inside)
	require.False(t, WritesOpen(after))
	require.NotEqual(t, inside, after)
	require.NotEqual(t, before, after)

	// A failed write still closes.
	require.Error(t, store.withWrite(ctx, func(context.Context, *sql.DB) error { return errors.New("boom") }))
	require.False(t, WritesOpen(WriteSequence()))
}

// The write counter moves for a store write from another handle, as from
// another process, and for a failed withWrite whose work may have committed,
// but not for a commit that bypasses the store, such as an embedding store's.
func TestWriteGenerationWitnessesStoreWritesOnly(t *testing.T) {
	ctx := context.Background()
	path := currentSchemaTestDBPath(t, "write-generation.db")
	store, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	read := func() int64 {
		value, err := store.WriteGeneration(ctx)
		require.NoError(t, err)
		return value
	}

	first := read()
	other, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = other.Close() }()
	_, err = other.SetValidationRunning(ctx)
	require.NoError(t, err)
	afterOther := read()
	require.NotEqual(t, first, afterOther, "another handle's write")

	require.Error(t, store.withWrite(ctx, func(context.Context, *sql.DB) error { return errors.New("boom") }))
	afterFailed := read()
	require.NotEqual(t, afterOther, afterFailed, "a failed withWrite")

	require.Error(t, store.withWriteTx(ctx, func(*sql.Tx) error { return errors.New("boom") }))
	require.Equal(t, afterFailed, read(), "a rolled-back withWriteTx")

	bypass, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{MaxOpenConns: 1})
	require.NoError(t, err)
	defer func() { _ = bypass.Close() }()
	_, err = bypass.ExecContext(ctx, `CREATE TABLE bypass (id INTEGER); INSERT INTO bypass VALUES (1)`)
	require.NoError(t, err)
	require.Equal(t, afterFailed, read(), "a commit outside the store")
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
