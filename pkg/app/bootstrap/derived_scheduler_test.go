package bootstrap

import (
	"context"
	"errors"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	codeembsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"

	anchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embedindex"
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
	schema, err := ontoschema.LoadSchema(rt.VaultPath)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(t.Context(), w.vaultDef, &obsidian.Note{}, schema, path)
	require.NoError(t, err)
	set, err := embedindex.BuildOntologyNodeChunks(schema, projection, w.nodeSyncer.ProviderInfo, 0)
	require.NoError(t, err)
	require.NotEmpty(t, set.States)
	staleHash := set.States[0].ChunkTextHash
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Saved B")), 0o600))
	// Direct save publication happens under the writer lease, without a watcher
	// dirty mark. It must independently invalidate the prepared source witness.
	save, _, err := w.lane.Submit(t.Context(), lane.Request{Kind: lane.KindValidationRefresh, Run: func(ctx context.Context, _ *indexlock.Lease, _ lane.Reporter) error {
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
		queue := w.structuralWriter(ctx)
		_, err = ontology.SyncPublishedPaths(ctx, w.noteMetadataIndexer, w.vaultDef, &obsidian.Note{}, w.intelStore, queue, []string{path}, nil)
		if err != nil {
			return errors.Join(err, queue.Close())
		}
		return errors.Join(queue.FlushAndWait(ctx), queue.Close())
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
	release, acquired, err := indexlock.TryAcquire(repoconfig.IndexLockPath(rt.VaultPath))
	require.NoError(t, err)
	require.True(t, acquired, "physical provider work must leave the writer lease available")
	require.NoError(t, release())
	priority, err := indexlock.RequestPriority(repoconfig.IndexLockPath(rt.VaultPath))
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
	index, err := codeembsqlite.OpenCode(context.Background(), codeembsqlite.File(filepath.Join(root, "code.db")), codeembsqlite.Options{Access: codeembsqlite.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	defer index.Close()
	id := codeindex.AnchorID("retired-anchor")
	require.NoError(t, index.UpsertItemMeta(t.Context(), codeindex.Item{AnchorID: id, Path: "deleted.go", Lang: "go", Kind: "module", Fingerprint: "retired"}))
	require.NoError(t, index.UpsertItemEmbedding(t.Context(), id, "old", embeddings.Embedding{1, 0, 0, 0, 0, 0, 0, 0}))
	w.codeSyncer = &embedindex.Syncer{Index: index, Provider: provider, Intel: store, Root: root}
	w.codeSemanticStore = true
	tickets, err := store.MarkDerivedDirty(t.Context(), []codeanchor.DerivedScope{{Kind: codeanchor.DerivedCode, Path: "deleted.go"}})
	require.NoError(t, err)
	require.Len(t, tickets, 1)
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
		nodeSyncer: &embedindex.OntologyNodeSyncer{Store: reopened, Provider: provider, ProviderInfo: w.nodeSyncer.ProviderInfo}}
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
	node := embedindex.NewSharedEmbeddingNode(rt.ctx, provider, 1, nil, embedindex.EmbedPackerOptions{MaxTexts: 1})
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
	priority, err := indexlock.RequestPriority(repoconfig.IndexLockPath(rt.VaultPath))
	require.NoError(t, err)
	defer priority.Close()
	// Priority cancels computation, but the dispatched physical request keeps
	// running so its vectors can still be cached.
	require.Never(t, func() bool { return provider.canceled.Load() > 0 }, 300*time.Millisecond, 5*time.Millisecond,
		"canceling one submitter preserves shared physical request ownership")
	release, acquired, err := indexlock.TryAcquire(repoconfig.IndexLockPath(rt.VaultPath))
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
	// As in production, the runtime cancels its context, and with it the
	// shared node, before closing the derived scheduler.
	rt.addCloser(w.derived.close)
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

// supersedingProvider records texts and runs onEmbed before the first call.
type supersedingProvider struct {
	recordingEmbeddingProvider
	onEmbed func()
}

func (p *supersedingProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	if p.onEmbed != nil {
		p.onEmbed()
		p.onEmbed = nil
	}
	return p.recordingEmbeddingProvider.EmbedTexts(ctx, texts)
}

func TestDerivedOntologySupersededWhileEmbeddingStillCachesVectors(t *testing.T) {
	root := t.TempDir()
	writeWatcherSchema(t, root, "type Project @node(paths: [\"notes/*.md\"]) {\n  name: String!\n}\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Roadmap.md"), []byte("---\ntype: Project\nname: Roadmap\n---\nShip the watcher fix.\n"), 0o644))
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}})
	defer store.Close()
	defer c.Close()
	provider := &supersedingProvider{}
	w.nodeSyncer = &embedindex.OntologyNodeSyncer{Store: store, Provider: provider, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "recording"}}
	c.MarkDirty("notes/Roadmap.md", cache.DirtyModified)
	runOwnershipBatch(t, w)
	work, err := store.PendingDerivedWork(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	var ticket codeanchor.DerivedWork
	for _, candidate := range work {
		if candidate.Kind == codeanchor.DerivedOntology {
			ticket = candidate
		}
	}
	require.Equal(t, codeanchor.DerivedOntology, ticket.Kind)

	// An unrelated change supersedes the ticket while the provider embeds.
	var replacements []codeanchor.DerivedWork
	var markErr error
	provider.onEmbed = func() {
		replacements, markErr = store.MarkDerivedDirty(context.Background(), []codeanchor.DerivedScope{ticket.DerivedScope})
	}
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	require.ErrorIs(t, d.execute(ticket), errDerivedSuperseded)
	require.NoError(t, markErr)
	embedded := len(provider.texts)
	require.Positive(t, embedded)

	require.NoError(t, store.ActivateDerivedWork(t.Context(), replacements))
	require.NoError(t, d.execute(replacements[0]))
	require.Len(t, provider.texts, embedded, "the replacement ticket reuses the superseded run's vectors")
	pending, err := store.HasDerivedWork(t.Context(), codeanchor.DerivedOntology)
	require.NoError(t, err)
	require.False(t, pending)
}

