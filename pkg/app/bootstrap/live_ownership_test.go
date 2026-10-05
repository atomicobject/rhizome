package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	anchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
	"github.com/stretchr/testify/require"
)

func TestLiveOwnership_HeldIndexLeaseLeavesRawDirtyPending(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# first\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer store.Close()
	defer cacheService.Close()

	cacheService.MarkDirty("note.md", cache.DirtyModified)
	release, acquired, err := indexlock.TryAcquire(obsidian.IndexLockPath(root))
	require.NoError(t, err)
	require.True(t, acquired)

	// The lane waits for the external holder instead of dropping the batch, so
	// the raw dirty path is still pending while the lease is held.
	handle := w.processOwnershipBatch()
	require.NotNil(t, handle)
	select {
	case <-handle.Done():
		t.Fatal("the batch must not run while another process holds the index lock")
	case <-time.After(100 * time.Millisecond):
	}
	require.Contains(t, cacheService.DirtySnapshot(), "note.md")
	require.NoError(t, release())

	select {
	case <-handle.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("the batch did not run after the lock was released")
	}
	paths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"note.md"}, paths)
}

func TestLiveOwnership_ReconciliationReadFailureRetainsDrainedEvents(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { require.NoError(t, cacheService.Close()) })

	cacheService.MarkDirty("note.md", cache.DirtyModified)
	require.NoError(t, store.Close())
	runOwnershipBatch(t, w)

	require.Contains(t, cacheService.DirtySnapshot(), "note.md",
		"a transient state-read failure must leave drained work available for retry")
}

func TestLiveOwnershipReloadSelectionUpdatesWatchHubExcludes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	configPath := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes: [\"**/*.md\"]\n  excludes: [\"private/**\"]\n"), 0o644))
	definition, err := obsidian.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, definition)
	defer store.Close()
	defer cacheService.Close()
	rt := &LiveRuntime{VaultDef: definition, VaultPath: root, noteFormats: w.noteRuntime}
	w.onNoteSelectionChanged = rt.setNoteSelectionPolicy

	hub, err := watchhub.NewHub(root, watchhub.Options{DisableFSNotify: true, Debounce: 5 * time.Millisecond})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub.Start(ctx)
	defer func() { _ = hub.Close() }()
	w.watchHub = hub
	eventsCh := make(chan []watchhub.WatchEvent, 1)
	unsub := hub.Subscribe("test", watchhub.Filter{IncludeFiles: true}, func(_ context.Context, events []watchhub.WatchEvent) {
		eventsCh <- events
	}, nil)
	defer unsub()

	require.NoError(t, w.reloadOwnershipSelection())
	require.False(t, rt.NotePathOwned("private/note.md"))
	hub.EmitHintPaths([]string{"private/note.md"})
	select {
	case events := <-eventsCh:
		t.Fatalf("unexpected excluded events: %#v", events)
	case <-time.After(30 * time.Millisecond):
	}

	require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, w.reloadOwnershipSelection())
	require.True(t, rt.NotePathOwned("private/note.md"))
	hub.EmitHintPaths([]string{"private/note.md"})
	select {
	case events := <-eventsCh:
		require.Len(t, events, 1)
		require.Equal(t, "private/note.md", events[0].RelPath)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for newly included WatchHub event")
	}
}

