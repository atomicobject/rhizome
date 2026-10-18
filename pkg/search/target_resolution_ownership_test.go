package search

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestResolvedAnchorPreservesSuppliedNoteOwnership(t *testing.T) {
	ownership := map[string]PathKind{"docs/widget.html": PathKindNote, "docs": PathKindNote}
	store := &stubTargetResolverStore{bySymbol: map[string][]codeanchor.IntelAnchor{
		"Widget": {{AnchorID: "widget", Path: "docs/widget.html", Symbol: "Widget", FQN: "pkg.Widget"}},
	}}
	spec, warnings := ResolveQuerySpecTargets(context.Background(), "", store, QuerySpec{
		Text: "Widget", Intent: IntentGoToDef, PathKinds: ownership,
	})
	require.Empty(t, warnings)
	require.Equal(t, PathKindNote, ExplicitSeedPathKind(spec, "docs/widget.html"))
	var seeds []string
	for _, seed := range spec.Seeds {
		seeds = append(seeds, seed.String())
	}
	require.Contains(t, seeds, "note:docs/widget.html")
	require.Contains(t, seeds, "anchor:widget")
	require.Equal(t, map[string]PathKind{"docs/widget.html": PathKindNote, "docs": PathKindNote}, ownership)
	require.Zero(t, store.notePathCalls+store.filePathCalls)
}
