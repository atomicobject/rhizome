package views

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestExecuteGroupsLinkFieldsByTargetTitle(t *testing.T) {
	ctx := context.Background()
	service := newLinkGroupService(t, map[string]string{
		"opportunities/IP opportunity - AI.md":   "# AI in products\n",
		"opportunities/IP opportunity - beta.md": "# beta tooling\n",
		"opportunities/zz.md":                    "# Cost savings\n",
		"ideas/one.md":                           "---\nopportunities:\n  - \"[[IP opportunity - AI]]\"\n  - \"[[opportunities/IP opportunity - beta|Beta alias]]\"\ncategory: \"[[IP opportunity - AI]]\"\n---\n# One\n",
		"ideas/two.md":                           "---\nopportunities:\n  - \"[[opportunities/IP opportunity - AI|the AI one]]\"\ncategory: \"[[zz]]\"\n---\n# Two\n",
		"ideas/three.md":                         "---\nopportunities:\n  - \"[[Missing opportunity]]\"\n---\n# Three\n",
		"ideas/four.md":                          "# Four\n",
	}, nil)

	type group struct {
		Label string
		Rows  []string
	}
	groupsFor := func(field string) ([]group, ExecuteResponse) {
		t.Helper()
		resp, err := service.Execute(ctx, "ideas", ExecuteRequest{
			Group: &viewconfig.GroupSpec{Field: field},
			Sort:  []viewconfig.SortSpec{{Field: "title"}},
		})
		require.NoError(t, err)
		out := make([]group, 0, len(resp.Groups))
		for _, g := range resp.Groups {
			out = append(out, group{Label: g.Label, Rows: rowTitles(resp.Rows[g.RowStart:g.RowEnd])})
		}
		return out, resp
	}

	listGroups, resp := groupsFor("opportunities")
	require.Equal(t, []group{
		{Label: "AI in products", Rows: []string{"One", "Two"}},
		{Label: "beta tooling", Rows: []string{"One"}},
		{Label: "Missing opportunity", Rows: []string{"Three"}},
		{Label: "(empty)", Rows: []string{"Four"}},
	}, listGroups)
	// A group's value stays a writable link so moves into it can stage it.
	require.Equal(t, "[[opportunities/IP opportunity - AI]]", resp.Groups[0].Value)
	capability, ok := resolveCapability(resp.Capabilities, "opportunities")
	require.True(t, ok)
	require.True(t, capability.Groupable)

	singleGroups, resp := groupsFor("category")
	require.Equal(t, []group{
		{Label: "AI in products", Rows: []string{"One"}},
		{Label: "Cost savings", Rows: []string{"Two"}},
		{Label: "(empty)", Rows: []string{"Four", "Three"}},
	}, singleGroups)
	// Filter options for a faceted link field name their targets by title.
	capability, ok = resolveCapability(resp.Capabilities, "category")
	require.True(t, ok)
	require.NotNil(t, capability.Facets)
	require.Equal(t, map[string]string{
		"[[opportunities/IP opportunity - AI]]": "AI in products",
		"[[opportunities/zz]]":                  "Cost savings",
	}, capability.Facets.Labels)

	board, err := service.Execute(ctx, "ideas", ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	require.NotNil(t, board.Board)
	columns := make([]string, 0, len(board.Board.Columns))
	for _, column := range board.Board.Columns {
		columns = append(columns, column.Label)
	}
	require.Equal(t, []string{"AI in products", "Cost savings", "(empty)"}, columns)
}

// Link groups take their value and key from the target, not from whichever
// row sorts first, so collapse state, group.values overrides, and board moves
// stay stable when the sort changes.
func TestLinkGroupsUseTargetIdentityWhateverTheSpelling(t *testing.T) {
	ctx := context.Background()
	values := []viewconfig.GroupValueSpec{
		{Value: "[[AI]]", Label: "AI column", Order: 2},
		{Value: "[[opportunities/zz]]", Label: "Savings", Order: 1},
	}
	service := newLinkGroupService(t, map[string]string{
		"opportunities/AI.md": "# AI in products\n",
		"opportunities/zz.md": "# Cost savings\n",
		"ideas/a.md":          "---\ncategory: \"[[AI]]\"\n---\n# A\n",
		"ideas/b.md":          "---\ncategory: \"[[opportunities/AI|the AI one]]\"\n---\n# B\n",
		"ideas/c.md":          "---\ncategory: \"[[zz]]\"\n---\n# C\n",
		"ideas/d.md":          "---\ncategory: \"[[ghost|Ghost note]]\"\n---\n# D\n",
	}, &viewconfig.GroupSpec{Field: "category", Values: values})

	type group struct{ Key, Value, Label string }
	groupsFor := func(direction string) []group {
		t.Helper()
		resp, err := service.Execute(ctx, "ideas", ExecuteRequest{
			Group: &viewconfig.GroupSpec{Field: "category", Values: values},
			Sort:  []viewconfig.SortSpec{{Field: "title", Direction: direction}},
		})
		require.NoError(t, err)
		out := make([]group, 0, len(resp.Groups))
		for _, g := range resp.Groups {
			out = append(out, group{Key: g.Key, Value: g.Value, Label: g.Label})
		}
		return out
	}
	ascending := groupsFor("asc")
	require.Equal(t, []group{
		{Key: "category=" + url.QueryEscape("[[opportunities/zz]]"), Value: "[[opportunities/zz]]", Label: "Savings"},
		{Key: "category=" + url.QueryEscape("[[opportunities/AI]]"), Value: "[[opportunities/AI]]", Label: "AI column"},
		// An unresolved link shows its alias, as table cells and previews do.
		{Key: "category=" + url.QueryEscape("[[ghost|Ghost note]]"), Value: "[[ghost|Ghost note]]", Label: "Ghost note"},
	}, ascending)
	require.Equal(t, ascending, groupsFor("desc"))

	// Two spellings of one target are one filter option.
	resp, err := service.Execute(ctx, "ideas", ExecuteRequest{})
	require.NoError(t, err)
	capability, ok := resolveCapability(resp.Capabilities, "category")
	require.True(t, ok)
	require.NotNil(t, capability.Facets)
	require.Equal(t, []any{"[[ghost|Ghost note]]", "[[opportunities/AI]]", "[[opportunities/zz]]"}, capability.Facets.Values)
	require.Equal(t, map[string]string{
		"[[opportunities/AI]]": "AI in products",
		"[[opportunities/zz]]": "Cost savings",
	}, capability.Facets.Labels)

	// Choosing a canonical option keeps rows written in every spelling.
	filtered, err := service.Execute(ctx, "ideas", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "category", Op: "eq", Value: "[[opportunities/AI]]"}},
	})
	require.NoError(t, err)
	titles := make([]string, 0, len(filtered.Rows))
	for _, row := range filtered.Rows {
		titles = append(titles, row.Title)
	}
	require.ElementsMatch(t, []string{"A", "B"}, titles)

	// Configured board columns match the target however the config spells it.
	board, err := service.Execute(ctx, "ideas", ExecuteRequest{Variant: "kanban", Sort: []viewconfig.SortSpec{{Field: "title", Direction: "desc"}}})
	require.NoError(t, err)
	require.NotNil(t, board.Board)
	columns := make([]group, 0, len(board.Board.Columns))
	for _, column := range board.Board.Columns {
		columns = append(columns, group{Key: column.Key, Value: column.Value, Label: column.Label})
	}
	require.Equal(t, ascending, columns)
}

