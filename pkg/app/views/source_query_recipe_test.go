package views

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestResolveQueryRecipeSourceReturnsRuntimeQueryErrors(t *testing.T) {
	root := t.TempDir()
	recipeDir := filepath.Join(root, ".rhizome", "query-recipes")
	require.NoError(t, os.MkdirAll(recipeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(recipeDir, "broken.yaml"), []byte(`apiVersion: rhizome.query-recipe.v1
id: broken-runtime
name: Broken runtime
problem: Exercise runtime errors.
inputSpec:
  mode: none
query:
  graphQL: |
    query BrokenRuntime {
      technicalSpec(find: "missing") { path title }
    }
outputContract:
  rowPath: notes.nodes
  empty: missing
adaptationGuidance:
  summary: test
`), 0o644))
	schema := &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			"TechnicalSpec": {
				Name: "TechnicalSpec",
				Role: ontology.TypeRoleNote,
				Fields: []*ontology.Field{
					{Name: "id", Kind: ontology.FieldKindScalar, TypeName: "String"},
				},
			},
		},
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)

	resolver := defaultSourceResolver{opts: ServiceOptions{
		VaultPath:  root,
		Schema:     schema,
		ExecSchema: execSchema,
	}}
	_, err = resolver.ResolveSource(context.Background(), viewconfig.ViewDefinition{
		SourceSpec: viewconfig.SourceSpec{
			Kind:        viewconfig.SourceKindQueryRecipe,
			QueryRecipe: "broken-runtime",
		},
	}, ExecuteRequest{})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Contains(t, err.Error(), "query recipe \"broken-runtime\" failed")
	require.Contains(t, err.Error(), "ontology store is required")
	require.False(t, errors.Is(err, ErrUnsupportedVariant))
}

func TestResolveQueryRecipeSourceDoesNotBlockOnUnrelatedBrokenRecipe(t *testing.T) {
	root := t.TempDir()
	recipeDir := filepath.Join(root, ".rhizome", "query-recipes")
	require.NoError(t, os.MkdirAll(recipeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(recipeDir, "recipes.yaml"), []byte(`apiVersion: rhizome.query-recipe.v1
id: target
name: Target
problem: Execute selected recipe.
inputSpec:
  mode: none
query:
  graphQL: |
    query Target {
      ontology { queryPlan(type: "Missing") { warnings { code } } }
    }
outputContract:
  empty: missing
adaptationGuidance:
  summary: test
---
apiVersion: rhizome.query-recipe.v1
id: unrelated
problem: Broken metadata should warn only.
inputSpec:
  mode: sometimes
query:
  graphQL: |
    query Unrelated {
      missingRoot { path }
    }
outputContract:
  empty: missing
adaptationGuidance:
  summary: test
`), 0o644))
	schema := &ontology.Schema{}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	resolver := defaultSourceResolver{opts: ServiceOptions{
		VaultPath:  root,
		Schema:     schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			Store:      store,
			NoteReader: &obsidian.Note{},
		},
	}}
	result, err := resolver.ResolveSource(context.Background(), viewconfig.ViewDefinition{
		SourceSpec: viewconfig.SourceSpec{
			Kind:        viewconfig.SourceKindQueryRecipe,
			QueryRecipe: "target",
		},
	}, ExecuteRequest{})

	require.NoError(t, err)
	require.Empty(t, result.Rows)
	requireNoWarning(t, result.Warnings, "missing_required_field")
	requireNoWarning(t, result.Warnings, "invalid_input_mode")
	requireWarning(t, result.Warnings, "view_result_path_required")
}

func TestExtractQueryRecipeRowsUsesConfiguredResultPath(t *testing.T) {
	rows, warnings := extractQueryRecipeRowsWithRecipePath(ontologyquery.Result{Data: map[string]any{
		"custom": map[string]any{
			"items": []any{
				map[string]any{"path": "docs/a.md", "title": "A", "resolvedType": "ProductSpec"},
			},
		},
	}}, "custom.items", "")

	require.Empty(t, warnings)
	require.Len(t, rows, 1)
	require.Equal(t, "docs/a.md", rows[0].Path)
	require.Equal(t, "A", rows[0].Title)
	require.Equal(t, "ProductSpec", rows[0].ResolvedType)
}

