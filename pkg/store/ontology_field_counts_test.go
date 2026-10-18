package store

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOntologyFieldValueCountsGroupNodesByNormalizedValue(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "counts.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	model := readmodel.NodeReadModel{NotePaths: []string{"n.md"}}
	for _, row := range []struct{ id, typ, status string }{
		{"a", "Task", "doing"}, {"b", "Task", "doing"}, {"c", "Chore", "planned"}, {"d", "Other", "doing"}, {"e", "Task", ""},
	} {
		model.Nodes = append(model.Nodes, readmodel.NodeRow{NodeID: row.id, NotePath: "n.md", NodeRefJSON: "{}", NodeKind: "EMBEDDED", TypeName: row.typ, Fragment: row.id, SourceLocator: "n.md#^" + row.id, UpdatedAt: 1})
		model.FieldValues = append(model.FieldValues, readmodel.FieldValueRow{NodeID: row.id, NotePath: "n.md", TypeName: row.typ, FieldName: "status", ValueKind: "enum", ValueText: row.status, ValueNorm: row.status, UpdatedAt: 1})
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))

	counts, err := store.OntologyFieldValueCounts(ctx, []string{"Task", "Chore"}, "Status")
	require.NoError(t, err)
	require.Equal(t, map[string]int{"doing": 2, "planned": 1}, counts)
}
