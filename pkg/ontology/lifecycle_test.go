package ontology

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func loadSDL(t *testing.T, sdl string) (*Schema, error) {
	t.Helper()
	root := t.TempDir()
	dir := OntologyDir(root)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(sdl), 0o644))
	return LoadSchema(root)
}

func mustLoadSDL(t *testing.T, sdl string) *Schema {
	t.Helper()
	schema, err := loadSDL(t, sdl)
	require.NoError(t, err)
	return schema
}

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
	require.ErrorContains(t, err, "invalid @view directive on WorkStatus.ready")
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

func TestNodeFieldCapabilityCarriesStage(t *testing.T) {
	schema := mustLoadSDL(t, `
enum WorkStatus {
  ready @view(stage: "open")
  done @view(stage: "done")
}
`+lifecycleTaskType)
	noteType := schema.Types["Task"]
	capability := buildNodeFieldCapability(schema, &NodeProjection{Type: noteType}, noteType.ByName["status"], FieldBinding{})
	require.Equal(t, []NodeFieldEnumOption{
		{Value: "ready", Label: "ready", Tone: "neutral", Stage: StageOpen},
		{Value: "done", Label: "done", Tone: "success", Stage: StageDone},
	}, capability.EnumOptions)
}

func TestSchemaDocsReportEnumValueView(t *testing.T) {
	schema := mustLoadSDL(t, `
enum WorkStatus {
  ready @view(label: "Ready", order: 10, stage: "open")
  cut @view(order: 90, stage: "dropped")
}
`+lifecycleTaskType)
	docs, err := SchemaDocs(schema, "Task")
	require.NoError(t, err)
	encoded, err := json.Marshal(docs[0].Enums[0].Values)
	require.NoError(t, err)
	require.JSONEq(t, `[
		{"name": "ready", "label": "Ready", "order": 10, "tone": "neutral", "stage": "open", "stageDeclared": true},
		{"name": "cut", "order": 90, "tone": "muted", "collapsed": true, "stage": "dropped", "stageDeclared": true}
	]`, string(encoded))

	// Inferred stages leave tone and collapsed as authored, so SPEC-0111's
	// "has order or tone metadata" lifecycle test still reads authored intent.
	schema = mustLoadSDL(t, `
enum WorkStatus {
  ready
  doing @view(tone: "progress")
}
`+lifecycleTaskType)
	docs, err = SchemaDocs(schema, "Task")
	require.NoError(t, err)
	encoded, err = json.Marshal(docs[0].Enums[0].Values)
	require.NoError(t, err)
	require.JSONEq(t, `[
		{"name": "ready", "stage": "open"},
		{"name": "doing", "tone": "progress", "stage": "active"}
	]`, string(encoded))
}
