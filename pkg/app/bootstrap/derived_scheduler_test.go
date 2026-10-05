package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	anchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	codeembsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestDerivedResultRejectsDirectSaveBeforeWatcherMarksNewSource(t *testing.T) {
	rt, w, provider := liveWorkloadWatcher(t)
	provider.block.Store(true)
	path := "notes/project-0000.md"
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Prepared A")), 0o600))
	rt.Cache().MarkDirty(path, cache.DirtyModified)
	handle := w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	require.NoError(t, handle.Err())
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not block")
	}
	schema, err := ontology.LoadSchema(rt.VaultPath)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(t.Context(), w.vaultDef, &obsidian.Note{}, schema, path)
	require.NoError(t, err)
	set, err := semantic.BuildOntologyNodeChunks(schema, projection, w.nodeSyncer.ProviderInfo, 0)
	require.NoError(t, err)
	require.NotEmpty(t, set.States)
	staleHash := set.States[0].ChunkTextHash
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Saved B")), 0o600))
	// Direct save publication happens under the writer lease, without a watcher
	// dirty mark. It must independently invalidate the prepared source witness.
	save, _, err := w.lane.Submit(t.Context(), lane.Request{Kind: lane.KindValidationRefresh, Run: func(ctx context.Context, _ lane.Reporter) error {
		baseline, err := w.noteMetadataIndexer.CapturePublishedBaseline(ctx, w.vaultDef, w.intelStore)
		if err != nil {
			return err
		}
		bytes, err := os.ReadFile(filepath.Join(rt.VaultPath, path))
		if err != nil {
			return err
		}
		stat, err := os.Stat(filepath.Join(rt.VaultPath, path))
		if err != nil {
			return err
		}
		format, ok := w.noteRuntime.Provider("markdown")
		if !ok {
			return fmt.Errorf("markdown provider unavailable")
		}
		source, err := noteformat.NewAuthoredSource(paths.NotePath(path), format.Descriptor(), bytes, stat.ModTime().Unix())
		if err != nil {
			return err
		}
		prep, err := w.noteMetadataIndexer.PreparePublishedMetadata(ctx, []paths.NotePath{paths.NotePath(path)}, map[paths.NotePath]noteformat.AuthoredSource{paths.NotePath(path): source})
		if err != nil {
			return err
		}
		delta, full, err := w.noteMetadataIndexer.BuildPublishedPathMetadataDeltaFromPreparation(ctx, w.vaultDef, w.intelStore, baseline, prep, nil)
		if full {
			return fmt.Errorf("direct-save fixture unexpectedly needs full metadata")
		}
		if err != nil {
			return err
		}
		if delta != nil {
			if err := w.intelStore.ApplyNoteMetadataDelta(ctx, *delta); err != nil {
				return err
			}
		}
		_, err = ontology.SyncPublishedPaths(ctx, w.noteMetadataIndexer, w.vaultDef, &obsidian.Note{}, w.intelStore, nil, []string{path}, nil)
		return err
	}})
	require.NoError(t, err)
	watcherJobTerminal(t, save)
	require.NoError(t, save.Err())
	provider.fail.Store(true)
	close(provider.release)
	require.Eventually(t, func() bool { return provider.failures.Load() > 0 }, 3*time.Second, 5*time.Millisecond)
	var staleStates int
	require.NoError(t, rt.IntelStore().DB().QueryRowContext(t.Context(), `SELECT count(*) FROM ontology_node_embedding_state WHERE note_path=? AND chunk_text_hash=?`, path, staleHash).Scan(&staleStates))
	require.Zero(t, staleStates, "the obsolete provider response must never authorize the old source")
	debt, err := rt.IntelStore().PendingDerivedWork(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.NotEmpty(t, debt)
}

