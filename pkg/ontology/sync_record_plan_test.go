package ontology

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRecordBindingPlanRebindsUnchangedAndUnresolvedSourceHosts(t *testing.T) {
	schema := recordFixtureSchema(t, recordRowsSDL)
	for _, deleted := range []bool{false, true} {
		plan := &syncPlan{LoadPaths: []string{"a.native"}}
		current := []string{"a.native", "b.native", "unrelated.md"}
		expected := []string{"a.native", "b.native"}
		var changed, gone []string
		if deleted {
			gone = []string{"a.native"}
			current = current[1:]
			plan.LoadPaths = nil
			expected = []string{"b.native"}
		} else {
			changed = []string{"a.native"}
		}
		expandRecordBindingPlan(plan, schema, current, changed, gone)
		require.Equal(t, expected, plan.LoadPaths)
		require.Contains(t, plan.ForcePaths, "b.native")
	}
	plan := &syncPlan{LoadPaths: []string{"unrelated.md"}}
	expandRecordBindingPlan(plan, schema, []string{"a.native", "unrelated.md"}, []string{"unrelated.md"}, nil)
	require.Equal(t, []string{"unrelated.md"}, plan.LoadPaths)
	require.Empty(t, plan.ForcePaths)
}

func TestRecordSelectorWrongShapeIsInspectableWithoutParsingFailure(t *testing.T) {
	schema := recordFixtureSchema(t, `type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"rows[].items",key:{field:"id"},title:["$key"],space:"test",logicalKind:"row") { name:String @field(path:"name") }`)
	p, err := PrepareDocument(recordFixtureRoot(t, "model.native", map[string]any{"rows": "wrong shape"}), schema)
	require.NoError(t, err)
	require.Empty(t, p.records)
	require.Len(t, p.Diagnostics(), 1)
	require.Equal(t, "record_selector_shape", p.Diagnostics()[0].Code)
	require.Equal(t, "/rows", p.Diagnostics()[0].Address)
}
