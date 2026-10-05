package views

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

var saveFixture = map[string]string{
	"tasks/a.md": "---\nstatus: planned\npriority: high\n---\n# A\n",
	"tasks/b.md": "---\nstatus: doing\n---\n# B\n",
}

func TestSaveFromGeneratedViewCreatesAReplacingView(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	writeSourceFixture(t, root, ".rhizome/views/task.yaml", "apiVersion: rhizome.view.v1\nid: other\nname: Taken\nsource: {kind: ontology_type, type: Area}\nmount: {kind: standalone}\nvariants: {table: {columns: [{field: title}]}}\n")
	service, _ := indexTypeViewsFixture(t, root)
	generatedID := viewconfig.GeneratedTypeID("Task")
	loaded, err := service.Execute(context.Background(), generatedID, ExecuteRequest{})
	require.NoError(t, err)

	resp, err := service.Save(context.Background(), generatedID, SaveRequest{
		DefinitionFingerprint: loaded.DefinitionFingerprint,
		State: SaveState{
			Variant:     "kanban",
			Filters:     []viewconfig.FilterSpec{{Field: "status", Op: "neq", Value: "cancelled"}},
			Sort:        []viewconfig.SortSpec{{Field: "title", Direction: "asc"}},
			Group:       &viewconfig.GroupSpec{Field: "due", Bucket: viewconfig.GroupBucketMonth},
			Columns:     []viewconfig.ViewColumn{{Field: "title"}, {Field: "status", Label: "Status"}},
			Density:     ptr(viewconfig.TableDensityOneLine),
			ColumnField: ptr("priority"),
			LaneField:   ptr("none"),
		},
	})
	require.NoError(t, err)
	require.Equal(t, SaveResponse{ID: "task-2", Path: ".rhizome/views/task-2.yaml", Created: true}, resp, "the slug skips the taken file name")

	defs, issues := viewconfig.LoadPath(filepath.Join(root, ".rhizome/views/task-2.yaml"))
	require.Empty(t, issues)
	saved := defs[0]
	require.Equal(t, viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: "Task", ReplaceGenerated: true}, saved.Mount)
	require.Equal(t, "Task", saved.Name)
	require.Equal(t, "kanban", saved.Defaults.Variant)
	require.Equal(t, &viewconfig.GroupSpec{Field: "due", Bucket: viewconfig.GroupBucketMonth}, saved.Defaults.Group)
	require.Equal(t, &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}, {Field: "status", Label: "Status"}}, Density: "one-line"}, saved.Variants.Table)
	require.Equal(t, &viewconfig.KanbanVariant{ColumnField: "priority", LaneField: "none"}, saved.Variants.Kanban)
	require.Equal(t, "summary", saved.Variants.Card.Preview, "the generated card spec is carried over")

	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, viewChoiceID("task-2", "kanban"), findTarget(t, catalog, viewconfig.MountKindType, "Task").DefaultChoiceID, "the next catalog read serves the saved view")
	board, err := service.Execute(context.Background(), "task-2", ExecuteRequest{})
	require.NoError(t, err)
	require.Equal(t, "priority", board.Board.ColumnField)
}

const authoredTaskView = `apiVersion: rhizome.view.v1
id: work
name: Work
# Shown on the Task page.
source:
  kind: ontology_type
  type: Task
mount:
  kind: type
  type: Task
defaults:
  variant: table
  first: 100 # keep pages short
  sort:
    - field: title
filterPresets:
  - id: doing
    filters: [{field: status, op: eq, value: doing}]
variants:
  table:
    columns:
      - field: title
  kanban:
    columnField: status
    hideEmptyColumns: true
`

