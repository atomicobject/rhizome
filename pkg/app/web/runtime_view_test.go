package web

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type runtimeViewNoteReader struct {
	content string
	paths   []string
}

func (r *runtimeViewNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return r.content, nil
}
func (r *runtimeViewNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return append([]string(nil), r.paths...), nil
}
func (*runtimeViewNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, nil
}
func (*runtimeViewNoteReader) Title(string) (string, bool) { return "", false }

type webRuntimeView struct {
	snapshot runtimeview.Snapshot
}

type delayedIntelRuntimeView struct {
	store atomic.Pointer[semdb.Store]
}

type swappingIntelRuntimeView struct {
	first *semdb.Store
	next  *semdb.Store
	calls atomic.Int32
}

type ownershipFlipRuntimeView struct {
	store    *semdb.Store
	calls    atomic.Int32
	onSecond func()
}

func (view *delayedIntelRuntimeView) Snapshot() runtimeview.Snapshot {
	return runtimeview.Snapshot{IntelStore: view.store.Load()}
}
func (*delayedIntelRuntimeView) WaitForSearch(context.Context) error    { return nil }
func (*delayedIntelRuntimeView) WaitForSemantic(context.Context) error  { return nil }
func (*delayedIntelRuntimeView) WaitForCodeIndex(context.Context) error { return nil }

func (view *swappingIntelRuntimeView) Snapshot() runtimeview.Snapshot {
	if view.calls.Add(1) == 1 {
		return runtimeview.Snapshot{IntelStore: view.first}
	}
	return runtimeview.Snapshot{IntelStore: view.next}
}
func (*swappingIntelRuntimeView) WaitForSearch(context.Context) error    { return nil }
func (*swappingIntelRuntimeView) WaitForSemantic(context.Context) error  { return nil }
func (*swappingIntelRuntimeView) WaitForCodeIndex(context.Context) error { return nil }

func (view *ownershipFlipRuntimeView) Snapshot() runtimeview.Snapshot {
	if view.calls.Add(1) == 2 && view.onSecond != nil {
		view.onSecond()
	}
	return runtimeview.Snapshot{IntelStore: view.store}
}
func (*ownershipFlipRuntimeView) WaitForSearch(context.Context) error    { return nil }
func (*ownershipFlipRuntimeView) WaitForSemantic(context.Context) error  { return nil }
func (*ownershipFlipRuntimeView) WaitForCodeIndex(context.Context) error { return nil }

func (view *webRuntimeView) Snapshot() runtimeview.Snapshot { return view.snapshot }
func (*webRuntimeView) WaitForSearch(context.Context) error { return nil }
func (*webRuntimeView) WaitForSemantic(context.Context) error {
	return nil
}
func (*webRuntimeView) WaitForCodeIndex(context.Context) error { return nil }

func replaceDurableNoteFacts(ctx context.Context, store *semdb.Store, notePath, title, summary, tag string) error {
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE notes SET title = ? WHERE path = ?`, title, notePath); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE note_property_values
		SET value_text = ?, value_norm = ?
		WHERE note_id = (SELECT id FROM notes WHERE path = ?)
		  AND property_id = (SELECT property_id FROM property_keys WHERE property_name = 'summary')
		  AND source = ?
	`, summary, summary, notePath, semdb.NotePropertySourceFrontmatter); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM note_tags WHERE note_id = (SELECT id FROM notes WHERE path = ?)`, notePath); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO note_tags(note_id, tag_norm)
		SELECT id, ? FROM notes WHERE path = ?
	`, tag, notePath); err != nil {
		return err
	}
	return tx.Commit()
}

func TestRuntimeReadsLiveSnapshotWithoutCopiedIntelState(t *testing.T) {
	view := &webRuntimeView{}
	copied := &semdb.Store{}
	runtime := &Runtime{Live: view, IntelStore: copied}
	require.Nil(t, runtime.Intel(), "pending live state must not leak a stale copied store")

	live := &semdb.Store{}
	view.snapshot.IntelStore = live
	view.snapshot.Code = runtimeview.CapabilityState{Done: true, Ready: true}
	require.Same(t, live, runtime.Intel())
}

func TestExactGraphQLReadWaitsForLateLiveIntelPublication(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	view := &delayedIntelRuntimeView{}
	runtime := &Runtime{Live: view}
	runtime.EnableIndexGate()
	srv := newFixtureServer(t, fixture, runtime)

	type execution struct {
		result map[string]any
		err    error
	}
	done := make(chan execution, 1)
	go func() {
		result, err := srv.executeOntologyQuery(context.Background(), `{ node(ref: "specs/100-demo/plan.md") { path title } }`)
		done <- execution{result: result.Data, err: err}
	}()
	select {
	case got := <-done:
		t.Fatalf("exact read returned before live intel publication: result=%#v err=%v", got.result, got.err)
	case <-time.After(20 * time.Millisecond):
	}

	view.store.Store(fixture.intelStore)
	runtime.MarkNoteReadReady()
	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.NotNil(t, got.result["node"])
	case <-time.After(time.Second):
		t.Fatal("exact read did not resume after live intel publication")
	}
}

