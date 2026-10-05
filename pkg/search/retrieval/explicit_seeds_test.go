package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestExplicitSeedRetriever_IncludesSiblingCodeWhenRequested(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "ontology", "query"), 0o755))
	for _, name := range []string{"execute.go", "prepare.go", "schema.go", "query_test.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "ontology", "query", name), []byte("package query\n"), 0o644))
	}

	r := &ExplicitSeedRetriever{VaultPath: root, Limit: 5, IncludeSiblingCode: true}
	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		ExplicitSeedPaths: []string{"pkg/ontology/query/execute.go"},
		HasExplicitSeeds:  true,
		PathKinds: map[string]search.PathKind{
			"pkg/ontology/query/execute.go":    search.PathKindCode,
			"pkg/ontology/query/prepare.go":    search.PathKindCode,
			"pkg/ontology/query/schema.go":     search.PathKindCode,
			"pkg/ontology/query/query_test.go": search.PathKindCode,
		},
	})
	require.NoError(t, err)

	paths := make([]string, 0, len(cands))
	for _, cand := range cands {
		paths = append(paths, filepath.ToSlash(cand.Path))
	}
	require.Contains(t, paths, "pkg/ontology/query/execute.go")
	require.Contains(t, paths, "pkg/ontology/query/prepare.go")
	require.Contains(t, paths, "pkg/ontology/query/schema.go")
	require.NotContains(t, paths, "pkg/ontology/query/query_test.go")
}

func TestExplicitSeedRetrieverMarksOnlySuppliedFileAsExact(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "service.go"), []byte("package pkg"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "helper.go"), []byte("package pkg"), 0o644))
	r := &ExplicitSeedRetriever{VaultPath: root, Limit: 5, IncludeSiblingCode: true}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{
		ExplicitSeedPaths: []string{"pkg/service.go"}, HasExplicitSeeds: true,
		PathKinds: map[string]search.PathKind{"pkg/service.go": search.PathKindCode, "pkg/helper.go": search.PathKindCode},
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.True(t, hasEvidenceType(results[0].Evidence, "path_exact"))
	require.False(t, hasEvidenceType(results[1].Evidence, "path_exact"))
}

func hasEvidenceType(evidence []search.Evidence, typ string) bool {
	for _, item := range evidence {
		if item.Type == typ {
			return true
		}
	}
	return false
}

func TestExplicitSeedRetriever_IncludesSiblingTestsWhenRequested(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "ontology", "query"), 0o755))
	for _, name := range []string{"execute.go", "query_test.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "ontology", "query", name), []byte("package query\n"), 0o644))
	}

	r := &ExplicitSeedRetriever{VaultPath: root, Limit: 5, IncludeSiblingTests: true}
	cands, err := r.Retrieve(context.Background(), search.QuerySpec{
		ExplicitSeedPaths: []string{"pkg/ontology/query/execute.go"},
		HasExplicitSeeds:  true,
		PathKinds: map[string]search.PathKind{
			"pkg/ontology/query/execute.go":    search.PathKindCode,
			"pkg/ontology/query/query_test.go": search.PathKindCode,
		},
	})
	require.NoError(t, err)

	paths := make([]string, 0, len(cands))
	for _, cand := range cands {
		paths = append(paths, filepath.ToSlash(cand.Path))
	}
	require.Contains(t, paths, "pkg/ontology/query/query_test.go")
}
