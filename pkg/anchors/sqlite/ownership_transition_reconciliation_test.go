package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyOwnershipTransitions_PersistsMonotonicReconciliationGeneration(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)

	first, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: "notes/first.html", Target: OwnershipTargetNote,
		Note: ownershipTransitionNoteSource("first"),
	}})
	require.NoError(t, err)
	require.EqualValues(t, 1, first.ReconciliationGeneration)

	second, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: "notes/second.html", Target: OwnershipTargetNote,
		Note: ownershipTransitionNoteSource("second"),
	}})
	require.NoError(t, err)
	require.EqualValues(t, 2, second.ReconciliationGeneration)

	generation, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.Equal(t, second.ReconciliationGeneration, generation)
	require.True(t, pending)
}

func TestApplyOwnershipTransitions_IdenticalNoteSourceDoesNotInvalidateOrAdvanceGeneration(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	state := ownershipTransitionNoteSource("unchanged")
	state.Status = NoteProjectionStatusStale
	state.Title = "Unchanged"
	state.Mtime = 12
	state.Size = 34
	state.ObservedAt = 7

	first, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: "notes/unchanged.html", Target: OwnershipTargetNote, Note: state,
	}})
	require.NoError(t, err)
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "current", RawNotesHash: "source", LoadedAt: 99, Ready: true},
		Notes: []NoteMetadataRow{{
			Path: "notes/unchanged.html", Title: "Projected title", ContentHash: state.ContentHash, Mtime: state.Mtime, Size: state.Size, FormatID: state.FormatID,
			Projection: NoteProjectionState{ProviderVersion: state.ProviderVersion, ProjectionVersion: state.ProjectionVersion, SourceContentHash: state.ContentHash, Status: NoteProjectionStatusCurrent, UpdatedAt: state.ObservedAt},
		}},
	}))
	before, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)

	reobserved := *state
	reobserved.Title = "Discovery title"
	reobserved.Mtime = 13
	reobserved.Size = 35
	reobserved.ObservedAt = 8
	result, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: "notes/unchanged.html", Target: OwnershipTargetNote, Note: &reobserved,
	}})
	require.NoError(t, err)
	require.Zero(t, result.ReconciliationGeneration)
	require.Empty(t, result.AffectedSourcePaths)
	require.Empty(t, result.AffectedAnchorIDs)
	after, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	generation, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.Equal(t, first.ReconciliationGeneration, generation)
	require.True(t, pending, "an existing reconciliation obligation must survive a source no-op")
}

func TestApplyOwnershipTransitions_IdenticalNoteSourceStillRetiresCoexistingCodeOwner(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	state := ownershipTransitionNoteSource("unchanged")
	state.Status = NoteProjectionStatusStale
	state.Title = "Unchanged"
	state.Mtime = 12
	state.Size = 34
	state.ObservedAt = 7
	first, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: "notes/unchanged.html", Target: OwnershipTargetNote, Note: state,
	}})
	require.NoError(t, err)
	require.NoError(t, store.db.QueryRowContext(ctx, `
		INSERT INTO files(path, lang, parse_status) VALUES (?, 'go', 'ok') RETURNING path
	`, "notes/unchanged.html").Scan(new(string)))

	reobserved := *state
	reobserved.ObservedAt = 8
	result, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: "notes/unchanged.html", Target: OwnershipTargetNote, Note: &reobserved,
	}})
	require.NoError(t, err)
	require.Greater(t, result.ReconciliationGeneration, first.ReconciliationGeneration)
	paths, err := store.IndexedFilePaths(ctx)
	require.NoError(t, err)
	require.Empty(t, paths)
}

func TestApplyOwnershipTransitions_ReconciliationMarkerFailureRollsBackTransition(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const path = "notes/reconciliation-rollback.md"
	seedOwnershipCodeAndNote(t, ctx, store, path)
	require.NoError(t, createOwnershipAbortTrigger(ctx, store, "before_reconciliation_generation_insert", `
		CREATE TRIGGER before_reconciliation_generation_insert
		BEFORE INSERT ON index_metadata
		WHEN NEW.key = 'ownership_reconciliation_generation'
		BEGIN SELECT RAISE(ABORT, 'injected reconciliation marker failure'); END;
	`))

	_, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: path, Target: OwnershipTargetNote,
		Note: ownershipTransitionNoteSource("reconciliation-replacement"),
	}})
	require.ErrorContains(t, err, "injected reconciliation marker failure")
	requireOwnershipCounts(t, ctx, store, path, seededOwnershipCounts())
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.True(t, state.Ready)
	require.Equal(t, "notes", state.NotesHash)

	generation, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.Zero(t, generation)
	require.False(t, pending)
}

func TestAcknowledgeOwnershipReconciliation_RequiresCurrentGeneration(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)

	first, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: "notes/ack-first.html", Target: OwnershipTargetNote,
		Note: ownershipTransitionNoteSource("ack-first"),
	}})
	require.NoError(t, err)
	acknowledged, err := store.AcknowledgeOwnershipReconciliation(ctx, first.ReconciliationGeneration)
	require.NoError(t, err)
	require.True(t, acknowledged)

	second, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{
		Path: "notes/ack-second.html", Target: OwnershipTargetNote,
		Note: ownershipTransitionNoteSource("ack-second"),
	}})
	require.NoError(t, err)
	acknowledged, err = store.AcknowledgeOwnershipReconciliation(ctx, first.ReconciliationGeneration)
	require.NoError(t, err)
	require.False(t, acknowledged)

	generation, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.Equal(t, second.ReconciliationGeneration, generation)
	require.True(t, pending)

	acknowledged, err = store.AcknowledgeOwnershipReconciliation(ctx, second.ReconciliationGeneration)
	require.NoError(t, err)
	require.True(t, acknowledged)
	generation, pending, err = store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.Equal(t, second.ReconciliationGeneration, generation)
	require.False(t, pending)
}

func ownershipTransitionNoteSource(hash string) *NoteSourceState {
	return &NoteSourceState{
		FormatID: "html", ContentHash: hash, ProviderVersion: "html-v1", ProjectionVersion: "root-v1",
		Status: NoteProjectionStatusCurrent, ObservedAt: 1,
	}
}
