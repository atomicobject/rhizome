package store

import (
	"fmt"
	"testing"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestDerivedWorkPrioritizesNotesAndOntologyBeforeLoadLimit(t *testing.T) {
	s := newOwnershipTransitionStore(t)
	scopes := []anchors.DerivedScope{{Kind: anchors.DerivedGraph}}
	for i := range 40 {
		scopes = append(scopes, anchors.DerivedScope{Kind: anchors.DerivedCode, Path: fmt.Sprintf("src/%d.go", i)})
	}
	scopes = append(scopes, anchors.DerivedScope{Kind: anchors.DerivedNotes, Path: "note.md"}, anchors.DerivedScope{Kind: anchors.DerivedOntology, Path: "note.md"})
	work, err := s.MarkDerivedDirty(t.Context(), scopes)
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(t.Context(), work))
	ready, err := s.PendingDerivedWork(t.Context(), time.Now(), 2)
	require.NoError(t, err)
	require.Len(t, ready, 2)
	require.Equal(t, anchors.DerivedNotes, ready[0].Kind)
	require.Equal(t, anchors.DerivedOntology, ready[1].Kind)
}
