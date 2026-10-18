package ontology

import (
	"encoding/json"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func loadSDL(t *testing.T, sdl string) (*ontoschema.Schema, error) {
	t.Helper()
	root := t.TempDir()
	dir := ontoschema.OntologyDir(root)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(sdl), 0o644))
	return ontoschema.LoadSchema(root)
}

func mustLoadSDL(t *testing.T, sdl string) *ontoschema.Schema {
	t.Helper()
	schema, err := loadSDL(t, sdl)
	require.NoError(t, err)
	return schema
}

const lifecycleTaskType = `
type Task @node(paths: ["tasks/*.md"]) { status: WorkStatus }
`

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
		{Value: "ready", Label: "ready", Tone: "neutral", Stage: ontoschema.StageOpen},
		{Value: "done", Label: "done", Tone: "success", Stage: ontoschema.StageDone},
	}, capability.EnumOptions)
}

func TestSchemaDocsReportEnumValueView(t *testing.T) {
	schema := mustLoadSDL(t, `
enum WorkStatus {
  ready @view(label: "Ready", order: 10, stage: "open")
  cut @view(order: 90, stage: "dropped")
}
`+lifecycleTaskType)
	docs, err := schemadoc.SchemaDocs(schema, "Task", identity.CurrentUserType)
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
	docs, err = schemadoc.SchemaDocs(schema, "Task", identity.CurrentUserType)
	require.NoError(t, err)
	encoded, err = json.Marshal(docs[0].Enums[0].Values)
	require.NoError(t, err)
	require.JSONEq(t, `[
		{"name": "ready", "stage": "open"},
		{"name": "doing", "tone": "progress", "stage": "active"}
	]`, string(encoded))
}
