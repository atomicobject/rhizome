package noderead

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopeInventoryReadsUseMaterializedFlags(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
}
`, `---
type: ProductSpec
---
# Product
`)
	counting := &countingStore{Store: store}
	scope := NewService(vaultDef, &obsidian.Note{}, counting, schema).NewScope(ctx, ScopeOptions{})

	all, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: ontology.TypeScopeAll})
	require.NoError(t, err)
	require.Equal(t, 1, all.Count)
	require.Equal(t, 1, all.IssueCount)
	require.True(t, all.Items[0].HasIssues)
	product, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "ProductSpec"})
	require.NoError(t, err)
	require.Equal(t, 1, product.Count)
	require.Equal(t, 1, product.IssueCount)
	require.Len(t, product.Items, 1)
	require.True(t, product.Items[0].HasIssues)

	require.Equal(t, 1, counting.ontologyAssessmentFlags)
	require.Zero(t, counting.ontologyAssessmentsByPaths, "inventory reads must not decode assessment JSON")

	issues, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: ontology.TypeScopeIssues})
	require.NoError(t, err)
	require.Equal(t, 1, issues.Count)
	require.Equal(t, 1, issues.IssueCount)
	require.Len(t, issues.Items, 1)
	require.True(t, issues.Items[0].HasIssues)
	require.Equal(t, "specs/product.md", issues.Items[0].NotePath)
	require.Zero(t, counting.ontologyAssessmentsByPaths)

	flags, err := scope.AssessmentFlags(ctx)
	require.NoError(t, err)
	require.True(t, flags["specs/product.md"].HasIssues)
	require.False(t, flags["specs/product.md"].TypeAmbiguous)
	require.Equal(t, 1, counting.ontologyAssessmentFlags, "flags load once per scope")

	decoded, err := scope.AssessmentsByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	require.NotNil(t, decoded["specs/product.md"])
	require.Equal(t, 1, counting.ontologyAssessmentsByPaths, "detail reads still load decoded assessments")
}

// TestScopeInventoryFallsBackToDecodeUntilMaterializationIsCurrent pins the
// upgrade window: a v64 index whose assessment rows still carry the migration's
// default flags (materialization version behind the binary) must report
// issues and ambiguity from assessment_json until the rebuild lands, and must
// return to the column path afterwards.
func TestScopeInventoryFallsBackToDecodeUntilMaterializationIsCurrent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, _ := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
}

type Alpha @node(paths: ["shared/*.md"]) {
  name: String
}

type Beta @node(paths: ["shared/*.md"]) {
  name: String
}
`, `---
type: ProductSpec
---
# Product
`)
	writeFixtureNote(t, vaultDef.Path, "shared/ambiguous.md", "---\nname: Ambiguous\n---\n")
	_, err := ontology.EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	schema, err := ontology.LoadSchema(vaultDef.Path)
	require.NoError(t, err)

	paths := []string{"specs/product.md", "shared/ambiguous.md"}
	rows, err := store.OntologyAssessmentsByPaths(ctx, paths)
	require.NoError(t, err)
	require.True(t, rows["specs/product.md"].HasIssues)
	require.True(t, rows["shared/ambiguous.md"].TypeAmbiguous)
	require.True(t, rows["shared/ambiguous.md"].HasIssues, "ambiguity is itself reported as an issue")

	// Shape the index the way the v63→v64 migration leaves it: columns at
	// their DEFAULT 0 and ontology_schema_state still on the previous
	// materialization version, rows and JSON otherwise untouched.
	db, err := sql.Open("sqlite3", sqliteutil.DSN(filepath.Join(vaultDef.Path, ".rhizome", "db.sqlite")))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, `UPDATE ontology_note_assessments SET has_issues = 0, type_ambiguous = 0`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE ontology_schema_state SET materialization_version = ?`, ontology.OntologyMaterializationVersion-1)
	require.NoError(t, err)

	counting := &countingStore{Store: store}
	scope := NewService(vaultDef, &obsidian.Note{}, counting, schema).NewScope(ctx, ScopeOptions{})
	flags, err := scope.AssessmentFlags(ctx)
	require.NoError(t, err)
	require.True(t, flags["specs/product.md"].HasIssues, "stale flags must be derived from assessment_json")
	require.False(t, flags["specs/product.md"].TypeAmbiguous)
	require.True(t, flags["shared/ambiguous.md"].TypeAmbiguous, "stale flags must be derived from assessment_json")
	require.Equal(t, 1, counting.ontologyAssessmentsByPaths, "the fallback decodes the vault once per scope")
	require.Zero(t, counting.ontologyAssessmentFlags, "untrusted columns are never read")

	all, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: ontology.TypeScopeAll})
	require.NoError(t, err)
	require.Equal(t, 2, all.Count)
	require.Equal(t, 2, all.IssueCount)
	require.Equal(t, 1, counting.ontologyAssessmentsByPaths)

	// The materialization bump rebuilds the rows; the column path takes over.
	result, err := ontology.EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	counting = &countingStore{Store: store}
	scope = NewService(vaultDef, &obsidian.Note{}, counting, schema).NewScope(ctx, ScopeOptions{})
	flags, err = scope.AssessmentFlags(ctx)
	require.NoError(t, err)
	require.True(t, flags["specs/product.md"].HasIssues)
	require.True(t, flags["shared/ambiguous.md"].TypeAmbiguous)
	require.Equal(t, 1, counting.ontologyAssessmentFlags)
	require.Zero(t, counting.ontologyAssessmentsByPaths, "a current materialization never decodes on the inventory path")
}