func TestLiveOwnership_ProjectableHTMLPublishesCurrentIdentity(t *testing.T) {
	root := t.TempDir()
	writeWatcherSchema(t, root, "type Project @node(paths: [\"notes/*.md\"]) {\n  name: String!\n}\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "release.html"), []byte("<h1>Release</h1>"), 0o644))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"docs/*.html"}}
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, definition)
	defer store.Close()
	defer cacheService.Close()
	provider := &recordingEmbeddingProvider{}
	w.nodeSyncer = &semantic.OntologyNodeSyncer{Store: store, Provider: provider}

	// HTML does not enter the Markdown compatibility cache. Its raw event still
	// reaches ownership and publishes the provider projection.
	_, cached := cacheService.Entry("docs/release.html")
	require.False(t, cached)
	cacheService.MarkDirty("docs/release.html", cache.DirtyModified)
	runOwnershipBatch(t, w)
	rows, err := store.CurrentNoteMetadataRowsByPaths(context.Background(), []string{"docs/release.html"})
	require.NoError(t, err)
	row, ok := rows["docs/release.html"]
	require.True(t, ok)
	require.Equal(t, "html", row.FormatID)
	require.Equal(t, anchorsqlite.NoteProjectionStatusCurrent, row.Projection.Status)
	_, last := w.health.snapshot()
	require.NotNil(t, last)
	require.Equal(t, "completed", last.Status, "epoch error: %s", last.Error)
	drainOntologyWork(t, w)
	require.Len(t, provider.texts, 1)
	require.Contains(t, provider.texts[0], "docs/release.html")
	require.Contains(t, provider.texts[0], "Release")
	require.NotContains(t, provider.texts[0], "<h1>")
	nodes, err := store.OntologyNodesByPaths(t.Context(), []string{"docs/release.html"})
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	chunks, err := store.IntelChunksByOwners(t.Context(), []string{nodes[0].NodeID})
	require.NoError(t, err)
	require.NotEmpty(t, chunks, "embedding owner must be present in the durable node catalog")
}

func TestLiveOwnership_ProjectorFatalClearsDerivedEvidenceAndRecovers(t *testing.T) {
	root := t.TempDir()
	path := "notes/Source.md"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("---\ncode-anchors:\n  go:\n    - label: fatal-source\n      glob: pkg/**/*.go\n---\n# Source\n"), 0o644))
	projector := &liveFatalProjector{}
	runtime, indexer := liveFatalRuntime(t, projector)
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}})
	defer store.Close()
	defer cacheService.Close()
	w.noteRuntime = runtime
	w.noteMetadataIndexer = indexer
	w.noteSvc, _ = NewCodeAnchorService(CodeAnchorServiceConfig{VaultPath: root, CodeCfg: testCodeConfig(root), Store: store, IncludeIndexers: true, Linker: liveFatalLinker{}, WriteAccess: true})

	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	assertLiveFatalDerivedEvidence(t, store, path, true)

	projector.fatal.Store(true)
	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	assertLiveFatalDerivedEvidence(t, store, path, false)
	// Read the source identity without relying on global metadata readiness.
	rows, err := store.DurableNoteMetadataRowsByPaths(context.Background(), []string{path})
	require.NoError(t, err)
	require.Equal(t, anchorsqlite.NoteProjectionStatusFatal, rows[path].Projection.Status)
	require.Equal(t, "projector_fatal", rows[path].Projection.DiagnosticCode)
	generation, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.False(t, pending)
	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	repeatedGeneration, repeatedPending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.Equal(t, generation, repeatedGeneration, "identical fatal projection must not create a new generation")
	require.False(t, repeatedPending)

	projector.fatal.Store(false)
	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	assertLiveFatalDerivedEvidence(t, store, path, true)
	rows, err = store.CurrentNoteMetadataRowsByPaths(context.Background(), []string{path})
	require.NoError(t, err)
	require.Equal(t, anchorsqlite.NoteProjectionStatusCurrent, rows[path].Projection.Status)
}

func TestLiveOwnership_PreparedFatalCommitSurvivesDestinationFailure(t *testing.T) {
	root := t.TempDir()
	path := "notes/Source.md"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("---\ncode-anchors:\n  go:\n    - label: fatal-source\n      glob: pkg/**/*.go\n---\n# Source\n"), 0o644))
	projector := &liveFatalProjector{}
	runtime, indexer := liveFatalRuntime(t, projector)
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}})
	defer store.Close()
	defer cacheService.Close()
	w.noteRuntime = runtime
	w.noteMetadataIndexer = indexer
	w.noteSvc, _ = NewCodeAnchorService(CodeAnchorServiceConfig{VaultPath: root, CodeCfg: testCodeConfig(root), Store: store, IncludeIndexers: true, Linker: liveFatalLinker{}, WriteAccess: true})
	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	assertLiveFatalDerivedEvidence(t, store, path, true)

	projector.fatal.Store(true)
	failure := errors.New("stop after prepared fatal ownership commit")
	w.afterPreparedOwnershipCommit = func(context.Context) error { return failure }
	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	assertLiveFatalDerivedEvidence(t, store, path, false)
	// The ownership commit makes fatal source identity durable before the
	// downstream failure. It also invalidates metadata readiness until retry.
	rows, err := store.DurableNoteMetadataRowsByPaths(context.Background(), []string{path})
	require.NoError(t, err)
	require.Equal(t, anchorsqlite.NoteProjectionStatusFatal, rows[path].Projection.Status)
	pendingGeneration, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.True(t, pending)

	w.afterPreparedOwnershipCommit = nil
	// The raw event was drained. The stale cache makes this a quiet recovery.
	runOwnershipBatch(t, w)
	acknowledgedGeneration, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.Equal(t, pendingGeneration, acknowledgedGeneration, "quiet recovery must acknowledge the committed generation")
	require.False(t, pending)
	rows, err = store.CurrentNoteMetadataRowsByPaths(context.Background(), []string{path})
	require.NoError(t, err)
	require.Equal(t, anchorsqlite.NoteProjectionStatusFatal, rows[path].Projection.Status)
}

