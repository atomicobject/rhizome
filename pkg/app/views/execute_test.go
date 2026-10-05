package views

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

type SourceResolverFunc func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error)

func (fn SourceResolverFunc) ResolveSource(ctx context.Context, view viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
	return fn(ctx, view, req)
}

func TestExecuteAppliesSearchFiltersSortGroupAndPagination(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Defaults: viewconfig.DefaultsSpec{
				Variant: "table",
				First:   10,
				Sort:    []viewconfig.SortSpec{{Field: "frontmatter.priority", Direction: "asc"}},
				Group:   &viewconfig.GroupSpec{Field: "frontmatter.status"},
			},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{
				{Field: "title"},
				{Field: "frontmatter.status"},
				{Field: "frontmatter.priority"},
			}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{
				testRow("docs/b.md", "Beta", map[string]any{"status": "ready", "priority": 2}),
				testRow("docs/a.md", "Alpha", map[string]any{"status": "ready", "priority": 1}),
				testRow("docs/c.md", "Archive", map[string]any{"status": "done", "priority": 3}),
			}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{
		Search: "a",
		Filters: []viewconfig.FilterSpec{{
			Field:  "frontmatter.status",
			Op:     "in",
			Values: []string{"ready"},
		}},
		Page: PageRequest{Offset: 0, First: 1},
	})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 1)
	require.Equal(t, "Alpha", resp.Rows[0].Title)
	require.Equal(t, 2, resp.PageInfo.Total)
	require.True(t, resp.PageInfo.HasMore)
	require.Len(t, resp.Groups, 1)
	require.Equal(t, "ready", resp.Groups[0].Value)
	require.Equal(t, "frontmatter.status=ready", resp.Groups[0].Key)
	require.Equal(t, "ready", resp.Groups[0].Label)
	require.Equal(t, 0, resp.Groups[0].RowStart)
	require.Equal(t, 1, resp.Groups[0].RowEnd)
	require.Equal(t, 1, resp.Groups[0].Count)
	require.Equal(t, 2, resp.Groups[0].TotalCount)
	require.NotEmpty(t, resp.DefinitionFingerprint)
	require.NotEmpty(t, resp.SourceFingerprint)
	require.NotEmpty(t, resp.ExecutionFingerprint)
}

func TestExecuteInterfaceViewAlwaysIncludesTypeColumn(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "specs",
			Name:       "Specs",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyInterface, Interface: "SpecLike"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindInterface, Interface: "SpecLike"},
			Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			row := testRow("docs/spec.md", "Spec", nil)
			row.ResolvedType = "ProductSpec"
			return SourceResult{Rows: []TableRow{row}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "specs", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, []TableColumn{
		{Field: "resolvedType", Label: "Type"},
		{Field: "title", Label: "Title"},
	}, resp.Columns)
	require.Equal(t, "ProductSpec", resp.Rows[0].ResolvedType)
}

func TestExecutePreservesExactScalarEqualityButNormalizesLinks(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			row := testRow("docs/a.md", "Alpha", map[string]any{
				"id":   "ABC",
				"slug": "foo.md",
			})
			row.Fields["assignedTo"] = "Notes/People/Alice.md"
			return SourceResult{
				Capabilities: []FieldCapability{{
					Key:       "assignedTo",
					ValueKind: "relation",
				}},
				Rows: []TableRow{row},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "frontmatter.slug", Op: "eq", Value: "foo"}},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Rows)

	resp, err = service.Execute(context.Background(), "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "frontmatter.id", Op: "in", Values: []string{"abc"}}},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Rows)

	resp, err = service.Execute(context.Background(), "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "path", Op: "eq", Value: "docs/a"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha"}, rowTitles(resp.Rows))

	resp, err = service.Execute(context.Background(), "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "assignedTo", Op: "eq", Value: "notes/people/alice"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha"}, rowTitles(resp.Rows))
}

func TestExecuteResidualFiltersUseTypedCapabilityComparison(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				Capabilities: []FieldCapability{
					{Key: "frontmatter.done", ValueKind: "bool"},
					{Key: "frontmatter.priority", ValueKind: "number"},
					{Key: "frontmatter.due", ValueKind: "date"},
				},
				Rows: []TableRow{
					testRow("docs/a.md", "Alpha", map[string]any{"done": true, "priority": "10", "due": "2026-05-10"}),
					testRow("docs/b.md", "Beta", map[string]any{"done": false, "priority": "2", "due": "2026-05-09"}),
				},
			}, nil
		}),
	})

	for _, filter := range []viewconfig.FilterSpec{
		{Field: "frontmatter.done", Op: "eq", Value: "true"},
		{Field: "frontmatter.priority", Op: "gt", Value: "2"},
		{Field: "frontmatter.due", Op: "gte", Value: "2026-05-10"},
	} {
		t.Run(filter.Field, func(t *testing.T) {
			resp, err := service.Execute(context.Background(), "work", ExecuteRequest{Filters: []viewconfig.FilterSpec{filter}})
			require.NoError(t, err)
			require.Equal(t, []string{"Alpha"}, rowTitles(resp.Rows))
		})
	}
}

