package noderead

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestFragmentlessSourceIDsHydrateIndexedRecordsWithoutReadingSources(t *testing.T) {
	vault, store, schema := buildFixture(t, `type Spec @node(paths:["specs/*.md"]){title:String}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model.yaml"],select:"rows",key:{field:"id"},title:["id"],space:"fixture",logicalKind:"row"){id:ID @field(path:"id")}`, "# Host\n")
	var rows []readmodel.NodeRow
	var refs []ontology.NodeRef
	for _, id := range []string{"a", "b"} {
		ref := ontology.NodeRef{NotePath: "model.yaml", Kind: ontology.NodeKindEmbedded, NodeID: "source-" + id, Fragment: "record:v1:" + id, TypeName: "Row"}
		encoded, err := json.Marshal(ref)
		require.NoError(t, err)
		rows = append(rows, readmodel.NodeRow{NodeID: "catalog-" + id, NotePath: ref.NotePath, NodeKind: string(ref.Kind), TypeName: ref.TypeName, NodeRefJSON: string(encoded), SourceLocator: ref.String(), DisplayLabel: "Record " + id})
		ref.Fragment = ""
		refs = append(refs, ref)
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(t.Context(), readmodel.NodeReadModel{NotePaths: []string{"model.yaml"}, Nodes: rows}))
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vault, reader, store, schema).NewScope(ScopeOptions{})
	records, err := scope.Hydrate(t.Context(), refs, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, "Record a", records[0].Title)
	require.Equal(t, "Record b", records[1].Title)
	require.Equal(t, "record:v1:a", records[0].Ref.Fragment)
	require.Zero(t, reader.contents)

	duplicate := rows[0]
	duplicate.NodeID = "another-catalog-row"
	duplicate.SourceLocator = "duplicate"
	rows = append(rows, duplicate)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(t.Context(), readmodel.NodeReadModel{NotePaths: []string{"model.yaml"}, Nodes: rows}))
	scope = NewService(vault, reader, store, schema).NewScope(ScopeOptions{})
	_, err = scope.Hydrate(t.Context(), refs, HydrateOptions{Profile: HydrateSummary})
	require.ErrorIs(t, err, ontology.ErrAmbiguousSourceSpanIdentity)
	require.Zero(t, reader.contents)
}
