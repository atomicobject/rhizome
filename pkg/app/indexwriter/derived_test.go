package indexwriter

import (
	"context"
	"errors"
	"testing"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestDerivedControlsFenceStructuralPayloadAndActivation(t *testing.T) {
	ctx := context.Background()
	var order []string
	q := New(ctx, Handlers{
		ApplyNoteIndexBatch: func(context.Context, []anchors.NoteIndexWork) error { order = append(order, "note"); return nil },
		MarkDerivedDirty: func(_ context.Context, scopes []anchors.DerivedScope) ([]anchors.DerivedWork, error) {
			order = append(order, "dirty")
			return []anchors.DerivedWork{{DerivedScope: scopes[0], Generation: 7}}, nil
		},
		ApplyStructuralFinalize: func(context.Context, StructuralFinalize) error {
			order = append(order, "structural-finalize")
			return nil
		},
		ActivateDerivedWork: func(_ context.Context, work []anchors.DerivedWork) error {
			order = append(order, "activate")
			require.EqualValues(t, 7, work[0].Generation)
			return nil
		},
	})
	t.Cleanup(func() { _ = q.Close() })
	require.NoError(t, q.SubmitNoteIndexWork(ctx, anchors.NoteIndexWork{}))
	tickets, err := q.SubmitDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedNotes, Path: "note.md"}})
	require.NoError(t, err)
	require.NoError(t, q.SubmitNoteIndexWork(ctx, anchors.NoteIndexWork{}))
	require.NoError(t, q.SubmitStructuralFinalize(ctx, StructuralFinalize{}))
	require.NoError(t, q.SubmitActivateDerived(ctx, tickets))
	require.Equal(t, []string{"note", "dirty", "note", "structural-finalize", "activate"}, order)
}
func TestDerivedDirtyFailureStopsFollowingStructuralPayload(t *testing.T) {
	ctx := context.Background()
	failed := errors.New("dirty publication failed")
	q := New(ctx, Handlers{MarkDerivedDirty: func(context.Context, []anchors.DerivedScope) ([]anchors.DerivedWork, error) { return nil, failed }})
	t.Cleanup(func() { _ = q.StopAndWait() })
	_, err := q.SubmitDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedGraph}})
	require.ErrorIs(t, err, failed)
	require.ErrorIs(t, q.SubmitNoteIndexWork(ctx, anchors.NoteIndexWork{}), failed)
}
