package ontology

import (
	"testing"
	"time"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

func TestFallbackWorkspaceStructuralRefsRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, `type Reference @node(paths: ["references/*.md"]) { title: String @field }`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		content string
	}{
		{"headings", "## Findings\n\nFinding body.\n\n### Detail\n\nDetail body.\n"},
		{"single H1 wrapper", "# Research\n\nIntroduction.\n\n## Findings\n\nFinding body.\n\n### Detail\n\nDetail body.\n"},
		{"block IDs", "# Research\n\n## Findings\n\nFinding body.\n^findings\n\n### Detail\n\nDetail body.\n^detail\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := BuildDocumentSnapshot("notes/research.md", tc.content, time.Time{})
			require.NoError(t, err)
			projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindNote})
			require.NoError(t, err)
			fallback, ok := AsFallbackNoteProjection(projection)
			require.True(t, ok)
			sections := FallbackSectionProjections(fallback)
			require.Len(t, sections, 2)

			for i, title := range []string{"Findings", "Detail"} {
				t.Run(title, func(t *testing.T) {
					direct, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindSection, Fragment: title})
					require.NoError(t, err)
					directFallback, ok := AsFallbackSectionProjection(direct)
					require.True(t, ok)
					require.Equal(t, sections[i].Ref, directFallback.Ref, "heading reads and enumeration share one canonical fallback identity")
					for _, source := range []struct {
						name       string
						projection *NodeProjection
					}{{"enumerated", sections[i]}, {"heading", directFallback}} {
						t.Run(source.name, func(t *testing.T) {
							workspace := BuildNodeWorkspaceFromProjectionWithSchema(schema, source.projection)
							require.Equal(t, title, workspace.Node.Title)
							emitted := workspace.Node.Ref
							require.NotEmpty(t, emitted.Structural)
							// Reload has only the #struct selector, not the original node ID or heading.
							reloaded, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{
								NotePath:   emitted.NotePath,
								Kind:       NodeKindSection,
								Structural: emitted.Structural,
							})
							require.NoError(t, err)
							refreshed, ok := AsFallbackSectionProjection(reloaded)
							require.True(t, ok)
							current := BuildNodeWorkspaceFromProjectionWithSchema(schema, refreshed)
							require.Equal(t, title, current.Node.Title)
							require.Equal(t, emitted, current.Node.Ref)
						})
					}
				})
			}

			if tc.name == "single H1 wrapper" {
				// Captured from the former direct-open identity with the raw H1 parent.
				legacy, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{
					NotePath: snapshot.NotePath, Kind: NodeKindSection,
					Structural: "6cb1d0d83e2ec651c7fc49dfa0efce302bb1ebe6c4a3261ecc4961333cde0dcd",
				})
				require.NoError(t, err)
				canonical, ok := AsFallbackSectionProjection(legacy)
				require.True(t, ok)
				require.Equal(t, sections[0].Ref, canonical.Ref)
				require.Equal(t, "6d46e8f2a7813e75817c9bb449a164ef89b28595042bc6f8f6c2e4aac28864df", canonical.Ref.Structural)
			}
		})
	}
}

func TestTypedSectionStructuralRefsStillRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, `
type Reference @node(paths: ["references/*.md"]) {
  findings: Findings @contains(level: H2, heading: "Findings")
}
type Findings implements Section {
  detail: Detail @contains(level: H3, heading: "Detail")
}
type Detail implements Section {}
`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := BuildDocumentSnapshot("references/research.md", "# Research\n\n## Findings\n\nFinding body.\n\n### Detail\n\nDetail body.\n", time.Time{})
	require.NoError(t, err)
	for _, title := range []string{"Findings", "Detail"} {
		t.Run(title, func(t *testing.T) {
			projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindSection, Fragment: title})
			require.NoError(t, err)
			require.Equal(t, title, projection.ResolvedType)
			emitted := BuildNodeWorkspaceFromProjectionWithSchema(schema, projection).Node.Ref
			reloaded, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindSection, Structural: emitted.Structural})
			require.NoError(t, err)
			require.Equal(t, emitted, reloaded.Ref)
			require.Equal(t, title, reloaded.ResolvedType)
		})
	}
}
