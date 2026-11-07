package noderead

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit/structuralfixture"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRecordRichReadsRestoreIndexedCompositionWithoutOtherSourceReads(t *testing.T) {
	vault, store, schema := buildFixture(t, `type Host @node(default:true){ status:String }
 type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"rows",key:null,title:["$key"],scalarAs:"id",space:"test",logicalKind:"row"){ id:String @field(path:"id") }`, "# Host\n")
	accountProvider, err := structuralfixture.New(map[string]any{"rows": []any{"account"}})
	require.NoError(t, err)
	productProvider, err := structuralfixture.New(map[string]any{"rows": []any{"product"}})
	require.NoError(t, err)
	prepare := func(path, content string, provider *structuralfixture.Provider) (*ontology.PreparedDocument, noteformat.AuthoredSource) {
		source, err := noteformat.NewAuthoredSource(paths.NotePath(path), provider.Descriptor(), []byte(content), 1)
		require.NoError(t, err)
		projection, err := provider.Project(source)
		require.NoError(t, err)
		root := &ontology.RootDocumentSnapshot{NotePath: paths.NotePath(path), Format: "synthetic", RawSource: []byte(content), ContentHash: source.ContentHash(), Projection: projection, Root: ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote}}
		document, err := ontology.PrepareDocument(root, schema)
		require.NoError(t, err)
		return document, source
	}
	account, accountSource := prepare("account.native", "! account source !", accountProvider)
	product, productSource := prepare("product.native", "! product source !", productProvider)
	ontology.BindRecordDocuments(account, product)
	var refs []ontology.NodeRef
	for _, document := range []*ontology.PreparedDocument{account, product} {
		root, err := document.Project(document.Source().Root)
		require.NoError(t, err)
		model, err := ontology.BuildIntelOntologyNodeReadModel(schema, root, 1)
		require.NoError(t, err)
		require.NoError(t, store.ReplaceOntologyNodeReadModel(t.Context(), model))
		nodes, err := document.Nodes()
		require.NoError(t, err)
		for _, node := range nodes {
			if node.Record != nil {
				refs = append(refs, node.Ref)
			}
		}
	}
	var metadata []semdb.NoteMetadataRow
	for _, source := range []noteformat.AuthoredSource{accountSource, productSource} {
		metadata = append(metadata, semdb.NoteMetadataRow{Path: source.Path().String(), FormatID: "synthetic", ContentHash: source.ContentHash(), Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent, ProviderVersion: "test-v1", ProjectionVersion: "test-v1", SourceContentHash: source.ContentHash()}})
	}
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(t.Context(), semdb.NoteMetadataSnapshot{State: semdb.NoteMetadataState{Ready: true, LoadedAt: 1}, Notes: metadata}))
	// Only the requested product source exists. Reading the account file would fail.
	require.NoError(t, os.WriteFile(filepath.Join(vault.Path, "product.native"), productSource.Bytes(), 0o644))
	registry, err := noteformat.NewRegistry(productProvider)
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, productProvider)
	require.NoError(t, err)
	reader := &structuralfixture.Reader{Note: &obsidian.Note{}, Paths: []string{"product.native"}}
	productProvider.Calls.Store(0)
	scope := NewService(vault, reader, store, schema).WithNoteFormats(formats).NewScope(ScopeOptions{})
	summary, err := scope.Hydrate(t.Context(), refs, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, summary, 2)
	require.Equal(t, "2", summary[1].Title)
	require.Zero(t, reader.Reads.Load())
	projection, err := scope.Projection(t.Context(), refs[1])
	require.NoError(t, err)
	require.Equal(t, "2", projection.Record.LogicalKey)
	require.Equal(t, "2", projection.DisplayTitle())
	require.EqualValues(t, 1, reader.Reads.Load())
	require.EqualValues(t, 1, productProvider.Calls.Load())
	require.Len(t, scope.preparedByPath, 1)
}
