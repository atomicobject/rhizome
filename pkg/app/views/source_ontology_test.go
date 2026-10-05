package views

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRowFromNodeListItemPreservesCanonicalIdentity(t *testing.T) {
	ref := ontology.NodeRef{
		NotePath:   "docs/specs/product/browser.md",
		Fragment:   "^SPEC-0014-US8",
		NodeID:     "story-8",
		Kind:       ontology.NodeKindEmbedded,
		Structural: "abc123",
	}

	row := rowFromNodeListItem(ontology.NodeListItem{
		Ref:          ref,
		Title:        "Configured views",
		ResolvedType: "UserStory",
		NotePath:     "docs/specs/product/browser.md",
		UpdatedAt:    42,
		HasIssues:    true,
	})

	require.Equal(t, ref, row.Ref)
	require.Equal(t, "Configured views", row.Title)
	require.Equal(t, "UserStory", row.ResolvedType)
	require.True(t, row.HasIssues)
	require.Equal(t, int64(42), row.UpdatedAt)
}

func TestEnrichRowFromRecordAddsFrontmatterInlineAndTags(t *testing.T) {
	row := rowFromNodeListItem(ontology.NodeListItem{
		Ref:      ontology.NodeRef{NotePath: "docs/a.md", Kind: ontology.NodeKindNote},
		NotePath: "docs/a.md",
		Title:    "A",
	})

	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			"ProductSpec": {
				Name: "ProductSpec",
				Fields: []*ontology.Field{
					{Name: "specStatus", Source: "status", SourceKind: ontology.FieldSourceFrontmatter},
					{Name: "owner", Source: "owner", SourceKind: ontology.FieldSourceInline},
				},
			},
		},
	}}}
	row = resolver.enrichRowFromRecord(row, noderead.NodeRecord{
		Ref:         ontology.NodeRef{NotePath: "docs/a.md", Kind: ontology.NodeKindNote},
		Path:        "docs/a.md",
		Title:       "A better title",
		TypeName:    "ProductSpec",
		Tags:        []string{"planning"},
		Frontmatter: map[string]any{"status": "ready"},
		InlineProps: map[string][]string{"owner": []string{"team"}},
	})

	require.Equal(t, "A better title", row.Title)
	require.Equal(t, "ProductSpec", row.ResolvedType)
	require.Equal(t, []string{"planning"}, row.Tags)
	require.Equal(t, "ready", row.Fields["frontmatter"].(map[string]any)["status"])
	require.Equal(t, []string{"team"}, row.Fields["inline"].(map[string]any)["owner"])
	require.Equal(t, "ready", row.Fields["specStatus"])
	require.Equal(t, []string{"team"}, row.Fields["owner"])
}

func TestOntologySourceCapIsIndependentOfDisplayPage(t *testing.T) {
	for _, first := range []int{10, 100} {
		require.Equal(t, SourceCapPolicy{Limit: 5000, Source: "default"}, sourceCapPolicy(viewconfig.ViewDefinition{}, ExecuteRequest{Page: PageRequest{First: first}}))
	}
	require.Equal(t, SourceCapPolicy{Limit: 250, Source: "view.defaults.sourceCap"}, sourceCapPolicy(viewconfig.ViewDefinition{
		Defaults: viewconfig.DefaultsSpec{SourceCap: 250},
	}, ExecuteRequest{}))
	require.Equal(t, SourceCapPolicy{Limit: 1000, Source: "request.source.maxRows"}, sourceCapPolicy(viewconfig.ViewDefinition{
		Defaults: viewconfig.DefaultsSpec{SourceCap: 250},
	}, ExecuteRequest{Source: SourceRequest{MaxRows: 1000}}))
	require.Equal(t, SourceCapPolicy{Limit: 50000, Source: "request.source.maxRows"}, sourceCapPolicy(viewconfig.ViewDefinition{}, ExecuteRequest{
		Source: SourceRequest{MaxRows: 50001},
	}))
}