func TestLiveOwnership_PreparedCurrentRecoveryCommitSurvivesDestinationFailure(t *testing.T) {
	root := t.TempDir()
	path := "notes/Source.md"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("---\ncode-anchors:\n  go:\n    - label: recovery-source\n      glob: pkg/**/*.go\n---\n# Source\n"), 0o644))
	projector := &liveFatalProjector{}
	runtime, indexer := liveFatalRuntime(t, projector)
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}})
	defer store.Close()
	defer cacheService.Close()
	w.noteRuntime = runtime
	w.noteMetadataIndexer = indexer
	w.noteSvc, _ = NewCodeAnchorService(CodeAnchorServiceConfig{VaultPath: root, CodeCfg: testCodeConfig(root), Store: store, IncludeIndexers: true, Linker: liveFatalLinker{}, WriteAccess: true})

	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	assertLiveFatalDerivedEvidence(t, store, path, true)

	projector.fatal.Store(true)
	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	assertLiveFatalDerivedEvidence(t, store, path, false)

	projector.fatal.Store(false)
	failure := errors.New("stop after prepared current recovery ownership commit")
	w.afterPreparedOwnershipCommit = func(context.Context) error { return failure }
	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	// The source bytes are unchanged. The prepared current transition must still
	// replace fatal ownership and leave a pending destination-rebuild debt.
	assertLiveFatalDerivedEvidence(t, store, path, false)
	rows, err := store.DurableNoteMetadataRowsByPaths(context.Background(), []string{path})
	require.NoError(t, err)
	require.Equal(t, anchorsqlite.NoteProjectionStatusCurrent, rows[path].Projection.Status)
	pendingGeneration, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.True(t, pending)

	w.afterPreparedOwnershipCommit = nil
	// The raw event was drained. The stale cache makes this a quiet recovery.
	runOwnershipBatch(t, w)
	acknowledgedGeneration, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.Equal(t, pendingGeneration, acknowledgedGeneration, "quiet recovery must acknowledge the committed generation")
	require.False(t, pending)
	assertLiveFatalDerivedEvidence(t, store, path, true)
	rows, err = store.CurrentNoteMetadataRowsByPaths(context.Background(), []string{path})
	require.NoError(t, err)
	require.Equal(t, anchorsqlite.NoteProjectionStatusCurrent, rows[path].Projection.Status)

	// Re-observing the same current projection must not create fresh work.
	cacheService.MarkDirty(path, cache.DirtyModified)
	runOwnershipBatch(t, w)
	repeatedGeneration, repeatedPending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.Equal(t, acknowledgedGeneration, repeatedGeneration, "identical current projection must not create a new generation")
	require.False(t, repeatedPending)
}

func TestLiveOwnership_UnavailableConfiguredSemanticsRetainsDebtAfterStructuralAcknowledgement(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644))
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer store.Close()
	defer c.Close()
	w.noteSemanticStore = true
	c.MarkDirty("note.md", cache.DirtyModified)
	runOwnershipBatch(t, w)
	_, pending, err := store.PendingOwnershipReconciliation(t.Context())
	require.NoError(t, err)
	require.False(t, pending, "structural acknowledgement must remain independent of provider availability")
	work, err := store.PendingDerivedWork(t.Context(), time.Now(), 100)
	require.NoError(t, err)
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	for _, ticket := range work {
		if ticket.Kind == codeanchor.DerivedNotes || ticket.Kind == codeanchor.DerivedOntology {
			require.Error(t, d.execute(ticket))
			current, err := store.CurrentDerivedWork(t.Context(), ticket)
			require.NoError(t, err)
			require.True(t, current, "unavailable configured provider must retain durable debt")
		}
	}
}

