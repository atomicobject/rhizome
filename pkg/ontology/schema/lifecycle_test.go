package schema

import (
	"github.com/stretchr/testify/require"
	"testing"
)

const lifecycleTaskType = `
type Task @node(paths: ["tasks/*.md"]) {
  status: WorkStatus
}
`

func TestDeclaredStagesDriveToneAndCollapse(t *testing.T) {
	schema := mustLoadSDL(t, `
enum WorkStatus {
  idea @view(stage: "open")
  doing @view(stage: "active")
  shipped @view(stage: "done")
  reviewed @view(stage: "done", tone: "info")
  cut @view(stage: "dropped")
  parked @view(stage: "dropped", collapsed: false)
}
`+lifecycleTaskType)
	status := schema.EnumTypes["WorkStatus"]

	require.Equal(t, StageActive, status.ByName["doing"].View.Stage)
	require.Equal(t, []EnumValueStage{
		{Stage: StageOpen, Declared: true},
		{Stage: StageActive, Declared: true},
		{Stage: StageDone, Declared: true},
		{Stage: StageDone, Declared: true},
		{Stage: StageDropped, Declared: true},
		{Stage: StageDropped, Declared: true},
	}, status.Stages())
	require.Equal(t, []string{"neutral", "progress", "success", "info", "muted", "muted"}, status.Tones())
	require.Equal(t, []bool{false, false, false, false, true, false}, status.CollapsedDefaults())
}

func TestStageRejectsUnknownValue(t *testing.T) {
	_, err := loadSDL(t, `
enum WorkStatus {
  ready @view(stage: "blocked")
}
`+lifecycleTaskType)
	rule := requireCompileError(t, err, "enum_view_stage")
	require.Equal(t, "WorkStatus", rule.TypeName)
	require.Equal(t, "ready", rule.FieldName)
	require.ErrorContains(t, err, "stage must be one of open, active, done, or dropped")
}

func TestStagesAreAllOrNone(t *testing.T) {
	_, err := loadSDL(t, `
enum WorkStatus {
  ready @view(stage: "open")
  doing
  done @view(tone: "success")
}
`+lifecycleTaskType)
	require.ErrorContains(t, err, "enum WorkStatus declares @view(stage:) on some values but not on doing, done")
}

func TestStagesInferFromAuthoredMetadata(t *testing.T) {
	schema := mustLoadSDL(t, `
enum WorkStatus {
  idea
  doing @view(tone: "progress")
  review @view(tone: "warning")
  shipped @view(tone: "success")
  cut @view(tone: "muted")
  parked @view(collapsed: true)
  hidden @view(tone: "info", collapsed: true)
}

enum Kind {
  alpha @view(label: "Alpha", order: 1)
  beta
}

type Task @node(paths: ["tasks/*.md"]) {
  status: WorkStatus
  kind: Kind
}
`)
	status := schema.EnumTypes["WorkStatus"]
	stages := make([]LifecycleStage, 0, len(status.Values))
	for _, stage := range status.Stages() {
		require.False(t, stage.Declared)
		stages = append(stages, stage.Stage)
	}
	require.Equal(t, []LifecycleStage{StageOpen, StageActive, StageOpen, StageDone, StageDropped, StageDropped, StageOpen}, stages)
	// Inferred stages never change tone or collapse: the existing fallback stays.
	require.Equal(t, []string{"neutral", "progress", "warning", "success", "muted", "muted", "info"}, status.Tones())
	require.Equal(t, []bool{false, false, false, false, false, true, true}, status.CollapsedDefaults())

	require.Nil(t, schema.EnumTypes["Kind"].Stages(), "labels and order alone infer no stages")
}
