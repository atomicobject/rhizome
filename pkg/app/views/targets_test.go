package views

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func customDefinition(id string, mount viewconfig.MountSpec) viewconfig.ViewDefinition {
	return viewconfig.ViewDefinition{APIVersion: viewconfig.APIVersion, ID: id, Name: id, Mount: mount, SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindCustom, Entry: "index.html"}}
}

func findTarget(t *testing.T, catalog Catalog, kind viewconfig.MountKind, name string) ViewTarget {
	t.Helper()
	for _, target := range catalog.Targets {
		if target.Kind == kind && target.Name == name {
			return target
		}
	}
	t.Fatalf("missing target %s/%s", kind, name)
	return ViewTarget{}
}

func TestTargetsResolveExactDefaultsRetainFallbacksAndExcludeInvalidViews(t *testing.T) {
	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{"Effort": {Name: "Effort", Role: ontology.TypeRoleNote, DisplayGroup: "Delivery"}, "Person": {Name: "Person", Role: ontology.TypeRoleNote, DisplayGroup: "People"}}}
	generic := customDefinition("groups", viewconfig.MountSpec{Kind: viewconfig.MountKindGroup, Group: "*", Default: true})
	specific := customDefinition("delivery", viewconfig.MountSpec{Kind: viewconfig.MountKindGroup, Group: "Delivery", Default: true})
	collection := customDefinition("efforts", viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Effort", Default: true})
	node := customDefinition("effort-detail", viewconfig.MountSpec{Kind: viewconfig.MountKindNode, Type: "Effort", Default: true})
	invalid := customDefinition("bad", viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Person", Default: true})
	invalid.SourceSpec.Entry = "../outside.html"
	catalog, err := New(ServiceOptions{Schema: schema, Views: []viewconfig.ViewDefinition{generic, specific, collection, node, invalid}}).Catalog(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, catalog.Issues)
	delivery := findTarget(t, catalog, viewconfig.MountKindGroup, "Delivery")
	require.Equal(t, viewChoiceID("delivery", "custom"), delivery.DefaultChoiceID)
	require.Len(t, delivery.Choices, 3)
	require.Equal(t, viewChoiceID("groups", "custom"), findTarget(t, catalog, viewconfig.MountKindGroup, "People").DefaultChoiceID)
	efforts := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
	require.Equal(t, viewChoiceID("efforts", "custom"), efforts.DefaultChoiceID)
	require.Len(t, efforts.Choices, 2) // custom, generated table; no summary field, so no cards
	person := findTarget(t, catalog, viewconfig.MountKindType, "Person")
	require.Equal(t, viewChoiceID(viewconfig.GeneratedTypeID("Person"), "table"), person.DefaultChoiceID, "no valid authored default opens the generated Table")
	require.Equal(t, viewChoiceID(viewconfig.GeneratedTypeID("Person"), "table"), person.Choices[0].ID, "a type offers no Overview (SPEC-0112)")
	for _, choice := range person.Choices {
		require.NotEqual(t, "bad", choice.ViewID)
	}
	detail := findTarget(t, catalog, viewconfig.MountKindNode, "Effort")
	require.Equal(t, viewChoiceID("effort-detail", "custom"), detail.DefaultChoiceID)
	require.Len(t, detail.Choices, 3)
}

func TestDuplicateDefaultsWarnAndKeepEveryViewSelectable(t *testing.T) {
	mount := viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Effort", Default: true}
	service := New(ServiceOptions{Schema: &ontology.Schema{Types: map[string]*ontology.NoteType{"Effort": {Name: "Effort", Role: ontology.TypeRoleNote}}}, Views: []viewconfig.ViewDefinition{customDefinition("two", mount), customDefinition("one", mount)}})
	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.Len(t, catalog.Issues, 2)
	for _, issue := range catalog.Issues {
		require.Equal(t, "duplicate_mount_default", issue.Code)
		require.Equal(t, viewconfig.IssueWarning, issue.Severity)
	}
	target := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
	require.Equal(t, viewChoiceID("one", "custom"), target.DefaultChoiceID, "first by order, then id, wins")
	var viewIDs []string
	for _, choice := range target.Choices {
		viewIDs = append(viewIDs, choice.ViewID)
	}
	require.Subset(t, viewIDs, []string{"one", "two"})
	for _, id := range []string{"one", "two"} {
		_, err := service.View(context.Background(), id)
		require.NoError(t, err)
	}
}

func TestAuthoredFilteredNativeViewRetainsFullCollectionFallback(t *testing.T) {
	native := testViewDefinition("priority-work")
	native.SourceSpec.Type = "Effort"
	native.Mount = viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Effort", Default: true}
	native.Defaults.Filters = []viewconfig.FilterSpec{{Field: "frontmatter.priority", Op: "eq", Value: "high"}}
	service := New(ServiceOptions{
		Schema: layoutEffortSchema(),
		Views:  []viewconfig.ViewDefinition{native},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{Rows: []TableRow{
				testRow("high.md", "High", map[string]any{"priority": "high"}),
				testRow("low.md", "Low", map[string]any{"priority": "low"}),
			}}, nil
		}),
	})
	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	target := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
	require.Equal(t, viewChoiceID(native.ID, "table"), target.DefaultChoiceID)
	require.Len(t, target.Choices, 4)
	generatedID := viewconfig.GeneratedTypeID("Effort")
	require.Equal(t, ViewChoice{ID: viewChoiceID(generatedID, "table"), Name: "Table", Renderer: "table", ViewID: generatedID, Variant: "table"}, target.Choices[0])
	require.Equal(t, "card", target.Choices[1].Renderer)
	require.Equal(t, "kanban", target.Choices[2].Renderer)
	require.Equal(t, ViewChoice{ID: viewChoiceID(native.ID, "table"), Name: "Work · Table", Renderer: "table", ViewID: native.ID, Variant: "table", Custom: true}, target.Choices[3])
	authored, err := service.Execute(context.Background(), native.ID, ExecuteRequest{})
	require.NoError(t, err)
	require.Len(t, authored.Rows, 1)
	require.Equal(t, "high.md", authored.Rows[0].Path)
	full, err := service.Execute(context.Background(), generatedID, ExecuteRequest{})
	require.NoError(t, err)
	require.Len(t, full.Rows, 2)
}

