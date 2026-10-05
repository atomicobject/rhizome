package views

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestCatalogIncludesRepoAuthoredAndGeneratedViews(t *testing.T) {
	root := t.TempDir()
	writeViewConfig(t, root, "standalone.yaml", `apiVersion: rhizome.view.v1
id: planning.backlog
name: Planning backlog
source:
  kind: ontology_type
  type: ProductSpec
mount:
  kind: standalone
  group: Planning
variants:
  table:
    columns:
      - field: title
`)

	service := New(ServiceOptions{
		VaultPath: root,
		Schema: &ontology.Schema{
			Types: map[string]*ontology.NoteType{
				"ProductSpec": {Name: "ProductSpec"},
				"EffortNote":  {Name: "EffortNote"},
			},
			Interfaces: map[string]*ontology.InterfaceType{},
		},
	})

	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.Empty(t, catalog.Issues)
	requireViewIDs(t, catalog.Views, "generated.type.EffortNote.table", "generated.type.ProductSpec.table", "planning.backlog")
	require.False(t, catalog.Views[2].Generated)
}

func TestCatalogGeneratedTypeDefaultsComeFromTheProfile(t *testing.T) {
	service := New(ServiceOptions{
		Schema: &ontology.Schema{
			EnumTypes: map[string]*ontology.EnumType{"SpecStatus": stagedEnum("SpecStatus",
				[2]string{"proposed", "open"}, [2]string{"live", "done"}, [2]string{"archived", "dropped"})},
			Types: map[string]*ontology.NoteType{
				"ProductSpec": {
					Name: "ProductSpec",
					Fields: []*ontology.Field{
						{Name: "summary", Source: "summary", SourceKind: ontology.FieldSourceFrontmatter, Kind: ontology.FieldKindScalar, TypeName: "String"},
						{Name: "id", Source: "id", SourceKind: ontology.FieldSourceFrontmatter, Kind: ontology.FieldKindScalar, TypeName: "String", IsPreferredIdentifier: true},
						keyField(&ontology.Field{Name: "specStatus", Source: "spec-status", SourceKind: ontology.FieldSourceFrontmatter, Kind: ontology.FieldKindEnum, TypeName: "SpecStatus"}),
					},
				},
			},
			Interfaces: map[string]*ontology.InterfaceType{},
		},
	})

	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.Len(t, catalog.Views, 1)
	require.Equal(t, []viewconfig.ViewColumn{
		{Field: "title"},
		{Field: "id", Label: "ID"},
		{Field: "specStatus", Label: "Spec status"},
		{Field: "updatedAt", Label: "Changed"},
	}, catalog.Views[0].Variants.Table.Columns)
	require.Equal(t, &viewconfig.CardSpec{Eyebrow: "id", Title: "title", Preview: "summary"}, catalog.Views[0].Variants.Card)
	require.Equal(t, &viewconfig.KanbanVariant{ColumnField: "specStatus"}, catalog.Views[0].Variants.Kanban)
	require.Equal(t, &viewconfig.GroupSpec{Field: "specStatus"}, catalog.Views[0].Defaults.Group, "a contract groups by its lifecycle")
	require.Equal(t, []viewconfig.SortSpec{{Field: "updatedAt", Direction: "desc"}}, catalog.Views[0].Defaults.Sort)
	require.Equal(t, []string{"table", "kanban", "card"}, catalog.Views[0].AvailableVariants)
}

func TestCatalogGeneratedInterfaceDefaultsUseDeclaredFields(t *testing.T) {
	service := New(ServiceOptions{Schema: &ontology.Schema{
		EnumTypes: map[string]*ontology.EnumType{"EffortStatus": stagedEnum("EffortStatus",
			[2]string{"planned", "open"}, [2]string{"active", "active"}, [2]string{"complete", "done"})},
		Types: map[string]*ontology.NoteType{},
		Interfaces: map[string]*ontology.InterfaceType{
			"Effort": {
				Name: "Effort",
				Fields: []*ontology.Field{
					{Name: "id", Kind: ontology.FieldKindScalar, TypeName: "String", IsIdentifier: true},
					{Name: "name", Kind: ontology.FieldKindScalar, TypeName: "String"},
					keyField(&ontology.Field{Name: "status", Kind: ontology.FieldKindEnum, TypeName: "EffortStatus"}),
					{Name: "summary", Kind: ontology.FieldKindScalar, TypeName: "String"},
				},
			},
		},
	}})

	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.Len(t, catalog.Views, 1)
	require.Equal(t, []viewconfig.ViewColumn{
		{Field: "title"},
		{Field: "id", Label: "ID"},
		{Field: "resolvedType", Label: "Type"},
		{Field: "status", Label: "Status"},
		{Field: "updatedAt", Label: "Changed"},
	}, catalog.Views[0].Variants.Table.Columns)
	require.Equal(t, "status", catalog.Views[0].Variants.Kanban.ColumnField)
}