func TestSaveAuthoredViewRewritesDefaultsAndVariantsInPlace(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	path := filepath.Join(root, ".rhizome/views/work.yaml")
	writeSourceFixture(t, root, ".rhizome/views/work.yaml", authoredTaskView)
	service, _ := indexTypeViewsFixture(t, root)

	resp, err := service.Save(context.Background(), "work", SaveRequest{State: SaveState{
		Variant:   "kanban",
		Sort:      []viewconfig.SortSpec{{Field: "updatedAt", Direction: "desc"}},
		Group:     &viewconfig.GroupSpec{Field: "status"},
		Columns:   []viewconfig.ViewColumn{{Field: "title"}, {Field: "priority"}},
		LaneField: ptr("priority"),
	}})
	require.NoError(t, err)
	require.Equal(t, SaveResponse{ID: "work", Path: ".rhizome/views/work.yaml"}, resp)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "# Shown on the Task page.")
	require.Contains(t, text, "first: 100 # keep pages short")
	require.Contains(t, text, "hideEmptyColumns: true")
	require.Contains(t, text, "id: doing")
	defs, issues := viewconfig.LoadPath(path)
	require.Empty(t, issues)
	require.Equal(t, "kanban", defs[0].Defaults.Variant)
	require.Equal(t, []viewconfig.SortSpec{{Field: "updatedAt", Direction: "desc"}}, defs[0].Defaults.Sort)
	require.Equal(t, &viewconfig.GroupSpec{Field: "status"}, defs[0].Defaults.Group)
	require.Equal(t, []viewconfig.ViewColumn{{Field: "title"}, {Field: "priority"}}, defs[0].Variants.Table.Columns)
	require.Equal(t, &viewconfig.KanbanVariant{ColumnField: "status", HideEmptyColumns: true, LaneField: "priority"}, defs[0].Variants.Kanban)
}

