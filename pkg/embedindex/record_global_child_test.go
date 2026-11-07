package embedindex

import (
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

func TestGlobalRecordChildGetsOwnChunkWithoutContainsField(t *testing.T) {
	root := testkit.Vault(t, map[string]string{".rhizome/ontology/schema.graphql": `type Host @node(default:true){status:String}
type Parent @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"parents",key:{field:"id"},title:["id"],space:"test",logicalKind:"parent"){id:ID @field(path:"id")}
type Child @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"parents[].children",key:{field:"id"},title:["id"],container:"Parent",space:"test",logicalKind:"child"){id:ID @field(path:"id")}`})
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	source := testkit.NoteSource(t, "model.yaml", "parents: [{id: P, children: [{id: C, raw: child-only evidence}]}]\n", 1)
	projection, err := ontology.ProjectNoteSourceSnapshot(source, schema)
	require.NoError(t, err)
	plan, err := (OntologyNodeSyncer{Schema: schema}).chunkSetForProjection(projection, 1)
	require.NoError(t, err)
	var parentID, childID string
	for _, node := range plan.Nodes {
		if node.TypeName == "Parent" {
			parentID = node.NodeID
		}
		if node.TypeName == "Child" {
			childID = node.NodeID
			require.Equal(t, parentID, node.ParentNodeID)
		}
	}
	require.NotEmpty(t, childID)
	var parentText, childText strings.Builder
	for _, chunk := range plan.Chunks {
		if chunk.OwnerID == parentID {
			parentText.WriteString(plan.Texts[chunk.ChunkID])
		}
		if chunk.OwnerID == childID {
			childText.WriteString(plan.Texts[chunk.ChunkID])
		}
	}
	require.Contains(t, childText.String(), "child-only evidence")
	require.NotContains(t, parentText.String(), "child-only evidence")
}