func TestCatalogPreservesAuthoredActionItemDefaults(t *testing.T) {
	root := t.TempDir()
	writeViewConfig(t, root, "action-items.yaml", `apiVersion: rhizome.view.v1
id: action-items.open
name: Action Items
source:
  kind: ontology_type
  type: ActionItem
mount:
  kind: standalone
defaults:
  variant: table
  first: 100
  sort:
    - field: done
      direction: asc
variants:
  table:
    columns:
      - field: done
      - field: title
`)
	service := New(ServiceOptions{
		VaultPath: root,
		Schema: &ontology.Schema{
			Types: map[string]*ontology.NoteType{
				"ActionItem": {
					Name: "ActionItem",
					Fields: []*ontology.Field{
						{Name: "done", SourceKind: ontology.FieldSourceCheckbox, Kind: ontology.FieldKindScalar, TypeName: "Boolean", Display: ontology.FieldDisplay{Importance: ontology.FieldImportanceDetail}},
						{Name: "owner", SourceKind: ontology.FieldSourceFrontmatter, Kind: ontology.FieldKindLink, TypeName: "Person", Display: ontology.FieldDisplay{Importance: ontology.FieldImportanceKey}},
					},
				},
			},
			Interfaces: map[string]*ontology.InterfaceType{},
		},
	})

	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	requireViewIDs(t, catalog.Views, "action-items.open", "generated.type.ActionItem.table")
	require.Nil(t, catalog.Views[0].Defaults.Group)
	require.Equal(t, 100, catalog.Views[0].Defaults.First)
	require.Equal(t, []viewconfig.ViewColumn{{Field: "done"}, {Field: "title"}}, catalog.Views[0].Variants.Table.Columns)
	require.Nil(t, catalog.Views[1].Defaults.Group, "a Boolean is never a lifecycle")
}

func TestExecutePreservesAuthoredTableColumnOrderAcrossImportance(t *testing.T) {
	view := testViewDefinition("work")
	view.Variants.Table.Columns = []viewconfig.ViewColumn{
		{Field: "summary", Label: "Authored summary"},
		{Field: "title", Label: "Authored title"},
	}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{view},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				Rows: []TableRow{testRow("docs/a.md", "Alpha", map[string]any{
					"summary": "Summary",
					"owner":   "Drew",
					"notes":   "Details",
				})},
				Capabilities: []FieldCapability{
					{Key: "owner", Label: "Owner", Importance: ontology.FieldImportanceKey},
					{Key: "notes", Label: "Notes", Importance: ontology.FieldImportanceDetail},
				},
			}, nil
		}),
	})

	response, err := service.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, []TableColumn{
		{Field: "summary", Label: "Authored summary"},
		{Field: "title", Label: "Authored title"},
	}, response.Columns)
}

func TestExecuteExposesCapabilityValues(t *testing.T) {
	svc := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{
				testRow("docs/a.md", "A", map[string]any{"id": "SPEC-0001", "lastUpdated": "2026-05-01", "specStatus": "active"}),
				testRow("docs/b.md", "B", map[string]any{"id": "SPEC-0002", "lastUpdated": "2026-05-02", "specStatus": "draft"}),
				testRow("docs/c.md", "C", map[string]any{"id": "SPEC-0003", "lastUpdated": "2026-05-03", "specStatus": "active"}),
			}}, nil
		}),
	})

	resp, err := svc.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	var status, id, lastUpdated FieldCapability
	for _, cap := range resp.Capabilities {
		if cap.Key == "frontmatter.specStatus" {
			status = cap
		}
		if cap.Key == "frontmatter.id" {
			id = cap
		}
		if cap.Key == "frontmatter.lastUpdated" {
			lastUpdated = cap
		}
	}
	require.Equal(t, "frontmatter.specStatus", status.Key)
	require.Equal(t, []any{"active", "draft"}, status.Values)
	require.Equal(t, "in", status.FilterOps[0])
	require.Equal(t, SourceUnknown, status.Completeness)
	require.True(t, status.ResidualSortable)
	require.Empty(t, status.IndexedFilterOps)
	require.Contains(t, status.ResidualFilterOps, "contains")
	require.Equal(t, "frontmatter.id", id.Key)
	require.Empty(t, id.Values)
	require.Equal(t, "frontmatter.lastUpdated", lastUpdated.Key)
	require.Empty(t, lastUpdated.Values)
}

