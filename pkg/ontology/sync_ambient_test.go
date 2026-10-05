package ontology

import (
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestBuildAmbientEdgesForSync_PreservesDirectionAndSuppressesStructuralTargets(t *testing.T) {
	schema := &Schema{Hash: "schema", Types: map[string]*NoteType{
		"Project": {Fields: []*Field{
			{Name: "outgoing", Kind: FieldKindNeighbor, TypeName: "Person", Direction: NeighborDirectionOutbound},
			{Name: "incoming", Kind: FieldKindNeighbor, TypeName: "Person", Direction: NeighborDirectionInbound},
		}},
		"Person": {},
	}}
	rows := []semdb.GraphDocEdge{
		{SrcPath: "project.md", DstPath: "person.md"},
		{SrcPath: "person.md", DstPath: "project.md"},
		{SrcPath: "project.md", DstPath: "person.md"}, // Duplicate links produce one row per direction.
		{SrcPath: "project.md", DstPath: "owner.md"},
		{SrcPath: "owner.md", DstPath: "project.md"},
		{SrcPath: "project.md", DstPath: "plain.md"},
		{SrcPath: "plain.md", DstPath: "project.md"},
		{SrcPath: "project.md", DstPath: "project.md"},
		{SrcPath: "elsewhere.md", DstPath: "person.md"},
	}
	structuralPairs := map[string]struct{}{
		"project.md\x00owner.md":  {},
		"person.md\x00project.md": {}, // The reverse structural pair does not hide this source's ambient links.
	}
	resolveType := func(path string) string {
		if path == "plain.md" {
			return "  "
		}
		return "Person"
	}
	edges := buildAmbientEdgesForSync("project.md", "Project", rows, structuralPairs, resolveType, schema, 123)
	require.Equal(t, []semdb.OntologyEdgeRow{
		{SrcPath: "project.md", RelationName: "incoming", DstPath: "person.md", DstType: "Person", Provenance: "backlink", SchemaHash: "schema", UpdatedAt: 123},
		{SrcPath: "project.md", RelationName: "outgoing", DstPath: "person.md", DstType: "Person", Provenance: "body_link", SchemaHash: "schema", UpdatedAt: 123},
	}, edges)
}
