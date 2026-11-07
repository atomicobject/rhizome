package embedindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit/structuralfixture"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

func TestRecordSyncerChunksNativeOwnersWithoutSiblingEvidence(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`type Host @node(default:true){ status:String }
 type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"rows",key:{field:"id"},title:["name"],space:"test",logicalKind:"row"){ name:String @field(path:"name") }`), 0o644))
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	provider, err := structuralfixture.New(map[string]any{"rows": []any{map[string]any{"id": "A", "name": "First", "detail": "alpha unique evidence"}, map[string]any{"id": "B", "name": "Second", "detail": "beta unique evidence"}}})
	require.NoError(t, err)
	projected, err := provider.Project(noteformat.AuthoredSource{})
	require.NoError(t, err)
	source := &ontology.RootDocumentSnapshot{NotePath: "model.native", Format: "synthetic", RawSource: []byte("raw poison never parsed"), ContentHash: "revision", Projection: projected, Root: ontology.NodeRef{NotePath: "model.native", Kind: ontology.NodeKindNote}}
	prepared, err := ontology.PrepareDocument(source, schema)
	require.NoError(t, err)
	projection, err := prepared.Project(source.Root)
	require.NoError(t, err)
	plan, err := (OntologyNodeSyncer{Schema: schema}).chunkSetForProjection(projection, 1)
	require.NoError(t, err)
	var bodies []string
	for _, node := range plan.Nodes {
		if node.TypeName != "Row" {
			continue
		}
		var body strings.Builder
		for _, chunk := range plan.Chunks {
			if chunk.OwnerID == node.NodeID {
				body.WriteString(plan.Texts[chunk.ChunkID])
			}
		}
		bodies = append(bodies, body.String())
	}
	require.Len(t, bodies, 2)
	require.Contains(t, bodies[0], "alpha unique evidence")
	require.NotContains(t, bodies[0], "beta unique evidence")
	require.Contains(t, bodies[1], "beta unique evidence")
	require.NotContains(t, bodies[1], "alpha unique evidence")
	require.NotContains(t, strings.Join(bodies, ""), "raw poison")
}
