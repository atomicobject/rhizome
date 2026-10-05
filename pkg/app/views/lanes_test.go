package views

import (
	"context"
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

var laneFixture = map[string]string{
	"projects/apollo.md": "---\nstatus: live\n---\n# Apollo\n",
	"projects/zeus.md":   "---\nstatus: proposed\n---\n# Zeus\n",
	"tasks/a.md":         "---\nstatus: planned\nproject: ['[[apollo]]']\n---\n# A\n",
	"tasks/b.md":         "---\nstatus: doing\nproject: ['[[apollo]]']\n---\n# B\n",
	"tasks/c.md":         "---\nstatus: doing\nproject: ['[[zeus]]']\n---\n# C\n",
	"tasks/d.md":         "---\nstatus: planned\nproject: ['[[apollo]]', '[[zeus]]']\n---\n# D\n",
	"tasks/e.md":         "---\nstatus: shipped\n---\n# E\n",
}

// laneCells maps each lane label to its non-empty cells' row titles.
func laneCells(t *testing.T, resp ExecuteResponse) (labels []string, cells map[string]map[string][]string) {
	cells = map[string]map[string][]string{}
	for _, lane := range resp.Board.Lanes {
		labels = append(labels, lane.Label)
		cells[lane.Label] = map[string][]string{}
		require.Len(t, lane.Cells, len(resp.Board.Columns))
		for _, cell := range lane.Cells {
			for _, index := range cell.Rows {
				cells[lane.Label][cell.Value] = append(cells[lane.Label][cell.Value], resp.Rows[index].Title)
			}
			sort.Strings(cells[lane.Label][cell.Value])
		}
	}
	return labels, cells
}

func TestBoardLanesDefaultToARelationOrderedByTargetStage(t *testing.T) {
	service, _ := newTypeViewsService(t, laneFixture)

	resp, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)

	require.Equal(t, "project", resp.Board.LaneField, "no priorities, and five targets over four records averages 1.25")
	labels, cells := laneCells(t, resp)
	require.Equal(t, []string{"Zeus", "Apollo", "No project"}, labels, "the proposed project's lane leads the live one")
	require.Equal(t, map[string][]string{"planned": {"D"}, "doing": {"C"}}, cells["Zeus"])
	require.Equal(t, map[string][]string{"planned": {"A", "D"}, "doing": {"B"}}, cells["Apollo"], "D appears in both of its projects' lanes")
	require.Equal(t, map[string][]string{"shipped": {"E"}}, cells["No project"])
	require.Equal(t, 3, resp.Board.Lanes[1].Count)
	require.Equal(t, "success", resp.Board.Lanes[1].Tone, "relation lanes take the target's status tone")
}

