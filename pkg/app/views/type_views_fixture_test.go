package views

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// typeViewsSchema covers the SPEC-0112 shapes: Task is a workflow, Project a
// contract, Meeting and the Event interface dated, and Area a catalog.
const typeViewsSchema = `
type Person @node(paths: ["people/*.md"]) {
  name: String @field
}

enum WorkStatus {
  planned @view(label: "Planned", stage: "open")
  doing @view(label: "Doing", stage: "active")
  shipped @view(label: "Shipped", stage: "done")
  cancelled @view(label: "Cancelled", stage: "dropped")
}

enum ProjectStatus {
  proposed @view(stage: "open")
  live @view(stage: "done")
}

enum Priority {
  low @view(tone: "neutral")
  high @view(tone: "progress")
}

enum AreaKind {
  product
  platform
}

type Project @node(paths: ["projects/*.md"]) {
  status: ProjectStatus @field @display(importance: KEY)
  tasks: [Task!] @reverse(field: "project")
}

type Task @node(paths: ["tasks/*.md"]) {
  status: WorkStatus @field @display(importance: KEY)
  priority: Priority @field @display(importance: KEY)
  summary: String @field
  state: String @field
  owner: Person @link
  project: [Project!] @link @display(importance: KEY)
  due: Date @field
}

interface Event {
  held: Date @field @display(importance: KEY)
}

type Meeting implements Event @node(paths: ["meetings/*.md"]) {
  held: Date @field @display(importance: KEY)
}

type Workshop implements Event @node(paths: ["workshops/*.md"]) {
  held: Date @field @display(importance: KEY)
}

type Area @node(paths: ["areas/*.md"]) {
  kind: AreaKind @field @display(importance: KEY)
}
`

func newTypeViewsService(t *testing.T, files map[string]string, views ...viewconfig.ViewDefinition) (*Service, *semdb.Store) {
	t.Helper()
	return indexTypeViewsFixture(t, writeTypeViewsFixture(t, files), views...)
}

func writeTypeViewsFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", typeViewsSchema)
	for path, body := range files {
		writeSourceFixture(t, root, path, body)
	}
	return root
}

func indexTypeViewsFixture(t *testing.T, root string, views ...viewconfig.ViewDefinition) (*Service, *semdb.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "type-views", Path: root, Links: obsidian.LinkTypeBoth}
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
		Views:      views,
	}), store
}

func TestExecuteReportsProfileRolesAndStagesFromTheSchema(t *testing.T) {
	service, _ := newTypeViewsService(t, map[string]string{
		"tasks/a.md": "---\nstatus: doing\nstate: waiting\nsummary: First\n---\n# A\n",
	})

	resp, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{})
	require.NoError(t, err)

	require.NotNil(t, resp.Profile)
	require.Equal(t, ontology.ShapeWorkflow, resp.Profile.Shape)
	require.Equal(t, "status", resp.Profile.LifecycleField)
	roles := map[string]string{}
	for _, capability := range resp.Capabilities {
		roles[capability.Key] = capability.SemanticRole
	}
	require.Equal(t, "status", roles["status"])
	require.Equal(t, "summary", roles["summary"])
	require.Equal(t, "date", roles["due"])
	require.Empty(t, roles["state"], "a field named like a status is not a lifecycle")
	require.Empty(t, roles["frontmatter.state"])

	status := capabilityByKey(resp.Capabilities, "status")
	require.Equal(t, []FieldEnumValue{
		{Value: "planned", Label: "Planned", Tone: "neutral", Stage: ontology.StageOpen, StageDeclared: true},
		{Value: "doing", Label: "Doing", Tone: "progress", Stage: ontology.StageActive, StageDeclared: true},
		{Value: "shipped", Label: "Shipped", Tone: "success", Stage: ontology.StageDone, StageDeclared: true},
		{Value: "cancelled", Label: "Cancelled", Tone: "muted", Stage: ontology.StageDropped, StageDeclared: true, CollapsedByDefault: true},
	}, status.EnumValues)
	priority := capabilityByKey(resp.Capabilities, "priority")
	require.Equal(t, ontology.StageActive, priority.EnumValues[1].Stage)
	require.False(t, priority.EnumValues[1].StageDeclared, "inferred from tone")

	// The generated table groups by stage, so Doing leads and Cancelled starts collapsed.
	require.Equal(t, "doing", resp.Groups[0].Value)
}

