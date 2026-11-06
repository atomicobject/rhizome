package query

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit/structuralfixture"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRecordQueriesReadNativeContentWithoutMutationSupport(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Document @node(default: true) { status: String }
type Row @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["*.native"], select: "rows", key: {field: "id"}, title: ["name"], space: "test", logicalKind: "row") {
 name: String @field(path: "name")
 empty: String @field(path: "empty")
 refs: [Row!] @field(path: "refs") @references(to:["Row"],mode:STRICT,values:ELEMENTS,edge:{name:"uses"})
 children: [Child!] @contains(shape:RECORD, select:"children")
}
type Child @node(locator: EMBEDDED) @source(shape:RECORD,paths:["*.native"], select:"rows[].children",key:null,title:["name"],container:"Row",space:"test",logicalKind:"child") { name:String @field(path:"name") }`, nil)
	provider, err := structuralfixture.New(map[string]any{"rows": []any{map[string]any{"id": "A", "name": "First", "empty": "", "refs": []any{"B", "B"}, "children": []any{map[string]any{"name": "Child"}}}, map[string]any{"id": "B", "name": "Second"}}})
	require.NoError(t, err)
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	content := "# Native host, not a Markdown heading\n"
	writeQueryTestNote(t, env.root, "model.native", content)
	source, err := noteformat.NewAuthoredSource(paths.NotePath("model.native"), provider.Descriptor(), []byte(content), 1)
	require.NoError(t, err)
	projection, err := formats.Project(source)
	require.NoError(t, err)
	root := &ontology.RootDocumentSnapshot{NotePath: "model.native", Format: "synthetic", RawSource: []byte(content), ContentHash: source.ContentHash(), Projection: projection, Root: ontology.NodeRef{NotePath: "model.native", Kind: ontology.NodeKindNote}}
	prepared, err := ontology.PrepareDocument(root, env.schema)
	require.NoError(t, err)
	rootNode, err := prepared.Project(root.Root)
	require.NoError(t, err)
	model, err := ontology.BuildIntelOntologyNodeReadModel(env.schema, rootNode, 1)
	require.NoError(t, err)
	require.NoError(t, env.store.ReplaceOntologyNodeReadModel(t.Context(), model))
	require.NoError(t, env.store.ReplaceNoteMetadataSnapshot(t.Context(), semdb.NoteMetadataSnapshot{State: semdb.NoteMetadataState{Ready: true, LoadedAt: 1}, Notes: []semdb.NoteMetadataRow{{Path: "model.native", Title: "Native host", FormatID: "synthetic", ContentHash: source.ContentHash(), Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent, ProviderVersion: "test-v1", ProjectionVersion: "test-v1", SourceContentHash: source.ContentHash()}}}}))
	reader := &structuralfixture.Reader{Note: &obsidian.Note{}, Paths: []string{"model.native"}}
	deps := env.deps(nil)
	deps.NoteFormats = formats
	deps.NoteReader = reader
	deps.ExactNoteMetadataRows = nil
	provider.Calls.Store(0)
	query, errs := Prepare(env.execSchema, `{ row { name empty } }`)
	require.Empty(t, errs)
	result := Execute(t.Context(), deps, env.schema, query)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{map[string]any{"name": "First", "empty": ""}, map[string]any{"name": "Second", "empty": nil}}, result.Data["row"])
	require.Zero(t, reader.Reads.Load())
	require.Zero(t, provider.Calls.Load())
	query, errs = Prepare(env.execSchema, `{ row { name content refs { name } children { name } workspace { source capabilities { canEdit canEditFields canNavigateChildren } fields { name values capability { writeOperation readOnlyReason } } bodies { title markdown source level blockId } structure { title level } } } }`)
	require.Empty(t, errs)
	result = Execute(t.Context(), deps, env.schema, query)
	require.Empty(t, result.Errors)
	require.EqualValues(t, 1, reader.Reads.Load())
	require.EqualValues(t, 1, provider.Calls.Load())
	rows := result.Data["row"].([]any)
	first := rows[0].(map[string]any)
	require.Equal(t, []any{map[string]any{"name": "Second"}, map[string]any{"name": "Second"}}, first["refs"])
	require.Equal(t, []any{map[string]any{"name": "Child"}}, first["children"])
	workspace := first["workspace"].(map[string]any)
	require.Equal(t, map[string]any{"canEdit": false, "canEditFields": false, "canNavigateChildren": true}, workspace["capabilities"])
	native := workspace["source"].(ontology.NodeSourceContent)
	require.Equal(t, "application/json", native.MediaType)
	require.Equal(t, "source_value", native.Representation)
	require.Equal(t, "/rows/0", native.Address)
	require.Equal(t, source.ContentHash(), native.SourceHash)
	require.JSONEq(t, `{"id":"A","name":"First","empty":"","refs":["B","B"],"children":[{"name":"Child"}]}`, first["content"].(string))
	for _, raw := range workspace["bodies"].([]any) {
		body := raw.(map[string]any)
		require.Empty(t, body["markdown"])
		require.Nil(t, body["level"])
		require.Nil(t, body["blockId"])
	}
	_, errs = Prepare(env.execSchema, `{ row { level } }`)
	require.NotEmpty(t, errs)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"providerVersion":"test-v1"`)
}
