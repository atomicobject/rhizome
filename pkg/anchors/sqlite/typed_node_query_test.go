package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestOntologyNodesByTypePlan_SortSelectedTypes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "sort.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	model := codeanchor.IntelOntologyNodeReadModel{}
	fixtures := []struct {
		id, typ, status string
		start           int64
		ranks           []int64
		null            bool
	}{
		{"a", "Wanted", "active", 10, []int64{2, 9}, false},
		{"b", "AlsoWanted", "active", 20, []int64{1, 5}, false},
		{"c", "Wanted", "active", 30, nil, false},
		{"d", "AlsoWanted", "active", 40, nil, true},
		{"e", "Wanted", "inactive", 50, []int64{2, 7}, false},
		{"f", "Other", "active", 60, []int64{-100, 100}, false},
	}
	model.NotePaths = []string{"shared.md"}
	for _, f := range fixtures {
		model.Nodes = append(model.Nodes, codeanchor.IntelOntologyNode{NodeID: f.id, NotePath: "shared.md", NodeRefJSON: fmt.Sprintf(`{"NodeID":%q}`, f.id), NodeKind: "EMBEDDED", TypeName: f.typ, StartByte: f.start, EndByte: f.start + 5, Fragment: f.id, SourceLocator: "shared.md#^" + f.id, UpdatedAt: 1})
		model.FieldValues = append(model.FieldValues, codeanchor.IntelOntologyNodeFieldValue{NodeID: f.id, NotePath: "shared.md", TypeName: f.typ, FieldName: "status", ValueKind: "string", ValueText: f.status, ValueNorm: f.status, UpdatedAt: 1})
		for i, rank := range f.ranks {
			rank := rank
			model.FieldValues = append(model.FieldValues, codeanchor.IntelOntologyNodeFieldValue{NodeID: f.id, NotePath: "shared.md", TypeName: f.typ, FieldName: "rank", ValueKind: "int", ListOrdinal: i, ValueInt: &rank, UpdatedAt: 1})
		}
		if f.null {
			model.FieldValues = append(model.FieldValues, codeanchor.IntelOntologyNodeFieldValue{NodeID: f.id, NotePath: "shared.md", TypeName: f.typ, FieldName: "rank", ValueKind: "int", UpdatedAt: 1})
		}
	}
	// Sorting historically aggregates every row for the field, regardless of its
	// denormalized type/kind. Candidate ownership comes from ontology_nodes.
	for i := range model.FieldValues {
		if model.FieldValues[i].NodeID == "a" && model.FieldValues[i].FieldName == "rank" {
			model.FieldValues[i].TypeName = "Legacy"
			model.FieldValues[i].ValueKind = "string"
		}
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))
	cases := []struct {
		name                    string
		desc, nullsLast, active bool
		offset, limit           int
		want                    []string
	}{
		{"ascending minimum", false, true, false, 0, 10, []string{"b", "a", "e", "c", "d"}},
		{"descending maximum", true, true, false, 0, 10, []string{"a", "e", "b", "c", "d"}},
		{"ascending default null placement", false, false, false, 0, 10, []string{"c", "d", "b", "a", "e"}},
		{"descending default null placement", true, false, false, 0, 10, []string{"a", "e", "b", "c", "d"}},
		{"filtered offset", false, true, true, 1, 2, []string{"a", "c"}},
		{"descending offset", true, true, true, 1, 2, []string{"b", "c"}},
		{"negative offset", false, true, true, -1, 2, []string{"b", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := codeanchor.OntologyNodeQueryPlan{TypeNames: []string{" Wanted ", "AlsoWanted", "Wanted", ""}, Sort: []codeanchor.OntologyFieldSort{{FieldName: "rank", ValueKind: "int", Desc: tc.desc, NullsLast: tc.nullsLast}}, Limit: tc.limit, Offset: tc.offset}
			if tc.active {
				plan.Predicates = []codeanchor.OntologyFieldPredicate{{FieldName: "status", Op: codeanchor.OntologyFieldOpEq, Values: []codeanchor.IntelOntologyNodeFieldValue{{ValueNorm: "active"}}}}
			}
			nodes, err := store.OntologyNodesByTypePlan(ctx, plan)
			require.NoError(t, err)
			ids := make([]string, len(nodes))
			for i, node := range nodes {
				ids[i] = node.NodeID
				require.Equal(t, "EMBEDDED", node.NodeKind)
				require.Equal(t, "shared.md#^"+node.NodeID, node.SourceLocator)
				require.Equal(t, fmt.Sprintf(`{"NodeID":%q}`, node.NodeID), node.NodeRefJSON)
			}
			require.Equal(t, tc.want, ids)
		})
	}
	nodes, err := store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{
		TypeNames: []string{"Wanted", "AlsoWanted"},
		Sort: []codeanchor.OntologyFieldSort{
			{FieldName: "rank", ValueKind: "int", NullsLast: true},
			{FieldName: "status", ValueKind: "string", Desc: true},
		}, Limit: 10,
	})
	require.NoError(t, err)
	var ids []string
	for _, node := range nodes {
		ids = append(ids, node.NodeID)
	}
	require.Equal(t, []string{"b", "e", "a", "c", "d"}, ids)

}