func TestOntologySourceProjectsReferencedRelationCountsBeforeFilterAndSort(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type FeatureArea @node(paths: ["areas/*.md"]) {
  specs: [Spec!] @neighbors(direction: INBOUND, type: "Spec")
  exactSpecs: [Spec!] @reverse(field: "area")
}

type Spec @node(paths: ["specs/*.md"]) {
  name: String
  area: FeatureArea @link(includeBodyLinks: false, includeBacklinks: false)
}
`)
	writeSourceFixture(t, root, "areas/alpha.md", "# Alpha\n")
	writeSourceFixture(t, root, "areas/beta.md", "# Beta\n")
	writeSourceFixture(t, root, "areas/gamma.md", "# Gamma\n")
	writeSourceFixture(t, root, "specs/one.md", "# One\n\nSee [[alpha]].\n")
	writeSourceFixture(t, root, "specs/two.md", "# Two\n\nSee [[beta]].\n")
	writeSourceFixture(t, root, "specs/three.md", "# Three\n\nSee [[beta]].\n")
	writeSourceFixture(t, root, "specs/four.md", "---\narea: '[[alpha]]'\n---\n# Four\n\nSee [[beta]] in prose.\n")
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	spy := &relationCountViewSpyStore{Store: store}
	definition := viewconfig.ViewDefinition{
		APIVersion: viewconfig.APIVersion,
		ID:         "areas",
		Name:       "Areas",
		SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "FeatureArea"},
		Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
		Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}, {Field: "specs"}, {Field: "exactSpecs"}}}},
	}
	service := New(ServiceOptions{
		VaultPath: root, VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: spy, Schema: runtime.Schema,
		Views: []viewconfig.ViewDefinition{definition},
	})

	response, err := service.Execute(ctx, "areas", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "specs", Op: "gte", Value: "1"}},
		Sort:    []viewconfig.SortSpec{{Field: "specs", Direction: "desc"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"areas/beta.md", "areas/alpha.md"}, rowPaths(response.Rows))
	require.Equal(t, 3, response.Rows[0].Fields["specs"])
	require.Equal(t, 2, response.Rows[1].Fields["specs"])
	require.Equal(t, 0, response.Rows[0].Fields["exactSpecs"])
	require.Equal(t, 1, response.Rows[1].Fields["exactSpecs"])
	capability, ok := resolveCapability(response.Capabilities, "specs")
	require.True(t, ok)
	require.Equal(t, "number", capability.ValueKind)
	require.Equal(t, "count", capability.SemanticRole)
	require.True(t, capability.Sortable)
	require.False(t, capability.Groupable)
	require.Nil(t, capability.Edit)
	require.Equal(t, 1, spy.edgeCalls)

	spy.edgeCalls = 0
	definition.Variants.Table.Columns = []viewconfig.ViewColumn{{Field: "title"}}
	service = New(ServiceOptions{
		VaultPath: root, VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: spy, Schema: runtime.Schema,
		Views: []viewconfig.ViewDefinition{definition},
	})
	_, err = service.Execute(ctx, "areas", ExecuteRequest{})
	require.NoError(t, err)
	require.Zero(t, spy.edgeCalls)
}

type relationCountViewSpyStore struct {
	*semdb.Store
	edgeCalls int
}

func (s *relationCountViewSpyStore) OntologyAmbientEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]semdb.OntologyEdgeRow, error) {
	s.edgeCalls++
	return s.Store.OntologyAmbientEdgesBySources(ctx, paths, relation, limit)
}

func TestOntologySourceExposesEditableEnumCheckboxAndRelationCapabilities(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
enum ActionStatus {
  open
  blocked
  done
}

type Person @node(paths: ["people/**/*.md"]) {
  name: String @field(source: "name")
}

type Meeting @node(paths: ["meetings/**/*.md"]) {
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}

type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  status: ActionStatus @field
  label: String @field
  due: Date @field
  assignedTo: Person @link
}
`)
	writeSourceFixture(t, root, "people/alice.md", `---
name: Alice
---
# Alice
`)
	writeSourceFixture(t, root, "people/bob.md", `---
name: Bob
---
# Bob
`)
	writeSourceFixture(t, root, "meetings/demo.md", `# Demo

- [ ] Follow up assigned-to:: people/alice status:: open #action-item
  label:: Follow up
  due:: 2026-05-09
`)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)

	service := New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			VaultDef:   vaultDef,
			NoteReader: &obsidian.Note{},
			Store:      store,
		},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "actions",
			Name:       "Actions",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "ActionItem"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{
				{Field: "status"},
				{Field: "done"},
				{Field: "label"},
				{Field: "due"},
				{Field: "assignedTo"},
			}}},
		}},
	})

	resp, err := service.Execute(ctx, "actions", ExecuteRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 1)
	require.Equal(t, "false", resp.Rows[0].Fields["done"])
	require.Equal(t, []string{"open"}, resp.Rows[0].Fields["status"])
	require.Equal(t, []string{"people/alice"}, resp.Rows[0].Fields["assignedTo"])

	status := capabilityByKey(resp.Capabilities, "status")
	require.NotNil(t, status.Edit)
	require.Equal(t, "enum", status.Edit.Kind)
	require.Equal(t, "setField", status.Edit.Operation)
	require.False(t, status.Edit.List)
	require.Equal(t, []string{"open", "blocked", "done"}, status.Edit.Options)
	require.Equal(t, []any{"open"}, status.Values)

	done := capabilityByKey(resp.Capabilities, "done")
	require.NotNil(t, done.Edit)
	require.Equal(t, "boolean", done.Edit.Kind)
	require.Equal(t, "setField", done.Edit.Operation)

	assignedTo := capabilityByKey(resp.Capabilities, "assignedTo")
	require.NotNil(t, assignedTo.Edit)
	require.Equal(t, "node", assignedTo.Edit.Kind)
	require.Equal(t, "setLinkField", assignedTo.Edit.Operation)
	require.Equal(t, "Person", assignedTo.Edit.TargetType)
	require.Equal(t, []string{"[[people/alice]]", "[[people/bob]]"}, candidateValues(assignedTo.Edit.Candidates))

	label := capabilityByKey(resp.Capabilities, "label")
	require.Contains(t, label.FilterOps, "contains")
	require.Contains(t, label.FilterOps, "neq")
	require.NotNil(t, label.Edit)
	require.Equal(t, "text", label.Edit.Kind)
	require.Equal(t, "setField", label.Edit.Operation)

	due := capabilityByKey(resp.Capabilities, "due")
	require.NotNil(t, due.Edit)
	require.Equal(t, "date", due.Edit.Kind)
	require.Equal(t, "setField", due.Edit.Operation)
}