func TestExecuteResidualRelationFiltersUseRowLinkVariants(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			row := testRow("docs/a.md", "Alpha", nil)
			row.Fields["owner"] = map[string]any{
				"path":    "people/alice-smith.md",
				"title":   "Alice Smith",
				"aliases": []any{"Alice", "A. Smith"},
			}
			return SourceResult{
				Capabilities: []FieldCapability{{Key: "owner", ValueKind: "relation"}},
				Rows:         []TableRow{row},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "owner", Op: "in", Values: []string{"[[people/alice-smith]]"}}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha"}, rowTitles(resp.Rows))

	resp, err = service.Execute(context.Background(), "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "owner", Op: "eq", Value: "Alice Smith"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha"}, rowTitles(resp.Rows))

	resp, err = service.Execute(context.Background(), "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "owner", Op: "eq", Value: "Alice"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha"}, rowTitles(resp.Rows))
}

func TestExecuteOrdersRowsByNestedGroupFieldsAndEnumRanks(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Defaults: viewconfig.DefaultsSpec{
				Group: &viewconfig.GroupSpec{Fields: []string{"frontmatter.status", "frontmatter.owner"}},
			},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				Capabilities: []FieldCapability{{
					Key:       "frontmatter.status",
					Groupable: true,
					EnumValues: []FieldEnumValue{
						{Value: "ready", Label: "Ready", Rank: 1},
						{Value: "done", Label: "Done", Rank: 2, CollapsedByDefault: true},
					},
				}},
				Rows: []TableRow{
					testRow("docs/c.md", "Charlie", map[string]any{"status": "done", "owner": "Maya"}),
					testRow("docs/b.md", "Beta", map[string]any{"status": "ready", "owner": "Gabe"}),
					testRow("docs/a.md", "Alpha", map[string]any{"status": "ready", "owner": "Anne"}),
				},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha", "Beta", "Charlie"}, rowTitles(resp.Rows))
	require.Len(t, resp.Groups, 2)
	require.Equal(t, "Ready", resp.Groups[0].Label)
	require.False(t, resp.Groups[0].CollapsedByDefault)
	require.Equal(t, "frontmatter.status=ready", resp.Groups[0].Key)
	require.Len(t, resp.Groups[0].Children, 2)
	require.Equal(t, "frontmatter.status=ready/frontmatter.owner=Anne", resp.Groups[0].Children[0].Key)
	require.Equal(t, "Done", resp.Groups[1].Label)
	require.True(t, resp.Groups[1].CollapsedByDefault)
}

func TestExecuteGroupValuesOverrideEnumLabelOrderAndCollapse(t *testing.T) {
	collapsed := false
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Defaults: viewconfig.DefaultsSpec{
				Group: &viewconfig.GroupSpec{
					Field: "frontmatter.status",
					Values: []viewconfig.GroupValueSpec{{
						Value:              "done",
						Label:              "Finished",
						Order:              -1,
						CollapsedByDefault: &collapsed,
					}},
				},
			},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				Capabilities: []FieldCapability{{
					Key: "frontmatter.status",
					EnumValues: []FieldEnumValue{
						{Value: "ready", Label: "Ready", Rank: 1},
						{Value: "done", Label: "Done", Rank: 2, CollapsedByDefault: true},
					},
				}},
				Rows: []TableRow{
					testRow("docs/c.md", "Charlie", map[string]any{"status": "ready"}),
					testRow("docs/a.md", "Alpha", map[string]any{"status": "done"}),
				},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha", "Charlie"}, rowTitles(resp.Rows))
	require.Equal(t, "Finished", resp.Groups[0].Label)
	require.False(t, resp.Groups[0].CollapsedByDefault)
	require.Equal(t, "Ready", resp.Groups[1].Label)
}

func TestExecuteGroupValuesSupportCollapsedAlias(t *testing.T) {
	collapsed := true
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Defaults: viewconfig.DefaultsSpec{
				Group: &viewconfig.GroupSpec{
					Field:  "frontmatter.done",
					Values: []viewconfig.GroupValueSpec{{Value: "true", Label: "Done", Collapsed: &collapsed}},
				},
			},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{
				testRow("docs/a.md", "Alpha", map[string]any{"done": "true"}),
			}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Groups, 1)
	require.Equal(t, "Done", resp.Groups[0].Label)
	require.True(t, resp.Groups[0].CollapsedByDefault)
}

func TestExecutePreservesSourceOrderWithoutExplicitSort(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{
				testRow("docs/c.md", "Charlie", nil),
				testRow("docs/a.md", "Alpha", nil),
				testRow("docs/b.md", "Beta", nil),
			}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"Charlie", "Alpha", "Beta"}, rowTitles(resp.Rows))
}