func TestOntologyNodesByTypePlan_SortUsesSelectedNodeIndex(t *testing.T) {
	store, err := Open(currentSchemaTestDBPath(t, "plan.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	for _, types := range [][]string{{"Wanted"}, {"Wanted", "AlsoWanted"}} {
		query, args, err := ontologyNodesByTypePlanSQL(codeanchor.OntologyNodeQueryPlan{TypeNames: types, Sort: []codeanchor.OntologyFieldSort{{FieldName: "rank", ValueKind: "int", NullsLast: true}}, Limit: 5})
		require.NoError(t, err)
		rows, err := store.db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
		require.NoError(t, err)
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
			details = append(details, detail)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		plan := strings.Join(details, "\n")
		t.Log(plan)
		require.Contains(t, plan, "SEARCH ontology_nodes USING INDEX idx_ontology_nodes_type")
		require.Contains(t, plan, "SEARCH ontology_node_field_values USING INDEX idx_ontology_node_field_values_node (node_id=? AND field_name=?)")
		require.NotContains(t, plan, "SCAN ontology_node_field_values")
	}
}

// The updatedAt builtin orders by the note's indexed modification time before
// LIMIT, so a capped read returns the newest records; notes without a time
// come last in either direction.
func TestOntologyNodesByTypePlan_SortByUpdatedAtBeforeLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "updated.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	model := codeanchor.IntelOntologyNodeReadModel{}
	mtimes := map[string]int64{"a.md": 300, "b.md": 100, "c.md": 0, "d.md": 500, "e.md": 400}
	for path := range mtimes {
		model.NotePaths = append(model.NotePaths, path)
		model.Nodes = append(model.Nodes, codeanchor.IntelOntologyNode{NodeID: path, NotePath: path, NodeRefJSON: fmt.Sprintf(`{"NotePath":%q}`, path), NodeKind: "NOTE", TypeName: "Area", SourceLocator: path, UpdatedAt: 1})
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))
	for path, mtime := range mtimes {
		if path == "c.md" {
			continue // never indexed: no metadata row
		}
		_, err := store.db.ExecContext(ctx, `INSERT INTO notes(path, mtime, indexed_at) VALUES (?, ?, 1)`, path, mtime)
		require.NoError(t, err)
	}
	read := func(desc bool, limit int) []string {
		nodes, err := store.OntologyNodesByTypePlan(ctx, codeanchor.OntologyNodeQueryPlan{
			TypeNames: []string{"Area"},
			Sort:      []codeanchor.OntologyFieldSort{{FieldName: "updatedAt", ValueKind: "builtin", Desc: desc}},
			Limit:     limit,
		})
		require.NoError(t, err)
		var paths []string
		for _, node := range nodes {
			paths = append(paths, node.NotePath)
		}
		return paths
	}
	require.Equal(t, []string{"d.md", "e.md"}, read(true, 2))
	require.Equal(t, []string{"d.md", "e.md", "a.md", "b.md", "c.md"}, read(true, 10))
	require.Equal(t, []string{"b.md", "a.md", "e.md", "d.md", "c.md"}, read(false, 10))
}