func TestOntologySourceScalarEditCapabilityPolicy(t *testing.T) {
	resolver := defaultSourceResolver{}
	editable := resolver.editCapabilityForField(context.Background(), nil, &ontology.Field{
		Name:       "summary",
		Kind:       ontology.FieldKindScalar,
		TypeName:   "String",
		SourceKind: ontology.FieldSourceInline,
	})
	require.NotNil(t, editable)
	require.Equal(t, "text", editable.Kind)
	require.Equal(t, "setField", editable.Operation)

	unsafeFields := []*ontology.Field{
		{Name: "title", Kind: ontology.FieldKindScalar, TypeName: "String"},
		{Name: "id", Kind: ontology.FieldKindScalar, TypeName: "String", SourceKind: ontology.FieldSourceFrontmatter, IsIdentifier: true},
		{Name: "derivedId", Kind: ontology.FieldKindScalar, TypeName: "String", SourceKind: ontology.FieldSourceInline, IsDerivableIdentifier: true},
		{Name: "tags", Kind: ontology.FieldKindScalar, TypeName: "String", SourceKind: ontology.FieldSourceFrontmatter, List: true},
		{Name: "ok", Kind: ontology.FieldKindScalar, TypeName: "Boolean", SourceKind: ontology.FieldSourceFrontmatter},
		{Name: "frozen", Kind: ontology.FieldKindScalar, TypeName: "String", SourceKind: ontology.FieldSourceFrontmatter, Policy: &ontology.PolicyHint{RequiresUserConfirmation: true}},
	}
	for _, field := range unsafeFields {
		require.Nil(t, resolver.editCapabilityForField(context.Background(), nil, field), field.Name)
	}
}

