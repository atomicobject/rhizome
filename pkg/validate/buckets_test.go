package validate

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"github.com/stretchr/testify/require"
)

func TestBuildMigrationBucketsGroupsByIssueShape(t *testing.T) {
	buckets := BuildMigrationBuckets([]Issue{
		{Code: "missing_required_field", Path: "docs/a.md", Type: "Spec", Field: "id"},
		{Code: "missing_required_field", Path: "docs/b.md", Type: "Spec", Field: "id"},
		{Code: "wrong_target_type", Path: "docs/c.md", Type: "Spec", Field: "owner", Target: "people/alice.md"},
		{Code: "query_compile_error", Path: ".rhizome/query-recipes/spec.yaml", Target: "stale-recipe"},
		{Code: "query_compile_error", Path: "docs/query-recipes/other.yaml", Target: "other-stale-recipe"},
		{Code: "field_type_mismatch", Path: "docs/ignored.md", Type: "Spec", Field: "status"},
	})

	require.Len(t, buckets, 3)
	require.Equal(t, "missing_required_field", buckets[0].Code)
	require.Equal(t, 2, buckets[0].IssueCount)
	require.Equal(t, "Spec", buckets[0].Type)
	require.Equal(t, "id", buckets[0].Field)
	require.Equal(t, FixSafetyAgent, buckets[0].Safety)
	require.Equal(t, []string{"docs/a.md", "docs/b.md"}, buckets[0].Paths)

	require.Equal(t, "query_compile_error", buckets[1].Code)
	require.Equal(t, 2, buckets[1].IssueCount)
	require.Equal(t, []string{".rhizome/query-recipes/spec.yaml", "docs/query-recipes/other.yaml"}, buckets[1].Paths)
	require.Equal(t, []string{"other-stale-recipe", "stale-recipe"}, buckets[1].Targets)

	require.Equal(t, "wrong_target_type", buckets[2].Code)
	require.Equal(t, "owner", buckets[2].Field)
}

func TestRunCheckBuildsMigrationBucketsFromFullIssueSetBeforeTruncation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["docs/*.md"]) {
  summary: String!
}
`,
		"docs/a.md": "---\ntype: Spec\n---\n# A\n",
		"docs/b.md": "---\ntype: Spec\n---\n# B\n",
	}))
	runCtx := RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
	}

	result := RunCheck(context.Background(), runCtx, Options{MaxIssues: 1}, CheckOntology)

	require.Empty(t, result.Error)
	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Issues, 1, "display is truncated to MaxIssues")
	require.Len(t, result.Buckets, 1)
	require.Equal(t, "missing_required_field", result.Buckets[0].Code)
	require.Equal(t, 2, result.Buckets[0].IssueCount, "buckets count every finding, not the displayed prefix")
	require.Equal(t, []string{"docs/a.md", "docs/b.md"}, result.Buckets[0].Paths)
}