func TestHiddenViewsRemainDirectlyAddressable(t *testing.T) {
	def := customDefinition("hidden", viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone, Hidden: true})
	service := New(ServiceOptions{Views: []viewconfig.ViewDefinition{def}})
	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.Empty(t, catalog.Views)
	require.Empty(t, catalog.Targets)
	view, err := service.View(context.Background(), "hidden")
	require.NoError(t, err)
	require.Equal(t, "hidden", view.ID)
}

func TestChoiceIDsPreserveDelimiterIdentity(t *testing.T) {
	require.NotEqual(t, viewChoiceID("a:b", "c"), viewChoiceID("a", "b:c"))
}

func TestStandaloneTargetUsesConfiguredVariant(t *testing.T) {
	def := testViewDefinition("board")
	def.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: "status"}
	def.Defaults.Variant = "kanban"
	catalog, err := New(ServiceOptions{Views: []viewconfig.ViewDefinition{def}}).Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, viewChoiceID("board", "kanban"), findTarget(t, catalog, viewconfig.MountKindStandalone, "board").DefaultChoiceID)
}

func TestGeneratedIDCollisionCannotDisableFallback(t *testing.T) {
	id := viewconfig.GeneratedTypeID("Effort")
	collision := customDefinition(id, viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Effort", Default: true})
	service := New(ServiceOptions{Schema: &ontology.Schema{Types: map[string]*ontology.NoteType{"Effort": {Name: "Effort"}}}, Views: []viewconfig.ViewDefinition{collision}})
	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, catalog.Issues)
	target := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
	require.Len(t, target.Choices, 1)
	require.Equal(t, "table", target.Choices[0].Renderer)
	entry, err := service.View(context.Background(), id)
	require.NoError(t, err)
	require.True(t, entry.Generated)
	require.Empty(t, entry.Issues)
}

