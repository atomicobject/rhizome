package store

import (
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestOntologyStructuralValuesAndOccurrencesSurviveUpgradeAndRestart(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "index.sqlite")
	baseline, err := openWithOptionsAtSchemaVersion(path, OpenOptions{}, 78)
	require.NoError(t, err)
	require.NoError(t, baseline.Close())
	store, err := Open(path)
	require.NoError(t, err)
	fields := []readmodel.FieldValueRow{
		{NodeID: "one", NotePath: "model.native", TypeName: "Row", FieldName: "empty", FieldKind: "scalar", SourceKind: "PROPERTY", SourcePresence: "value", SourceValueKind: "string", ValueKind: "string"},
		{NodeID: "one", NotePath: "model.native", TypeName: "Row", FieldName: "null", FieldKind: "scalar", SourceKind: "PROPERTY", SourcePresence: "null", SourceValueKind: "null", ValueKind: "string"},
		{NodeID: "one", NotePath: "model.native", TypeName: "Row", FieldName: "list", FieldKind: "scalar", SourceKind: "PROPERTY", SourcePresence: "value", SourceValueKind: "array", ValueKind: "string"},
		{NodeID: "one", NotePath: "model.native", TypeName: "Row", FieldName: "reads", FieldKind: "link", SourceKind: "PROPERTY", SourcePresence: "value", SourceValueKind: "string", ValueKind: "link", ValueText: "field:B.name", ValueNorm: "field:b.name", ReferenceAt: "name", TargetNodeID: "two"},
	}
	edge := OntologyEdgeRow{SrcPath: "model.native", SrcNodeID: "one", DstPath: "model.native", DstNodeID: "two", DstType: "Row", RelationName: "reads", Provenance: "field", Structural: true, OccurrencesJSON: `[{"ordinal":0,"value":"field:B.name","at":"name"},{"ordinal":1,"value":"field:B.name","at":"name"}]`}
	require.NoError(t, store.ApplyOntologyDelta(ctx, OntologyDelta{ReplacePaths: []string{"model.native"}, EdgeSources: []string{"model.native"}, Edges: []OntologyEdgeRow{edge}, ReadModels: []readmodel.NodeReadModel{{NotePaths: []string{"model.native"}, Nodes: []readmodel.NodeRow{{NodeID: "one", NotePath: "model.native", TypeName: "Row", NodeKind: "EMBEDDED", NodeRefJSON: `{"notePath":"model.native","nodeId":"one","kind":"EMBEDDED"}`, RecordJSON: `{"Binding":"Row:rows","Space":"test","LogicalKind":"row","LogicalKey":"2","Positional":true,"SourceOrder":1}`}}, FieldValues: fields}}}))
	require.NoError(t, store.Close())
	store, err = Open(path)
	require.NoError(t, err)
	defer store.Close()
	nodes, err := store.OntologyNodesByIDs(ctx, []string{"one"})
	require.NoError(t, err)
	require.JSONEq(t, `{"Binding":"Row:rows","Space":"test","LogicalKind":"row","LogicalKey":"2","Positional":true,"SourceOrder":1}`, nodes["one"].RecordJSON)
	actual, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, []string{"one"}, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, fields, actual)
	edges, err := store.AllOntologyEdges(ctx, true, 0)
	require.NoError(t, err)
	require.Equal(t, []OntologyEdgeRow{edge}, edges)
	graph, err := store.GraphOntologyEdges(ctx, readmodel.GraphEdgeQuery{Paths: []string{"model.native"}, IncludeAmbient: true})
	require.NoError(t, err)
	require.Len(t, graph, 1)
	require.Equal(t, edge.OccurrencesJSON, graph[0].OccurrencesJSON)
	for i, field := range fields[:3] {
		predicate := readmodel.FieldPredicate{FieldName: field.FieldName, Op: readmodel.FieldOpExists}
		where, args, err := ontologyNodePredicateSQL(0, []string{"Row"}, predicate)
		require.NoError(t, err)
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ontology_nodes n WHERE "+where, args...).Scan(&count))
		require.Equal(t, []bool{true, false, true}[i], count > 0)
	}
}

func TestOntologyRecordContextUpgradesExistingStructuralSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.sqlite")
	prior, err := openWithOptionsAtSchemaVersion(path, OpenOptions{}, 79)
	require.NoError(t, err)
	require.NoError(t, prior.Close())
	current, err := Open(path)
	require.NoError(t, err)
	defer current.Close()
	var count int
	require.NoError(t, current.db.QueryRowContext(t.Context(), `SELECT count(*) FROM pragma_table_info('ontology_nodes') WHERE name='record_identity_json'`).Scan(&count))
	require.Equal(t, 1, count)
}