// Hydration failures before and after paging report one warning, not two.
func TestRelationHydrationFailureWarnsOnce(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.Close())
	vaultDef := obsidian.VaultDefinition{Name: "closed", Path: root}
	scope := noderead.NewService(vaultDef, &obsidian.Note{}, store, &ontology.Schema{}).NewScope(ctx, noderead.ScopeOptions{})
	row := testRow("ideas/a.md", "A", map[string]any{"owner": "[[x]]"})
	row.RelationValues = map[string][]TableRelationValue{"owner": {{Value: "[[x]]", Ref: &ontology.NodeRef{NotePath: "x.md", Kind: ontology.NodeKindNote}}}}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{testViewDefinition("work")},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				Rows:          []TableRow{row},
				Capabilities:  []FieldCapability{{Key: "owner", CanonicalField: "owner", ValueKind: "relation"}},
				relationScope: scope,
			}, nil
		}),
	})
	resp, err := service.Execute(ctx, "work", ExecuteRequest{Group: &viewconfig.GroupSpec{Field: "owner"}})
	require.NoError(t, err)
	failures := 0
	for _, warning := range resp.Warnings {
		if warning.Code == "view_relation_hydration_failed" {
			failures++
		}
	}
	require.Equal(t, 1, failures)
}

// Group headers need titles only for rows that pass the filters; facet
// options need them for every source row.
func TestGroupOnlyLinkTitlesHydrateFilteredRows(t *testing.T) {
	link := func(path string) []TableRelationValue {
		return []TableRelationValue{{Value: "[[" + path + "]]", Ref: &ontology.NodeRef{NotePath: path + ".md", Kind: ontology.NodeKindNote}}}
	}
	hidden := TableRow{RelationValues: map[string][]TableRelationValue{"owner": link("hidden"), "team": link("hidden-team")}}
	shown := TableRow{RelationValues: map[string][]TableRelationValue{"owner": link("shown"), "team": link("shown-team")}}
	capabilities := []FieldCapability{
		{Key: "owner", CanonicalField: "owner", ValueKind: "relation"},
		{Key: "team", CanonicalField: "team", ValueKind: "relation", Facets: &FacetCapability{}},
	}
	refs := groupAndFacetRelationRefs([]TableRow{hidden, shown}, []TableRow{shown}, capabilities, &viewconfig.GroupSpec{Field: "owner"})
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		paths = append(paths, ref.NotePath)
	}
	require.ElementsMatch(t, []string{"hidden-team.md", "shown-team.md", "shown.md"}, paths)
}