func TestExecuteEchoesSourceConstraintPlan(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				ConstraintPlan: true,
				PushedConstraints: SourceConstraints{
					Filters: []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "open"}},
					Sort:    []viewconfig.SortSpec{{Field: "due", Direction: "asc"}},
					Page:    PageRequest{First: 50},
				},
				ResidualConstraints: SourceConstraints{
					Search: "docs",
				},
				Rows: []TableRow{
					testRow("docs/a.md", "Alpha", map[string]any{"status": "open", "due": "2026-05-08"}),
				},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.True(t, resp.ConstraintPlan)
	require.Equal(t, []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "open"}}, resp.PushedConstraints.Filters)
	require.Equal(t, []viewconfig.SortSpec{{Field: "due", Direction: "asc"}}, resp.PushedConstraints.Sort)
	require.Equal(t, PageRequest{First: 50}, resp.PushedConstraints.Page)
	require.Equal(t, "docs", resp.ResidualConstraints.Search)
	require.Equal(t, SourceComplete, resp.Plan.SourceCompleteness)
	require.Equal(t, ConstraintsExact, resp.Plan.Reliability)
	require.Equal(t, SourceCapPolicy{Limit: defaultSourceCap, Source: "default"}, resp.Plan.CapPolicy)
	require.Equal(t, 1, resp.Plan.CandidateCount)
	require.Equal(t, defaultSourceCap, resp.Plan.CandidateLimit)
	require.Equal(t, "docs", resp.Plan.Residual.Search)
	require.Equal(t, []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "open"}}, resp.Plan.Pushed.Filters)
}

func TestExecuteReportsCapBoundResidualFilterSearchAndSort(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				ConstraintPlan: true,
				ResidualConstraints: SourceConstraints{
					Search:  "alpha",
					Filters: []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "open"}},
					Sort:    []viewconfig.SortSpec{{Field: "priority", Direction: "asc"}},
				},
				Plan: ConstraintPlanSummary{
					CandidateLimit:     1,
					CandidateCount:     1,
					SourceCompleteness: SourceBounded,
				},
				Rows: []TableRow{
					testRow("docs/a.md", "Alpha", map[string]any{"status": "open", "priority": 2}),
				},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, ConstraintsCapBound, resp.Plan.Reliability)
	require.Equal(t, SourceBounded, resp.Plan.SourceCompleteness)
	requireExecuteWarningCode(t, resp.Warnings, "view_search_cap_bound")
	requireExecuteWarningCode(t, resp.Warnings, "view_residual_constraints_cap_bound")
	requireExecuteWarningCode(t, resp.Warnings, "view_filter_cap_bound")
	requireExecuteWarningCode(t, resp.Warnings, "view_sort_cap_bound")
	requireExecuteWarningCode(t, resp.Plan.Warnings, "view_search_cap_bound")
	requireExecuteWarningCode(t, resp.Plan.Warnings, "view_residual_constraints_cap_bound")
	requireExecuteWarningCode(t, resp.Plan.Warnings, "view_filter_cap_bound")
	requireExecuteWarningCode(t, resp.Plan.Warnings, "view_sort_cap_bound")
}

func TestExecuteReportsPushedOnlyBoundedPlanAsExact(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				ConstraintPlan: true,
				PushedConstraints: SourceConstraints{
					Filters: []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "open"}},
					Sort:    []viewconfig.SortSpec{{Field: "priority", Direction: "asc"}},
				},
				Plan: ConstraintPlanSummary{
					CandidateLimit:     1,
					CandidateCount:     1,
					SourceCompleteness: SourceBounded,
				},
				Rows: []TableRow{
					testRow("docs/a.md", "Alpha", map[string]any{"status": "open", "priority": 2}),
				},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, ConstraintsExact, resp.Plan.Reliability)
	require.Equal(t, SourceBounded, resp.Plan.SourceCompleteness)
	require.NotContains(t, warningCodes(resp.Warnings), "view_residual_constraints_cap_bound")
	require.Equal(t, []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "open"}}, resp.Plan.Pushed.Filters)
	require.Empty(t, resp.Plan.Residual.Filters)
}

func TestExecuteClearsDefaultFiltersSortAndGroupWithExplicitEmptyState(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Defaults: viewconfig.DefaultsSpec{
				Filters: []viewconfig.FilterSpec{{Field: "frontmatter.status", Op: "eq", Value: "ready"}},
				Sort:    []viewconfig.SortSpec{{Field: "frontmatter.priority", Direction: "asc"}},
				Group:   &viewconfig.GroupSpec{Field: "frontmatter.status"},
			},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{
				{Field: "title"},
				{Field: "frontmatter.status"},
				{Field: "frontmatter.priority"},
			}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{
				testRow("docs/b.md", "Beta", map[string]any{"status": "ready", "priority": 2}),
				testRow("docs/a.md", "Alpha", map[string]any{"status": "done", "priority": 1}),
			}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{},
		Sort:    []viewconfig.SortSpec{},
		Group:   &viewconfig.GroupSpec{Field: ""},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Beta", "Alpha"}, rowTitles(resp.Rows))
	require.Empty(t, resp.Groups)
	require.Empty(t, resp.State.Filters)
	require.Empty(t, resp.State.Sort)
	require.Equal(t, "", resp.State.Group.Field)
}

