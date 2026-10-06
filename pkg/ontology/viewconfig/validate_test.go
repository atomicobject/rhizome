package viewconfig

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireIssueField(t *testing.T, issues []Issue, code, field string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code && issue.Field == field {
			return
		}
	}
	t.Fatalf("issue (%s, %s) not found in %#v", code, field, issues)
}

func TestValidateReportsMetadataAndDuplicateIssues(t *testing.T) {
	views := []ViewDefinition{
		{
			APIVersion: APIVersion,
			ID:         "dupe",
			Name:       "One",
			SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
			Mount:      MountSpec{Kind: MountKindStandalone},
			Variants:   VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
			Source:     Source{Path: "a.yaml", Line: 1},
		},
		{
			APIVersion: "old.version",
			ID:         "dupe",
			Mount:      MountSpec{Kind: "sometimes"},
			Source:     Source{Path: "b.yaml", Line: 1},
		},
	}

	result := Validate(views, ValidateOptions{})
	require.Len(t, result.Views, 2)
	requireIssueCode(t, result.Issues, "duplicate_view_id")
	requireIssueCode(t, result.Issues, "unsupported_api_version")
	requireIssueCode(t, result.Issues, "missing_required_field")
	requireIssueCode(t, result.Issues, "unsupported_mount_kind")
	for _, issue := range result.Issues {
		require.Equal(t, IssueFatal, issue.Severity)
	}
}

func TestValidateRejectsInvalidIDAndMissingTableVariant(t *testing.T) {
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         "bad id",
		Name:       "Bad ID",
		SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
		Mount:      MountSpec{Kind: MountKindStandalone},
	}}, ValidateOptions{})

	requireIssueCode(t, result.Issues, "invalid_view_id")
	requireIssueCode(t, result.Issues, "missing_table_variant")
}

func TestValidateChecksSchemaAndRecipeTargets(t *testing.T) {
	result := Validate([]ViewDefinition{
		{
			APIVersion: APIVersion,
			ID:         "missing-type",
			Name:       "Missing type",
			SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "MissingType"},
			Mount:      MountSpec{Kind: MountKindType, Type: "MissingType"},
			Variants:   VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
		},
		{
			APIVersion: APIVersion,
			ID:         "missing-interface",
			Name:       "Missing interface",
			SourceSpec: SourceSpec{Kind: SourceKindOntologyInterface, Interface: "MissingInterface"},
			Mount:      MountSpec{Kind: MountKindInterface, Interface: "MissingInterface"},
			Variants:   VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
		},
		{
			APIVersion: APIVersion,
			ID:         "missing-recipe",
			Name:       "Missing recipe",
			SourceSpec: SourceSpec{Kind: SourceKindQueryRecipe, QueryRecipe: "missing-recipe"},
			Mount:      MountSpec{Kind: MountKindStandalone},
			Variants:   VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
		},
	}, ValidateOptions{
		TypeNames:       map[string]struct{}{"TechnicalSpec": {}},
		InterfaceNames:  map[string]struct{}{"SpecLike": {}},
		QueryRecipeIDs:  map[string]struct{}{"existing-recipe": {}},
		CheckReferences: true,
	})

	requireIssueCode(t, result.Issues, "unknown_ontology_type")
	requireIssueCode(t, result.Issues, "unknown_ontology_interface")
	requireIssueCode(t, result.Issues, "unknown_query_recipe")
	requireIssueField(t, result.Issues, "invalid_mount_target", "mount.type")
	requireIssueField(t, result.Issues, "invalid_mount_target", "mount.interface")
}

func TestValidateReportsGeneratedIDCollisions(t *testing.T) {
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         GeneratedTypeID("TechnicalSpec"),
		Name:       "Collision",
		SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
		Mount:      MountSpec{Kind: MountKindStandalone},
		Variants:   VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
	}}, ValidateOptions{GeneratedIDs: GeneratedIDs([]string{"TechnicalSpec"}, nil)})

	requireIssueCode(t, result.Issues, "duplicate_generated_view_id")
}