func newLinkGroupService(t *testing.T, files map[string]string, group *viewconfig.GroupSpec) *Service {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type Opportunity @node(paths: ["opportunities/**/*.md"]) {
  name: String @field(source: "name")
}

type Idea @node(paths: ["ideas/**/*.md"]) {
  opportunities: [Opportunity!] @link(source: "opportunities")
  category: Opportunity @link(source: "category")
}
`)
	for path, body := range files {
		writeSourceFixture(t, root, path, body)
	}
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "group-links", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)
	return New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps:  ontologyquery.Deps{VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: store},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "ideas",
			Name:       "Ideas",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Idea"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Defaults:   viewconfig.DefaultsSpec{Group: group},
			Variants: viewconfig.VariantSet{
				Table:  &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}},
				Kanban: &viewconfig.KanbanVariant{ColumnField: "category"},
			},
		}},
	})
}

// Residual filtering matches a canonical option to the row's resolved target,
// however the note spelled the link.
func TestResidualLinkFilterMatchesCanonicalOption(t *testing.T) {
	row := TableRow{
		Fields: map[string]any{"category": "[[AI]]"},
		RelationValues: map[string][]TableRelationValue{
			"category": {{Value: "[[AI]]", Ref: &ontology.NodeRef{NotePath: "opportunities/AI.md"}}},
		},
	}
	caps := map[string]FieldCapability{"category": {Key: "category", CanonicalField: "category", ValueKind: "relation"}}
	for _, filter := range []viewconfig.FilterSpec{
		{Field: "category", Op: "eq", Value: "[[opportunities/AI]]"},
		{Field: "category", Op: "in", Values: []string{"[[opportunities/zz]]", "[[opportunities/AI]]"}},
	} {
		ok, err := rowMatchesFilter(row, filter, caps)
		require.NoError(t, err)
		require.True(t, ok, filter.Op)
	}
	ok, err := rowMatchesFilter(row, viewconfig.FilterSpec{Field: "category", Op: "neq", Value: "[[opportunities/AI]]"}, caps)
	require.NoError(t, err)
	require.False(t, ok)
}
