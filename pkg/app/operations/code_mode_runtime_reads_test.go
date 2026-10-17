package operations

import (
	"context"
	"errors"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type fakeReadRuntime struct {
	vault           string
	indexErr        error
	syncErr         error
	readiness       semdb.OntologyReadinessState
	readinessSource func(context.Context) (semdb.OntologyReadinessState, error)
	depsCalled      bool
}

func (f *fakeReadRuntime) VaultPath() string                      { return f.vault }
func (f *fakeReadRuntime) WaitForCodeIndex(context.Context) error { return f.indexErr }
func (f *fakeReadRuntime) WaitForSemantic(context.Context) error  { return nil }
func (f *fakeReadRuntime) SyncSources(context.Context) error      { return f.syncErr }
func (f *fakeReadRuntime) QueryDeps(schema *ontoschema.Schema, exec *ontologyquery.ExecutableSchema) (ontologyquery.Deps, error) {
	// Current-user lookups prepare their own query against the executable schema.
	f.depsCalled = schema != nil && exec != nil
	return ontologyquery.Deps{}, nil
}
func (f *fakeReadRuntime) ProjectionReadiness(ctx context.Context) (semdb.OntologyReadinessState, error) {
	if f.readinessSource != nil {
		return f.readinessSource(ctx)
	}
	return f.readiness, nil
}
func (f *fakeReadRuntime) Views(*ontoschema.Schema, *ontologyquery.ExecutableSchema) (CodeModeViewRuntime, error) {
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
	schema, err := ontoschema.LoadSchema(vault)
	require.NoError(t, err)
	current := func() *fakeReadRuntime {
		return &fakeReadRuntime{vault: vault, readiness: semdb.OntologyReadinessState{
			Ready:         true,
			SchemaState:   semdb.OntologySchemaState{Ready: true, SchemaHash: schema.Hash, NotesHash: "n", MaterializationVersion: ontology.OntologyMaterializationVersion},
			MetadataState: semdb.NoteMetadataState{Ready: true, NotesHash: "n"}}}
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
		"schema changed":                            func(f *fakeReadRuntime) { f.readiness.SchemaState.SchemaHash = "old" },
		"old materialization":                       func(f *fakeReadRuntime) { f.readiness.SchemaState.MaterializationVersion-- },
		"proof missing with unchanged ready states": func(f *fakeReadRuntime) { f.readiness.Ready = false },
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

func TestCodeModeRuntimeReadsRejectsOnlyTheInvalidatedProof(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	schemaPath := filepath.Join(root, ".rhizome", "ontology", "schema.graphql")
	require.NoError(t, os.MkdirAll(filepath.Dir(schemaPath), 0o755))
	require.NoError(t, os.WriteFile(schemaPath, []byte("type Task @node(paths: [\"notes/*.md\"]) {\n status: String\n}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "task.md"), []byte("---\nstatus: open\n---\n# Task\n"), 0o644))
	metadata := testkit.NoteMetadataIndexer(t)
	runtime, cleanup, err := ontologytest.Open(ctx, metadata, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}}, &obsidian.Note{})
	require.NoError(t, err)
	defer cleanup()
	require.True(t, runtime.Ready)
	formats, err := metadata.FormatRuntime()
	require.NoError(t, err)
	providerHash := ontology.OntologyProviderHash(formats)
	reads := CodeModeRuntimeReads{Runtime: &fakeReadRuntime{vault: root,
		readinessSource: func(ctx context.Context) (semdb.OntologyReadinessState, error) {
			return runtime.Store.GetOntologyReadiness(ctx, providerHash)
		}}}
	require.True(t, reads.projectionCurrent(ctx, runtime.Schema))
	before, err := runtime.Store.GetOntologyReadiness(ctx, providerHash)
	require.NoError(t, err)
	_, err = runtime.Store.DB().ExecContext(ctx, `DELETE FROM ontology_readiness`)
	require.NoError(t, err)
	require.False(t, reads.projectionCurrent(ctx, runtime.Schema))
	after, err := runtime.Store.GetOntologyReadiness(ctx, providerHash)
	require.NoError(t, err)
	require.Equal(t, before.SchemaState, after.SchemaState)
	require.Equal(t, before.MetadataState, after.MetadataState)
	require.True(t, after.SchemaState.Ready)
	require.True(t, after.MetadataState.Ready)
	require.False(t, after.Ready, "the reader must not recertify an invalidated proof")
}