func TestValidateRejectsConflictingSourceAndMountFields(t *testing.T) {
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         "conflicting",
		Name:       "Conflicting",
		SourceSpec: SourceSpec{
			Kind:        SourceKindOntologyType,
			Type:        "TechnicalSpec",
			QueryRecipe: "wrong",
		},
		Mount:    MountSpec{Kind: MountKindStandalone, Type: "TechnicalSpec"},
		Variants: VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
	}}, ValidateOptions{})

	requireIssueField(t, result.Issues, "unexpected_field", "source.queryRecipe")
	requireIssueField(t, result.Issues, "unexpected_field", "mount.type")
}

func TestValidateNormalizesDefaults(t *testing.T) {
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         "specs",
		Name:       "Specs",
		Generated:  true,
		SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
		Mount:      MountSpec{Kind: MountKindType, Type: "TechnicalSpec"},
		Variants:   VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
	}}, ValidateOptions{})

	require.Empty(t, result.Issues)
	require.Equal(t, "table", result.Views[0].Defaults.Variant)
	require.Equal(t, 200, result.Views[0].Defaults.First)
	require.Equal(t, []SortSpec{{Field: "title", Direction: "asc"}}, result.Views[0].Defaults.Sort)
}

func TestValidateRejectsDefaultStateThatCannotExecute(t *testing.T) {
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         "bad-defaults",
		Name:       "Bad defaults",
		SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
		Mount:      MountSpec{Kind: MountKindStandalone},
		Defaults: DefaultsSpec{
			SourceCap: -1,
			Filters: []FilterSpec{
				{Field: "frontmatter.status", Op: "near", Value: "ready"},
				{Field: "frontmatter.kind", Op: "in"},
			},
			Sort:  []SortSpec{{Field: "title", Direction: "sideways"}},
			Group: &GroupSpec{},
		},
		Variants: VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
	}}, ValidateOptions{})

	requireIssueCode(t, result.Issues, "unsupported_filter_operator")
	requireIssueCode(t, result.Issues, "missing_required_field")
	requireIssueField(t, result.Issues, "missing_required_field", "defaults.filters.values")
	requireIssueField(t, result.Issues, "missing_required_field", "defaults.group.fields")
	requireIssueCode(t, result.Issues, "unsupported_sort_direction")
	requireIssueCode(t, result.Issues, "invalid_source_cap")
}

func TestValidateAcceptsGroupValueSpecs(t *testing.T) {
	collapsed := true
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         "grouped",
		Name:       "Grouped",
		SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
		Mount:      MountSpec{Kind: MountKindStandalone},
		Defaults: DefaultsSpec{
			Group: &GroupSpec{
				Fields: []string{"status", "owner"},
				Values: []GroupValueSpec{{
					Field:              "status",
					Value:              "done",
					Label:              "Finished",
					Order:              20,
					CollapsedByDefault: &collapsed,
				}},
			},
		},
		Variants: VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
	}}, ValidateOptions{})

	require.Empty(t, result.Issues)
	require.Equal(t, "Finished", result.Views[0].Defaults.Group.Values[0].Label)
}

func TestValidateRejectsAmbiguousGroupValueSpecs(t *testing.T) {
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         "grouped",
		Name:       "Grouped",
		SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
		Mount:      MountSpec{Kind: MountKindStandalone},
		Defaults: DefaultsSpec{
			Group: &GroupSpec{
				Fields: []string{"status", "owner"},
				Values: []GroupValueSpec{{Value: "done"}},
			},
		},
		Variants: VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
	}}, ValidateOptions{})

	requireIssueCode(t, result.Issues, "missing_required_field")
}