func TestExtractQueryRecipeRowsRequiresConfiguredResultPath(t *testing.T) {
	rows, warnings := extractQueryRecipeRowsWithRecipePath(ontologyquery.Result{Data: map[string]any{
		"custom": map[string]any{
			"items": []any{
				map[string]any{"path": "docs/a.md", "title": "A"},
			},
		},
	}}, "", "")

	require.Empty(t, rows)
	require.Len(t, warnings, 1)
	require.Equal(t, "view_result_path_required", warnings[0].Code)

	rows, warnings = extractQueryRecipeRowsWithRecipePath(ontologyquery.Result{Data: map[string]any{
		"alpha": []any{map[string]any{"path": "docs/a.md"}},
		"beta":  []any{map[string]any{"path": "docs/b.md"}},
	}}, "", "")
	require.Empty(t, rows)
	require.Len(t, warnings, 1)
	require.Equal(t, "view_result_path_required", warnings[0].Code)

	rows, warnings = extractQueryRecipeRowsWithRecipePath(ontologyquery.Result{Data: map[string]any{
		"notes": map[string]any{
			"edges": []any{
				map[string]any{"path": "docs/a.md", "title": "A"},
			},
		},
	}}, "notes.nodes", "")

	require.Empty(t, rows)
	require.Len(t, warnings, 1)
	require.Equal(t, "view_result_path_not_found", warnings[0].Code)
	require.Contains(t, warnings[0].Message, "notes.nodes")
}

func TestExtractQueryRecipeRowsUsesRecipeDeclaredRowPath(t *testing.T) {
	rows, warnings := extractQueryRecipeRowsWithRecipePath(ontologyquery.Result{Data: map[string]any{
		"notes": map[string]any{
			"nodes": []any{
				map[string]any{"path": "docs/a.md", "title": "A"},
			},
		},
	}}, "", "notes.nodes")

	require.Empty(t, warnings)
	require.Len(t, rows, 1)
	require.Equal(t, "docs/a.md", rows[0].Path)
}

func TestExtractQueryRecipeRowsReportsNonArrayResultPath(t *testing.T) {
	rows, warnings := extractQueryRecipeRowsWithRecipePath(ontologyquery.Result{Data: map[string]any{
		"notes": map[string]any{
			"node": map[string]any{"path": "docs/a.md"},
		},
	}}, "notes.node", "")

	require.Empty(t, rows)
	require.Len(t, warnings, 1)
	require.Equal(t, "view_result_path_not_array", warnings[0].Code)
	require.Contains(t, warnings[0].Message, "notes.node")
}

func TestExtractQueryRecipeRowsPreservesCanonicalNodeRef(t *testing.T) {
	rows, warnings := extractQueryRecipeRowsWithRecipePath(ontologyquery.Result{Data: map[string]any{
		"notes": map[string]any{
			"nodes": []any{
				map[string]any{
					"ref": map[string]any{
						"notePath":   "docs/spec.md",
						"fragment":   "^story-a",
						"nodeId":     "story-a",
						"kind":       "EMBEDDED",
						"typeName":   "UserStory",
						"structural": "abc123",
					},
					"title": "Story A",
				},
			},
		},
	}}, "", "notes.nodes")

	require.Empty(t, warnings)
	require.Len(t, rows, 1)
	require.Equal(t, ontology.NodeRef{
		NotePath:   "docs/spec.md",
		Fragment:   "^story-a",
		NodeID:     "story-a",
		Kind:       ontology.NodeKindEmbedded,
		TypeName:   "UserStory",
		Structural: "abc123",
	}, rows[0].Ref)
	require.Equal(t, "docs/spec.md", rows[0].Path)
	require.Equal(t, "Story A", rows[0].Title)
}