func TestExactGraphQLUsesTheStoreThatPassedTheAvailabilityGate(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	emptyStore, err := semdb.Open(filepath.Join(t.TempDir(), "replacement.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, emptyStore.Close()) })
	view := &swappingIntelRuntimeView{first: fixture.intelStore, next: emptyStore}
	runtime := &Runtime{Live: view}
	runtime.EnableIndexGate()
	runtime.MarkNoteReadReady()
	srv := newFixtureServer(t, fixture, runtime)
	view.calls.Store(0)

	result, err := srv.executeOntologyQuery(context.Background(), `{ node(ref: "specs/100-demo/plan.md") { path title } }`)
	require.NoError(t, err)
	require.NotNil(t, result.Data["node"])
	require.Greater(t, view.calls.Load(), int32(1), "test must exercise a live store swap after the gate probe")
}

func TestExactGraphQLUsesTheOwnershipDecisionThatPassedTheAvailabilityGate(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	var owned atomic.Bool
	owned.Store(true)
	view := &ownershipFlipRuntimeView{store: fixture.intelStore}
	runtime := &Runtime{Live: view}
	runtime.EnableIndexGate()
	runtime.MarkNoteReadReady()
	srv := newFixtureServer(t, fixture, runtime)
	srv.cfg.NotePathOwned = func(string) bool { return owned.Load() }
	view.calls.Store(0)
	view.onSecond = func() { owned.Store(false) }

	result, err := srv.executeOntologyQuery(context.Background(), `{ node: note(path: "specs/100-demo/plan.md") { path frontmatter tags } }`)
	require.NoError(t, err)
	require.False(t, owned.Load(), "test must change ownership after the gate probe")
	node := result.Data["node"].(map[string]any)
	require.Equal(t, "Demo plan summary", node["frontmatter"].(map[string]any)["summary"])
	require.Contains(t, node, "tags")
}

func TestExactGraphQLHydratesOneDurableFactSnapshot(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	const notePath = "specs/100-demo/plan.md"
	require.NoError(t, replaceDurableNoteFacts(context.Background(), fixture.intelStore, notePath, "Generation A title", "Generation A summary", "generation-a"))

	view := &ownershipFlipRuntimeView{store: fixture.intelStore}
	runtime := &Runtime{Live: view}
	runtime.EnableIndexGate()
	runtime.MarkNoteReadReady()
	srv := newFixtureServer(t, fixture, runtime)
	view.calls.Store(0)
	var mutationErr error
	view.onSecond = func() {
		mutationErr = replaceDurableNoteFacts(context.Background(), fixture.intelStore, notePath, "Generation B title", "Generation B summary", "generation-b")
	}

	result, err := srv.executeOntologyQuery(context.Background(), `{ node: note(path: "specs/100-demo/plan.md") { title frontmatter tags } }`)
	require.NoError(t, err)
	require.NoError(t, mutationErr)
	require.GreaterOrEqual(t, view.calls.Load(), int32(2), "test must replace durable facts after admission")
	node := result.Data["node"].(map[string]any)
	require.Equal(t, "Generation A title", node["title"])
	require.Equal(t, "Generation A summary", node["frontmatter"].(map[string]any)["summary"])
	require.Equal(t, []string{"generation-a"}, node["tags"])

	current, err := fixture.intelStore.DurableNoteFactsByPaths(context.Background(), []string{notePath})
	require.NoError(t, err)
	require.Equal(t, "Generation B title", current.MetadataRows[notePath].Title)
	var currentSummary string
	for _, row := range current.PropertyValues {
		if row.PropertyName == "summary" {
			currentSummary = row.ValueText
			break
		}
	}
	require.Equal(t, "Generation B summary", currentSummary)
	require.Equal(t, "generation-b", current.Tags[0].TagNorm)
}

func TestPublicQueryRecipeHydratesOneDurableFactSnapshot(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	const notePath = "specs/100-demo/plan.md"
	require.NoError(t, replaceDurableNoteFacts(context.Background(), fixture.intelStore, notePath, "Recipe generation A title", "Recipe generation A summary", "recipe-generation-a"))

	view := &ownershipFlipRuntimeView{store: fixture.intelStore}
	runtime := &Runtime{Live: view}
	runtime.EnableIndexGate()
	runtime.MarkNoteReadReady()
	srv := newFixtureServer(t, fixture, runtime)
	view.calls.Store(0)
	var mutationErr error
	view.onSecond = func() {
		mutationErr = replaceDurableNoteFacts(context.Background(), fixture.intelStore, notePath, "Recipe generation B title", "Recipe generation B summary", "recipe-generation-b")
	}
	recipe := queryrecipe.Recipe{
		APIVersion: queryrecipe.APIVersion,
		ID:         "exact-note-snapshot",
		Name:       "Exact note snapshot",
		Problem:    "Read one note through a saved query recipe.",
		InputSpec: queryrecipe.InputSpec{
			Mode: queryrecipe.InputModeRequiredAnchor,
			Inputs: []queryrecipe.Input{{
				Name:     "path",
				Required: true,
				Kind:     "path",
			}},
		},
		Query: queryrecipe.QuerySpec{GraphQL: `query ExactNoteSnapshot($path: String!) {
  note(path: $path) { title frontmatter tags }
}`},
		OutputContract:     queryrecipe.OutputContract{ExpectedPaths: []string{"note.title"}, Empty: "No note found."},
		AdaptationGuidance: queryrecipe.AdaptationGuidance{Summary: "Use the selected note."},
	}

	run, issues, err := srv.executePublicQueryRecipe(context.Background(), recipe, map[string]string{"path": notePath})
	require.NoError(t, err)
	require.Empty(t, issues)
	require.NoError(t, mutationErr)
	require.GreaterOrEqual(t, view.calls.Load(), int32(2), "test must replace durable facts after admission")
	note := run.Result.Data["note"].(map[string]any)
	require.Equal(t, "Recipe generation A title", note["title"])
	require.Equal(t, "Recipe generation A summary", note["frontmatter"].(map[string]any)["summary"])
	require.Equal(t, []string{"recipe-generation-a"}, note["tags"])
}

func TestInitRuntimeWithLiveViewDoesNotOpenFallbackStores(t *testing.T) {
	root := t.TempDir()
	view := &webRuntimeView{}
	provided := &Runtime{Live: view}
	runtime, status, err := initRuntime(context.Background(), Config{
		Vault:        &obsidian.Vault{Name: "test"},
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root},
		Runtime:      provided,
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.NoError(t, err)
	require.Same(t, provided, runtime)
	require.False(t, status.Embeddings)
	require.False(t, status.CodeIndex)
	require.False(t, runtime.ownsIntel)
}

func TestOntologyDefaultsAdoptLateRuntimeNoteReader(t *testing.T) {
	view := &webRuntimeView{}
	startup := &runtimeViewNoteReader{content: "startup", paths: []string{"startup.md"}}
	srv := &Server{
		cfg:        Config{VaultDef: obsidian.VaultDefinition{Path: t.TempDir()}},
		runtime:    &Runtime{Live: view},
		noteReader: startup,
	}

	before := srv.ontologyQueryDeps(nil, nil).NoteReader
	content, err := before.GetContents(srv.cfg.VaultDef, "note.md")
	require.NoError(t, err)
	require.Equal(t, "startup", content)

	late := &runtimeViewNoteReader{content: "late", paths: []string{"late.md", "added.md"}}
	view.snapshot.NoteReader = late
	view.snapshot.IntelStore = &semdb.Store{}
	srv.ontologyDefs = &ontologyDefinitions{schema: &ontology.Schema{}}
	after := srv.ontologyQueryDeps(nil, nil).NoteReader
	content, err = after.GetContents(srv.cfg.VaultDef, "note.md")
	require.NoError(t, err)
	require.Equal(t, "late", content)
	paths, err := after.GetNotesList(srv.cfg.VaultDef)
	require.NoError(t, err)
	require.Equal(t, []string{"late.md", "added.md"}, paths)
	service, _, err := srv.ontologyContext()
	require.NoError(t, err)
	serviceContent, err := service.NoteReader.GetContents(srv.cfg.VaultDef, "note.md")
	require.NoError(t, err)
	require.Equal(t, "late", serviceContent)

	explicit := &runtimeViewNoteReader{content: "explicit"}
	content, err = srv.ontologyQueryDeps(nil, explicit).NoteReader.GetContents(srv.cfg.VaultDef, "note.md")
	require.NoError(t, err)
	require.Equal(t, "explicit", content)
}

func TestOntologyQueryDepsKeepTheServiceStoreAcrossLivePublication(t *testing.T) {
	requestStore, err := semdb.Open(filepath.Join(t.TempDir(), "request.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, requestStore.Close()) })
	liveStore, err := semdb.Open(filepath.Join(t.TempDir(), "live.sqlite"))
	require.NoError(t, err)

	view := &webRuntimeView{snapshot: runtimeview.Snapshot{IntelStore: liveStore}}
	srv := &Server{
		cfg:     Config{VaultDef: obsidian.VaultDefinition{Path: t.TempDir()}},
		runtime: &Runtime{Live: view},
	}
	service := ontology.NewService(srv.cfg.VaultDef, &obsidian.Note{}, requestStore, &ontology.Schema{})
	deps := srv.ontologyQueryDeps(service, nil)
	require.Same(t, requestStore, deps.Store)

	require.NoError(t, liveStore.Close())
	_, err = deps.ExactNoteTags(context.Background(), []string{"note.md"})
	require.NoError(t, err, "exact loaders must retain the request store after the live store changes")
}
