package search

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestExplicitSeedPathsFromHandlesPreservesOrderAndDeduplicatesPaths(t *testing.T) {
	got := ExplicitSeedPathsFromHandles([]knowledge.Handle{
		knowledge.FileHandle("pkg/app/mcp/semantic_query_unified.go"),
		knowledge.NoteHandle("docs/hubs/Search (Hub).md"),
		knowledge.FileHandle("pkg/app/mcp/semantic_query_unified.go"),
		knowledge.NodeChunkHandle("search", "docs/hubs/Search (Hub).md", "node_body", 0),
		knowledge.NoteHandle("notes/Decision.MD"),
	})

	require.Equal(t, []string{
		"pkg/app/mcp/semantic_query_unified.go",
		"docs/hubs/Search (Hub).md",
		"notes/Decision.MD",
	}, got)
}

func TestExplicitSeedPathKind_PreservesMixedCaseMarkdownCompatibility(t *testing.T) {
	spec := QuerySpec{PathKinds: map[string]PathKind{
		"notes/Decision.mD": PathKindNote,
		"pkg/Decision.mD":   PathKindCode,
	}}
	require.Equal(t, PathKindNote, ExplicitSeedPathKind(spec, "notes/Decision.mD"))
	require.Equal(t, PathKindCode, ExplicitSeedPathKind(spec, "pkg/Decision.mD"))
	require.Equal(t, PathKindNote, ExplicitSeedPathKind(QuerySpec{}, "notes/Decision.MD"))
}