func TestValidateRecipeRowPathContract(t *testing.T) {
	cases := []struct {
		name        string
		resultPath  string
		recipeRow   string
		expectIssue bool
	}{
		{name: "result_path_set", resultPath: "notes.nodes", recipeRow: "", expectIssue: false},
		{name: "recipe_row_path_set", resultPath: "", recipeRow: "notes.nodes", expectIssue: false},
		{name: "both_set", resultPath: "notes.nodes", recipeRow: "alt.path", expectIssue: false},
		{name: "both_empty", resultPath: "", recipeRow: "", expectIssue: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			views := []ViewDefinition{{
				APIVersion: APIVersion,
				ID:         "recipe-view",
				Name:       "Recipe view",
				SourceSpec: SourceSpec{
					Kind:        SourceKindQueryRecipe,
					QueryRecipe: "recipe-id",
					ResultPath:  tc.resultPath,
				},
				Mount:    MountSpec{Kind: MountKindStandalone},
				Variants: VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}},
			}}
			result := Validate(views, ValidateOptions{
				QueryRecipeIDs:      map[string]struct{}{"recipe-id": {}},
				QueryRecipeRowPaths: map[string]string{"recipe-id": tc.recipeRow},
				CheckReferences:     true,
			})
			if tc.expectIssue {
				requireIssueCode(t, result.Issues, "missing_recipe_row_path")
				for _, issue := range result.Issues {
					if issue.Code == "missing_recipe_row_path" {
						require.Equal(t, IssueWarning, issue.Severity)
					}
				}
			} else {
				for _, issue := range result.Issues {
					require.NotEqual(t, "missing_recipe_row_path", issue.Code, "unexpected issue: %#v", issue)
				}
			}
		})
	}
}

func TestValidateKeepsDeclaredCardDefault(t *testing.T) {
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         "future-default",
		Name:       "Future default",
		SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
		Mount:      MountSpec{Kind: MountKindStandalone},
		Defaults:   DefaultsSpec{Variant: "card"},
		Variants: VariantSet{
			Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}},
			Card:  &CardSpec{Title: "title"},
		},
	}}, ValidateOptions{})

	require.Empty(t, result.Issues)
	require.Equal(t, "card", result.Views[0].Defaults.Variant)
}

func TestValidateFallsBackUnknownDefaultVariantToSynthesizedTable(t *testing.T) {
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         "future-default",
		Name:       "Future default",
		SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
		Mount:      MountSpec{Kind: MountKindStandalone},
		Defaults:   DefaultsSpec{Variant: "timeline"},
		Variants:   VariantSet{Card: &CardSpec{Title: "title"}},
	}}, ValidateOptions{})

	require.Empty(t, result.Issues)
	require.Equal(t, "table", result.Views[0].Defaults.Variant)
}

func TestValidateAcceptsCardAndKanbanWithoutDeclaredTable(t *testing.T) {
	result := Validate([]ViewDefinition{
		{
			APIVersion: APIVersion,
			ID:         "cards",
			Name:       "Cards",
			SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
			Mount:      MountSpec{Kind: MountKindStandalone},
			Defaults:   DefaultsSpec{Variant: "card"},
			Variants:   VariantSet{Card: &CardSpec{}},
		},
		{
			APIVersion: APIVersion,
			ID:         "board",
			Name:       "Board",
			SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
			Mount:      MountSpec{Kind: MountKindStandalone},
			Defaults:   DefaultsSpec{Variant: "kanban"},
			Variants:   VariantSet{Kanban: &KanbanVariant{ColumnField: "status"}},
		},
	}, ValidateOptions{})

	require.Empty(t, result.Issues)
	require.Equal(t, "card", result.Views[0].Defaults.Variant)
	require.Equal(t, "kanban", result.Views[1].Defaults.Variant)
}

func TestValidateReportsKanbanWithoutColumnAndUnsupportedDefaultWithoutFallback(t *testing.T) {
	result := Validate([]ViewDefinition{{
		APIVersion: APIVersion,
		ID:         "board",
		Name:       "Board",
		SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
		Mount:      MountSpec{Kind: MountKindStandalone},
		Defaults:   DefaultsSpec{Variant: "kanban"},
		Variants:   VariantSet{Kanban: &KanbanVariant{}},
	}}, ValidateOptions{})

	requireIssueCode(t, result.Issues, "missing_kanban_column_field")
	requireIssueCode(t, result.Issues, "missing_table_variant")
	requireIssueCode(t, result.Issues, "unsupported_variant")
}