func TestOntologySourcePushesIndexedEmbeddedFiltersAndSort(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type Person @node(paths: ["people/**/*.md"]) {
  name: String @field(source: "name")
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person! @link
  due: Date @field
}
`)
	writeSourceFixture(t, root, "people/alice.md", `---
name: Alice
---
`)
	writeSourceFixture(t, root, "people/bob.md", `---
name: Bob
---
`)
	writeSourceFixture(t, root, "notes/actions.md", `# Actions

- [ ] Later #action-item
  assignee:: [[people/alice]]
  due:: 2026-05-10
- [ ] Earlier #action-item
  assignee:: [[people/alice]]
  due:: 2026-05-08
- [ ] Bob task #action-item
  assignee:: [[people/bob]]
  due:: 2026-05-01
`)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)

	service := New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			VaultDef:   vaultDef,
			NoteReader: &obsidian.Note{},
			Store:      store,
		},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "actions",
			Name:       "Actions",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "ActionItem"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{
				{Field: "title"},
				{Field: "due"},
			}}},
		}},
	})

	resp, err := service.Execute(ctx, "actions", ExecuteRequest{
		Search: "task",
		Filters: []viewconfig.FilterSpec{
			{Field: "assignee", Op: "eq", Value: "people/alice"},
			{Field: "title", Op: "contains", Value: "Later"},
		},
		Sort: []viewconfig.SortSpec{
			{Field: "due", Direction: "asc"},
			{Field: "notePath", Direction: "asc"},
		},
		Page: PageRequest{First: 25},
	})
	require.NoError(t, err)
	require.True(t, resp.ConstraintPlan)
	require.Equal(t, []viewconfig.FilterSpec{{Field: "assignee", Op: "eq", Value: "people/alice"}}, resp.PushedConstraints.Filters)
	require.Equal(t, []viewconfig.SortSpec{{Field: "due", Direction: "asc"}, {Field: "notePath", Direction: "asc"}}, resp.PushedConstraints.Sort)
	require.Equal(t, "task", resp.ResidualConstraints.Search)
	require.Equal(t, []viewconfig.FilterSpec{{Field: "title", Op: "contains", Value: "Later"}}, resp.ResidualConstraints.Filters)
	require.Equal(t, PageRequest{First: 25}, resp.ResidualConstraints.Page)
	require.Empty(t, resp.Rows)

	resp, err = service.Execute(ctx, "actions", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "assignee", Op: "eq", Value: "people/alice"}},
		Sort:    []viewconfig.SortSpec{{Field: "due", Direction: "asc"}, {Field: "notePath", Direction: "asc"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Earlier", "Later"}, rowTitles(resp.Rows))

	// Regression: indexed-embedded view rows must carry their schema fields
	// (done/assignee/due) so the renderer can show the configured columns.
	// Previously the view's hydrate lookup keyed on Structural; the GraphQL
	// source query didn't request it so row.Ref.Structural was empty while the
	// catalog ref had a fingerprint, and the lookup missed. nodeRefKey now
	// matches on NotePath+Fragment+NodeID+Kind so view ref selections don't
	// have to opt into requesting structural.
	require.NotEmpty(t, resp.Rows)
	first := resp.Rows[0]
	require.Contains(t, first.Fields, "done")
	require.Contains(t, first.Fields, "assignee")
	require.Contains(t, first.Fields, "due")
	require.Equal(t, "false", first.Fields["done"], "indexed embedded fields must surface on the row")
	require.Len(t, first.RelationValues["assignee"], 1)
	require.NotNil(t, first.RelationValues["assignee"][0].Ref)
	require.Equal(t, "people/alice.md", first.RelationValues["assignee"][0].Ref.NotePath)
	require.NotEmpty(t, first.RelationValues["assignee"][0].Title)

	// Regression: the indexed-embedded path used to pass a nil scope to
	// ontologyEditCapabilities, so editCandidatesForType returned no
	// candidates and the UI fell back to a read-only cell instead of a
	// Person picker dropdown.
	assignee := capabilityByKey(resp.Capabilities, "assignee")
	require.NotNil(t, assignee.Edit, "assignee field needs an edit capability")
	require.Equal(t, "node", assignee.Edit.Kind)
	require.NotEmpty(t, assignee.Edit.Candidates, "relation edit must include link target candidates so the renderer shows a picker")
}

func TestOntologySourceLinkFilterResolvesUniqueAndRejectsAmbiguousAlias(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type Person @node(paths: ["people/**/*.md"]) {
  name: String @field(source: "name")
  aliases: [String!] @field
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`)
	writeSourceFixture(t, root, "people/alice.md", `---
name: Alice
aliases:
  - Shared Alias
  - Alice Primary
---
# Alice
`)
	writeSourceFixture(t, root, "people/alicia.md", `---
name: Alicia
aliases:
  - Shared Alias
---
# Alicia
`)
	writeSourceFixture(t, root, "notes/actions.md", `# Actions

- [ ] Follow up #action-item
  assignee:: [[people/alice]]
`)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)
	service := New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			VaultDef:   vaultDef,
			NoteReader: &obsidian.Note{},
			Store:      store,
		},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "actions",
			Name:       "Actions",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "ActionItem"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{
				{Field: "title"},
				{Field: "assignee"},
			}}},
		}},
	})

	resp, err := service.Execute(ctx, "actions", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "assignee", Op: "eq", Value: "Alice Primary"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"Follow up"}, rowTitles(resp.Rows))

	_, err = service.Execute(ctx, "actions", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "assignee", Op: "eq", Value: "Shared Alias"}},
	})
	require.ErrorContains(t, err, `link filter input "Shared Alias" resolved to multiple Person targets`)
}