func TestDerivedPriorityCancelsProviderWithoutHoldingWriterLease(t *testing.T) {
	rt, w, provider := liveWorkloadWatcher(t)
	provider.block.Store(true)
	path := "notes/project-0000.md"
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Priority")), 0o600))
	rt.Cache().MarkDirty(path, cache.DirtyModified)
	handle := w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	require.NoError(t, handle.Err())
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not block")
	}
	release, acquired, err := indexlock.TryAcquire(obsidian.IndexLockPath(rt.VaultPath))
	require.NoError(t, err)
	require.True(t, acquired, "physical provider work must leave the writer lease available")
	require.NoError(t, release())
	priority, err := indexlock.RequestPriority(obsidian.IndexLockPath(rt.VaultPath))
	require.NoError(t, err)
	defer priority.Close()
	require.Eventually(t, func() bool { return provider.canceled.Load() > 0 }, time.Second, 5*time.Millisecond)
	_, pending, err := rt.IntelStore().PendingOwnershipReconciliation(t.Context())
	require.NoError(t, err)
	require.False(t, pending)
	debt, err := rt.IntelStore().PendingDerivedWork(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.NotEmpty(t, debt)
	provider.block.Store(false)
	close(provider.release)
	priority.Close()
	require.Eventually(t, func() bool {
		work, err := rt.IntelStore().PendingDerivedWork(t.Context(), time.Now().Add(time.Hour), 100)
		return err == nil && len(work) == 0
	}, 3*time.Second, 5*time.Millisecond)
}

func TestDerivedShutdownJoinsCanceledProviderBeforeStoreCloses(t *testing.T) {
	rt, w, provider := liveWorkloadWatcher(t)
	provider.block.Store(true)
	path := "notes/project-0000.md"
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Shutdown")), 0o600))
	rt.Cache().MarkDirty(path, cache.DirtyModified)
	handle := w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not block")
	}
	done := make(chan struct{})
	go func() { w.derived.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("derived shutdown did not drain")
	}
	require.Positive(t, provider.canceled.Load())
	rows, err := rt.IntelStore().CurrentNoteMetadataRowsByPaths(t.Context(), []string{path})
	require.NoError(t, err)
	require.Contains(t, rows, path)
}

func TestLiveWriteAheadDebtTriggersQuietStructuralRecovery(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# note\n"), 0o600))
	w, c, s := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer s.Close()
	defer c.Close()
	_, err := s.MarkDerivedDirty(t.Context(), []codeanchor.DerivedScope{{Kind: codeanchor.DerivedOntology, Path: "note.md"}})
	require.NoError(t, err)
	_, pending, err := s.PendingOwnershipReconciliation(t.Context())
	require.NoError(t, err)
	require.False(t, pending, "crash gap is before structural ownership marker")
	runOwnershipBatch(t, w)
	unready, err := s.HasUnreadyDerivedWork(t.Context())
	require.NoError(t, err)
	require.False(t, unready)
	rows, err := s.CurrentNoteMetadataRowsByPaths(t.Context(), []string{"note.md"})
	require.NoError(t, err)
	require.Contains(t, rows, "note.md")
}

func TestDerivedGlobalCodeRecoveryPrunesLastRetiredSource(t *testing.T) {
	root := t.TempDir()
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer c.Close()
	defer store.Close()
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: 8})
	index, err := codeembsqlite.Open(filepath.Join(root, "code.db"), 8)
	require.NoError(t, err)
	defer index.Close()
	id := codeindex.AnchorID("retired-anchor")
	require.NoError(t, index.UpsertItemMeta(t.Context(), codeindex.Item{AnchorID: id, Path: "deleted.go", Lang: "go", Kind: "module", Fingerprint: "retired"}))
	require.NoError(t, index.UpsertItemEmbedding(t.Context(), id, "old", embeddings.Embedding{1, 0, 0, 0, 0, 0, 0, 0}))
	w.codeSyncer = &semantic.Syncer{Index: index, Provider: provider, Intel: store, Root: root}
	w.codeSemanticStore = true
	tickets, err := store.MarkDerivedDirty(t.Context(), []codeanchor.DerivedScope{{Kind: codeanchor.DerivedCode, Path: "deleted.go"}})
	require.NoError(t, err)
	// Crash recovery replaces the path ticket with a global obligation after
	// structural rows have already been retired, leaving an empty live keep set.
	tickets, err = store.MarkDerivedDirty(t.Context(), []codeanchor.DerivedScope{{Kind: codeanchor.DerivedCode}})
	require.NoError(t, err)
	require.NoError(t, store.ActivateDerivedWork(t.Context(), tickets))
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	require.NoError(t, d.execute(tickets[0]))
	items, err := index.ListItems(t.Context())
	require.NoError(t, err)
	require.Empty(t, items)
	remaining, err := store.HasDerivedWork(t.Context(), codeanchor.DerivedCode)
	require.NoError(t, err)
	require.False(t, remaining)
}