func TestExecuteHonorsDefaultPageAndRequestOverride(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Defaults: viewconfig.DefaultsSpec{
				Page: &viewconfig.PageSpec{Offset: 1, First: 2},
			},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{
				testRow("docs/a.md", "Alpha", nil),
				testRow("docs/b.md", "Beta", nil),
				testRow("docs/c.md", "Charlie", nil),
				testRow("docs/d.md", "Delta", nil),
			}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, PageRequest{Offset: 1, First: 2}, resp.State.Page)
	require.Equal(t, []string{"Beta", "Charlie"}, rowTitles(resp.Rows))

	resp, err = service.Execute(context.Background(), "work", ExecuteRequest{
		Page: PageRequest{Offset: 2, First: 1},
	})
	require.NoError(t, err)
	require.Equal(t, PageRequest{Offset: 2, First: 1}, resp.State.Page)
	require.Equal(t, []string{"Charlie"}, rowTitles(resp.Rows))

	var req ExecuteRequest
	require.NoError(t, json.Unmarshal([]byte(`{"page":{"offset":0,"first":1}}`), &req))
	resp, err = service.Execute(context.Background(), "work", req)
	require.NoError(t, err)
	require.Equal(t, PageRequest{Offset: 0, First: 1}, resp.State.Page)
	require.Equal(t, []string{"Alpha"}, rowTitles(resp.Rows))
}

func TestExecuteWarnsForUnknownFilterPreset(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			FilterPresets: []viewconfig.FilterPreset{{
				ID:      "known",
				Filters: []viewconfig.FilterSpec{{Field: "frontmatter.status", Op: "eq", Value: "ready"}},
			}},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{testRow("docs/a.md", "Alpha", nil)}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{FilterPreset: "missing"})
	require.NoError(t, err)
	require.Len(t, resp.Warnings, 1)
	require.Equal(t, "unknown_filter_preset", resp.Warnings[0].Code)
	require.Equal(t, "filterPreset", resp.Warnings[0].Path)
}

func TestCatalogExcludesHiddenViews(t *testing.T) {
	service := New(ServiceOptions{Views: []viewconfig.ViewDefinition{
		testViewDefinition("visible"),
		func() viewconfig.ViewDefinition {
			def := testViewDefinition("hidden")
			def.Mount.Hidden = true
			return def
		}(),
	}})

	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"visible"}, catalogEntryIDs(catalog.Views))
}

func TestExecuteRejectsInvalidCatalogEntry(t *testing.T) {
	service := New(ServiceOptions{
		Schema: &ontology.Schema{Types: map[string]*ontology.NoteType{"Task": {Name: "Task"}}},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Missing"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			t.Fatal("invalid view should not resolve source")
			return SourceResult{}, nil
		}),
	})

	_, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidView)
}

