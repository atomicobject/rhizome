package views

import (
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestGeneratedWorkflowDefaultVariantAgreesWithTheCatalog(t *testing.T) {
	spread := map[string]string{
		"tasks/a.md": "---\nstatus: planned\n---\n# A\n",
		"tasks/b.md": "---\nstatus: doing\n---\n# B\n",
		"tasks/c.md": "---\nstatus: shipped\n---\n# C\n",
	}
	oneColumn := map[string]string{
		"tasks/a.md": "---\nstatus: doing\n---\n# A\n",
		"tasks/b.md": "---\nstatus: doing\n---\n# B\n",
		"tasks/c.md": "---\nstatus: shipped\n---\n# C\n",
	}
	crowded := map[string]string{"tasks/z.md": "---\nstatus: doing\n---\n# Z\n"}
	for i := range boardOpenRecordLimit {
		crowded[fmt.Sprintf("tasks/t%02d.md", i)] = "---\nstatus: planned\n---\n# T\n"
	}
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"open work spans two columns", spread, "kanban"},
		{"one populated open column", oneColumn, "table"},
		{"more than sixty open records", crowded, "table"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, _ := newTypeViewsService(t, tc.files)
			id := viewconfig.GeneratedTypeID("Task")

			catalog, err := service.Catalog(context.Background())
			require.NoError(t, err)
			require.Equal(t, viewChoiceID(id, tc.want), findTarget(t, catalog, viewconfig.MountKindType, "Task").DefaultChoiceID)
			entry, err := service.View(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, tc.want, entry.Defaults.Variant)
			require.Equal(t, "table", entry.Definition.Defaults.Variant, "the definition stays count-independent")
			resp, err := service.Execute(context.Background(), id, ExecuteRequest{})
			require.NoError(t, err)
			require.Equal(t, tc.want, resp.Variant)
		})
	}
}

func TestBoardDefaultNeedsAWorkflowShape(t *testing.T) {
	service, _ := newTypeViewsService(t, map[string]string{
		"projects/a.md": "---\nstatus: proposed\n---\n# A\n",
		"projects/b.md": "---\nstatus: live\n---\n# B\n",
	})
	catalog, err := service.Catalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, viewChoiceID(viewconfig.GeneratedTypeID("Project"), "table"), findTarget(t, catalog, viewconfig.MountKindType, "Project").DefaultChoiceID)
}

func TestBoardFitsOpenWorkCountsOnlyOpenAndActiveValues(t *testing.T) {
	stages := map[string]ontology.LifecycleStage{"Planned": ontology.StageOpen, "Doing": ontology.StageActive, "Done": ontology.StageDone}
	require.True(t, boardFitsOpenWork(stages, map[string]int{"planned": 1, "doing": 59, "done": 500}))
	require.False(t, boardFitsOpenWork(stages, map[string]int{"planned": 2, "doing": 59}))
	require.False(t, boardFitsOpenWork(stages, map[string]int{"doing": 3, "done": 3}))
}
