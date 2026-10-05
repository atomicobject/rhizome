package search

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeExplicitSeedPaths_PreservesCanonicalHandles(t *testing.T) {
	got := NormalizeExplicitSeedPaths("/tmp/vault", []string{"file:pkg/app/mcp/semantic_query_unified.go", "note:docs/hubs/Search (Hub).md"}, nil)

	require.Equal(t, []string{
		"pkg/app/mcp/semantic_query_unified.go",
		"docs/hubs/Search (Hub).md",
	}, got)
}

func TestNormalizeExplicitSeedPaths_DropsOutsideVaultInputs(t *testing.T) {
	vaultPath := filepath.Join(t.TempDir(), "vault")
	outsidePath := filepath.Join(t.TempDir(), "outside.go")

	got := NormalizeExplicitSeedPaths(vaultPath,
		[]string{
			"pkg/search",
			"../escape.go",
			outsidePath,
			"file:pkg/search/service.go",
		},
		[]string{
			"/tmp/also-outside",
			"docs/guide.md",
		},
	)

	require.Equal(t, []string{
		"pkg/search",
		"pkg/search/service.go",
		"docs/guide.md",
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
