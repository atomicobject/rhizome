package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/stretchr/testify/require"
)

type fakeReadRuntime struct {
	vault      string
	indexErr   error
	syncErr    error
	state      semdb.OntologySchemaState
	notes      semdb.NoteMetadataState
	depsCalled bool
}

func (f *fakeReadRuntime) VaultPath() string                      { return f.vault }
func (f *fakeReadRuntime) WaitForCodeIndex(context.Context) error { return f.indexErr }
func (f *fakeReadRuntime) WaitForSemantic(context.Context) error  { return nil }
func (f *fakeReadRuntime) SyncSources(context.Context) error      { return f.syncErr }
func (f *fakeReadRuntime) QueryDeps(schema *ontology.Schema, exec *ontologyquery.ExecutableSchema) ontologyquery.Deps {
	// Current-user lookups prepare their own query against the executable schema.
	f.depsCalled = schema != nil && exec != nil
	return ontologyquery.Deps{}
}
func (f *fakeReadRuntime) ProjectionStates(context.Context) (semdb.OntologySchemaState, semdb.NoteMetadataState, error) {
	return f.state, f.notes, nil
}
func (f *fakeReadRuntime) Views(*ontology.Schema, *ontologyquery.ExecutableSchema) (CodeModeViewRuntime, error) {
	return fakeCatalogViews{}, nil
}

type fakeCatalogViews struct{ CodeModeViewRuntime }

func (fakeCatalogViews) Catalog(context.Context) (appviews.Catalog, error) {
	return appviews.Catalog{}, nil
}

func TestCodeModeRuntimeReadsHandBackWhatTheyCannotProveCurrent(t *testing.T) {
	vault := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vault, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vault, ".rhizome", "ontology", "schema.graphql"),
		[]byte("type Task @node(paths: [\"notes/*.md\"]) {\n  status: String\n}\n"), 0o644))
	schema, err := ontology.LoadSchema(vault)
	require.NoError(t, err)
	current := func() *fakeReadRuntime {
		return &fakeReadRuntime{vault: vault,
			state: semdb.OntologySchemaState{Ready: true, SchemaHash: schema.Hash, NotesHash: "n", MaterializationVersion: ontology.OntologyMaterializationVersion},
			notes: semdb.NoteMetadataState{Ready: true, NotesHash: "n"}}
	}
	query := map[string]any{"query": "{ task(first: 1) { path } }"}
	pending := func(runtime *fakeReadRuntime, name string, input map[string]any) bool {
		outcome := (&CodeModeRuntimeReads{Runtime: runtime}).Call(context.Background(), name, input)
		return IsCodeModeRuntimeReadPending(outcome.Diagnostic)
	}

	runtime := current()
	runtime.indexErr = errors.New("opening")
	require.True(t, pending(runtime, "view", map[string]any{"action": "list"}))

	runtime = current()
	runtime.syncErr = errors.New("unapplied change")
	require.True(t, pending(runtime, "ontology_query", query))
	require.False(t, runtime.depsCalled)

	for name, mutate := range map[string]func(*fakeReadRuntime){
		"schema changed":        func(f *fakeReadRuntime) { f.state.SchemaHash = "old" },
		"ontology behind notes": func(f *fakeReadRuntime) { f.notes.NotesHash = "newer" },
		"projection not ready":  func(f *fakeReadRuntime) { f.state.Ready = false },
	} {
		runtime = current()
		mutate(runtime)
		require.True(t, pending(runtime, "ontology_query", query), name)
	}

	// An invalid query is the caller's error, not a reason to hand it back.
	runtime = current()
	outcome := (&CodeModeRuntimeReads{Runtime: runtime}).Call(context.Background(), "ontology_query", map[string]any{"query": "{ nope }"})
	require.Equal(t, 1, outcome.ExitCode)
	require.False(t, IsCodeModeRuntimeReadPending(outcome.Diagnostic))
	require.Contains(t, outcome.Diagnostic.(map[string]any), "errors")

	// Views read the persisted projection and need no source sync.
	runtime = current()
	runtime.syncErr = errors.New("unapplied change")
	outcome = (&CodeModeRuntimeReads{Runtime: runtime}).Call(context.Background(), "view", map[string]any{"action": "list"})
	require.True(t, outcome.OK, outcome.Stderr)

	runtime = current()
	outcome = (&CodeModeRuntimeReads{Runtime: runtime}).Call(context.Background(), "ontology_query", query)
	require.True(t, runtime.depsCalled, "a current projection is read from the runtime")
	require.False(t, IsCodeModeRuntimeReadPending(outcome.Diagnostic))
}