func TestSaveRefusesInvalidResultsConflictsAndSymlinks(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	path := filepath.Join(root, ".rhizome/views/work.yaml")
	writeSourceFixture(t, root, ".rhizome/views/work.yaml", authoredTaskView)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	require.NoError(t, os.WriteFile(outside, []byte("apiVersion: rhizome.view.v1\nid: linked\nname: Linked\nsource: {kind: ontology_type, type: Task}\nmount: {kind: standalone}\nvariants: {table: {columns: [{field: title}]}}\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".rhizome/views/linked.yaml")))
	service, _ := indexTypeViewsFixture(t, root)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	_, err = service.Save(context.Background(), "work", SaveRequest{State: SaveState{Group: &viewconfig.GroupSpec{Field: "status", Bucket: viewconfig.GroupBucketMonth}}})
	require.ErrorIs(t, err, ErrInvalidView, "a month bucket on an enum fails view validation")
	_, err = service.Save(context.Background(), viewconfig.GeneratedTypeID("Task"), SaveRequest{State: SaveState{Filters: []viewconfig.FilterSpec{{Field: "status"}}}})
	require.ErrorIs(t, err, ErrInvalidView, "a filter without an operator fails view validation")
	_, err = service.Save(context.Background(), viewconfig.GeneratedTypeID("Task"), SaveRequest{State: SaveState{Variant: "gallery"}})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = service.Save(context.Background(), "work", SaveRequest{DefinitionFingerprint: "stale", State: SaveState{Variant: "table"}})
	require.ErrorIs(t, err, ErrSaveConflict)
	_, err = service.Save(context.Background(), "linked", SaveRequest{State: SaveState{Variant: "table"}})
	require.ErrorIs(t, err, ErrInvalidRequest, "a symlinked view file is refused")
	_, err = service.Save(context.Background(), "missing", SaveRequest{})
	require.ErrorIs(t, err, ErrViewNotFound)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	entries, err := os.ReadDir(filepath.Join(root, ".rhizome/views"))
	require.NoError(t, err)
	require.Len(t, entries, 2, "no file was created")
}

func TestSaveFromAReplacedGeneratedViewConflictsInsteadOfAddingASecondReplacement(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	service, _ := indexTypeViewsFixture(t, root)
	id := viewconfig.GeneratedTypeID("Task")
	loaded, err := service.Execute(context.Background(), id, ExecuteRequest{})
	require.NoError(t, err)
	req := SaveRequest{DefinitionFingerprint: loaded.DefinitionFingerprint, State: SaveState{Variant: "table"}}

	_, err = service.Save(context.Background(), id, req)
	require.NoError(t, err)
	_, err = service.Save(context.Background(), id, req)
	require.ErrorIs(t, err, ErrSaveConflict)
	require.ErrorContains(t, err, ".rhizome/views/task.yaml already replaces")

	entries, err := os.ReadDir(filepath.Join(root, ".rhizome/views"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	result := validate.RunViews(context.Background(), validate.RunContext{VaultPath: root})
	require.True(t, result.OK, "%+v", result.Issues)
}

func TestSaveRefusesAViewFileThatValidationWouldWarnAbout(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	path := filepath.Join(root, ".rhizome/views/work.yaml")
	writeSourceFixture(t, root, ".rhizome/views/work.yaml", strings.Replace(authoredTaskView, "  type: Task\ndefaults:", "  type: Task\n  group: stray\ndefaults:", 1))
	service, _ := indexTypeViewsFixture(t, root)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	_, err = service.Save(context.Background(), "work", SaveRequest{State: SaveState{Variant: "table"}})
	require.ErrorIs(t, err, ErrInvalidView, "rzm validate views reports the ignored mount.group warning")
	require.ErrorContains(t, err, "mount.group")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
}

func TestSaveRefusesStateTheRuntimeWouldRefuse(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	service, _ := indexTypeViewsFixture(t, root)
	id := viewconfig.GeneratedTypeID("Task")

	for name, state := range map[string]SaveState{
		"unknown lane field":     {Variant: "kanban", LaneField: ptr("bogus")},
		"lane equals the column": {Variant: "kanban", LaneField: ptr("status")},
		"summary as the column":  {Variant: "kanban", ColumnField: ptr("summary")},
		"contains on a link":     {Variant: "table", Filters: []viewconfig.FilterSpec{{Field: "owner", Op: "contains", Value: "x"}}},
		"bad lane from a table":  {Variant: "table", LaneField: ptr("bogus")},
		"bad column from cards":  {Variant: "card", ColumnField: ptr("summary")},
	} {
		_, err := service.Save(context.Background(), id, SaveRequest{State: state})
		require.ErrorIs(t, err, ErrInvalidRequest, name)
	}
	_, err := os.Stat(filepath.Join(root, ".rhizome/views"))
	require.ErrorIs(t, err, os.ErrNotExist, "nothing was written")
}

func TestConcurrentSavesOfAGeneratedViewLeaveOneReplacement(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	service, _ := indexTypeViewsFixture(t, root)
	id := viewconfig.GeneratedTypeID("Task")

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = service.Save(context.Background(), id, SaveRequest{State: SaveState{Variant: "table"}})
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, ErrSaveConflict)
		}
	}
	require.Equal(t, 1, succeeded)
	entries, err := os.ReadDir(filepath.Join(root, ".rhizome/views"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func ptr(value string) *string { return &value }

// hookResolver runs hook before each source read, standing in for a writer
// that edits view files while Save validates its state.
type hookResolver struct {
	SourceResolver
	hook func()
}

func (h hookResolver) ResolveSource(ctx context.Context, def viewconfig.ViewDefinition, req ExecuteRequest) (SourceResult, error) {
	h.hook()
	return h.SourceResolver.ResolveSource(ctx, def, req)
}

func TestSaveConflictsWhenTheViewFileChangesWhileSaving(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	path := filepath.Join(root, ".rhizome/views/work.yaml")
	writeSourceFixture(t, root, ".rhizome/views/work.yaml", authoredTaskView)
	service, _ := indexTypeViewsFixture(t, root)
	edited := strings.Replace(authoredTaskView, "name: Work", "name: Work edited elsewhere", 1)
	service.opts.SourceResolver = hookResolver{SourceResolver: service.opts.SourceResolver, hook: func() {
		require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))
	}}

	_, err := service.Save(context.Background(), "work", SaveRequest{State: SaveState{Variant: "kanban"}})
	require.ErrorIs(t, err, ErrSaveConflict)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, edited, string(after), "the other writer's edit survives")
}

func TestSaveNeverReplacesAViewFileCreatedWhileSaving(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	path := filepath.Join(root, ".rhizome/views/task.yaml")
	service, _ := indexTypeViewsFixture(t, root)
	other := "apiVersion: rhizome.view.v1\nid: other\nname: Other\nsource: {kind: ontology_type, type: Area}\nmount: {kind: standalone}\nvariants: {table: {columns: [{field: title}]}}\n"
	service.opts.SourceResolver = hookResolver{SourceResolver: service.opts.SourceResolver, hook: func() {
		writeSourceFixture(t, root, ".rhizome/views/task.yaml", other)
	}}

	_, err := service.Save(context.Background(), viewconfig.GeneratedTypeID("Task"), SaveRequest{State: SaveState{Variant: "table"}})
	require.ErrorIs(t, err, ErrSaveConflict)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, other, string(after))
}

func TestSaveRefusesASymlinkedRhizomeFolderBeforeCreatingAnything(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()
	writeSourceFixture(t, outside, "ontology/schema.graphql", typeViewsSchema)
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".rhizome")))
	for path, body := range saveFixture {
		writeSourceFixture(t, root, path, body)
	}
	service, _ := indexTypeViewsFixture(t, root)

	_, err := service.Save(context.Background(), viewconfig.GeneratedTypeID("Task"), SaveRequest{State: SaveState{Variant: "table"}})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = os.Stat(filepath.Join(outside, "views"))
	require.ErrorIs(t, err, os.ErrNotExist, "no folder was created outside the vault")
}