func TestInvalidConfigurationDoesNotBreakCatalogSerialization(t *testing.T) {
	def := customDefinition("bad-config", viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone})
	def.Configuration = map[string]any{"bad": make(chan int)}
	catalog, err := New(ServiceOptions{Views: []viewconfig.ViewDefinition{def}}).Catalog(context.Background())
	require.NoError(t, err)
	require.Empty(t, catalog.Targets)
	require.Equal(t, "invalid_view_configuration", catalog.Issues[0].Code)
	_, err = json.Marshal(catalog)
	require.NoError(t, err)
}

func TestAuthoredGeneratedFlagCannotBypassInvalidityOrReserveFallbackIDs(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			id := "invalid"
			if collision {
				id = viewconfig.GeneratedTypeID("Effort")
			}
			def := customDefinition(id, viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Effort", Default: true})
			def.Generated = true
			def.SourceSpec.Entry = "../outside.html"
			service := New(ServiceOptions{Schema: &ontology.Schema{Types: map[string]*ontology.NoteType{"Effort": {Name: "Effort"}}}, Views: []viewconfig.ViewDefinition{def}})
			catalog, err := service.Catalog(context.Background())
			require.NoError(t, err)
			target := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
			require.Equal(t, viewChoiceID(viewconfig.GeneratedTypeID("Effort"), "table"), target.DefaultChoiceID)
			require.Len(t, target.Choices, 1)
			for _, choice := range target.Choices {
				require.NotEqual(t, "custom", choice.Renderer)
			}
			fallback, err := service.View(context.Background(), viewconfig.GeneratedTypeID("Effort"))
			require.NoError(t, err)
			require.True(t, fallback.Generated)
			require.Empty(t, fallback.Issues)
		})
	}
}

func TestMissingIDDefaultCannotHideValidSiblingsOrFallbacks(t *testing.T) {
	for _, idField := range []string{"", "id: \"   \"\n"} {
		t.Run(fmt.Sprintf("field=%q", idField), func(t *testing.T) {
			root := t.TempDir()
			body := `apiVersion: rhizome.view.v1
name: Dashboard
source:
  kind: custom
  entry: index.html
mount:
  kind: type
  type: Effort
  default: true
`
			writeViewConfig(t, root, "missing.yaml", idField+body)
			writeViewConfig(t, root, "valid.yaml", "id: valid\n"+body)
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "views", "index.html"), []byte("<!doctype html><title>Dashboard</title>"), 0o644))
			catalog, err := New(ServiceOptions{VaultPath: root, Schema: &ontology.Schema{Types: map[string]*ontology.NoteType{"Effort": {Name: "Effort"}}}}).Catalog(context.Background())
			require.NoError(t, err)
			target := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
			require.Equal(t, viewChoiceID("valid", "custom"), target.DefaultChoiceID)
			require.Len(t, target.Choices, 2)
			for _, choice := range target.Choices {
				require.NotEqual(t, viewChoiceID("", "custom"), choice.ID)
			}
			for _, entry := range catalog.Views {
				if entry.ID == "" {
					require.NotEmpty(t, entry.Issues)
				}
				if entry.ID == "valid" {
					require.Empty(t, entry.Issues)
				}
			}
		})
	}
}