func TestOntologySourceFiltersAndSortsIndexedRowsBeforePagination(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type Person @node(paths: ["people/**/*.md"]) {
  name: String @field(source: "name")
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person! @link
  due: Date @field
}
`)
	writeSourceFixture(t, root, "people/alice.md", `---
name: Alice
---
`)
	writeSourceFixture(t, root, "people/bob.md", `---
name: Bob
---
`)
	var body strings.Builder
	body.WriteString("# Actions\n\n")
	for i := 0; i < 1200; i++ {
		assignee := "[[people/bob]]"
		if i%200 == 0 {
			assignee = "[[people/alice]]"
		}
		day := (1200 - i) % 28
		if day == 0 {
			day = 28
		}
		body.WriteString("- [ ] Task ")
		body.WriteString(fmt.Sprintf("%04d", i))
		body.WriteString(" #action-item\n")
		body.WriteString("  assignee:: ")
		body.WriteString(assignee)
		body.WriteString("\n")
		body.WriteString(fmt.Sprintf("  due:: 2026-05-%02d\n", day))
	}
	writeSourceFixture(t, root, "notes/actions.md", body.String())
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)

	service := New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			VaultDef:   vaultDef,
			NoteReader: &obsidian.Note{},
			Store:      store,
		},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "actions",
			Name:       "Actions",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "ActionItem"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{
				{Field: "title"},
				{Field: "due"},
			}}},
		}},
	})

	resp, err := service.Execute(ctx, "actions", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "assignee", Op: "eq", Value: "people/alice"}},
		Sort:    []viewconfig.SortSpec{{Field: "due", Direction: "asc"}, {Field: "notePath", Direction: "asc"}},
		Page:    PageRequest{First: 3},
	})
	require.NoError(t, err)
	require.True(t, resp.ConstraintPlan)
	require.Equal(t, []viewconfig.FilterSpec{{Field: "assignee", Op: "eq", Value: "people/alice"}}, resp.PushedConstraints.Filters)
	require.Equal(t, []viewconfig.SortSpec{{Field: "due", Direction: "asc"}, {Field: "notePath", Direction: "asc"}}, resp.PushedConstraints.Sort)
	require.Len(t, resp.Rows, 3)
	require.Equal(t, []string{"Task 1000", "Task 0800", "Task 0600"}, rowTitles(resp.Rows))
}

func TestOntologySourcePushesNotePathFilterBeforeSourceCap(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	var filler strings.Builder
	filler.WriteString("# Filler\n\n")
	for i := 0; i < defaultSourceCap+25; i++ {
		filler.WriteString(fmt.Sprintf("- [ ] Filler %04d #action-item\n", i))
	}
	writeSourceFixture(t, root, "notes/a-filler.md", filler.String())
	writeSourceFixture(t, root, "notes/z-target.md", `# Target

- [ ] Target task #action-item
`)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)

	service := New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			VaultDef:   vaultDef,
			NoteReader: &obsidian.Note{},
			Store:      store,
		},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "actions",
			Name:       "Actions",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "ActionItem"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
	})

	resp, err := service.Execute(ctx, "actions", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "notePath", Op: "eq", Value: "notes/z-target.md"}},
		Page:    PageRequest{First: 10},
	})
	require.NoError(t, err)
	require.Equal(t, []viewconfig.FilterSpec{{Field: "notePath", Op: "eq", Value: "notes/z-target.md"}}, resp.PushedConstraints.Filters)
	require.Empty(t, resp.ResidualConstraints.Filters)
	require.Equal(t, []string{"Target task"}, rowTitles(resp.Rows))

	resp, err = service.Execute(ctx, "actions", ExecuteRequest{
		Search: "Target",
		Page:   PageRequest{First: 10},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Rows)
	requireWarningCode(t, resp.Warnings, "view_source_may_be_truncated")
	requireWarningCode(t, resp.Warnings, "view_residual_constraints_cap_bound")
	requireWarningCode(t, resp.Warnings, "view_search_cap_bound")
}

func TestOntologySourcePushesNoteRootFiltersAndSort(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
enum SpecStatus {
  proposed
  active
  retired
}

type Spec @node(paths: ["docs/specs/**/*.md"]) {
  status: SpecStatus @field
  lastUpdated: Date @field(source: "last-updated")
}
`)
	writeSourceFixture(t, root, "docs/specs/alpha.md", `---
type: Spec
status: active
last-updated: 2026-04-01
---
# Alpha
`)
	writeSourceFixture(t, root, "docs/specs/beta.md", `---
type: Spec
status: active
last-updated: 2026-05-01
---
# Beta
`)
	writeSourceFixture(t, root, "docs/specs/gamma.md", `---
type: Spec
status: proposed
last-updated: 2026-05-03
---
# Gamma
`)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)

	service := New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			VaultDef:   vaultDef,
			NoteReader: &obsidian.Note{},
			Store:      store,
		},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "specs",
			Name:       "Specs",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Spec"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{
				{Field: "status"},
				{Field: "lastUpdated"},
			}}},
		}},
	})

	resp, err := service.Execute(ctx, "specs", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "active"}},
		Sort:    []viewconfig.SortSpec{{Field: "lastUpdated", Direction: "desc"}},
	})
	require.NoError(t, err)
	require.True(t, resp.ConstraintPlan)
	require.Equal(t, []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "active"}}, resp.PushedConstraints.Filters)
	require.Equal(t, []viewconfig.SortSpec{{Field: "lastUpdated", Direction: "desc"}}, resp.PushedConstraints.Sort)
	require.Empty(t, resp.ResidualConstraints.Filters)
	require.Empty(t, resp.ResidualConstraints.Sort)
	require.Equal(t, []string{"docs/specs/beta.md", "docs/specs/alpha.md"}, rowPaths(resp.Rows))

	resp, err = service.Execute(ctx, "specs", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "notePath", Op: "eq", Value: "docs/specs/beta"}},
	})
	require.NoError(t, err)
	require.Equal(t, []viewconfig.FilterSpec{{Field: "notePath", Op: "eq", Value: "docs/specs/beta"}}, resp.PushedConstraints.Filters)
	require.Equal(t, []string{"docs/specs/beta.md"}, rowPaths(resp.Rows))

	resp, err = service.Execute(ctx, "specs", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "notePath", Op: "eq", Value: "docs/specs/alpha"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"docs/specs/alpha.md"}, rowPaths(resp.Rows))
}