func TestSaveCanSayNoGroupingAndExecutionHonorsIt(t *testing.T) {
	root := writeTypeViewsFixture(t, map[string]string{
		"meetings/a.md": "---\nheld: 2026-09-01\n---\n# A\n",
		"meetings/b.md": "---\nheld: 2026-08-01\n---\n# B\n",
	})
	service, _ := indexTypeViewsFixture(t, root)
	generated := viewconfig.GeneratedTypeID("Meeting")
	before, err := service.Execute(context.Background(), generated, ExecuteRequest{Variant: "table"})
	require.NoError(t, err)
	require.Len(t, before.Groups, 2, "the generated view groups meetings by month")

	resp, err := service.Save(context.Background(), generated, SaveRequest{State: SaveState{Variant: "table", Group: &viewconfig.GroupSpec{Field: ""}}})
	require.NoError(t, err)
	defs, issues := viewconfig.LoadPath(filepath.Join(root, resp.Path))
	require.Empty(t, issues)
	require.Equal(t, &viewconfig.GroupSpec{Field: viewconfig.GroupNone}, defs[0].Defaults.Group)
	result := validate.RunViews(context.Background(), validate.RunContext{VaultPath: root})
	require.True(t, result.OK, "%+v", result.Issues)

	saved, err := service.Execute(context.Background(), resp.ID, ExecuteRequest{})
	require.NoError(t, err)
	require.Empty(t, saved.Groups, "no default grouping overrides the saved choice")
	ungrouped, err := service.Execute(context.Background(), generated, ExecuteRequest{Variant: "table", Group: &viewconfig.GroupSpec{Field: viewconfig.GroupNone}})
	require.NoError(t, err)
	require.Empty(t, ungrouped.Groups, "a request can ask for no grouping the same way")
}

func TestSaveLeavesSettingsTheStateOmitsUnchanged(t *testing.T) {
	root := writeTypeViewsFixture(t, saveFixture)
	path := filepath.Join(root, ".rhizome/views/work.yaml")
	authored := strings.Replace(authoredTaskView, "  table:\n    columns:", "  table:\n    density: one-line\n    columns:", 1)
	authored = strings.Replace(authored, "    hideEmptyColumns: true\n", "    hideEmptyColumns: true\n    laneField: priority\n", 1)
	authored = strings.Replace(authored, "  sort:\n    - field: title\n", "  sort:\n    - field: title\n  group: {field: status}\n", 1)
	writeSourceFixture(t, root, ".rhizome/views/work.yaml", authored)
	service, _ := indexTypeViewsFixture(t, root)

	_, err := service.Save(context.Background(), "work", SaveRequest{State: SaveState{Variant: "kanban", Sort: []viewconfig.SortSpec{{Field: "title"}}}})
	require.NoError(t, err)
	defs, issues := viewconfig.LoadPath(path)
	require.Empty(t, issues)
	require.Equal(t, "one-line", defs[0].Variants.Table.Density)
	require.Equal(t, &viewconfig.KanbanVariant{ColumnField: "status", HideEmptyColumns: true, LaneField: "priority"}, defs[0].Variants.Kanban)
	require.Equal(t, &viewconfig.GroupSpec{Field: "status"}, defs[0].Defaults.Group)

	_, err = service.Save(context.Background(), "work", SaveRequest{State: SaveState{Variant: "kanban", Density: ptr(""), LaneField: ptr("")}})
	require.NoError(t, err)
	defs, issues = viewconfig.LoadPath(path)
	require.Empty(t, issues)
	require.Empty(t, defs[0].Variants.Table.Density, "an explicit empty density removes the key")
	require.Empty(t, defs[0].Variants.Kanban.LaneField, "an explicit empty lane field returns to the default lanes")
}
