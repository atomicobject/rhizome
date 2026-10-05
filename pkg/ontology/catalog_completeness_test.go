package ontology

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestCatalogCompletenessAcceptsIntentionalAbsence(t *testing.T) {
	for _, mode := range []string{"full", "published"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologySchema(t, root, `
type Project @node(paths: ["projects/*.md"]) { name: String! }
type First @node(matches: ["tag:overlap"]) { name: String }
type Second @node(matches: ["tag:overlap"]) { name: String }
`)
			writeOntologyNote(t, root, "notes/blank.md", "")
			writeOntologyNote(t, root, "notes/space.md", " \n\t")
			writeOntologyNote(t, root, "notes/ambiguous.md", "---\ntags: [overlap]\n---\n")
			writeOntologyNote(t, root, "projects/normal.md", "---\nname: Normal\n---\n")
			store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			indexer := testNoteMetadataIndexer(t)
			vault := obsidian.VaultDefinition{Path: root}
			_, err = indexer.EnsureIndexed(ctx, vault, &obsidian.Note{}, store)
			require.NoError(t, err)
			sync := func(paths ...string) bool {
				if len(paths) > 0 {
					require.NoError(t, indexer.SyncPaths(ctx, vault, &obsidian.Note{}, store, paths, nil))
				}
				if mode == "full" {
					result, err := EnsureIndexed(ctx, indexer, vault, &obsidian.Note{}, store)
					require.NoError(t, err)
					return result.Dirty
				}
				result, err := SyncPublishedPaths(ctx, indexer, vault, &obsidian.Note{}, store, nil, paths, nil)
				require.NoError(t, err)
				return result.Dirty
			}
			require.True(t, sync())
			runtime, err := PublishedRuntimeWithStore(ctx, indexer, vault, store)
			require.NoError(t, err)
			require.True(t, runtime.Ready, "a completed projection with blank and ambiguous sources must be ready")
			require.False(t, sync(), "unchanged sources must not rebuild")

			schemaHash := runtime.Schema.Hash
			allPaths := []string{"notes/blank.md", "notes/space.md", "notes/ambiguous.md", "projects/normal.md"}
			assertCounts := func() {
				metadata, err := indexer.UsableNoteMetadataRowsByPaths(ctx, store, allPaths)
				require.NoError(t, err)
				published, err := PublishedCatalogPaths(ctx, store, metadata, schemaHash)
				require.NoError(t, err)
				rows, err := store.OntologyAssessmentsByPaths(ctx, allPaths)
				require.NoError(t, err)
				nodes, err := store.OntologyNodesByPaths(ctx, allPaths)
				require.NoError(t, err)
				catalog := catalogNodeWitnesses(nodes)
				for _, path := range allPaths {
					assessment, err := AssessmentFromJSON(rows[path].AssessmentJSON)
					require.NoError(t, err)
					require.Equal(t, metadata[path].ContentHash, assessment.SourceContentHash, path)
					require.True(t, published[path], path)
					require.NotNil(t, assessment.CatalogNodeCount, path)
					witness := catalogWitnessForPath(catalog, path)
					require.Equal(t, witness.Count, *assessment.CatalogNodeCount, path)
					require.Equal(t, witness.Digest, assessment.CatalogNodeDigest, path)
				}
				require.Positive(t, catalog["notes/blank.md"].Count)
				require.Positive(t, catalog["notes/space.md"].Count)
				require.Zero(t, catalog["notes/ambiguous.md"].Count)
			}
			assertCounts()

			// A normal source losing its required catalog rows must still be repaired.
			require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{NotePaths: []string{"projects/normal.md"}}))
			runtime, err = PublishedRuntimeWithStore(ctx, indexer, vault, store)
			require.NoError(t, err)
			require.False(t, runtime.Ready)
			writeOntologyNote(t, root, "notes/blank.md", " ")
			require.True(t, sync("notes/blank.md"), "an unrelated edit must repair missing normal rows")
			require.False(t, sync())
			assertCounts()

			// Both transitions must replace the old catalog, including explicit emptiness.
			for _, content := range []string{"Plain note\n\n## Detail\nBody\n", "---\ntags: [overlap]\n---\n"} {
				writeOntologyNote(t, root, "notes/ambiguous.md", content)
				require.True(t, sync("notes/ambiguous.md"))
				nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/ambiguous.md"})
				require.NoError(t, err)
				if content[0] == 'P' {
					require.Greater(t, len(nodes), 1)
					// Partial loss must also fail when the note still has a root.
					require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
						NotePaths: []string{"notes/ambiguous.md"}, Nodes: nodes[:1],
					}))
					writeOntologyNote(t, root, "notes/blank.md", "  ")
					require.True(t, sync("notes/blank.md"))
					repaired, err := store.OntologyNodesByPaths(ctx, []string{"notes/ambiguous.md"})
					require.NoError(t, err)
					require.Len(t, repaired, len(nodes))
					// Equal cardinality cannot conceal substituted node identity.
					ids := make([]string, len(repaired))
					for i, node := range repaired {
						ids[i] = node.NodeID
					}
					fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, ids, nil)
					require.NoError(t, err)
					repaired[0].StructuralFingerprint = "substituted"
					require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
						NotePaths: []string{"notes/ambiguous.md"}, Nodes: repaired, FieldValues: fields,
					}))
					runtime, err := PublishedRuntimeWithStore(ctx, indexer, vault, store)
					require.NoError(t, err)
					require.False(t, runtime.Ready)
					writeOntologyNote(t, root, "notes/blank.md", "   ")
					require.True(t, sync("notes/blank.md"))
					restored, err := store.OntologyNodesByPaths(ctx, []string{"notes/ambiguous.md"})
					require.NoError(t, err)
					require.Equal(t, catalogNodeWitnesses(nodes), catalogNodeWitnesses(restored))
				} else {
					require.Empty(t, nodes)
				}
				require.False(t, sync())
			}
			assertCounts()

			// Missing evidence is not permission to accept an empty projection.
			rows, err := store.OntologyAssessmentsByPaths(ctx, []string{"notes/ambiguous.md"})
			require.NoError(t, err)
			row := rows["notes/ambiguous.md"]
			assessment, err := AssessmentFromJSON(row.AssessmentJSON)
			require.NoError(t, err)
			assessment.CatalogNodeCount = nil
			setAssessmentRow(&row, *assessment)
			require.NoError(t, store.UpsertOntologyAssessments(ctx, []semdb.OntologyNoteAssessmentRow{row}))
			writeOntologyNote(t, root, "projects/normal.md", "---\nname: Changed\n---\n")
			require.True(t, sync("projects/normal.md"), "an unrelated edit must also repair missing evidence")
			require.False(t, sync())
			assertCounts()
		})
	}
}
