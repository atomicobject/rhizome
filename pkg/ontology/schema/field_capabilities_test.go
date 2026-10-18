package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFieldQueryCapabilitySeparatesIndexedAndResidualOperators(t *testing.T) {
	capability, ok := FieldQueryCapabilityForField(nil, &Field{Name: " label ", Kind: FieldKindScalar, TypeName: "String"})
	require.True(t, ok)
	require.Equal(t, FieldQueryCapability{
		FieldName: "label", ValueKind: "string", Sortable: true, Groupable: true,
		FilterOps:        []string{"eq", "in", "exists", "neq", "contains"},
		IndexedFilterOps: []string{"eq", "in", "exists"}, ResidualFilterOps: []string{"neq", "contains"},
	}, capability)
	numeric, ok := FieldQueryCapabilityForField(nil, &Field{Name: "rank", Kind: FieldKindScalar, TypeName: "Int", List: true})
	require.True(t, ok)
	require.Equal(t, []string{"eq", "in", "exists", "gt", "gte", "lt", "lte"}, numeric.IndexedFilterOps)
	require.Equal(t, numeric.IndexedFilterOps, numeric.FilterOps)
	require.Nil(t, numeric.ResidualFilterOps)
	require.False(t, numeric.Sortable)
	require.False(t, numeric.Groupable)
	for _, field := range []*Field{nil, {Name: ""}, {Name: "section", Kind: FieldKindSection}} {
		_, ok := FieldQueryCapabilityForField(nil, field)
		require.False(t, ok)
	}
}