func TestExecuteComparesNumericFiltersAndSortsNumerically(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{
				testRow("docs/two.md", "Two", map[string]any{"priority": json.Number("2")}),
				testRow("docs/ten.md", "Ten", map[string]any{"priority": json.Number("10")}),
				testRow("docs/one.md", "One", map[string]any{"priority": json.Number("1")}),
			}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "frontmatter.priority", Op: "gt", Value: "1"}},
		Sort:    []viewconfig.SortSpec{{Field: "frontmatter.priority", Direction: "asc"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Two", "Ten"}, rowTitles(resp.Rows))
}

func TestExecuteRejectsUnsupportedVariant(t *testing.T) {
	service := New(ServiceOptions{Views: []viewconfig.ViewDefinition{{
		APIVersion: viewconfig.APIVersion,
		ID:         "work",
		Name:       "Work",
		SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
		Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
		Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
	}}})

	_, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban"})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsupportedVariant)
}

func TestExecuteCardResolvesLayoutFromCapabilities(t *testing.T) {
	view := testViewDefinition("work")
	view.Defaults.Variant = "card"
	view.Variants.Table.Columns = []viewconfig.ViewColumn{
		{Field: "id"},
		{Field: "title"},
		{Field: "status"},
		{Field: "summary"},
		{Field: "owner"},
	}
	view.Variants.Card = &viewconfig.CardSpec{}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			row := testRow("docs/a.md", "Alpha", nil)
			row.Fields["id"] = "TASK-1"
			row.Fields["status"] = "ready"
			row.Fields["summary"] = "First task"
			row.Fields["owner"] = "Drew"
			return SourceResult{
				Rows: []TableRow{row},
				Capabilities: []FieldCapability{
					{Key: "id", Label: "ID", CanonicalField: "id", SemanticRole: "identifier"},
					{Key: "status", Label: "Status", CanonicalField: "status", SemanticRole: "status"},
					{Key: "summary", Label: "Summary", CanonicalField: "summary", SemanticRole: "summary"},
					{Key: "owner", Label: "Owner", CanonicalField: "owner"},
				},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, "card", resp.Variant)
	require.NotNil(t, resp.Card)
	require.Equal(t, &TableColumn{Field: "id", Label: "ID"}, resp.Card.Eyebrow)
	require.Equal(t, TableColumn{Field: "title", Label: "Title"}, resp.Card.Title)
	require.Equal(t, &TableColumn{Field: "summary", Label: "Summary"}, resp.Card.Preview)
	require.Equal(t, []TableColumn{{Field: "status", Label: "Status"}, {Field: "owner", Label: "Owner"}}, resp.Card.Fields)
	require.Nil(t, resp.Board)
}

func TestExecuteCardFingerprintDistinguishesExplicitEmptyFields(t *testing.T) {
	view := testViewDefinition("work")
	view.Variants.Card = &viewconfig.CardSpec{}
	resolver := SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
		return SourceResult{Rows: []TableRow{testRow("docs/a.md", "Alpha", map[string]any{"priority": "high"})}}, nil
	})
	defaultFields, err := New(ServiceOptions{Views: []viewconfig.ViewDefinition{view}, SourceResolver: resolver}).Execute(
		context.Background(), "work", ExecuteRequest{Variant: "card"},
	)
	require.NoError(t, err)
	require.Equal(t, []TableColumn{{Field: "frontmatter.priority", Label: "Priority"}}, defaultFields.Card.Fields)

	view.Variants.Card = &viewconfig.CardSpec{Fields: []viewconfig.ViewColumn{}}
	explicitEmpty, err := New(ServiceOptions{Views: []viewconfig.ViewDefinition{view}, SourceResolver: resolver}).Execute(
		context.Background(), "work", ExecuteRequest{Variant: "card"},
	)
	require.NoError(t, err)
	require.Empty(t, explicitEmpty.Card.Fields)
	require.NotEqual(t, defaultFields.DefinitionFingerprint, explicitEmpty.DefinitionFingerprint)
}

func TestExecuteTableSynthesizesColumnsFromCard(t *testing.T) {
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "cards",
			Name:       "Cards",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants: viewconfig.VariantSet{Card: &viewconfig.CardSpec{
				Eyebrow: "id",
				Title:   "title",
				Preview: "summary",
				Fields:  []viewconfig.ViewColumn{{Field: "status", Label: "State"}, {Field: "id"}},
			}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{testRow("docs/a.md", "Alpha", nil)}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "cards", ExecuteRequest{Variant: "table"})
	require.NoError(t, err)
	require.Equal(t, []TableColumn{
		{Field: "id", Label: "ID"},
		{Field: "title", Label: "Title"},
		{Field: "summary", Label: "Summary"},
		{Field: "status", Label: "State"},
	}, resp.Columns)
	require.Equal(t, []string{"table", "card"}, resp.View.AvailableVariants)
}

func TestExecuteKanbanBuildsBoardWithTotalsAndEmptyColumns(t *testing.T) {
	collapsed := false
	view := testViewDefinition("work")
	view.Defaults.Group = &viewconfig.GroupSpec{
		Field: "status",
		Values: []viewconfig.GroupValueSpec{{
			Value:              "done",
			Label:              "Complete",
			CollapsedByDefault: &collapsed,
		}},
	}
	view.Variants.Card = &viewconfig.CardSpec{Title: "title"}
	view.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "status"}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			rows := []TableRow{
				testRow("docs/a.md", "Alpha", map[string]any{"status": "backlog"}),
				testRow("docs/b.md", "Beta", map[string]any{"status": "backlog"}),
				testRow("docs/c.md", "Charlie", map[string]any{"status": "done"}),
				testRow("docs/d.md", "Delta", nil),
			}
			return SourceResult{
				Rows: rows,
				Capabilities: []FieldCapability{{
					Key:            "frontmatter.status",
					CanonicalField: "status",
					SourceKeys:     []string{"status", "frontmatter.status"},
					SemanticRole:   "status",
					ValueKind:      "enum",
					EnumValues: []FieldEnumValue{
						{Value: "backlog", Label: "Backlog", Rank: 10, Tone: "neutral"},
						{Value: "doing", Label: "Doing", Rank: 20, Tone: "progress"},
						{Value: "done", Label: "Done", Rank: 30, Tone: "success", CollapsedByDefault: true},
					},
					Edit: &EditCapability{Kind: "enum", List: false},
				}},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{
		Variant: "kanban",
		Group:   &viewconfig.GroupSpec{Field: "owner"},
		Page:    PageRequest{First: 1},
		PageSet: true,
	})
	require.NoError(t, err)
	require.Equal(t, "kanban", resp.Variant)
	require.Equal(t, "status", resp.State.Group.Field)
	require.NotNil(t, resp.Card)
	require.NotNil(t, resp.Board)
	require.True(t, resp.Board.Editable)
	require.Equal(t, "frontmatter.status", resp.Board.ColumnField)
	require.Equal(t, 4, resp.PageInfo.Total)
	require.Len(t, resp.Rows, 1)
	require.Equal(t, 2, resp.Groups[0].TotalCount)
	require.Equal(t, 1, resp.Groups[0].Count)
	require.Equal(t, "neutral", resp.Groups[0].Tone)
	require.Equal(t, []BoardColumn{
		{Key: "status=backlog", Value: "backlog", Label: "Backlog", Tone: "neutral", Count: 2, RowStart: 0, RowEnd: 1},
		{Key: "status=doing", Value: "doing", Label: "Doing", Tone: "progress", Count: 0, RowStart: 1, RowEnd: 1},
		{Key: "status=done", Value: "done", Label: "Complete", Tone: "success", Count: 1, RowStart: 1, RowEnd: 1},
		{Key: "status=", Value: "", Label: "(empty)", Count: 1, RowStart: 1, RowEnd: 1},
	}, resp.Board.Columns)

	laterPage, err := service.Execute(context.Background(), "work", ExecuteRequest{
		Variant: "kanban",
		Page:    PageRequest{Offset: 2, First: 1},
		PageSet: true,
	})
	require.NoError(t, err)
	require.Equal(t, []BoardColumn{
		{Key: "status=backlog", Value: "backlog", Label: "Backlog", Tone: "neutral", Count: 2},
		{Key: "status=doing", Value: "doing", Label: "Doing", Tone: "progress"},
		{Key: "status=done", Value: "done", Label: "Complete", Tone: "success", Count: 1, RowEnd: 1},
		{Key: "status=", Value: "", Label: "(empty)", Count: 1, RowStart: 1, RowEnd: 1},
	}, laterPage.Board.Columns)

	defaultPage, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	require.Equal(t, kanbanDefaultPageSize, defaultPage.PageInfo.First)
}

