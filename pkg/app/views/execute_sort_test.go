package views

import (
	"context"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestExecuteMixedSortIsIndependentOfSourceOrder(t *testing.T) {
	cases := []struct {
		name       string
		values     []any
		groupOrder []string
	}{
		{name: "numeric and text", values: []any{"2", "10", "1a"}},
		{name: "date and text", values: []any{"2026-01-01T00:30:00+02:00", "2025-12-31T23:30:00Z", "2025-12-31x"}},
		{name: "all categories", values: []any{"10", "2026-01-01", "1a"}},
		{name: "numeric", values: []any{"-1", "2", "10"}},
		{name: "text", values: []any{"a", "b", "c"}},
		{name: "blank", values: []any{nil, "", "2"}, groupOrder: []string{"row2", "row0", "row1"}},
		{name: "nonfinite", values: []any{math.Inf(-1), math.Inf(1), math.NaN()}},
		{name: "nan text", values: []any{"2", "10", "NaN"}},
		{name: "equal numeric values", values: []any{2, "2", "2.0"}},
	}
	permutations := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, grouped := range []bool{false, true} {
				for _, direction := range []string{"asc", "desc"} {
					// Groups have their own ascending order; row direction orders members.
					if grouped && direction == "desc" {
						continue
					}
					want := []string{"row0", "row1", "row2"}
					if grouped && tc.groupOrder != nil {
						want = tc.groupOrder
					}
					if direction == "desc" && tc.name != "equal numeric values" {
						slices.Reverse(want)
						if tc.name == "blank" {
							want = []string{"row2", "row0", "row1"}
						}
					}
					for _, order := range permutations {
						service := New(ServiceOptions{Views: []viewconfig.ViewDefinition{testViewDefinition("work")}, SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
							var rows []TableRow
							for _, index := range order {
								title := fmt.Sprintf("row%d", index)
								rows = append(rows, testRow("docs/"+title+".md", title, map[string]any{"priority": tc.values[index]}))
							}
							return SourceResult{Rows: rows}, nil
						})})
						req := ExecuteRequest{Sort: []viewconfig.SortSpec{{Field: "frontmatter.priority", Direction: direction}}}
						if grouped {
							req.Group = &viewconfig.GroupSpec{Field: "frontmatter.priority"}
						}
						response, err := service.Execute(context.Background(), "work", req)
						require.NoError(t, err)
						require.Equal(t, want, rowTitles(response.Rows), "grouped=%v direction=%s input=%v", grouped, direction, order)
					}
				}
			}
		})
	}
}
