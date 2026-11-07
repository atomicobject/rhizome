package noderead

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/internal/testkit/structuralfixture"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/structured"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type countedNativeProjector struct {
	noteformat.Projector
	calls atomic.Int64
}

func (p *countedNativeProjector) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	p.calls.Add(1)
	return p.Projector.Project(source)
}

func TestStructuredRecordsShareOneCaptureParseAndResolver(t *testing.T) {
	for _, provider := range []noteformat.Projector{structured.NewYAML(), structured.NewJSON()} {
		t.Run(string(provider.Descriptor().ID), func(t *testing.T) {
			path := "model" + provider.Descriptor().Extensions[0]
			root := testkit.Vault(t, map[string]string{
				".rhizome/ontology/schema.graphql": strings.ReplaceAll(recordProviderSDL, "*.native", "*"+provider.Descriptor().Extensions[0]),
				path:                               `{"rows":[{"id":"A","name":"First","children":[{"name":"Child"}]},{"id":"B","name":"Second"}]}`,
			})
			schema, err := ontoschema.LoadSchema(root)
			require.NoError(t, err)
			store, err := semdb.Open(filepath.Join(t.TempDir(), "nodes.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			source := testkit.NoteSource(t, path, `{"rows":[{"id":"A","name":"First","children":[{"name":"Child"}]},{"id":"B","name":"Second"}]}`, 1)
			projection, err := ontology.ProjectNoteSourceSnapshot(source, schema)
			require.NoError(t, err)
			model, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
			require.NoError(t, err)
			require.NoError(t, store.ReplaceOntologyNodeReadModel(t.Context(), model))
			nodes, err := projection.Prepared.Nodes()
			require.NoError(t, err)
			var refs []ontology.NodeRef
			for _, node := range nodes {
				if node.Record != nil {
					refs = append(refs, node.Ref)
				}
			}
			require.Len(t, refs, 3)
			counted := &countedNativeProjector{Projector: provider}
			registry, err := noteformat.NewRegistry(counted)
			require.NoError(t, err)
			formats, err := noteformat.NewRuntime(registry, counted)
			require.NoError(t, err)
			vault := obsidian.VaultDefinition{Root: root, Includes: []string{path}}
			metadata, err := notemeta.NewIndexer(formats)
			require.NoError(t, err)
			_, err = metadata.EnsureIndexed(t.Context(), vault, &obsidian.Note{}, store)
			require.NoError(t, err)
			counted.calls.Store(0)
			reader := &structuralfixture.Reader{Note: &obsidian.Note{}, Paths: []string{path}}
			scope := NewService(vault, reader, store, schema).WithNoteFormats(formats).NewScope(ScopeOptions{})
			_, err = scope.Hydrate(t.Context(), refs, HydrateOptions{Profile: HydrateSummary})
			require.NoError(t, err)
			require.Zero(t, reader.Reads.Load())
			require.Zero(t, counted.calls.Load())
			_, err = scope.Hydrate(t.Context(), refs, HydrateOptions{Profile: HydrateContent})
			require.NoError(t, err)
			var prepared *ontology.PreparedDocument
			for _, ref := range refs {
				node, err := scope.Projection(t.Context(), ref)
				require.NoError(t, err)
				require.NotNil(t, node)
				if prepared == nil {
					prepared = node.Prepared
				} else {
					require.Same(t, prepared, node.Prepared)
				}
				require.Nil(t, node.Snapshot, "native reads never create a Markdown snapshot")
			}
			require.EqualValues(t, 1, reader.Reads.Load())
			require.EqualValues(t, 1, counted.calls.Load())
			require.Len(t, scope.preparedByPath, 1)
			t.Logf("records=3 captures=%d parses=%d prepared/resolvers=%d", reader.Reads.Load(), counted.calls.Load(), len(scope.preparedByPath))
		})
	}
}