func TestViewPathPredicateKeepsMarkdownCompatibilitySeparateFromAuthoredPaths(t *testing.T) {
	builtinResult := pushdown.PredicateResult{
		Key: pushdown.FieldKey{Source: pushdown.FieldKeyBuiltin},
		Predicate: codeanchor.OntologyFieldPredicate{
			FieldName: "notePath",
			Op:        codeanchor.OntologyFieldOpEq,
			Values:    []codeanchor.IntelOntologyNodeFieldValue{{ValueText: "docs/specs/beta", ValueNorm: "docs/specs/beta"}},
		},
	}

	extensionless := viewPathPredicateWithMarkdownCompatibility(
		viewconfig.FilterSpec{Field: "notePath", Op: "eq", Value: "docs/specs/beta"},
		builtinResult,
	)
	require.Equal(t, codeanchor.OntologyFieldOpIn, extensionless.Op)
	require.Equal(t, []string{"docs/specs/beta", "docs/specs/beta.md"}, predicateValueTexts(extensionless))

	explicitHTML := viewPathPredicateWithMarkdownCompatibility(
		viewconfig.FilterSpec{Field: "notePath", Op: "eq", Value: "docs/specs/beta.html"},
		pushdown.PredicateResult{
			Key: pushdown.FieldKey{Source: pushdown.FieldKeyBuiltin},
			Predicate: codeanchor.OntologyFieldPredicate{
				FieldName: "notePath",
				Op:        codeanchor.OntologyFieldOpEq,
				Values:    []codeanchor.IntelOntologyNodeFieldValue{{ValueText: "docs/specs/beta.html", ValueNorm: "docs/specs/beta.html"}},
			},
		},
	)
	require.Equal(t, codeanchor.OntologyFieldOpEq, explicitHTML.Op)
	require.Equal(t, []string{"docs/specs/beta.html"}, predicateValueTexts(explicitHTML))
}

func predicateValueTexts(predicate codeanchor.OntologyFieldPredicate) []string {
	values := make([]string, 0, len(predicate.Values))
	for _, value := range predicate.Values {
		values = append(values, value.ValueText)
	}
	return values
}

func TestOntologyInterfacePushesImplementorFiltersAndGlobalSort(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
interface WorkItem {
  status: String
  due: Date
}

type TaskA implements WorkItem @node(paths: ["tasks/a/**/*.md"]) {
  status: String @field
  due: Date @field
}

type TaskB implements WorkItem @node(paths: ["tasks/b/**/*.md"]) {
  status: String @field
  due: Date @field
}
`)
	writeSourceFixture(t, root, "tasks/a/later.md", `---
type: TaskA
status: active
due: 2026-05-10
---
# Later A
`)
	writeSourceFixture(t, root, "tasks/a/closed.md", `---
type: TaskA
status: closed
due: 2026-05-01
---
# Closed A
`)
	writeSourceFixture(t, root, "tasks/b/earlier.md", `---
type: TaskB
status: active
due: 2026-05-08
---
# Earlier B
`)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)

	service := New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			VaultDef:   vaultDef,
			NoteReader: &obsidian.Note{},
			Store:      store,
		},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyInterface, Interface: "WorkItem"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "status"}, {Field: "due"}}}},
		}},
	})

	resp, err := service.Execute(ctx, "work", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "frontmatter.status", Op: "eq", Value: "active"}},
		Sort:    []viewconfig.SortSpec{{Field: "frontmatter.due", Direction: "asc"}},
	})
	require.NoError(t, err)
	require.True(t, resp.ConstraintPlan)
	require.Equal(t, []viewconfig.FilterSpec{{Field: "frontmatter.status", Op: "eq", Value: "active"}}, resp.PushedConstraints.Filters)
	require.Equal(t, []viewconfig.SortSpec{{Field: "frontmatter.due", Direction: "asc"}}, resp.PushedConstraints.Sort)
	require.Empty(t, resp.ResidualConstraints.Filters)
	require.Empty(t, resp.ResidualConstraints.Sort)
	require.Equal(t, []string{"tasks/b/earlier.md", "tasks/a/later.md"}, rowPaths(resp.Rows))
	require.NotEmpty(t, capabilityByKey(resp.Capabilities, "status").FilterOps)
	require.NotEmpty(t, capabilityByKey(resp.Capabilities, "frontmatter.status").FilterOps)
}

func TestOntologyInterfaceSourceUsesHumanNoteTitle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
interface Effort {
  name: String!
}

type EffortNote implements Effort @node(paths: ["docs/efforts/**/*.md"]) {
  name: String!
}
`)
	writeSourceFixture(t, root, "docs/efforts/2026-04-12-12-00-kb-migration-phase-1.md", `---
type: EffortNote
name: Knowledge-base migration phase 1
---
# Knowledge-base migration phase 1
`)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)

	service := New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			VaultDef:   vaultDef,
			NoteReader: &obsidian.Note{},
			Store:      store,
		},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "efforts",
			Name:       "Efforts",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyInterface, Interface: "Effort"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
	})

	resp, err := service.Execute(ctx, "efforts", ExecuteRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Rows, 1)
	require.Equal(t, "Knowledge-base migration phase 1", resp.Rows[0].Title)
	require.Equal(t, "Knowledge-base migration phase 1", resp.Rows[0].Fields["title"])
	require.Equal(t, "docs/efforts/2026-04-12-12-00-kb-migration-phase-1.md", resp.Rows[0].Path)
}

