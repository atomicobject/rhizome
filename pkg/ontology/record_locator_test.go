package ontology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPositionalLocatorsDistinguishBindingsOfTheSameOwner(t *testing.T) {
	schema := recordFixtureSchema(t, `
type First @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"rows",key:null,title:["$key"],space:"test",logicalKind:"first") { id:String @field(path:"id") }
type Second @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"rows",key:null,title:["$key"],space:"test",logicalKind:"second") { id:String @field(path:"id") }
`)
	prepared, err := PrepareDocument(recordFixtureRoot(t, "model.native", map[string]any{"rows": []any{map[string]any{"id": "one"}}}), schema)
	require.NoError(t, err)
	nodes, err := prepared.Nodes()
	require.NoError(t, err)
	require.Len(t, nodes, 3)
	require.NotEqual(t, nodes[1].Ref.Fragment, nodes[2].Ref.Fragment)
	for _, node := range nodes[1:] {
		resolved, err := prepared.Project(NodeRef{NotePath: "model.native", Fragment: node.Ref.Fragment})
		require.NoError(t, err)
		require.Equal(t, node.Ref, resolved.Ref)
	}
}