func TestExecuteKanbanUsesConfiguredOrderWithCanonicalFieldAlias(t *testing.T) {
	view := testViewDefinition("work")
	view.Defaults.Group = &viewconfig.GroupSpec{
		Field:  "status",
		Values: []viewconfig.GroupValueSpec{{Value: "done", Order: 5}},
	}
	view.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "status"}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				Rows: []TableRow{
					testRow("docs/a.md", "Ready", map[string]any{"status": "ready"}),
					testRow("docs/b.md", "Done", map[string]any{"status": "done"}),
				},
				Capabilities: []FieldCapability{{
					Key: "frontmatter.status", CanonicalField: "status", SourceKeys: []string{"status", "frontmatter.status"}, ValueKind: "enum",
					EnumValues: []FieldEnumValue{{Value: "ready", Rank: 10}, {Value: "done", Rank: 20}},
				}},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	require.Equal(t, []string{"Done", "Ready"}, rowTitles(resp.Rows))
	require.Equal(t, []string{"done", "ready"}, []string{resp.Board.Columns[0].Value, resp.Board.Columns[1].Value})
	require.Equal(t, []int{0, 1}, []int{resp.Board.Columns[0].RowStart, resp.Board.Columns[1].RowStart})
}

func TestExecuteKanbanPreservesTypedObservedValueOrder(t *testing.T) {
	view := testViewDefinition("work")
	view.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "priority"}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			ten := testRow("docs/ten.md", "Ten", nil)
			ten.Fields["priority"] = 10
			two := testRow("docs/two.md", "Two", nil)
			two.Fields["priority"] = 2
			return SourceResult{Rows: []TableRow{ten, two}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	require.Equal(t, []string{"Two", "Ten"}, rowTitles(resp.Rows))
	require.Equal(t, []BoardColumn{
		{Key: "priority=2", Value: "2", Label: "2", Count: 1, RowEnd: 1},
		{Key: "priority=10", Value: "10", Label: "10", Count: 1, RowStart: 1, RowEnd: 2},
	}, resp.Board.Columns)
}