func assertLiveFatalDerivedEvidence(t *testing.T, store *anchorsqlite.Store, path string, present bool) {
	t.Helper()
	var anchors, sections, links int
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM note_anchors na JOIN notes n ON n.id = na.note_id WHERE n.path = ?`, path).Scan(&anchors))
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM intel_doc_sections WHERE path = ?`, path).Scan(&sections))
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM doc_links WHERE src_path = ?`, path).Scan(&links))
	if present {
		require.Positive(t, anchors)
		require.Positive(t, sections)
		require.Positive(t, links)
		return
	}
	require.Zero(t, anchors)
	require.Zero(t, sections)
	require.Zero(t, links)
}

type liveFatalProjector struct{ fatal atomic.Bool }

type liveFatalLinker struct{}

func (liveFatalLinker) LinksForCode(string, []byte, codeanchor.FileContext) []codeanchor.DocLink {
	return nil
}

func (liveFatalLinker) LinksForNote(note codeanchor.Note) []codeanchor.DocLink {
	return []codeanchor.DocLink{{SrcType: "note", SrcPath: note.Path, DstKind: "note", DstPath: "notes/Target.md"}}
}

func (p *liveFatalProjector) Descriptor() noteformat.Descriptor {
	return noteformat.Descriptor{ID: "markdown", Extensions: []string{".md"}, ProviderVersion: "live-fatal-provider-v1", ProjectionVersion: "live-fatal-projection-v1", OwnershipPolicy: noteformat.OwnershipDefault}
}

func (p *liveFatalProjector) Project(noteformat.AuthoredSource) (noteformat.Projection, error) {
	descriptor := p.Descriptor()
	if p.fatal.Load() {
		return noteformat.NewProjection(descriptor.ProviderVersion, descriptor.ProjectionVersion, noteformat.ProjectionStatusFatal, []noteformat.Diagnostic{{Code: "projector_fatal", Message: "projector rejected source", Blocking: true}}, descriptor.Capabilities)
	}
	return noteformat.NewProjection(descriptor.ProviderVersion, descriptor.ProjectionVersion, noteformat.ProjectionStatusCurrent, nil, descriptor.Capabilities)
}

func liveFatalRuntime(t *testing.T, projector noteformat.Projector) (noteformat.Runtime, notemeta.Indexer) {
	t.Helper()
	registry, err := noteformat.NewRegistry(projector)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, projector)
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return runtime, indexer
}

func TestLiveOwnership_ReobservesTransitionAffectedSourcePath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte("# Source\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.md"), []byte("# Target\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer store.Close()
	defer cacheService.Close()

	cacheService.MarkDirty("source.md", cache.DirtyModified)
	cacheService.MarkDirty("target.md", cache.DirtyModified)
	runOwnershipBatch(t, w)
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(context.Background(), "source.md", "wikilink", []string{"target.md"}))

	var processed []string
	w.onProcessedPath = func(path string) { processed = append(processed, path) }
	require.NoError(t, os.Remove(filepath.Join(root, "target.md")))
	cacheService.MarkDirty("target.md", cache.DirtyRemoved)
	runOwnershipBatch(t, w)
	require.Contains(t, processed, "source.md", "transition-affected sources must be re-observed and re-ingested")
}

func TestLiveOwnership_ScopedChangeRetainsUntouchedMetadataOwner(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "changed.md"), []byte("# Changed\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "untouched.md"), []byte("# Untouched\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer store.Close()
	defer cacheService.Close()

	cacheService.MarkDirty("changed.md", cache.DirtyModified)
	cacheService.MarkDirty("untouched.md", cache.DirtyModified)
	runOwnershipBatch(t, w)
	require.Equal(t, []string{"changed.md", "untouched.md"}, liveCurrentMetadataPaths(t, store))
	require.NoError(t, os.WriteFile(filepath.Join(root, "changed.md"), []byte("# Changed again\n"), 0o644))
	cacheService.MarkDirty("changed.md", cache.DirtyModified)
	runOwnershipBatch(t, w)
	require.Equal(t, []string{"changed.md", "untouched.md"}, liveCurrentMetadataPaths(t, store))
}

func TestLiveOwnership_ConvergesUnreadableAffectedSourceTransition(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce POSIX unreadable file permissions")
	}
	root := t.TempDir()
	source := "notes/Source.md"
	target := "notes/Target.md"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, source), []byte("---\ncode-anchors:\n  go:\n    - label: dependent-source\n      glob: pkg/**/*.go\n---\n# Source\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, target), []byte("# Target\n"), 0o600))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}})
	defer store.Close()
	defer cacheService.Close()
	w.noteSvc, _ = NewCodeAnchorService(CodeAnchorServiceConfig{VaultPath: root, CodeCfg: testCodeConfig(root), Store: store, IncludeIndexers: true, Linker: liveFatalLinker{}, WriteAccess: true})

	cacheService.MarkDirty(source, cache.DirtyModified)
	cacheService.MarkDirty(target, cache.DirtyModified)
	runOwnershipBatch(t, w)
	assertLiveFatalDerivedEvidence(t, store, source, true)
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(context.Background(), source, "wikilink", []string{target}))

	// Source is not a raw dirty event. Its dependency is returned only after the
	// target transition removes graph evidence, so the second fixed-point pass
	// must observe and commit the unreadable source's fatal transition.
	fullSourcePath := filepath.Join(root, filepath.FromSlash(source))
	require.NoError(t, os.Chmod(fullSourcePath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(fullSourcePath, 0o600) })
	require.NoError(t, os.Remove(filepath.Join(root, filepath.FromSlash(target))))
	cacheService.MarkDirty(target, cache.DirtyRemoved)
	runOwnershipBatch(t, w)

	assertLiveFatalDerivedEvidence(t, store, source, false)
	rows, err := store.CurrentNoteMetadataRowsByPaths(context.Background(), []string{source})
	require.NoError(t, err)
	require.Equal(t, anchorsqlite.NoteProjectionStatusFatal, rows[source].Projection.Status)
	require.Equal(t, "unreadable_source", rows[source].Projection.DiagnosticCode)
	_, pending, err := store.PendingOwnershipReconciliation(context.Background())
	require.NoError(t, err)
	require.False(t, pending, "acknowledgement must wait for the affected-source fatal transition")
}

// runOwnershipBatch queues a reconciliation on the test lane and waits for it.
// Production submits without waiting; tests need the result.
func runOwnershipBatch(t *testing.T, w *unifiedSemanticWatcher) {
	t.Helper()
	handle := w.processOwnershipBatch()
	require.NotNil(t, handle, "the watcher must submit a reconciliation job")
	select {
	case <-handle.Done():
	case <-time.After(60 * time.Second):
		t.Fatal("ownership batch did not finish")
	}
}

func newLiveOwnershipTestWatcher(t *testing.T, root string, definition obsidian.VaultDefinition) (*unifiedSemanticWatcher, *cache.Service, *anchorsqlite.Store) {
	t.Helper()
	runtime := testNoteRuntime(t)
	policy, err := liveCacheSelectionPolicy(definition, runtime, testCodeConfig(root))
	require.NoError(t, err)
	cacheService, err := cache.NewService(root, cache.Options{DiscoverFiles: policy.DiscoverFiles, AdmitNote: policy.Admit, UserExcludes: policy.UserExcludes})
	require.NoError(t, err)
	require.NoError(t, cacheService.EnsureReady(context.Background()))
	store, err := anchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	noteSvc, _ := NewCodeAnchorService(CodeAnchorServiceConfig{VaultPath: root, CodeCfg: testCodeConfig(root), Store: store, IncludeIndexers: true, WriteAccess: true})
	indexLane := lane.New(lane.Options{LockPath: obsidian.IndexLockPath(root), PriorityPoll: 20 * time.Millisecond})
	t.Cleanup(indexLane.Close)
	return &unifiedSemanticWatcher{
		runCtx:              context.Background(),
		lane:                indexLane,
		vaultPath:           root,
		vaultDef:            definition,
		noteRuntime:         runtime,
		codeCfg:             testCodeConfig(root),
		cacheService:        cacheService,
		noteSvc:             noteSvc,
		noteMetadataIndexer: testNoteMetadataIndexer(t),
		intelStore:          store,
		health:              newLiveHealthTracker(root),
		opts:                UnifiedSemanticWatcherOptions{}.normalize(),
	}, cacheService, store
}

func TestLiveRuntimeNotePathOwnedUsesCurrentSelectionPolicy(t *testing.T) {
	root := t.TempDir()
	runtime := testNoteRuntime(t)
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}, Excludes: []string{"notes/private/**"}}
	rt := &LiveRuntime{VaultDef: definition, VaultPath: root, noteFormats: runtime}
	codeCfg := testCodeConfig(root)
	rt.codeCfg.Store(&codeCfg)

	require.True(t, rt.NotePathOwned("notes/public/example.md"))
	require.False(t, rt.NotePathOwned("notes/private/example.md"))
	require.False(t, rt.NotePathOwned("outside/example.md"))
	require.False(t, rt.NotePathOwned("../escape.md"))
}

func TestLeaderOwnershipConfigurationReloadsSelectionFromDisk(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	configPath := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes: [\"**/*.md\"]\n  excludes: [\"private/**\"]\n"), 0o644))
	definition, err := obsidian.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	runtime := testNoteRuntime(t)
	codeCfg := testCodeConfig(root)
	policy, err := liveCacheSelectionPolicy(definition, runtime, codeCfg)
	require.NoError(t, err)
	cacheService, err := cache.NewService(root, cache.Options{DiscoverFiles: policy.DiscoverFiles, AdmitNote: policy.Admit, UserExcludes: policy.UserExcludes})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cacheService.Close() })

	rt := &LiveRuntime{VaultDef: definition, VaultPath: root, noteFormats: runtime}
	rt.ownershipVaultDef.Store(&definition)
	rt.codeCfg.Store(&codeCfg)
	rt.cache.Store(cacheService)
	rt.setNoteSelectionPolicy(policy)
	require.False(t, rt.NotePathOwned("private/note.md"))

	// The owner always refreshes ownership from disk when it starts leader work,
	// so a configuration edit made before startup is never missed.
	require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	rt.leaderFlag.Store(true)
	promotedDefinition, promotedCodeCfg, err := rt.leaderOwnershipConfiguration()
	require.NoError(t, err)
	require.Empty(t, promotedDefinition.Excludes)
	require.NotEmpty(t, promotedCodeCfg.IndexPath)
	require.True(t, rt.NotePathOwned("private/note.md"))
	require.True(t, cacheService.AdmitsNotePath("private/note.md"))
}

func liveCurrentMetadataPaths(t *testing.T, store *anchorsqlite.Store) []string {
	t.Helper()
	paths, err := store.CurrentNoteMetadataPaths(context.Background())
	require.NoError(t, err)
	return paths
}

func testCodeConfig(root string) codeanchor.Config {
	return codeanchor.DefaultConfig(root)
}

type recordingEmbeddingProvider struct {
	fail  atomic.Bool
	mu    sync.Mutex
	texts []string
}

func (p *recordingEmbeddingProvider) EmbedTexts(_ context.Context, texts []string) ([]embeddings.Embedding, error) {
	if p.fail.Load() {
		return nil, errors.New("provider outage")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.texts = append(p.texts, texts...)
	out := make([]embeddings.Embedding, len(texts))
	for i := range out {
		out[i] = embeddings.Embedding{1, 0, 0}
	}
	return out, nil
}

func (p *recordingEmbeddingProvider) Dimensions() int { return 3 }

func TestLiveOwnership_ResyncKeepsCodePathsOutOfNodeEmbeddingSync(t *testing.T) {
	root := t.TempDir()
	writeWatcherSchema(t, root, "type Project @node(paths: [\"notes/*.md\"]) {\n  name: String!\n}\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Roadmap.md"), []byte("---\ntype: Project\nname: Roadmap\n---\nShip the watcher fix.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}})
	defer store.Close()
	defer cacheService.Close()
	w.codeCfg.GoRoots = []string{root}
	provider := &recordingEmbeddingProvider{}
	w.nodeSyncer = &semantic.OntologyNodeSyncer{Store: store, Provider: provider}

	// The first resync rebuilds the ontology and syncs every current note path;
	// only a later resync hands the affected set to the node syncer.
	for i := 0; i < 2; i++ {
		resyncs := cacheService.Metrics().ResyncCount
		cacheService.MarkStale()
		// The first pass starts the background recrawl. Wait for it to finish,
		// then let the watcher consume the completed resync.
		runOwnershipBatch(t, w)
		require.Eventually(t, func() bool {
			return cacheService.Metrics().ResyncCount > resyncs
		}, 5*time.Second, 5*time.Millisecond)
		runOwnershipBatch(t, w)
	}

	_, last := w.health.snapshot()
	require.NotNil(t, last)
	require.Equal(t, "completed", last.Status, "epoch error: %s", last.Error)
	drainOntologyWork(t, w)
	require.NotEmpty(t, provider.texts)
	for _, text := range provider.texts {
		require.NotContains(t, text, "package main")
	}
}

func drainOntologyWork(t *testing.T, w *unifiedSemanticWatcher) {
	t.Helper()
	work, err := w.intelStore.PendingDerivedWork(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	for _, ticket := range work {
		if ticket.Kind == codeanchor.DerivedOntology {
			require.NoError(t, d.execute(ticket))
		}
	}
}
