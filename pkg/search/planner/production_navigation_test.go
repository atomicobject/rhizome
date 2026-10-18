package planner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestProductionWiringNavigation(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer store.Close()
	titles := []string{"Innovation teams", "Innovation team plans", "Team innovation", "Project Larkspur", "Travel support procedures", "Travel expenses", "IT support", "Budget review", "Forecasts", "Hiring", "Operations", "Products", "Index one", "Index two", "Index three"}
	paths := make([]string, len(titles))
	var notes []semdb.NoteMetadataRow
	var reps []codeanchor.IntelDocReplace
	var chunks []codeanchor.IntelChunk
	var owners []string
	for i, title := range titles {
		path := fmt.Sprintf("notes/%02d.md", i)
		paths[i] = path
		body := "# " + title + "\n\nSource explanation for " + title + ".\n"
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(body), 0o600))
		hash := fmt.Sprintf("source-%d", i)
		notes = append(notes, semdb.NoteMetadataRow{Path: path, Title: title, ContentHash: hash, IndexedAt: 1, Projection: semdb.NoteProjectionState{ProviderVersion: "markdown-v1", ProjectionVersion: "root-v1", SourceContentHash: hash, Status: semdb.NoteProjectionStatusCurrent}})
		id := fmt.Sprintf("section-%d", i)
		owners = append(owners, id)
		reps = append(reps, codeanchor.IntelDocReplace{Path: path, Sections: []codeanchor.IntelDocSection{{SectionID: id, SourceNodeID: ontology.CanonicalSectionID(path, title, 0), Path: path, Title: title, Level: 1, Content: body, StartByte: 0, EndByte: int64(len(body)), Fingerprint: hash}}, FTSRows: []codeanchor.IntelFTSRow{{ItemType: "doc_section", ItemID: id, Path: path, Title: title, Body: body}}})
		chunks = append(chunks, codeanchor.IntelChunk{ChunkID: fmt.Sprintf("chunk-%d", i), OwnerID: id, OwnerType: "doc_section", Ord: 0, Granularity: "section", Heading: title, ContentHash: hash})
	}
	var edges []semdb.GraphDocEdgeRow
	for _, source := range paths[12:] {
		edges = append(edges, semdb.GraphDocEdgeRow{SrcPath: source, DstPath: paths[3], Kind: "wikilink", LinkText: semdb.AppendLinkText("", "catalog migration", "catalog migration")})
	}
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(t.Context(), semdb.NoteMetadataSnapshot{State: semdb.NoteMetadataState{NotesHash: "fixture", RawNotesHash: "fixture", LoadedAt: 1, Ready: true}, Notes: notes, WikilinkEdges: edges}))
	require.NoError(t, store.ReplaceIntelDocSectionsBatch(t.Context(), reps))
	require.NoError(t, store.ReplaceIntelChunks(t.Context(), owners, chunks))
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "navigation-fixture", Dimensions: 8})
	vectors, err := provider.EmbedTexts(t.Context(), titles)
	require.NoError(t, err)
	byChunk := map[string]embeddings.Embedding{}
	for i, chunk := range chunks {
		byChunk[chunk.ChunkID] = vectors[i]
	}
	require.NoError(t, store.UpsertEmbeddings(t.Context(), byChunk))
	require.NoError(t, store.ReplaceOntologySnapshot(t.Context(), semdb.OntologySnapshot{SchemaState: semdb.OntologySchemaState{SchemaHash: "fixture", NotesHash: "fixture", MaterializationVersion: ontology.OntologyMaterializationVersion, LoadedAt: 1, Ready: true}, NoteTypes: []semdb.OntologyNoteTypeRow{{NotePath: paths[3], TypeName: "Project", SchemaHash: "fixture", UpdatedAt: 1}}}))

	corpus, err := search.LoadCorpusSnapshot(t.Context(), store, search.CorpusMetadata)
	require.NoError(t, err)
	p := Planner{Deps: Deps{IntelStore: store, Corpus: corpus, VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{}, Semantic: &semantic.Searcher{IntelStore: store, NoteProvider: provider}}, Options: Options{EnableVector: true, EnableIntel: true, EnableRefs: true, MaxPerOwner: 3}}
	for _, tc := range []struct {
		name, query, want string
		filters           search.Filters
	}{
		{"exact title", "Innovation teams", paths[0], search.Filters{}},
		{"agreed link label", "catalog migration", paths[3], search.Filters{}},
		{"all short-query concepts", "travel support", paths[4], search.Filters{}},
		{"owning note type", "catalog migration", paths[3], search.Filters{Types: []string{"note"}, NoteTypes: []string{"Project"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := search.QuerySpec{Text: tc.query, Intent: search.IntentSearch, Limits: search.Limits{Total: 15}, Filters: tc.filters}
			plan, err := p.Plan(t.Context(), spec)
			require.NoError(t, err)
			results, err := plan.Search(t.Context(), spec)
			require.NoError(t, err)
			require.NotEmpty(t, results.Results)
			require.Equal(t, tc.want, results.Results[0].Path, "results: %+v", results.Results)
			if len(tc.filters.NoteTypes) > 0 {
				for _, result := range results.Results {
					require.Equal(t, tc.want, result.Path)
				}
			}
		})
	}
}
