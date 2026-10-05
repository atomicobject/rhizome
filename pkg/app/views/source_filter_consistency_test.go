package views

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestOntologySourceRepeatedRangeFilterAgreesWithResidual(t *testing.T) {
	for _, tc := range []struct{ scalar, low, middle, high string }{
		{"Int", "2", "5", "10"},
		{"Float", "2", "5", "10"},
		{"Date", "2026-01-01", "2026-02-01", "2026-03-01"},
	} {
		t.Run(tc.scalar, func(t *testing.T) {
			service := scalarSortService(t, tc.scalar, "# Actions\n\n"+
				"- [ ] Repeated #action-item\n  rank:: "+tc.low+"\n  rank:: "+tc.high+"\n  rank:: invalid\n"+
				"- [ ] Middle #action-item\n  rank:: "+tc.middle+"\n"+
				"- [ ] Invalid #action-item\n  rank:: invalid\n  rank:: invalid-again\n"+
				"- [ ] Empty #action-item\n"+
				"- [ ] LowNaN #action-item\n  rank:: "+tc.low+"\n  rank:: NaN\n"+
				"- [ ] HighNaN #action-item\n  rank:: "+tc.high+"\n  rank:: NaN\n"+
				"- [ ] OnlyNaN #action-item\n  rank:: NaN\n  rank:: NaN\n")
			opts := service.opts
			indexed := opts.SourceResolver
			opts.SourceResolver = SourceResolverFunc(func(ctx context.Context, def viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
				filters := req.Filters
				req.Filters = nil
				source, err := indexed.ResolveSource(ctx, def, req)
				source.ResidualConstraints.Filters = filters
				return source, err
			})
			residualService := New(opts)
			for _, field := range []string{"rank", "inline.rank"} {
				for _, op := range []string{"gt", "gte", "lt", "lte"} {
					t.Run(field+"/"+op, func(t *testing.T) {
						filters := []viewconfig.FilterSpec{{Field: field, Op: op, Value: tc.middle}}
						pushed, err := service.Execute(context.Background(), "actions", ExecuteRequest{Filters: filters})
						require.NoError(t, err)
						require.Equal(t, filters, pushed.PushedConstraints.Filters)
						require.Empty(t, pushed.ResidualConstraints.Filters)
						want := []string{"Repeated"}
						if op == "gt" || op == "gte" {
							want = append(want, "HighNaN")
						} else {
							want = append(want, "LowNaN")
						}
						if op == "gte" || op == "lte" {
							want = append(want, "Middle")
						}
						require.ElementsMatch(t, want, rowTitles(pushed.Rows))
						residual, err := residualService.Execute(context.Background(), "actions", ExecuteRequest{Filters: filters})
						require.NoError(t, err)
						require.Empty(t, residual.PushedConstraints.Filters)
						require.Equal(t, filters, residual.ResidualConstraints.Filters)
						require.ElementsMatch(t, want, rowTitles(residual.Rows))
					})
				}
			}
		})
	}
}

func TestExecuteRangeFilterAcceptsAdapterLists(t *testing.T) {
	type ranks []int
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"typed", []int{2, 10}},
		{"named", ranks{2, 10}},
		{"array", [2]int{2, 10}},
		{"nested", []any{[]int{2}, ranks{10}}},
		{"strings", []string{"2", "10"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := New(ServiceOptions{
				Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
				SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
					return SourceResult{Rows: []TableRow{
						testRow("match.md", "Match", map[string]any{"rank": tc.value}),
						testRow("empty.md", "Empty", map[string]any{"rank": ranks(nil)}),
						testRow("middle.md", "Middle", map[string]any{"rank": []int{5}}),
					}, Capabilities: []FieldCapability{{Key: "frontmatter.rank", ValueKind: "number", FilterOps: []string{"gt", "gte", "lt", "lte"}}}}, nil
				}),
			})
			for _, op := range []string{"gt", "gte", "lt", "lte"} {
				t.Run(op, func(t *testing.T) {
					resp, err := service.Execute(context.Background(), "work", ExecuteRequest{Filters: []viewconfig.FilterSpec{{Field: "frontmatter.rank", Op: op, Value: "5"}}})
					require.NoError(t, err)
					want := []string{"Match"}
					if op == "gte" || op == "lte" {
						want = append(want, "Middle")
					}
					require.ElementsMatch(t, want, rowTitles(resp.Rows))
				})
			}
		})
	}
}