func TestQueryRecipeRowsDeriveSchemaEditCapabilities(t *testing.T) {
	rows, warnings := extractQueryRecipeRowsWithRecipePath(ontologyquery.Result{Data: map[string]any{
		"actionItem": []any{
			map[string]any{
				"ref": map[string]any{
					"notePath": "docs/tasks.md",
					"nodeId":   "docs/tasks.md#item-1",
					"kind":     "EMBEDDED",
					"typeName": "ActionItem",
				},
				"title": "Buy flour",
				"done":  "false",
				"type":  "open",
			},
		},
	}}, "actionItem", "")
	require.Empty(t, warnings)

	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{
		"ActionItem": {
			Name: "ActionItem",
			Fields: []*ontology.Field{
				{Name: "done", Kind: ontology.FieldKindScalar, TypeName: "Boolean", SourceKind: ontology.FieldSourceCheckbox},
				{Name: "due", Kind: ontology.FieldKindScalar, TypeName: "Date", SourceKind: ontology.FieldSourceInline},
			},
		},
	}}
	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: schema}}

	caps := resolver.ontologyEditCapabilitiesForTypes(context.Background(), nil, rowResolvedTypeNames(rows))

	done := capabilityByKey(caps, "done")
	require.NotNil(t, done.Edit)
	require.Equal(t, "boolean", done.Edit.Kind)
	require.Equal(t, "setField", done.Edit.Operation)

	title := capabilityByKey(caps, "title")
	require.NotNil(t, title.Edit)
	require.Equal(t, "text", title.Edit.Kind)

	due := capabilityByKey(caps, "due")
	require.NotNil(t, due.Edit)
	require.Equal(t, "date", due.Edit.Kind)
}

func TestQueryRecipeRowsRemainReadOnlyForUnknownTypes(t *testing.T) {
	rows := []TableRow{{
		Ref:          ontology.NodeRef{NotePath: "docs/report.md", TypeName: "ComputedPacket", Kind: ontology.NodeKindNote},
		Path:         "docs/report.md",
		ResolvedType: "ComputedPacket",
		Fields:       map[string]any{"score": 10},
	}}
	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: &ontology.Schema{Types: map[string]*ontology.NoteType{}}}}

	caps := resolver.ontologyEditCapabilitiesForTypes(context.Background(), nil, rowResolvedTypeNames(rows))

	require.Empty(t, caps)
}

func TestQueryRecipeMixedTypesDropIncompatibleEditors(t *testing.T) {
	rows := []TableRow{
		{Ref: ontology.NodeRef{NotePath: "docs/a.md", TypeName: "Article", Kind: ontology.NodeKindNote}, ResolvedType: "Article"},
		{Ref: ontology.NodeRef{NotePath: "docs/t.md", TypeName: "Task", Kind: ontology.NodeKindNote}, ResolvedType: "Task"},
	}
	schema := &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			"Article": {
				Name: "Article",
				Fields: []*ontology.Field{
					{Name: "status", Kind: ontology.FieldKindEnum, TypeName: "ArticleStatus", SourceKind: ontology.FieldSourceFrontmatter},
				},
			},
			"Task": {
				Name: "Task",
				Fields: []*ontology.Field{
					{Name: "status", Kind: ontology.FieldKindScalar, TypeName: "String", SourceKind: ontology.FieldSourceInline},
				},
			},
		},
		EnumTypes: map[string]*ontology.EnumType{
			"ArticleStatus": {Name: "ArticleStatus", Values: []*ontology.EnumValue{{Name: "draft"}, {Name: "ready"}}},
		},
	}
	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: schema}}

	caps := resolver.ontologyEditCapabilitiesForTypes(context.Background(), nil, rowResolvedTypeNames(rows))

	status := capabilityByKey(caps, "status")
	require.Equal(t, "status", status.Key)
	require.Nil(t, status.Edit)
}

func requireWarning(t *testing.T, warnings []Warning, code string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == code {
			return
		}
	}
	t.Fatalf("warning %q not found in %#v", code, warnings)
}

func requireNoWarning(t *testing.T, warnings []Warning, code string) {
	t.Helper()
	for _, warning := range warnings {
		require.NotEqual(t, code, warning.Code)
	}
}

func TestExtractQueryRecipeRowsWarnsForUnresolvedItems(t *testing.T) {
	rows, warnings := extractQueryRecipeRowsWithRecipePath(ontologyquery.Result{Data: map[string]any{
		"notes": map[string]any{
			"nodes": []any{
				map[string]any{"title": "Missing path"},
				"not-object",
			},
		},
	}}, "", "notes.nodes")

	require.Empty(t, rows)
	require.Len(t, warnings, 1)
	require.Equal(t, "view_source_items_unresolved", warnings[0].Code)
	require.Contains(t, warnings[0].Message, "2 row item")
}