func TestDerivedRestartRetriesActiveDebtWithoutFilesystemEvents(t *testing.T) {
	rt, w, provider := liveWorkloadWatcher(t)
	provider.fail.Store(true)
	path := "notes/project-0000.md"
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Restart")), 0o600))
	rt.Cache().MarkDirty(path, cache.DirtyModified)
	h := w.processOwnershipBatch()
	watcherJobTerminal(t, h)
	require.NoError(t, h.Err())
	require.Eventually(t, func() bool { return provider.failures.Load() > 0 }, time.Second, 5*time.Millisecond)
	w.derived.close()
	// Open a fresh durable handle and scheduler, with no new watch events or
	// structural replay. The debt, retry deadline and source survive startup.
	reopened, err := anchorsqlite.Open(filepath.Join(rt.VaultPath, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer reopened.Close()
	provider.fail.Store(false)
	next := &unifiedSemanticWatcher{runCtx: t.Context(), lane: w.lane, vaultPath: w.vaultPath,
		vaultDef: w.vaultDef, noteMetadataIndexer: w.noteMetadataIndexer, intelStore: reopened,
		nodeSyncer: &semantic.OntologyNodeSyncer{Store: reopened, Provider: provider, ProviderInfo: w.nodeSyncer.ProviderInfo}}
	d := newDerivedScheduler(next)
	defer d.close()
	require.Eventually(t, func() bool {
		pending, err := reopened.HasDerivedWork(t.Context(), codeanchor.DerivedOntology)
		return err == nil && !pending
	}, 3*time.Second, 5*time.Millisecond)
	var vectors int
	require.NoError(t, reopened.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id JOIN intel_embeddings e ON e.chunk_id=s.chunk_id WHERE s.note_path=? AND s.node_structure_fingerprint=n.structural_fingerprint`, path).Scan(&vectors))
	require.Positive(t, vectors)
}

func TestDerivedSharedNodePriorityYieldsAndRuntimeShutdownDrainsPhysicalProvider(t *testing.T) {
	rt, w, provider := liveWorkloadWatcher(t)
	w.derived.close()
	node := semantic.NewSharedEmbeddingNode(rt.ctx, provider, 1, nil, semantic.EmbedPackerOptions{MaxTexts: 1})
	rt.addCloser(node.Close)
	w.nodeSyncer.EmbeddingNode = node
	w.derived = newDerivedScheduler(w)
	provider.block.Store(true)
	path := "notes/project-0000.md"
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Shared priority")), 0o600))
	rt.Cache().MarkDirty(path, cache.DirtyModified)
	h := w.processOwnershipBatch()
	watcherJobTerminal(t, h)
	require.NoError(t, h.Err())
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("shared physical provider did not start")
	}
	priority, err := indexlock.RequestPriority(obsidian.IndexLockPath(rt.VaultPath))
	require.NoError(t, err)
	defer priority.Close()
	require.Eventually(t, func() bool { w.derived.mu.Lock(); defer w.derived.mu.Unlock(); return w.derived.running == "" }, time.Second, 5*time.Millisecond)
	require.Zero(t, provider.canceled.Load(), "canceling one submitter preserves shared physical request ownership")
	release, acquired, err := indexlock.TryAcquire(obsidian.IndexLockPath(rt.VaultPath))
	require.NoError(t, err)
	require.True(t, acquired, "shared provider dispatch must leave writer lease available")
	require.NoError(t, release())
	priority.Close()
	// The new structural source invalidates any late shared response while the
	// physical request remains dispatched. Runtime close cancels and joins that
	// owned shared node before closing its Intel store.
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Shared newer")), 0o600))
	rt.Cache().MarkDirty(path, cache.DirtyModified)
	h = w.processOwnershipBatch()
	watcherJobTerminal(t, h)
	require.NoError(t, h.Err())
	w.derived.close()
	done := make(chan error, 1)
	go func() { done <- rt.Close() }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("runtime close did not join its shared physical request")
	}
	require.Positive(t, provider.canceled.Load(), "owned shared-node shutdown must cancel dispatched provider work")
}
