package ontology_test

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/query"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRecordLogicalEvidenceHasIncomingQueryEdge(t *testing.T) {
	root := testkit.Vault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `type Host @node(default:true){status:String}
interface Evidence {aoId:String}
type Assertion implements Evidence @node(matches:["assertions AND product:alpha"]){aoId:String @field(source:"id")}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model.yaml"],select:"rows",key:{field:"id"},title:["id"],space:"alpha",logicalKind:"row"){
 evidence:[Evidence!] @field(path:"supported_by") @references(to:["Evidence"],targetKey:"aoId",mode:STRICT,values:ELEMENTS,edge:{name:"evidenced-by"})
}`,
		"model.yaml":               "rows: [{id: M, supported_by: [a-000000000001]}]\n",
		"assertions/selected.md":   "---\nid: a-000000000001\nproduct: alpha\n---\n# Selected\n",
		"assertions/unselected.md": "---\nid: a-000000000001\nproduct: beta\n---\n# Unselected\n",
	})
	store, err := semdb.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"**/*.md", "*.yaml"}}
	indexer := testkit.NoteMetadataIndexer(t)
	_, err = ontologytest.Publish(t.Context(), indexer, vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	executable, err := query.BuildExecutableSchema(schema)
	require.NoError(t, err)
	formats, err := indexer.FormatRuntime()
	require.NoError(t, err)
	prepared, errs := query.Prepare(executable, `{node(ref:"assertions/selected.md"){neighborhood(direction:INBOUND){edges{relation direction target{notePath kind nodeId}}}}}`)
	require.Empty(t, errs)
	result := query.Execute(t.Context(), query.Deps{VaultDef: vault, NoteReader: &obsidian.Note{}, NoteFormats: formats, Store: store}, schema, prepared)
	require.Empty(t, result.Errors)
	edges := result.Data["node"].(map[string]any)["neighborhood"].(map[string]any)["edges"].([]any)
	require.Len(t, edges, 1)
	edge := edges[0].(map[string]any)
	require.Equal(t, "evidenced-by", edge["relation"])
	require.Equal(t, "INBOUND", edge["direction"])
	target := edge["target"].(map[string]any)
	require.Equal(t, "model.yaml", target["notePath"])
	require.Equal(t, "EMBEDDED", target["kind"])
	require.NotEmpty(t, target["nodeId"])
}