func TestQueryRecipeRowsWithoutSchemaKeepNameRoles(t *testing.T) {
	caps := mergeCapabilities(nil, []TableRow{{Fields: map[string]any{"state": "open", "summary": "x", "dueDate": "2026-01-01"}}}, SourceUnknown)
	require.Equal(t, "status", capabilityByKey(caps, "state").SemanticRole)
	require.Equal(t, "summary", capabilityByKey(caps, "summary").SemanticRole)
	require.Equal(t, "date", capabilityByKey(caps, "dueDate").SemanticRole)
}

func TestLifecycleGroupsFollowStagesWhileBoardColumnsFollowTheEnum(t *testing.T) {
	files := map[string]string{
		"tasks/a.md": "---\nstatus: planned\n---\n# A\n",
		"tasks/b.md": "---\nstatus: doing\n---\n# B\n",
		"tasks/c.md": "---\nstatus: shipped\n---\n# C\n",
		"tasks/d.md": "---\nstatus: cancelled\n---\n# D\n",
	}
	authored := viewconfig.ViewDefinition{
		APIVersion: viewconfig.APIVersion, ID: "work", Name: "Work",
		SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
		Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
		Defaults:   viewconfig.DefaultsSpec{Group: &viewconfig.GroupSpec{Field: "status", Values: []viewconfig.GroupValueSpec{{Value: "cancelled", Order: -1}}}},
		Variants: viewconfig.VariantSet{
			Table:  &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}},
			Kanban: &viewconfig.KanbanVariant{ColumnField: "status", LaneField: "none"},
		},
	}
	service, _ := newTypeViewsService(t, files, authored)
	groupValues := func(groups []TableGroup) []string {
		var out []string
		for _, group := range groups {
			out = append(out, group.Value)
		}
		return out
	}

	generated, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{Variant: "table"})
	require.NoError(t, err)
	require.Equal(t, []string{"doing", "planned", "shipped", "cancelled"}, groupValues(generated.Groups))
	table, err := service.Execute(context.Background(), "work", ExecuteRequest{Variant: "table"})
	require.NoError(t, err)
	require.Equal(t, []string{"cancelled", "doing", "planned", "shipped"}, groupValues(table.Groups), "an authored group order wins")
	board, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	var columns []string
	for _, column := range board.Board.Columns {
		columns = append(columns, column.Value)
	}
	require.Equal(t, []string{"planned", "doing", "shipped", "cancelled"}, columns)
}

func TestAuthoredViewsKeepEnumOrderForInferredStages(t *testing.T) {
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
enum S {
  todo @view(tone: "neutral")
  doing @view(tone: "progress")
  done @view(tone: "success")
}
type Item @node(paths: ["items/*.md"]) {
  status: S @field @display(importance: KEY)
}
`)
	for name, status := range map[string]string{"a": "todo", "b": "doing", "c": "done"} {
		writeSourceFixture(t, root, "items/"+name+".md", "---\nstatus: "+status+"\n---\n# "+name+"\n")
	}
	authored := viewconfig.ViewDefinition{
		APIVersion: viewconfig.APIVersion, ID: "items", Name: "Items",
		SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Item"},
		Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
		Defaults:   viewconfig.DefaultsSpec{Group: &viewconfig.GroupSpec{Field: "status"}},
		Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
	}
	service, _ := indexTypeViewsFixture(t, root, authored)
	groupValues := func(id string) []string {
		resp, err := service.Execute(context.Background(), id, ExecuteRequest{Variant: "table"})
		require.NoError(t, err)
		var out []string
		for _, group := range resp.Groups {
			out = append(out, group.Value)
		}
		return out
	}

	require.Equal(t, []string{"todo", "doing", "done"}, groupValues("items"), "inferred stages do not reorder an authored view")
	require.Equal(t, []string{"doing", "todo", "done"}, groupValues(viewconfig.GeneratedTypeID("Item")), "the generated view groups by stage")
}