func TestUnconfiguredTargetsOpenGeneratedTableAndNodeTargetsNeedNodeTypes(t *testing.T) {
	schema := &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			"Effort":  {Name: "Effort", Role: ontology.TypeRoleNote, DisplayGroup: "Delivery", Implements: []string{"Work"}},
			"Spec":    {Name: "Spec", Role: ontology.TypeRoleNote, Implements: []string{"Work"}},
			"Heading": {Name: "Heading", Role: ontology.TypeRoleSection},
		},
		Interfaces: map[string]*ontology.InterfaceType{"Work": {Name: "Work"}},
	}
	catalog, err := New(ServiceOptions{Schema: schema}).Catalog(context.Background())
	require.NoError(t, err)
	effort := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
	require.Equal(t, viewChoiceID(viewconfig.GeneratedTypeID("Effort"), "table"), effort.DefaultChoiceID)
	require.Equal(t, viewChoiceID(viewconfig.GeneratedTypeID("Effort"), "table"), effort.Choices[0].ID, "a type offers no Overview")
	work := findTarget(t, catalog, viewconfig.MountKindInterface, "Work")
	require.Equal(t, viewChoiceID(viewconfig.GeneratedInterfaceID("Work"), "table"), work.DefaultChoiceID)
	require.Equal(t, "builtin:overview", findTarget(t, catalog, viewconfig.MountKindGroup, "Other").DefaultChoiceID)
	require.Equal(t, "builtin:read", findTarget(t, catalog, viewconfig.MountKindNode, "Effort").DefaultChoiceID)
	for _, target := range catalog.Targets {
		require.False(t, target.Kind == viewconfig.MountKindNode && target.Name == "Heading", "section types are not node targets")
	}
}

func choiceSummary(target ViewTarget) []string {
	var out []string
	for _, choice := range target.Choices {
		out = append(out, fmt.Sprintf("%s|%s|%v", choice.Name, choice.ViewID, choice.Custom))
	}
	return out
}

func replacementCatalog(t *testing.T, defs ...viewconfig.ViewDefinition) (*Service, Catalog) {
	t.Helper()
	service := New(ServiceOptions{Schema: layoutEffortSchema(), Views: defs})
	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	return service, catalog
}

// layoutEffortSchema gives Effort a staged lifecycle and a summary, so its
// generated view offers Table, Cards, and Board (SPEC-0112).
func layoutEffortSchema() *ontology.Schema {
	return &ontology.Schema{
		EnumTypes: map[string]*ontology.EnumType{"EffortStatus": stagedEnum("EffortStatus",
			[2]string{"planned", "open"}, [2]string{"active", "active"}, [2]string{"complete", "done"})},
		Types: map[string]*ontology.NoteType{"Effort": {Name: "Effort", Role: ontology.TypeRoleNote, Fields: []*ontology.Field{
			keyField(&ontology.Field{Name: "status", Kind: ontology.FieldKindEnum, TypeName: "EffortStatus"}),
			{Name: "summary", Kind: ontology.FieldKindScalar, TypeName: "String"},
		}}},
	}
}

func nativeEffortView(id, name string, order int, replace bool) viewconfig.ViewDefinition {
	def := testViewDefinition(id)
	def.Name = name
	def.SourceSpec.Type = "Effort"
	def.Mount = viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Effort", Order: order, ReplaceGenerated: replace}
	def.Variants.Card = &viewconfig.CardSpec{Fields: []viewconfig.ViewColumn{{Field: "title"}}}
	return def
}

