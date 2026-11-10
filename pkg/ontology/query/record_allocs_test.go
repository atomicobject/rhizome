package query

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// maxAllocsPerRecord bounds the marginal allocations an orientation-class read
// adds per record: scalars, a declared link, a computed relation count, and a
// capability-only workspace. Measured on 2026-11-09 at 1,059 (1,284 before
// SPEC-0136 R8's capability path); raise it only with a measurement that
// explains the new work.
const maxAllocsPerRecord = 1200

// SPEC-0136 R8: testing.AllocsPerRun per record stays within a recorded bound.
func TestOrientationReadAllocationsPerRecordStayBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("allocation measurement")
	}
	const small, large = 20, 120
	notes := map[string]string{"areas/core.md": "---\nname: Core\n---\n"}
	for i := range large {
		notes[fmt.Sprintf("items/%03d.md", i)] = fmt.Sprintf("---\nname: Item %d\nstatus: open\narea: '[[core]]'\n---\nBody of item %d links [[core]].\n", i, i)
	}
	env := newCustomQueryTestEnv(t, `
type Area @node(paths: ["areas/*.md"]) {
  name: String
  items: [Item!] @reverse(field: "area")
}
type Item @node(paths: ["items/*.md"]) {
  name: String
  status: String
  area: Area @link
}
`, notes)
	allocs := func(first int) float64 {
		prepared, errs := Prepare(env.execSchema, fmt.Sprintf(`{ item(first: %d) { path title name status area { path name itemsCount } workspace { capabilities { canEdit readOnlyReason } } } }`, first))
		require.Empty(t, errs)
		deps := env.deps(nil)
		return testing.AllocsPerRun(3, func() {
			result := Execute(t.Context(), deps, env.schema, prepared)
			require.Empty(t, result.Errors)
			require.Len(t, result.Data["item"], first)
		})
	}
	perRecord := (allocs(large) - allocs(small)) / (large - small)
	t.Logf("marginal allocations per record: %.0f", perRecord)
	require.LessOrEqual(t, perRecord, float64(maxAllocsPerRecord))
}