func TestOntologyInterfaceEditCapabilitiesDropIncompatibleEditors(t *testing.T) {
	schema := &ontology.Schema{
		EnumTypes: map[string]*ontology.EnumType{
			"WorkStatus": {
				Name: "WorkStatus",
				Values: []*ontology.EnumValue{
					{Name: "open"},
					{Name: "done"},
				},
			},
			"ReviewStatus": {
				Name: "ReviewStatus",
				Values: []*ontology.EnumValue{
					{Name: "queued"},
					{Name: "approved"},
				},
			},
		},
		Interfaces: map[string]*ontology.InterfaceType{
			"Trackable": {Name: "Trackable"},
		},
		Types: map[string]*ontology.NoteType{
			"ActionItem": {
				Name:       "ActionItem",
				Implements: []string{"Trackable"},
				Fields: []*ontology.Field{
					{Name: "status", Kind: ontology.FieldKindEnum, TypeName: "WorkStatus", SourceKind: ontology.FieldSourceInline},
				},
			},
			"ReviewItem": {
				Name:       "ReviewItem",
				Implements: []string{"Trackable"},
				Fields: []*ontology.Field{
					{Name: "status", Kind: ontology.FieldKindEnum, TypeName: "ReviewStatus", SourceKind: ontology.FieldSourceInline},
				},
			},
		},
	}
	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: schema}}

	caps := resolver.ontologyEditCapabilities(context.Background(), nil, viewconfig.ViewDefinition{
		SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyInterface, Interface: "Trackable"},
	})

	status := capabilityByKey(caps, "status")
	require.Nil(t, status.Edit)
	require.ElementsMatch(t, []string{"eq", "in", "exists", "missing"}, status.FilterOps)
	require.ElementsMatch(t, []string{"eq", "in", "exists"}, status.IndexedFilterOps)
}

func TestOntologyTitleCapabilityIsEditable(t *testing.T) {
	schema := &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			"Spec": {
				Name: "Spec",
				Role: ontology.TypeRoleNote,
				Fields: []*ontology.Field{
					{Name: "summary", Kind: ontology.FieldKindScalar, TypeName: "String", SourceKind: ontology.FieldSourceFrontmatter},
				},
			},
			"Story": {
				Name: "Story",
				Role: ontology.TypeRoleSection,
				Fields: []*ontology.Field{
					{Name: "status", Kind: ontology.FieldKindScalar, TypeName: "String", SourceKind: ontology.FieldSourceInline},
				},
			},
			"ActionItem": {
				Name:        "ActionItem",
				Role:        ontology.TypeRoleEmbeddedNode,
				SourceShape: ontology.EmbeddedSourceShapeCheckboxItem,
				Fields: []*ontology.Field{
					{Name: "done", Kind: ontology.FieldKindScalar, TypeName: "Boolean", SourceKind: ontology.FieldSourceCheckbox},
				},
			},
		},
	}
	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: schema}}

	for _, typeName := range []string{"Spec", "Story", "ActionItem"} {
		caps := resolver.ontologyEditCapabilities(context.Background(), nil, viewconfig.ViewDefinition{
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: typeName},
		})

		title := capabilityByKey(caps, "title")
		require.NotNil(t, title.Edit, typeName)
		require.Equal(t, "text", title.Edit.Kind, typeName)
		require.Equal(t, "setField", title.Edit.Operation, typeName)
		require.Equal(t, "title", title.Edit.Field, typeName)
		require.Equal(t, "doubleClick", title.Edit.InputMode, typeName)
	}
}

func TestEnumValuesForCapabilityPropagatesViewMetadata(t *testing.T) {
	collapsed := true
	schema := &ontology.Schema{
		EnumTypes: map[string]*ontology.EnumType{
			"WorkStatus": {
				Name: "WorkStatus",
				Values: []*ontology.EnumValue{
					{Name: "ready", View: ontology.EnumValueView{Label: "Ready", Order: 10, Tone: "info"}},
					{Name: "done", View: ontology.EnumValueView{Label: "Finished", Order: 20, Collapsed: &collapsed}},
				},
			},
		},
	}

	values := enumValuesForCapability(schema, "WorkStatus")

	require.Equal(t, []FieldEnumValue{
		{Value: "ready", Label: "Ready", Rank: 10, Tone: "info", Stage: ontology.StageOpen},
		{Value: "done", Label: "Finished", Rank: 20, Tone: "muted", CollapsedByDefault: true, Stage: ontology.StageDropped},
	}, values, "stages are inferred from authored tone and collapse")
}

func TestEnumValuesForCapabilityInfersToneByRank(t *testing.T) {
	schema := &ontology.Schema{EnumTypes: map[string]*ontology.EnumType{
		"State": {
			Name: "State",
			Values: []*ontology.EnumValue{
				{Name: "later", View: ontology.EnumValueView{Order: 20}},
				{Name: "first", View: ontology.EnumValueView{Order: 10}},
				{Name: "also_first", View: ontology.EnumValueView{Order: 10}},
			},
		},
	}}

	values := enumValuesForCapability(schema, "State")
	require.Equal(t, "progress", values[0].Tone)
	require.Equal(t, "neutral", values[1].Tone)
	require.Equal(t, "progress", values[2].Tone)
}