func TestBoardLanesPreferAnOrderedFieldAndCanBeSwitchedOff(t *testing.T) {
	files := map[string]string{}
	for path, body := range laneFixture {
		files[path] = body
	}
	files["tasks/a.md"] = "---\nstatus: planned\npriority: high\n---\n# A\n"
	files["tasks/b.md"] = "---\nstatus: doing\npriority: low\n---\n# B\n"
	service, _ := newTypeViewsService(t, files)
	id := viewconfig.GeneratedTypeID("Task")

	resp, err := service.Execute(context.Background(), id, ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	require.Equal(t, "priority", resp.Board.LaneField)
	labels, _ := laneCells(t, resp)
	require.Equal(t, []string{"low", "high", "No priority"}, labels, "enum lanes follow the enum")

	resp, err = service.Execute(context.Background(), id, ExecuteRequest{Variant: "kanban", LaneField: "none"})
	require.NoError(t, err)
	require.Empty(t, resp.Board.LaneField)
	require.Empty(t, resp.Board.Lanes)

	resp, err = service.Execute(context.Background(), id, ExecuteRequest{Variant: "kanban", LaneField: "owner"})
	require.NoError(t, err)
	labels, _ = laneCells(t, resp)
	require.Equal(t, []string{"No owner"}, labels)
}

func TestBoardColumnFieldSwitchesAmongLifecycleAndOrderedFields(t *testing.T) {
	service, _ := newTypeViewsService(t, laneFixture)
	id := viewconfig.GeneratedTypeID("Task")

	resp, err := service.Execute(context.Background(), id, ExecuteRequest{Variant: "kanban", ColumnField: "priority", LaneField: "none"})
	require.NoError(t, err)
	require.Equal(t, "priority", resp.Board.ColumnField)
	require.Equal(t, "priority", resp.State.ColumnField)
	var columns []string
	for _, column := range resp.Board.Columns {
		columns = append(columns, column.Value)
	}
	require.Equal(t, []string{"low", "high", ""}, columns)

	for _, req := range []ExecuteRequest{
		{Variant: "kanban", ColumnField: "summary"},
		{Variant: "kanban", LaneField: "status"},
		{Variant: "kanban", LaneField: "missing"},
	} {
		_, err := service.Execute(context.Background(), id, req)
		require.ErrorIs(t, err, ErrInvalidRequest, req)
	}
}

func TestBoardRowsNameTheirReverseRelations(t *testing.T) {
	files := map[string]string{
		"projects/apollo.md": "---\nstatus: live\n---\n# Apollo\n",
		"projects/empty.md":  "---\nstatus: proposed\n---\n# Empty\n",
	}
	for _, name := range []string{"k", "c", "j", "a", "i", "b", "h", "d", "g"} {
		files["tasks/"+name+".md"] = "---\nstatus: planned\nproject: ['[[apollo]]']\n---\n# " + name + "\n"
	}
	service, _ := newTypeViewsService(t, files)

	resp, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Project"), ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	byTitle := map[string]TableRow{}
	for _, row := range resp.Rows {
		byTitle[row.Title] = row
	}
	apollo := byTitle["Apollo"]
	require.Equal(t, 9, apollo.Fields["tasks"], "the count keeps every linked record")
	var names []string
	for _, value := range apollo.RelationValues["tasks"] {
		names = append(names, value.Title)
		require.NotNil(t, value.Ref)
	}
	require.Equal(t, []string{"a", "b", "c", "d", "g", "h", "i", "j"}, names, "the first eight by title")
	require.Equal(t, "[[tasks/a]]", apollo.RelationValues["tasks"][0].Value)
	require.Empty(t, byTitle["Empty"].RelationValues["tasks"])
}

func TestConfiguredBoardFieldsThatDoNotWorkFallBackWithAWarning(t *testing.T) {
	files := map[string]string{
		"tasks/a.md": "---\nstatus: planned\npriority: high\n---\n# A\n",
		"tasks/b.md": "---\nstatus: doing\npriority: low\n---\n# B\n",
	}
	board := func(id, lane string) viewconfig.ViewDefinition {
		return viewconfig.ViewDefinition{
			APIVersion: viewconfig.APIVersion, ID: id, Name: id,
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Defaults:   viewconfig.DefaultsSpec{Variant: "kanban"},
			Variants:   viewconfig.VariantSet{Kanban: &viewconfig.KanbanVariant{ColumnField: "status", LaneField: lane}},
		}
	}
	service, _ := newTypeViewsService(t, files, board("unknown", "bogus"), board("same", "status"))

	for _, id := range []string{"unknown", "same"} {
		resp, err := service.Execute(context.Background(), id, ExecuteRequest{})
		require.NoError(t, err, id)
		require.Equal(t, "priority", resp.Board.LaneField, "%s falls back to the profile default lane", id)
		require.Contains(t, warningCodes(resp.Warnings), "view_kanban_lane_unsupported", id)
	}
	_, err := service.Execute(context.Background(), "unknown", ExecuteRequest{LaneField: "bogus"})
	require.ErrorIs(t, err, ErrInvalidRequest, "a requested lane field still fails")
}

func TestListEnumsAreNotBoardColumnOptions(t *testing.T) {
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
enum Stage { todo @view(stage: "open") doing @view(stage: "active") }
enum Tag { a @view(tone: "neutral") b @view(tone: "progress") }
type Item @node(paths: ["items/*.md"]) {
  stage: Stage @field @display(importance: KEY)
  tags: [Tag!] @field @display(importance: KEY)
}
`)
	writeSourceFixture(t, root, "items/one.md", "---\nstage: todo\ntags: [a, b]\n---\n# One\n")
	service, _ := indexTypeViewsFixture(t, root)
	id := viewconfig.GeneratedTypeID("Item")

	resp, err := service.Execute(context.Background(), id, ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	require.Contains(t, resp.Profile.OrderedFields, "tags")
	_, err = service.Execute(context.Background(), id, ExecuteRequest{Variant: "kanban", ColumnField: "tags"})
	require.ErrorIs(t, err, ErrInvalidRequest, "a list cannot hold one column value")
}

func TestTableRowsCountReverseRelationsWithoutNamingThem(t *testing.T) {
	service, _ := newTypeViewsService(t, laneFixture)

	resp, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Project"), ExecuteRequest{Variant: "table"})
	require.NoError(t, err)
	for _, row := range resp.Rows {
		if row.Title == "Apollo" {
			require.Equal(t, 3, row.Fields["tasks"], "the table keeps its count column")
			require.Empty(t, row.RelationValues["tasks"], "only boards and cards name linking records")
			return
		}
	}
	t.Fatal("Apollo row missing")
}

func TestBoardAndCardRelationValuesCarryTheLinkedRecordsStatus(t *testing.T) {
	service, _ := newTypeViewsService(t, laneFixture)
	rowByTitle := func(resp ExecuteResponse, title string) TableRow {
		for _, row := range resp.Rows {
			if row.Title == title {
				return row
			}
		}
		t.Fatalf("row %s missing", title)
		return TableRow{}
	}

	board, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	project := rowByTitle(board, "A").RelationValues["project"]
	require.Len(t, project, 1)
	require.Equal(t, &TableRelationStatus{Value: "live", Label: "live", Tone: "success", Stage: ontology.StageDone}, project[0].Status, "forward link to a project")

	cards, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Project"), ExecuteRequest{Variant: "kanban"})
	require.NoError(t, err)
	statuses := map[string]string{}
	for _, value := range rowByTitle(cards, "Apollo").RelationValues["tasks"] {
		require.NotNil(t, value.Status, value.Title)
		statuses[value.Title] = string(value.Status.Stage)
	}
	require.Equal(t, map[string]string{"A": "open", "B": "active", "D": "open"}, statuses, "reverse links to tasks")

	table, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{Variant: "table"})
	require.NoError(t, err)
	require.Nil(t, rowByTitle(table, "A").RelationValues["project"][0].Status, "tables read no linked statuses")
}