func TestValidateUsesVariantCodesForMalformedCardFields(t *testing.T) {
	for _, tc := range []struct {
		name     string
		variants VariantSet
		code     string
	}{
		{
			name:     "card",
			variants: VariantSet{Card: &CardSpec{Fields: []ViewColumn{{Label: "Owner"}}}},
			code:     "invalid_card_variant",
		},
		{
			name: "kanban card",
			variants: VariantSet{Kanban: &KanbanVariant{
				ColumnField: "status",
				Card:        &CardSpec{Fields: []ViewColumn{{Label: "Owner"}}},
			}},
			code: "invalid_kanban_variant",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := Validate([]ViewDefinition{{
				APIVersion: APIVersion,
				ID:         "malformed",
				Name:       "Malformed",
				SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "TechnicalSpec"},
				Mount:      MountSpec{Kind: MountKindStandalone},
				Variants:   tc.variants,
			}}, ValidateOptions{})

			requireIssueCode(t, result.Issues, tc.code)
			requireIssueCode(t, result.Issues, "missing_table_variant")
		})
	}
}

func TestValidateMountGroupOnlyForGroupAndStandaloneMounts(t *testing.T) {
	for _, mount := range []MountSpec{
		{Kind: MountKindType, Type: "Doc", Group: "Custom"},
		{Kind: MountKindNode, Type: "Doc", Group: "Custom"},
		{Kind: MountKindInterface, Interface: "Shared", Group: "Custom"},
	} {
		def := ViewDefinition{APIVersion: APIVersion, ID: "v", Name: "V", SourceSpec: SourceSpec{Kind: SourceKindCustom, Entry: "index.html"}, Mount: mount}
		issues := Validate([]ViewDefinition{def}, ValidateOptions{}).Issues
		// Earlier v1 files may carry a stray group; it is ignored with a warning.
		require.Equal(t, []Issue{issueWithSeverity(def, "unexpected_field", "mount.group", issues[0].Message, IssueWarning)}, issues)
		require.Contains(t, issues[0].Message, "ignored")
	}
	// Standalone views use mount.group as their rail-section heading.
	standalone := ViewDefinition{APIVersion: APIVersion, ID: "v", Name: "V", SourceSpec: SourceSpec{Kind: SourceKindCustom, Entry: "index.html"}, Mount: MountSpec{Kind: MountKindStandalone, Group: "Custom"}}
	for _, issue := range Validate([]ViewDefinition{standalone}, ValidateOptions{}).Issues {
		require.NotEqual(t, "mount.group", issue.Field)
	}
}

func TestValidateNodeMountsRequireNodeTypes(t *testing.T) {
	opts := ValidateOptions{CheckReferences: true, TypeNames: map[string]struct{}{"Doc": {}, "Heading": {}}, NodeTypeNames: map[string]struct{}{"Doc": {}}}
	for typeName, valid := range map[string]bool{"Doc": true, "Heading": false} {
		def := ViewDefinition{APIVersion: APIVersion, ID: "v", Name: "V", SourceSpec: SourceSpec{Kind: SourceKindCustom, Entry: "index.html"}, Mount: MountSpec{Kind: MountKindNode, Type: typeName}}
		issues := Validate([]ViewDefinition{def}, opts).Issues
		if valid {
			require.Empty(t, issues)
		} else {
			requireIssueField(t, issues, "invalid_mount_target", "mount.type")
		}
	}
}

func TestLoadedConfigurationAcceptsNonStringYAMLKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.yaml")
	writeViewFile(t, path, `apiVersion: rhizome.view.v1
id: keys
name: Keys
source:
  kind: custom
  entry: index.html
mount:
  kind: standalone
configuration:
  1: top
  nested:
    2026: year
    true: yes
    rows:
      - 3: three
    2026-01-02: date
    2026-01-02T10:30:00+02:00: instant
    ~: none
`)
	views, loadIssues := LoadPath(path)
	require.Empty(t, loadIssues)
	result := Validate(views, ValidateOptions{})
	for _, issue := range result.Issues {
		require.NotEqual(t, "invalid_view_configuration", issue.Code)
	}
	encoded, err := json.Marshal(result.Views[0].Configuration)
	require.NoError(t, err)
	require.JSONEq(t, `{"1":"top","nested":{"2026":"year","true":"yes","rows":[{"3":"three"}],"2026-01-02":"date","2026-01-02T10:30:00+02:00":"instant","null":"none"}}`, string(encoded))
}