// failingTextProvider records texts and fails any batch containing a marker.
type failingTextProvider struct {
	recordingEmbeddingProvider
	failOn string
}

func (p *failingTextProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.mu.Lock()
	failOn := p.failOn
	p.mu.Unlock()
	for _, text := range texts {
		if failOn != "" && strings.Contains(text, failOn) {
			// Fail only after the other two slices succeed.
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				p.mu.Lock()
				done := len(p.texts) >= 2
				p.mu.Unlock()
				if done {
					break
				}
			}
			p.mu.Lock()
			p.texts = append(p.texts, texts...)
			p.mu.Unlock()
			return nil, errors.New("provider outage")
		}
	}
	return p.recordingEmbeddingProvider.EmbedTexts(ctx, texts)
}

func TestDerivedOntologyProviderFailureCachesSucceededSlices(t *testing.T) {
	root := t.TempDir()
	writeWatcherSchema(t, root, "type Spec @node(paths: [\"notes/*.md\"]) {\n  status: String @field\n  userStories: UserStoriesSection @contains(level: H2, heading: \"User Stories\")\n}\n\ntype UserStoriesSection implements Section {\n  stories: [UserStory!] @contains(level: H3)\n}\n\ntype UserStory implements Section @node(locator: EMBEDDED) {\n  storyId: String @field\n}\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "checkout.md"), []byte("---\ntype: Spec\nstatus: draft\n---\n# Checkout Refresh\n\n## User Stories\n\n### Faster checkout\nstoryId:: US-1\nUsers need quicker checkout.\n"), 0o644))
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}})
	defer store.Close()
	defer c.Close()
	provider := &failingTextProvider{failOn: "Type: UserStory\n"}
	w.nodeSyncer = &embedindex.OntologyNodeSyncer{Store: store, Provider: provider, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "recording"}, BatchSize: 1, MaxConcurrent: 3}
	c.MarkDirty("notes/checkout.md", cache.DirtyModified)
	runOwnershipBatch(t, w)
	work, err := store.PendingDerivedWork(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	var ticket codeanchor.DerivedWork
	for _, candidate := range work {
		if candidate.Kind == codeanchor.DerivedOntology {
			ticket = candidate
		}
	}
	require.Equal(t, codeanchor.DerivedOntology, ticket.Kind)

	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	require.ErrorContains(t, d.execute(ticket), "provider outage")
	sent := len(provider.texts)
	require.Equal(t, 3, sent, "each of the fixture's three texts is its own slice")

	provider.mu.Lock()
	provider.failOn = ""
	provider.mu.Unlock()
	require.NoError(t, d.execute(ticket))
	require.Len(t, provider.texts, sent+1, "the retry sends only the text whose slice failed")
}
