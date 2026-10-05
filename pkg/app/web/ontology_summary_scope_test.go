package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func newSummaryParityFixture(t *testing.T) *Server {
	t.Helper()
	fixture := prepareOntologyFixtureVault(t)
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.root, "specs", "200-broken"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "specs", "200-broken", "spec.md"), []byte(`---
summary: Broken spec summary
---
# Broken Spec

No requirements heading here.
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "loose.md"), []byte("# Loose note\n\nUntyped body.\n"), 0o644))
	_, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	return newFixtureServer(t, fixture, &Runtime{IntelStore: fixture.intelStore})
}

func TestOntologySummary_ReadsInventoryThroughScope(t *testing.T) {
	t.Parallel()
	srv := newSummaryParityFixture(t)

	summary, err := srv.ontologySummary(context.Background())
	require.NoError(t, err)

	require.True(t, summary.SchemaPresent)
	require.Equal(t, 7, summary.TotalNotes)
	require.Equal(t, 6, summary.TypedNotes)
	require.Equal(t, 1, summary.UntypedNotes)
	require.Equal(t, 0, summary.AmbiguousNotes)
	require.Equal(t, 5, summary.IssueNotes)

	byName := map[string]OntologyTypeSummary{}
	for _, item := range summary.Types {
		byName[item.Name] = item
	}
	require.Equal(t, 2, byName["Spec"].Count)
	require.Equal(t, "Specifications", byName["Spec"].PluralLabel)
	require.Equal(t, "Delivery", byName["Spec"].DisplayGroup)
	require.Equal(t, "Artifact", byName["Spec"].DisplayParent)
	require.Equal(t, 1, byName["Spec"].IssueCount)
	require.Equal(t, 1, byName["Plan"].Count)
	require.Equal(t, 1, byName["Plan"].IssueCount)
	require.Equal(t, []string{"specs/100-demo/plan.md"}, byName["Plan"].StartingNotes)

	require.Empty(t, summary.Interfaces)

	// The issues pseudo-type lists the same notes the summary counts.
	issues, err := srv.ontologyType(context.Background(), pseudoTypeIssues, nil)
	require.NoError(t, err)
	require.Equal(t, 5, issues.Count)
}

func TestOntologySummary_RetainsPresentationInterfaceParents(t *testing.T) {
	for _, implementorCount := range []int{0, 1} {
		t.Run(fmt.Sprintf("%d implementors", implementorCount), func(t *testing.T) {
			fixture := prepareInterfaceFixtureVault(t)
			implementation := ""
			if implementorCount == 1 {
				implementation = "implements Category"
			}
			schema := fmt.Sprintf(`
interface Category @display(singular: "Category", plural: "Categories") {
  summary: String!
}
interface Ordinary { summary: String! }
type ProcessSpec %s @node(paths: ["specs/proc.md"]) @display(parent: "Category") {
  summary: String!
}
type ProductSpec implements Ordinary @node(paths: ["specs/prod.md"]) {
  summary: String!
}
`, implementation)
			require.NoError(t, os.WriteFile(filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql"), []byte(schema), 0o644))
			_, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
			require.NoError(t, err)
			srv := newFixtureServer(t, fixture, nil)
			summary, err := srv.ontologySummary(context.Background())
			require.NoError(t, err)
			require.Len(t, summary.Interfaces, 1, "only the presentation parent bypasses the ordinary interface threshold")
			category := summary.Interfaces[0]
			require.Equal(t, "Category", category.Name)
			require.Equal(t, "Categories", category.PluralLabel)
			require.Equal(t, implementorCount, category.Count, "presentation membership must not alter semantic counts")
			require.Len(t, category.Implementors, implementorCount)
		})
	}
}

// TestOntologySummary_CorruptAssessmentRowStaysInventoryReadable pins the
// documented relaxation: inventory reads trust the materialized flag columns
// and never decode assessment JSON, so a corrupt row no longer fails the
// summary. Detail reads still decode and still fail.
func TestOntologySummary_CorruptAssessmentRowStaysInventoryReadable(t *testing.T) {
	t.Parallel()
	srv := newSummaryParityFixture(t)
	ctx := context.Background()

	store := srv.runtime.Intel()
	require.NotNil(t, store)
	_, err := store.DB().ExecContext(ctx, `UPDATE ontology_note_assessments SET assessment_json = '{' WHERE note_path = ?`, "specs/100-demo/spec.md")
	require.NoError(t, err)

	summary, err := srv.ontologySummary(ctx)
	require.NoError(t, err)
	require.Equal(t, 7, summary.TotalNotes)
	require.Equal(t, 5, summary.IssueNotes)

	_, err = srv.ontologyType(ctx, pseudoTypeAll, nil)
	require.Error(t, err)
}

// An ownership transition clears the published metadata state until the
// batch republishes it. A summary read in that window must keep the last
// published counts and say it is rebuilding, never report an empty vault.
func TestOntologySummary_ServesLastPublishedSnapshotWhileRebuilding(t *testing.T) {
	t.Parallel()
	srv := newSummaryParityFixture(t)
	ctx := context.Background()

	published, err := srv.ontologySummary(ctx)
	require.NoError(t, err)
	require.False(t, published.Rebuilding)
	require.Equal(t, 7, published.TotalNotes)

	_, err = srv.runtime.Intel().DB().ExecContext(ctx, `DELETE FROM note_metadata_state`)
	require.NoError(t, err)

	rebuilding, err := srv.ontologySummary(ctx)
	require.NoError(t, err)
	require.True(t, rebuilding.Rebuilding)
	require.Equal(t, published.TotalNotes, rebuilding.TotalNotes)
	require.Equal(t, published.TypedNotes, rebuilding.TypedNotes)
	require.Equal(t, published.IssueNotes, rebuilding.IssueNotes)
	require.Equal(t, published.Types, rebuilding.Types)
}

func TestOntologySummary_ReportsRebuildingBeforeFirstPublication(t *testing.T) {
	t.Parallel()
	srv := newSummaryParityFixture(t)
	ctx := context.Background()
	_, err := srv.runtime.Intel().DB().ExecContext(ctx, `DELETE FROM note_metadata_state`)
	require.NoError(t, err)

	summary, err := srv.ontologySummary(ctx)
	require.NoError(t, err)
	require.True(t, summary.Rebuilding)
	require.True(t, summary.SchemaPresent)
	require.Zero(t, summary.TotalNotes)
	require.Zero(t, summary.TypedNotes)
	require.Zero(t, summary.UntypedNotes)
	require.Zero(t, summary.AmbiguousNotes)
	require.Zero(t, summary.IssueNotes)
	require.Empty(t, summary.Types)
	require.Empty(t, summary.Interfaces)
}