func TestOntologyInterfaceWarnsWhenFieldIsOnlyPartiallyPushable(t *testing.T) {
	schema := &ontology.Schema{
		Interfaces: map[string]*ontology.InterfaceType{
			"Trackable": {Name: "Trackable"},
		},
		Types: map[string]*ontology.NoteType{
			"ActionItem": {
				Name:       "ActionItem",
				Implements: []string{"Trackable"},
				Fields: []*ontology.Field{
					{Name: "status", Kind: ontology.FieldKindScalar, TypeName: "String", SourceKind: ontology.FieldSourceFrontmatter},
					{Name: "due", Kind: ontology.FieldKindScalar, TypeName: "Date", SourceKind: ontology.FieldSourceFrontmatter},
				},
				ByName: map[string]*ontology.Field{},
			},
			"ReviewItem": {
				Name:       "ReviewItem",
				Implements: []string{"Trackable"},
				Fields: []*ontology.Field{
					{Name: "due", Kind: ontology.FieldKindScalar, TypeName: "Date", SourceKind: ontology.FieldSourceFrontmatter},
				},
				ByName: map[string]*ontology.Field{},
			},
		},
	}
	for _, noteType := range schema.Types {
		for _, field := range noteType.Fields {
			noteType.ByName[field.Name] = field
		}
	}
	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: schema}}

	warnings := resolver.interfacePartialFieldWarnings([]string{"ActionItem", "ReviewItem"}, ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "status", Op: "eq", Value: "active"}},
		Sort:    []viewconfig.SortSpec{{Field: "due", Direction: "asc"}, {Field: "status", Direction: "asc"}},
	})

	require.Len(t, warnings, 2)
	require.Equal(t, "interface_partial_field_residual", warnings[0].Code)
	require.Equal(t, "status", warnings[0].Path)
	require.Equal(t, "interface_partial_field_residual", warnings[1].Code)
	require.Equal(t, "status", warnings[1].Path)
}

func TestExecutionPlanPreservesBoundedWarningBelowLimit(t *testing.T) {
	source := SourceResult{
		ConstraintPlan: true,
		ResidualConstraints: SourceConstraints{
			Search: "target",
		},
		Warnings: []Warning{{
			Code: "view_source_residual_constraints_after_cap",
		}},
		Plan: ConstraintPlanSummary{
			CandidateLimit:     10,
			CandidateCount:     1,
			SourceCompleteness: SourceComplete,
		},
	}

	plan := executionPlanSummary(source, source.ResidualConstraints, 1)
	require.Equal(t, SourceBounded, plan.SourceCompleteness)
	require.Equal(t, ConstraintsCapBound, plan.Reliability)
}

func TestOntologyFieldCapabilitiesExposeIndexedAndResidualOps(t *testing.T) {
	schema := &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			"ActionItem": {
				Name: "ActionItem",
				Fields: []*ontology.Field{
					{Name: "summary", Kind: ontology.FieldKindScalar, TypeName: "String", SourceKind: ontology.FieldSourceInline, Required: true, Display: ontology.FieldDisplay{Importance: ontology.FieldImportanceKey}},
					{Name: "assignee", Kind: ontology.FieldKindLink, TypeName: "Person", SourceKind: ontology.FieldSourceInline},
				},
			},
		},
	}
	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: schema}}

	summary := resolver.capabilitiesForField(context.Background(), nil, schema.Types["ActionItem"].Fields[0])[0]
	require.True(t, summary.Required)
	require.Equal(t, ontology.FieldImportanceKey, summary.Importance)
	require.ElementsMatch(t, []string{"eq", "in", "exists", "neq", "contains", "missing"}, summary.FilterOps)
	require.ElementsMatch(t, []string{"eq", "in", "exists"}, summary.IndexedFilterOps)
	require.ElementsMatch(t, []string{"neq", "contains", "missing"}, summary.ResidualFilterOps)

	assignee := resolver.capabilitiesForField(context.Background(), nil, schema.Types["ActionItem"].Fields[1])[0]
	require.Equal(t, ontology.FieldImportanceNormal, assignee.Importance)
	require.ElementsMatch(t, []string{"eq", "in", "exists", "missing"}, assignee.FilterOps)
	require.ElementsMatch(t, []string{"eq", "in", "exists"}, assignee.IndexedFilterOps)
	require.Equal(t, []string{"missing"}, assignee.ResidualFilterOps)
}

func writeSourceFixture(t testing.TB, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

func testNoteMetadataIndexer(t testing.TB) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

func rowPaths(rows []TableRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Path)
	}
	return out
}

func capabilityByKey(caps []FieldCapability, key string) FieldCapability {
	for _, cap := range caps {
		if cap.Key == key {
			return cap
		}
	}
	return FieldCapability{}
}

func candidateValues(candidates []EditCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Value)
	}
	return out
}

func requireWarningCode(t *testing.T, warnings []Warning, code string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == code {
			return
		}
	}
	t.Fatalf("warning code %q not found in %#v", code, warnings)
}
