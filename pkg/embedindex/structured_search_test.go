package embedindex

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestStructuredRecordRawValuesAreSearchableWithCanonicalIdentity(t *testing.T) {
	root := testkit.Vault(t, map[string]string{".rhizome/ontology/schema.graphql": `type Host @node(default:true){status:String}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model.*"],select:"rows",key:{field:"id"},title:["name"],space:"test",logicalKind:"row"){id:ID @field(path:"id")}`})
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "search.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	provider := embeddings.NewDeterministicProvider(providerInfo)
	syncer := OntologyNodeSyncer{Schema: schema, Store: store, Provider: provider, ProviderInfo: providerInfo}
	searcher := semantic.Searcher{IntelStore: store, NoteProvider: provider}
	for _, path := range []string{"model.yaml", "model.json"} {
		t.Run(path, func(t *testing.T) {
			source := testkit.NoteSource(t, path, `{"rows":[{"id":"A","name":"Same title","unbound":{"evidence":"orchid telescope"}}]}`, 1)
			projection, err := ontology.ProjectNoteSourceSnapshot(source, schema)
			require.NoError(t, err)
			model, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
			require.NoError(t, err)
			require.NoError(t, store.ReplaceOntologyNodeReadModel(t.Context(), model))
			require.NoError(t, syncer.SyncProjections(t.Context(), projection))
			plan, err := syncer.chunkSetForProjection(projection, 1)
			require.NoError(t, err)
			var text string
			for _, row := range model.Nodes {
				if row.TypeName != "Row" {
					continue
				}
				for _, chunk := range plan.Chunks {
					if chunk.OwnerID == row.NodeID && strings.Contains(plan.Texts[chunk.ChunkID], "orchid telescope") {
						text = plan.Texts[chunk.ChunkID]
					}
				}
				require.NotEmpty(t, text, "raw unbound values belong to their record's semantic evidence")
				results, err := searcher.Search(t.Context(), semantic.SearchRequest{QueryText: text, K: 10, Filters: semantic.SearchFilters{Types: []string{"note"}, PathPrefixes: []string{path}}})
				require.NoError(t, err)
				require.NotEmpty(t, results)
				require.Equal(t, row.NodeID, results[0].NodeID)
				require.True(t, results[0].Record, "record chunks keep their own search owner")
				require.Equal(t, row.SourceLocator, ontology.NodeSourceLocator(*results[0].NodeRef))
				require.Contains(t, row.SourceLocator, "#record:v1:")
			}
		})
	}
}
