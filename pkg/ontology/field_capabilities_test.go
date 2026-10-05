package ontology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndexedFieldCapabilityForField_PlansIndexedOperators(t *testing.T) {
	status, ok := IndexedFieldCapabilityForField(&Schema{}, &Field{Name: "status", Kind: FieldKindEnum, TypeName: "Status"})
	require.True(t, ok)
	require.Equal(t, "enum", status.ValueKind)
	require.Equal(t, []string{"eq", "in", "exists"}, status.FilterOps)
	require.True(t, status.Sortable)
	require.True(t, status.Groupable)

	due, ok := IndexedFieldCapabilityForField(&Schema{}, &Field{Name: "due", Kind: FieldKindScalar, TypeName: "Date"})
	require.True(t, ok)
	require.Equal(t, "date", due.ValueKind)
	require.Equal(t, []string{"eq", "in", "exists", "gt", "gte", "lt", "lte"}, due.FilterOps)
	require.True(t, due.Sortable)

	done, ok := IndexedFieldCapabilityForField(&Schema{}, &Field{Name: "done", Kind: FieldKindScalar, TypeName: "Boolean"})
	require.True(t, ok)
	require.Equal(t, "bool", done.ValueKind)
	require.Equal(t, []string{"eq", "exists"}, done.FilterOps)
	require.False(t, done.Sortable)

	assignee, ok := IndexedFieldCapabilityForField(&Schema{}, &Field{Name: "assignee", Kind: FieldKindLink, TypeName: "Person"})
	require.True(t, ok)
	require.Equal(t, "relation", assignee.ValueKind)
	require.Equal(t, []string{"eq", "in", "exists"}, assignee.FilterOps)
	require.False(t, assignee.Sortable)
	require.True(t, assignee.Groupable)
	require.Equal(t, "Person", assignee.TargetType)
}