func TestOntologySourceScalarFiltersAgreeWithResidual(t *testing.T) {
	rangeOps := []string{"gt", "gte", "lt", "lte"}
	equalOps := []string{"eq", "in"}
	for _, tc := range []struct {
		name, scalar, actual, threshold string
		ops, matching                   []string
	}{
		{"large integer", "Int", "9007199254740993", "9007199254740992", rangeOps, []string{"gt", "gte"}},
		{"datetime offset", "DateTime", "2026-05-08T09:00:00+02:00", "2026-05-08T08:00:00Z", rangeOps, []string{"gt", "gte"}},
		{"datetime fractions range", "DateTime", "2026-05-08T09:00:00.999Z", "2026-05-08T09:00:00Z", rangeOps, []string{"gte", "lte"}},
		{"text case", "String", "APPLE", "apple", equalOps, equalOps},
		{"text link", "String", "[[Apple|Label]]", "apple", equalOps, equalOps},
		{"float spelling", "Float", "2.0", "2", equalOps, equalOps},
		{"invalid integer", "Int", "INVALID", "invalid", equalOps, equalOps},
		{"NaN equality", "Float", "NaN", "NaN", equalOps, nil},
		{"integer spelling", "Int", "002", "2", equalOps, equalOps},
		{"datetime offset equality", "DateTime", "2026-05-08T09:00:00+02:00", "2026-05-08T08:00:00Z", equalOps, nil},
		{"datetime fractions equality", "DateTime", "2026-05-08T09:00:00.999Z", "2026-05-08T09:00:00Z", equalOps, equalOps},
		{"invalid Int threshold", "Int", "2", "junk", rangeOps, []string{"lt", "lte"}},
		{"invalid Float threshold", "Float", "2", "junk", rangeOps, []string{"lt", "lte"}},
		{"invalid Date threshold", "Date", "2026-05-08", "junk", rangeOps, []string{"lt", "lte"}},
		{"invalid DateTime threshold", "DateTime", "2026-05-08T09:00:00Z", "junk", rangeOps, []string{"lt", "lte"}},
		{"datetime normalized-text fallback", "DateTime", "2026-05-08T09:00:00.999Z", "2026-05-08t09:00:00.9z", rangeOps, []string{"gt", "gte"}},
		{"unparseable date-shaped threshold", "Date", "2026-05-08", "9999-99-99", rangeOps, []string{"lt", "lte"}},
		{"NaN range", "Float", "2", "NaN", rangeOps, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := scalarSortService(t, tc.scalar, "# Actions\n\n- [ ] Match #action-item\n  rank:: "+tc.actual+"\n- [ ] Empty #action-item\n")
			opts := service.opts
			indexed := opts.SourceResolver
			opts.SourceResolver = SourceResolverFunc(func(ctx context.Context, def viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
				filters := req.Filters
				req.Filters = nil
				source, err := indexed.ResolveSource(ctx, def, req)
				source.ResidualConstraints.Filters = filters
				return source, err
			})
			for _, op := range tc.ops {
				t.Run(op, func(t *testing.T) {
					req := ExecuteRequest{Filters: []viewconfig.FilterSpec{{Field: "rank", Op: op, Value: tc.threshold, Values: []string{tc.threshold}}}}
					pushed, err := service.Execute(context.Background(), "actions", req)
					require.NoError(t, err)
					require.Equal(t, req.Filters, pushed.PushedConstraints.Filters)
					require.Empty(t, pushed.ResidualConstraints.Filters)
					residual, err := New(opts).Execute(context.Background(), "actions", req)
					require.NoError(t, err)
					require.Empty(t, residual.PushedConstraints.Filters)
					require.Equal(t, req.Filters, residual.ResidualConstraints.Filters)
					want := []string{}
					for _, matching := range tc.matching {
						if op == matching {
							want = []string{"Match"}
						}
					}
					require.Equal(t, want, rowTitles(pushed.Rows))
					require.Equal(t, want, rowTitles(residual.Rows))
				})
			}
		})
	}
}