func TestReplacingViewTakesGeneratedSlot(t *testing.T) {
	replacing := nativeEffortView("ip", "IP ideas", 0, true)
	replacing.Defaults.Variant = "card"
	other := nativeEffortView("other", "Other", 1, false)
	dashboard := customDefinition("dashboard", viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Effort", Order: 2})
	service, catalog := replacementCatalog(t, dashboard, other, replacing)
	require.Empty(t, catalog.Issues)
	target := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
	require.Equal(t, []string{
		"Table|ip|false",
		"Cards|ip|false",
		"Other · Table|other|true",
		"Other · Cards|other|true",
		"dashboard|dashboard|true",
	}, choiceSummary(target), "the generated view contributes no choices")
	require.Equal(t, viewChoiceID("ip", "card"), target.DefaultChoiceID, "the replacing view inherits the generated default role")
	generated, err := service.View(context.Background(), viewconfig.GeneratedTypeID("Effort"))
	require.NoError(t, err, "the replaced generated view stays addressable")
	require.True(t, generated.Generated)
}

func TestReplacingViewYieldsToExplicitAuthoredDefault(t *testing.T) {
	replacing := nativeEffortView("ip", "IP ideas", 0, true)
	other := nativeEffortView("other", "Other", 1, false)
	other.Mount.Default = true
	_, catalog := replacementCatalog(t, replacing, other)
	require.Equal(t, viewChoiceID("other", "table"), findTarget(t, catalog, viewconfig.MountKindType, "Effort").DefaultChoiceID)
}

func TestWithoutReplacementGeneratedLayoutsComeFirst(t *testing.T) {
	_, catalog := replacementCatalog(t, nativeEffortView("other", "Other", 0, false))
	target := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
	generatedID := viewconfig.GeneratedTypeID("Effort")
	require.Equal(t, []string{
		"Table|" + generatedID + "|false",
		"Cards|" + generatedID + "|false",
		"Board|" + generatedID + "|false",
		"Other · Table|other|true",
		"Other · Cards|other|true",
	}, choiceSummary(target))
	require.Equal(t, viewChoiceID(generatedID, "table"), target.DefaultChoiceID)
}

func TestDuplicateReplacingViewsWarnAndFirstReplaces(t *testing.T) {
	_, catalog := replacementCatalog(t, nativeEffortView("b", "B", 0, true), nativeEffortView("a", "A", 0, true))
	require.Len(t, catalog.Issues, 2)
	for _, issue := range catalog.Issues {
		require.Equal(t, "duplicate_replace_generated", issue.Code)
		require.Equal(t, viewconfig.IssueWarning, issue.Severity)
	}
	target := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
	require.Equal(t, []string{"Table|a|false", "Cards|a|false", "B · Table|b|true", "B · Cards|b|true"}, choiceSummary(target))
	require.Equal(t, viewChoiceID("a", "table"), target.DefaultChoiceID)
}

func TestIgnoredReplaceGeneratedKeepsGeneratedLayouts(t *testing.T) {
	custom := customDefinition("dashboard", viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Effort", ReplaceGenerated: true})
	standalone := nativeEffortView("board", "Board view", 0, true)
	standalone.Mount = viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone, ReplaceGenerated: true}
	node := customDefinition("detail", viewconfig.MountSpec{Kind: viewconfig.MountKindNode, Type: "Effort"})
	_, catalog := replacementCatalog(t, custom, standalone, node)
	require.Len(t, catalog.Issues, 2)
	for _, issue := range catalog.Issues {
		require.Equal(t, "mount.replaceGenerated", issue.Field)
		require.Equal(t, viewconfig.IssueWarning, issue.Severity)
	}
	target := findTarget(t, catalog, viewconfig.MountKindType, "Effort")
	require.Equal(t, viewChoiceID(viewconfig.GeneratedTypeID("Effort"), "table"), target.DefaultChoiceID)
	require.Equal(t, "dashboard|dashboard|true", choiceSummary(target)[3])
	require.Equal(t, []string{"Table|board|false", "Cards|board|false"}, choiceSummary(findTarget(t, catalog, viewconfig.MountKindStandalone, "board")),
		"a standalone view's layouts are its standard choices")
	require.Equal(t, []string{"Structured||false", "Source||false", "detail|detail|true"}, choiceSummary(findTarget(t, catalog, viewconfig.MountKindNode, "Effort")))
}

func TestDefaultVariantFallsBackToFirstListedLayout(t *testing.T) {
	// Choices list Table, Cards, Board, so a view without its declared variant
	// opens on its first button.
	require.Equal(t, "card", defaultVariant(CatalogEntry{AvailableVariants: []string{"kanban", "card"}}))
}