func TestExecuteKanbanBooleanColumnsAndHideEmpty(t *testing.T) {
	visible := testViewDefinition("visible")
	visible.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "done"}
	hidden := testViewDefinition("hidden")
	hidden.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "done", HideEmptyColumns: true}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{visible, hidden},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			row := testRow("docs/a.md", "Alpha", nil)
			row.Fields["done"] = "TRUE"
			empty := testRow("docs/b.md", "Beta", nil)
			return SourceResult{
				Rows: []TableRow{row, empty},
				Capabilities: []FieldCapability{{
					Key: "done", CanonicalField: "done", ValueKind: "bool",
					Edit: &EditCapability{Kind: "boolean"},
				}},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "visible", ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	require.True(t, resp.Board.Editable)
	require.Equal(t, []BoardColumn{
		{Key: "done=false", Value: "false", Label: "false", Tone: "neutral"},
		{Key: "done=true", Value: "true", Label: "true", Tone: "success", Count: 1, RowEnd: 1},
		{Key: "done=", Value: "", Label: "(empty)", Count: 1, RowStart: 1, RowEnd: 2},
	}, resp.Board.Columns)
	require.Equal(t, "success", resp.Groups[0].Tone)
	require.Empty(t, resp.Groups[1].Tone)

	resp, err = service.Execute(context.Background(), "hidden", ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	require.Equal(t, []BoardColumn{
		{Key: "done=true", Value: "true", Label: "true", Tone: "success", Count: 1, RowEnd: 1},
		{Key: "done=", Value: "", Label: "(empty)", Count: 1, RowStart: 1, RowEnd: 2},
	}, resp.Board.Columns)
}

func TestExecuteKanbanGivesOutOfDomainValuesATrailingColumn(t *testing.T) {
	for _, tc := range []struct {
		name       string
		value      any
		wantValue  string
		capability FieldCapability
	}{
		{
			name:      "enum",
			value:     "stale",
			wantValue: "stale",
			capability: FieldCapability{
				Key: "status", CanonicalField: "status", ValueKind: "enum",
				EnumValues: []FieldEnumValue{{Value: "ready"}, {Value: "done"}},
			},
		},
		{
			name:       "boolean",
			value:      "not-a-boolean",
			wantValue:  "not-a-boolean",
			capability: FieldCapability{Key: "status", CanonicalField: "status", ValueKind: "bool"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := testViewDefinition("work")
			view.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "status"}
			service := New(ServiceOptions{
				Views: []viewconfig.ViewDefinition{view},
				SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
					row := testRow("docs/a.md", "Alpha", nil)
					row.Fields["status"] = tc.value
					return SourceResult{Rows: []TableRow{row}, Capabilities: []FieldCapability{tc.capability}}, nil
				}),
			})

			resp, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban"})
			require.NoError(t, err)
			require.NotNil(t, resp.Board)
			require.Len(t, resp.Board.Columns, 3)
			last := resp.Board.Columns[2]
			require.Equal(t, tc.wantValue, last.Value)
			require.Equal(t, 1, last.Count)
			require.Equal(t, []int{0, 1}, []int{last.RowStart, last.RowEnd})
		})
	}
}

func TestExecuteKanbanIgnoresFilteredOutDomainValues(t *testing.T) {
	view := testViewDefinition("work")
	view.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "status"}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			ready := testRow("docs/a.md", "Keep", nil)
			ready.Fields["status"] = "ready"
			stale := testRow("docs/b.md", "Drop", nil)
			stale.Fields["status"] = "stale"
			return SourceResult{
				Rows: []TableRow{ready, stale},
				Capabilities: []FieldCapability{{
					Key: "status", CanonicalField: "status", ValueKind: "enum",
					EnumValues: []FieldEnumValue{{Value: "ready"}, {Value: "done"}},
				}},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban", Search: "keep"})
	require.NoError(t, err)
	require.NotNil(t, resp.Board)
	require.Equal(t, []BoardColumn{
		{Key: "status=ready", Value: "ready", Label: "ready", Count: 1, RowEnd: 1},
		{Key: "status=done", Value: "done", Label: "done", RowStart: 1, RowEnd: 1},
	}, resp.Board.Columns)
}

func TestExecuteKanbanWarnsForListColumn(t *testing.T) {
	view := testViewDefinition("work")
	view.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "labels"}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			row := testRow("docs/a.md", "Alpha", nil)
			row.Fields["labels"] = []string{"one", "two"}
			return SourceResult{
				Rows: []TableRow{row},
				Capabilities: []FieldCapability{{
					Key: "labels", List: true, cardinalityKnown: true,
				}},
			}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	require.Nil(t, resp.Board)
	require.NotNil(t, resp.Card)
	requireExecuteWarningCode(t, resp.Warnings, "view_kanban_column_unsupported")
}

func TestExecuteKanbanWarnsForInferredListAndMissingColumns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		rows  []TableRow
	}{
		{
			name:  "scalar before named list",
			field: "priority",
			rows: func() []TableRow {
				first := testRow("docs/a.md", "Alpha", nil)
				first.Fields["priority"] = 1
				second := testRow("docs/b.md", "Beta", nil)
				second.Fields["priority"] = []int{2, 3}
				return []TableRow{first, second}
			}(),
		},
		{name: "missing", field: "missing", rows: []TableRow{testRow("docs/a.md", "Alpha", nil)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := testViewDefinition("work")
			view.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: tc.field}
			service := New(ServiceOptions{
				Views: []viewconfig.ViewDefinition{view},
				SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
					return SourceResult{Rows: tc.rows}, nil
				}),
			})

			resp, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban"})
			require.NoError(t, err)
			require.Nil(t, resp.Board)
			requireExecuteWarningCode(t, resp.Warnings, "view_kanban_column_unsupported")
		})
	}
}

