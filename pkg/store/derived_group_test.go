package store

import (
	"fmt"
	"testing"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestDerivedGroupLoadsWithinKindAndAcknowledgesOnlyMatchingRows(t *testing.T) {
	s := newOwnershipTransitionStore(t)
	var scopes []anchors.DerivedScope
	for _, kind := range []anchors.DerivedKind{anchors.DerivedNotes, anchors.DerivedOntology, anchors.DerivedCode} {
		for i := range 70 {
			scopes = append(scopes, anchors.DerivedScope{Kind: kind, Path: fmt.Sprintf("notes/%02d.md", i)})
		}
	}
	work, err := s.MarkDerivedDirty(t.Context(), scopes)
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(t.Context(), work))
	group, err := s.PendingDerivedWorkForKind(t.Context(), anchors.DerivedOntology, time.Now(), 64)
	require.NoError(t, err)
	require.Len(t, group, 64)
	replacement, err := s.MarkDerivedDirty(t.Context(), []anchors.DerivedScope{group[0].DerivedScope})
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(t.Context(), replacement))
	matched, err := s.AckDerivedWorkMatched(t.Context(), group)
	require.NoError(t, err)
	require.Len(t, matched, 63)
	remaining, err := s.PendingDerivedWorkForKind(t.Context(), anchors.DerivedOntology, time.Now(), 64)
	require.NoError(t, err)
	require.Len(t, remaining, 7)
	matched, err = s.AckDerivedWorkMatched(t.Context(), group)
	require.NoError(t, err)
	require.Empty(t, matched)
}

func TestDerivedGroupWitnessPreservesPathNormalization(t *testing.T) {
	s := newOwnershipTransitionStore(t)
	_, err := s.db.ExecContext(t.Context(), `INSERT INTO notes(path,title,content_hash,mtime,size,indexed_at) VALUES ('notes/existing.md','Existing','hash',1,1,1)`)
	require.NoError(t, err)
	fingerprints, err := s.DerivedSourceFingerprints(t.Context(), anchors.DerivedNotes, []string{"notes/existing.md", "notes/./existing.md"})
	require.NoError(t, err)
	require.Equal(t, fingerprints["notes/existing.md"], fingerprints["notes/./existing.md"])
}