func TestExecuteKeepsFilterOpsAsIndexedResidualUnion(t *testing.T) {
	svc := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				Rows: []TableRow{testRow("docs/a.md", "A", map[string]any{"status": "active"})},
				Capabilities: []FieldCapability{{
					Key:               "status",
					Label:             "Status",
					IndexedFilterOps:  []string{"eq", "in"},
					ResidualFilterOps: []string{"contains", "exists"},
					IndexedSortable:   true,
					Sortable:          true,
				}},
				ConstraintPlan: true,
			}, nil
		}),
	})

	resp, err := svc.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	var status FieldCapability
	for _, cap := range resp.Capabilities {
		if cap.Key == "status" {
			status = cap
			break
		}
	}
	require.Equal(t, []string{"eq", "in", "contains", "exists"}, status.FilterOps)
	require.Equal(t, []string{"eq", "in", "contains", "exists"}, status.Filter.Ops)
}

func TestCatalogRetainsGeneratedFallbackForExplicitMountedDefault(t *testing.T) {
	for _, mountType := range []string{"ProductSpec", " ProductSpec "} {
		t.Run(mountType, func(t *testing.T) {
			root := t.TempDir()
			writeViewConfig(t, root, "product.yaml", `apiVersion: rhizome.view.v1
id: product.default
name: Product specs
source:
  kind: ontology_type
  type: ProductSpec
mount:
  kind: type
  type: "`+mountType+`"
  default: true
variants:
  table:
    columns:
      - field: title
`)

			service := New(ServiceOptions{
				VaultPath: root,
				Schema: &ontology.Schema{
					Types:      map[string]*ontology.NoteType{"ProductSpec": {Name: "ProductSpec"}},
					Interfaces: map[string]*ontology.InterfaceType{},
				},
			})
			catalog, err := service.Catalog(context.Background())
			require.NoError(t, err)
			requireViewIDs(t, catalog.Views, "generated.type.ProductSpec.table", "product.default")
		})
	}
}

func TestCatalogReturnsInvalidViewIssuesWithValidSiblings(t *testing.T) {
	root := t.TempDir()
	writeViewConfig(t, root, "valid.yaml", `apiVersion: rhizome.view.v1
id: valid
name: Valid
source:
  kind: ontology_type
  type: ProductSpec
mount:
  kind: standalone
variants:
  table:
    columns:
      - field: title
`)
	writeViewConfig(t, root, "invalid.yaml", `apiVersion: rhizome.view.v1
id: invalid
name: Invalid
source:
  kind: ontology_type
  type: MissingType
mount:
  kind: standalone
variants:
  table:
    columns:
      - field: title
`)

	service := New(ServiceOptions{
		VaultPath: root,
		Schema: &ontology.Schema{
			Types:      map[string]*ontology.NoteType{"ProductSpec": {Name: "ProductSpec"}},
			Interfaces: map[string]*ontology.InterfaceType{},
		},
	})

	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	requireViewIDs(t, catalog.Views, "generated.type.ProductSpec.table", "invalid", "valid")
	requireIssueCode(t, catalog.Issues, "unknown_ontology_type")
}

func TestExecuteAllowsWarningIssuesButBlocksFatalIssues(t *testing.T) {
	warningService := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		LoadIssues: []viewconfig.Issue{{
			Code:     "catalog_diagnostic",
			Severity: viewconfig.IssueWarning,
			View:     "work",
			Message:  "diagnostic only",
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{testRow("docs/a.md", "Alpha", nil)}}, nil
		}),
	})
	resp, err := warningService.Execute(context.Background(), "work", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha"}, rowTitles(resp.Rows))

	fatalService := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		LoadIssues: []viewconfig.Issue{{
			Code:     "fatal_diagnostic",
			Severity: viewconfig.IssueFatal,
			View:     "work",
			Message:  "fatal",
		}},
	})
	_, err = fatalService.Execute(context.Background(), "work", ExecuteRequest{})
	require.ErrorIs(t, err, ErrInvalidView)
}

func TestCatalogIncludesInjectedLoadIssues(t *testing.T) {
	service := New(ServiceOptions{
		Views:      []viewconfig.ViewDefinition{},
		LoadIssues: []viewconfig.Issue{{Code: "view_yaml_decode_error", Path: "bad.yaml", Message: "bad yaml"}},
	})

	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	requireIssueCode(t, catalog.Issues, "view_yaml_decode_error")
}

func writeViewConfig(t *testing.T, root string, name string, body string) {
	t.Helper()
	path := filepath.Join(root, ".rhizome", "views", name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func requireViewIDs(t *testing.T, views []CatalogEntry, ids ...string) {
	t.Helper()
	got := make([]string, 0, len(views))
	for _, view := range views {
		got = append(got, view.ID)
	}
	require.Equal(t, ids, got)
}

func requireIssueCode(t *testing.T, issues []viewconfig.Issue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("issue code %q not found in %#v", code, issues)
}