func TestValidateReplaceGeneratedOnlyForNativeTypeAndInterfaceMounts(t *testing.T) {
	native := func(id string, mount MountSpec) ViewDefinition {
		return ViewDefinition{APIVersion: APIVersion, ID: id, Name: id, SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "Doc"}, Mount: mount,
			Variants: VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}}}}
	}
	custom := func(mount MountSpec) ViewDefinition {
		return ViewDefinition{APIVersion: APIVersion, ID: "v", Name: "V", SourceSpec: SourceSpec{Kind: SourceKindCustom, Entry: "index.html"}, Mount: mount}
	}
	for _, def := range []ViewDefinition{
		native("v", MountSpec{Kind: MountKindStandalone, ReplaceGenerated: true}),
		custom(MountSpec{Kind: MountKindType, Type: "Doc", ReplaceGenerated: true}),
		custom(MountSpec{Kind: MountKindGroup, Group: "Docs", ReplaceGenerated: true}),
	} {
		issues := Validate([]ViewDefinition{def}, ValidateOptions{}).Issues
		require.Equal(t, []Issue{issueWithSeverity(def, "unexpected_field", "mount.replaceGenerated", issues[0].Message, IssueWarning)}, issues)
		require.False(t, ReplacesGenerated(def))
	}
	typed := native("a", MountSpec{Kind: MountKindType, Type: "Doc", ReplaceGenerated: true})
	iface := native("i", MountSpec{Kind: MountKindInterface, Interface: "Shared", ReplaceGenerated: true})
	require.Empty(t, Validate([]ViewDefinition{typed, iface}, ValidateOptions{}).Issues)
	require.True(t, ReplacesGenerated(typed))

	// Two replacing views on one target both warn; neither becomes invalid.
	second := native("b", typed.Mount)
	hidden := native("c", MountSpec{Kind: MountKindType, Type: "Doc", ReplaceGenerated: true, Hidden: true})
	issues := Validate([]ViewDefinition{typed, second, hidden}, ValidateOptions{}).Issues
	require.Len(t, issues, 2)
	for _, issue := range issues {
		require.Equal(t, "duplicate_replace_generated", issue.Code)
		require.Equal(t, IssueWarning, issue.Severity)
		require.NotEqual(t, "c", issue.View, "hidden views do not replace")
	}
}

func TestWorkspaceMountHasNoSubjectAndConflictsOnDefaults(t *testing.T) {
	def := ViewDefinition{APIVersion: APIVersion, ID: "workspace.one", Name: "One", SourceSpec: SourceSpec{Kind: SourceKindCustom, Entry: "one.tsx"}, Mount: MountSpec{Kind: MountKindWorkspace, Default: true}}
	require.Empty(t, validateMount(def, ValidateOptions{CheckReferences: true}))
	require.True(t, MatchesMount(def.Mount, MountKindWorkspace, ""))
	require.False(t, MatchesMount(def.Mount, MountKindWorkspace, "Other"))
	require.False(t, MatchesMount(def.Mount, MountKindStandalone, ""))
	for _, mount := range []MountSpec{{Kind: MountKindWorkspace, Type: "A"}, {Kind: MountKindWorkspace, Interface: "A"}, {Kind: MountKindWorkspace, Group: "A"}} {
		bad := def
		bad.Mount = mount
		requireIssueCode(t, validateMount(bad, ValidateOptions{}), "unexpected_field")
	}
	other := def
	other.ID = "workspace.two"
	issues := validateMountedDefaults([]ViewDefinition{def, other}, nil)
	require.Len(t, issues, 2)
	requireIssueCode(t, issues, "duplicate_mount_default")
}
