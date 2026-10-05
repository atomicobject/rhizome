package viewconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadDefaultSourceSkipsMissingViewsDir(t *testing.T) {
	views, issues := LoadDefaultSource(t.TempDir())
	require.Empty(t, views)
	require.Empty(t, issues)
}

func TestLoadPathReportsMissingExplicitPath(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.yaml")
	views, issues := LoadPath(missingPath)
	require.Empty(t, views)
	requireIssueCode(t, issues, "view_path_error")
}

func TestLoadDefaultSourceLoadsOneYAMLView(t *testing.T) {
	root := t.TempDir()
	viewPath := filepath.Join(root, ".rhizome", "views", "specs.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(viewPath), 0o755))
	writeViewFile(t, viewPath, `apiVersion: rhizome.view.v1
id: specs.default
name: Specs
source:
  kind: ontology_type
  type: TechnicalSpec
mount:
  kind: type
  type: TechnicalSpec
defaults:
  variant: table
variants:
  table:
    columns:
      - field: title
`)

	views, issues := LoadDefaultSource(root)
	require.Empty(t, issues)
	require.Len(t, views, 1)
	require.Equal(t, "specs.default", views[0].ID)
	require.Equal(t, viewPath, views[0].Source.Path)
	require.Equal(t, 1, views[0].Source.Line)
}

func TestLoadDefaultSourceLoadsGroupCollapsedAlias(t *testing.T) {
	root := t.TempDir()
	viewPath := filepath.Join(root, ".rhizome", "views", "actions.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(viewPath), 0o755))
	writeViewFile(t, viewPath, `apiVersion: rhizome.view.v1
id: action-items.open
name: Actions
source:
  kind: ontology_type
  type: ActionItem
mount:
  kind: standalone
defaults:
  group:
    field: done
    values:
      - value: "true"
        label: Done
        collapsed: true
variants:
  table:
    columns:
      - field: title
`)

	views, issues := LoadDefaultSource(root)
	require.Empty(t, issues)
	require.Len(t, views, 1)
	require.NotNil(t, views[0].Defaults.Group)
	require.Len(t, views[0].Defaults.Group.Values, 1)
	require.NotNil(t, views[0].Defaults.Group.Values[0].Collapsed)
	require.True(t, *views[0].Defaults.Group.Values[0].Collapsed)
}

func TestLoadPathSkipsHiddenDirectoriesAndNonYAMLFiles(t *testing.T) {
	root := t.TempDir()
	writeViewFile(t, filepath.Join(root, "visible.yaml"), minimalViewYAML("visible"))
	writeViewFile(t, filepath.Join(root, "notes.txt"), minimalViewYAML("ignored"))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".hidden"), 0o755))
	writeViewFile(t, filepath.Join(root, ".hidden", "hidden.yaml"), minimalViewYAML("hidden"))

	views, issues := LoadPath(root)
	require.Empty(t, issues)
	require.Len(t, views, 1)
	require.Equal(t, "visible", views[0].ID)
}

func TestLoadPathRejectsMultiDocumentYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.yaml")
	writeViewFile(t, path, minimalViewYAML("one")+"---\n"+minimalViewYAML("two"))

	views, issues := LoadPath(path)
	require.Empty(t, views)
	requireIssueCode(t, issues, "multiple_documents_unsupported")
}

func TestLoadPathReportsInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.yaml")
	writeViewFile(t, path, "apiVersion: [")

	views, issues := LoadPath(path)
	require.Empty(t, views)
	requireIssueCode(t, issues, "view_yaml_decode_error")
}

func TestLoadPathIgnoresUnknownForwardCompatibleFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.yaml")
	writeViewFile(t, path, `apiVersion: rhizome.view.v1
id: future
name: Future
source:
  kind: ontology_type
  type: TechnicalSpec
  newerSourceOption: true
mount:
  kind: standalone
  futureRailIcon: table
defaults:
  first: 10
  futureDefault: yes
variants:
  table:
    columns:
      - field: title
  futureVariant:
    enabled: true
`)

	views, issues := LoadPath(path)
	require.Empty(t, issues)
	require.Len(t, views, 1)
	require.Equal(t, "future", views[0].ID)
}

func TestLoadPathDecodesTypedCardAndKanbanVariants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.yaml")
	writeViewFile(t, path, `apiVersion: rhizome.view.v1
id: delivery
name: Delivery
source:
  kind: ontology_type
  type: TechnicalSpec
mount:
  kind: standalone
variants:
  card:
    eyebrow: id
    title: title
    preview: summary
    fields: []
  kanban:
    columnField: status
    hideEmptyColumns: true
    card:
      title: title
      fields:
        - field: owner
`)

	views, issues := LoadPath(path)
	require.Empty(t, issues)
	require.Len(t, views, 1)
	require.Equal(t, &CardSpec{Eyebrow: "id", Title: "title", Preview: "summary", Fields: []ViewColumn{}}, views[0].Variants.Card)
	require.Equal(t, "status", views[0].Variants.Kanban.ColumnField)
	require.True(t, views[0].Variants.Kanban.HideEmptyColumns)
	require.Equal(t, []ViewColumn{{Field: "owner"}}, views[0].Variants.Kanban.Card.Fields)
}

func TestLoadPathKeepsMalformedKnownVariantsForValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "card", body: "  card: [title]\n", code: "invalid_card_variant"},
		{name: "kanban", body: "  kanban: board\n", code: "invalid_kanban_variant"},
		{name: "nested kanban card", body: "  kanban:\n    columnField: status\n    card: [title]\n", code: "invalid_kanban_variant"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "views.yaml")
			writeViewFile(t, path, `apiVersion: rhizome.view.v1
id: malformed
name: Malformed
source:
  kind: ontology_type
  type: TechnicalSpec
mount:
  kind: standalone
variants:
  table:
    columns:
      - field: title
`+tt.body)

			views, loadIssues := LoadPath(path)
			require.Empty(t, loadIssues)
			require.Len(t, views, 1)
			result := Validate(views, ValidateOptions{})
			requireIssueCode(t, result.Issues, tt.code)
		})
	}
}

func minimalViewYAML(id string) string {
	return `apiVersion: rhizome.view.v1
id: ` + id + `
name: Test view
source:
  kind: ontology_type
  type: TechnicalSpec
mount:
  kind: standalone
defaults:
  variant: table
variants:
  table:
    columns:
      - field: title
`
}

func writeViewFile(t *testing.T, path string, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func requireIssueCode(t *testing.T, issues []Issue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("issue code %q not found in %#v", code, issues)
}
