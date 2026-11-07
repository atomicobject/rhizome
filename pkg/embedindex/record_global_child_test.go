package embedindex

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
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

func TestDuplicateParentKeysDoNotAbortChunkPreparation(t *testing.T) {
	root := testkit.Vault(t, map[string]string{".rhizome/ontology/schema.graphql": `type Host @node(default:true){status:String}
type Parent @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"parents",key:{field:"id"},title:["id"],space:"test",logicalKind:"parent"){id:ID @field(path:"id")}
type Child @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"parents[].children",key:{field:"id"},title:["id"],container:"Parent",space:"test",logicalKind:"child"){id:ID @field(path:"id")}`})
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	syncer := OntologyNodeSyncer{Schema: schema, Store: store, Provider: embeddings.NewDeterministicProvider(providerInfo), ProviderInfo: providerInfo}
	ambiguous := testkit.NoteSource(t, "ambiguous.yaml", "parents: [{id: P, children: [{id: C}]}, {id: P, children: [{id: D}]}]\n", 1)
	valid := testkit.NoteSource(t, "valid.yaml", "parents: [{id: Q, children: [{id: E}]}]\n", 1)

	projection, err := ontology.ProjectNoteSourceSnapshot(ambiguous, schema)
	require.NoError(t, err)
	model, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)
	nodeIDs := map[string]bool{}
	for _, node := range model.Nodes {
		nodeIDs[node.NodeID] = true
	}
	for _, node := range model.Nodes {
		require.NotEqual(t, "Parent", node.TypeName, "duplicate keys admit no typed identity")
		require.NotEqual(t, "Child", node.TypeName, "a child of an invalid container has no stable parent identity")
		require.True(t, node.ParentNodeID == "" || nodeIDs[node.ParentNodeID], "catalog node %s names missing parent %s", node.NodeID, node.ParentNodeID)
	}
	var codes []string
	for _, diagnostic := range projection.Prepared.Diagnostics() {
		codes = append(codes, diagnostic.Code)
	}
	require.Equal(t, []string{"record_key_duplicate", "record_key_duplicate", "record_container_invalid", "record_container_invalid"}, codes)

	plan, err := syncer.PrepareSourceSnapshots(t.Context(), []notemeta.NoteSourceSnapshot{ambiguous, valid})
	require.NoError(t, err, "one ambiguous source must not abort the batch")
	var headings []string
	for _, chunk := range plan.chunks {
		headings = append(headings, chunk.Heading)
	}
	require.Subset(t, headings, []string{"Q", "E"})
	require.NotContains(t, headings, "C")
	require.NotContains(t, headings, "D")
}