func TestExecuteListGroupsFanOutAtEveryLevel(t *testing.T) {
	type teamIDs []int
	view := testViewDefinition("work")
	view.Defaults.Group = &viewconfig.GroupSpec{Fields: []string{"labels", "teams"}}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			first := testRow("docs/a.md", "Alpha", nil)
			first.Fields["labels"] = []string{"red", "blue"}
			first.Fields["teams"] = teamIDs{1, 2}
			second := testRow("docs/b.md", "Beta", nil)
			second.Fields["labels"] = []string{}
			return SourceResult{Rows: []TableRow{first, second}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, 5, resp.PageInfo.Total)
	require.Len(t, resp.Rows, 5)
	empty := tableGroupByValue(resp.Groups, "")
	red := tableGroupByValue(resp.Groups, "red")
	blue := tableGroupByValue(resp.Groups, "blue")
	require.Equal(t, 1, empty.TotalCount)
	require.Equal(t, 2, red.TotalCount)
	require.Equal(t, 2, blue.TotalCount)
	require.Equal(t, 1, tableGroupByValue(red.Children, "1").TotalCount)
	require.Equal(t, 1, tableGroupByValue(red.Children, "2").TotalCount)
}

func tableGroupByValue(groups []TableGroup, value string) TableGroup {
	for _, group := range groups {
		if group.Value == value {
			return group
		}
	}
	return TableGroup{}
}

func testViewDefinition(id string) viewconfig.ViewDefinition {
	return viewconfig.ViewDefinition{
		APIVersion: viewconfig.APIVersion,
		ID:         id,
		Name:       "Work",
		SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
		Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
		Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{
			{Field: "title"},
			{Field: "frontmatter.priority"},
		}}},
	}
}

func testRow(path string, title string, fm map[string]any) TableRow {
	if fm == nil {
		fm = map[string]any{}
	}
	return TableRow{
		Ref:   ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote},
		Path:  path,
		Title: title,
		Fields: map[string]any{
			"title":       title,
			"path":        path,
			"frontmatter": fm,
		},
	}
}

func rowTitles(rows []TableRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Title)
	}
	return out
}

func catalogEntryIDs(entries []CatalogEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.ID)
	}
	return out
}

func requireExecuteWarningCode(t *testing.T, warnings []Warning, code string) {
	t.Helper()
	require.Contains(t, warningCodes(warnings), code)
}

func warningCodes(warnings []Warning) []string {
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, warning.Code)
	}
	return out
}

func TestExecuteKanbanKeepsBoardPageFloorWhenRequestSendsEmptyPage(t *testing.T) {
	view := testViewDefinition("work")
	view.Defaults.First = 100
	view.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "status"}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Capabilities: []FieldCapability{{Key: "status", CanonicalField: "status", ValueKind: "string"}}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban", PageSet: true})
	require.NoError(t, err)
	require.Equal(t, kanbanDefaultPageSize, resp.PageInfo.First)

	resp, err = service.Execute(context.Background(), "work", ExecuteRequest{Variant: "kanban", PageSet: true, Page: PageRequest{First: 25}})
	require.NoError(t, err)
	require.Equal(t, 25, resp.PageInfo.First)
}

func TestExecuteComposesFilterPresetWithManualFilters(t *testing.T) {
	view := testViewDefinition("work")
	view.FilterPresets = []viewconfig.FilterPreset{{ID: "ready", Filters: []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "ready"}}}}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			both := testRow("docs/a.md", "Alpha", nil)
			both.Fields["status"] = "ready"
			presetOnly := testRow("docs/b.md", "Bravo", nil)
			presetOnly.Fields["status"] = "ready"
			manualOnly := testRow("docs/c.md", "Alpha", nil)
			manualOnly.Fields["status"] = "done"
			return SourceResult{Rows: []TableRow{both, presetOnly, manualOnly}, Capabilities: []FieldCapability{{Key: "status", CanonicalField: "status", ValueKind: "string", FilterOps: []string{"eq"}, ResidualFilterOps: []string{"eq"}}, {Key: "title", CanonicalField: "title", ValueKind: "string", FilterOps: []string{"eq"}, ResidualFilterOps: []string{"eq"}}}}, nil
		}),
	})

	resp, err := service.Execute(context.Background(), "work", ExecuteRequest{FilterPreset: "ready", Filters: []viewconfig.FilterSpec{}})
	require.NoError(t, err)
	require.Equal(t, []string{"docs/a.md", "docs/b.md"}, rowPaths(resp.Rows))
	manual := viewconfig.FilterSpec{Field: "title", Op: "eq", Value: "Alpha"}
	resp, err = service.Execute(context.Background(), "work", ExecuteRequest{FilterPreset: "ready", Filters: []viewconfig.FilterSpec{manual}})
	require.NoError(t, err)
	require.Equal(t, []string{"docs/a.md"}, rowPaths(resp.Rows))
	require.Equal(t, []viewconfig.FilterSpec{manual, {Field: "status", Op: "eq", Value: "ready"}}, resp.State.Filters)
}
