package pushdown

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestPlannerSQLAndMaterializedEvaluationAgree(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "query.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	label := &ontoschema.Field{Name: "label", Kind: ontoschema.FieldKindScalar, TypeName: "String"}
	at := &ontoschema.Field{Name: "at", Kind: ontoschema.FieldKindScalar, TypeName: "DateTime"}
	link := &ontoschema.Field{Name: "owner", Kind: ontoschema.FieldKindLink, TypeName: "Task"}
	noteType := &ontoschema.NoteType{Name: "Task", Fields: []*ontoschema.Field{label, at, link}, ByName: map[string]*ontoschema.Field{"label": label, "at": at, "owner": link}}
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{"Task": noteType}}
	model := readmodel.NodeReadModel{FullReplace: true}
	rows := map[string]map[string][]readmodel.FieldValueRow{}
	for _, item := range []struct{ id, label, at string }{
		{"a", "Ship | Docs", "2026-06-01T08:00:00+02:00"},
		{"b", "[[Ship|Alias]]", "2026-06-01T06:00:00.1Z"},
		{"c", "ÄrGeR", "2026-06-01T06:00:00Z"},
		{"d", "", "invalid"},
	} {
		model.Nodes = append(model.Nodes, readmodel.NodeRow{NodeID: item.id, NotePath: item.id + ".md", NodeKind: "NOTE", TypeName: "Task", NodeRefJSON: fmt.Sprintf(`{"notePath":%q,"kind":"NOTE"}`, item.id+".md")})
		rows[item.id] = map[string][]readmodel.FieldValueRow{}
		for _, value := range []struct {
			field *ontoschema.Field
			text  string
		}{{label, item.label}, {at, item.at}, {link, "[[tasks/" + item.id + ".md]]"}} {
			row := ontology.FieldValueRow(value.field, value.text)
			row.NodeID, row.NotePath, row.TypeName, row.FieldName = item.id, item.id+".md", "Task", value.field.Name
			model.FieldValues = append(model.FieldValues, row)
			rows[item.id][value.field.Name] = append(rows[item.id][value.field.Name], row)
		}
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))
	for _, resolver := range []Resolver{SchemaResolver{NoteType: noteType}, ViewResolver{NoteType: noteType}} {
		planner := Planner{Schema: schema, Resolver: resolver}
		for _, test := range []struct {
			filter   FilterInput
			expected []string
		}{
			{FilterInput{Field: "label", Op: "eq", Value: "Ship | Docs"}, []string{"a"}},
			{FilterInput{Field: "label", Op: "eq", Value: "Ship"}, []string{}},
			{FilterInput{Field: "label", Op: "in", Values: []string{"[[Ship|Alias]]", "ärger"}}, []string{"b", "c"}},
			{FilterInput{Field: "label", Op: "exists"}, []string{"a", "b", "c"}},
			{FilterInput{Field: "label", Op: "in", Values: []string{"", "Ship | Docs"}}, []string{"a"}},
			{FilterInput{Field: "at", Op: "eq", Value: "2026-06-01T06:00:00Z"}, []string{"a", "c"}},
			{FilterInput{Field: "at", Op: "gt", Value: "2026-06-01T06:00:00Z"}, []string{"b"}},
			{FilterInput{Field: "owner", Op: "eq", Value: "tasks/a"}, []string{"a"}},
			{FilterInput{Field: "owner", Op: "in", Values: []string{"[[tasks/a.md|Alias]]", "tasks/c"}}, []string{"a", "c"}},
		} {
			plan := planner.Plan(ctx, []FilterInput{test.filter}, nil)
			require.Empty(t, plan.Residual)
			plan.Pushed.TypeNames = []string{"Task"}
			plan.Pushed.Limit = 100
			indexed, err := store.OntologyNodesByTypePlan(ctx, plan.Pushed)
			require.NoError(t, err)
			sqlIDs, residualIDs := []string{}, []string{}
			for _, node := range indexed {
				sqlIDs = append(sqlIDs, node.NodeID)
			}
			for _, node := range model.Nodes {
				if Match(plan.Pushed.Predicates[0], rows[node.NodeID][test.filter.Field]) {
					residualIDs = append(residualIDs, node.NodeID)
				}
			}
			require.Equal(t, test.expected, sqlIDs, "%+v", test.filter)
			require.Equal(t, test.expected, residualIDs, "%+v", test.filter)
		}
		plan := planner.Plan(ctx, nil, []SortInput{{Field: "at"}})
		plan.Pushed.TypeNames = []string{"Task"}
		plan.Pushed.Limit = 100
		indexed, err := store.OntologyNodesByTypePlan(ctx, plan.Pushed)
		require.NoError(t, err)
		ordered := append([]readmodel.NodeRow(nil), model.Nodes...)
		SortItems(ordered, plan.Pushed.Sort, func(node readmodel.NodeRow, field string) []readmodel.FieldValueRow {
			return rows[node.NodeID][field]
		}, func(node readmodel.NodeRow) StableKey {
			return StableKey{NotePath: node.NotePath, NodeID: node.NodeID}
		})
		sqlIDs, residualIDs := []string{}, []string{}
		for _, node := range indexed {
			sqlIDs = append(sqlIDs, node.NodeID)
		}
		for _, node := range ordered {
			residualIDs = append(residualIDs, node.NodeID)
		}
		require.Equal(t, []string{"a", "c", "b", "d"}, sqlIDs)
		require.Equal(t, sqlIDs, residualIDs)
		blank := planner.Plan(ctx, []FilterInput{{Field: "label", Op: "eq", Value: ""}}, nil)
		require.Empty(t, blank.Pushed.Predicates)
		require.Len(t, blank.Residual, 1)
		blankList := planner.Plan(ctx, []FilterInput{{Field: "label", Op: "in", Values: []string{"", " "}}}, nil)
		require.Empty(t, blankList.Pushed.Predicates)
		require.Len(t, blankList.Residual, 1)
	}
}
