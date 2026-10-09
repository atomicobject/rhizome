package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// controlledWorkloadProvider changes provider availability without changing any
// source files. The real syncer still builds chunks and writes durable vectors.
type controlledWorkloadProvider struct {
	inner    embeddings.Provider
	fail     atomic.Bool
	block    atomic.Bool
	calls    atomic.Int64
	failures atomic.Int64
	canceled atomic.Int64
	entered  chan struct{}
	release  chan struct{}
}

func (p *controlledWorkloadProvider) Dimensions() int { return p.inner.Dimensions() }
func (p *controlledWorkloadProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.calls.Add(1)
	if p.fail.Load() {
		p.failures.Add(1)
		return nil, errors.New("synthetic provider outage")
	}
	if p.block.Load() {
		select {
		case p.entered <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			p.canceled.Add(1)
			return nil, ctx.Err()
		case <-p.release:
		}
	}
	return p.inner.EmbedTexts(ctx, texts)
}

func liveWorkloadWatcher(t *testing.T) (*LiveRuntime, *unifiedSemanticWatcher, *controlledWorkloadProvider) {
	t.Helper()
	root := t.TempDir()
	indexingworkload.Write(t, root, 16, 1)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	rt, err := NewLiveRuntime(ctx, LiveOptions{
		VaultName: root, DisableLeaderWork: true, DisableWatchHub: true,
		DisableSessionStore: true,
		Requirements:        RequireRuntimeCapabilities(RuntimeCapabilitySearch, RuntimeCapabilityCodeIndex, RuntimeCapabilityCodeAnchorWarmup),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	require.NoError(t, rt.WaitForSearch(ctx))
	require.NoError(t, rt.WaitForCodeIndex(ctx))
	indexLane := lane.New(lane.Options{LockPath: obsidian.IndexLockPath(root)})
	provider := &controlledWorkloadProvider{
		inner:   embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: 8}),
		entered: make(chan struct{}, 1), release: make(chan struct{}),
	}
	// Runtime boot is real. The deterministic scheduler clock isolates indexing
	// execution latency from the production three-second polling interval.
	w := StartUnifiedSemanticWatcher(ctx, root, WatcherDeps{
		VaultDef: rt.VaultDef, NoteRuntime: rt.noteFormats,
		CodeConfig: *rt.codeCfg.Load(), NoteMetadataIndexer: rt.noteMetadataIndexer,
		CacheService: rt.Cache(), NoteSvc: rt.CodeAnchorService(), IntelStore: rt.IntelStore(),
		NodeSyncer: &semantic.OntologyNodeSyncer{Store: rt.IntelStore(), Provider: provider,
			ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: 8}},
		Lane: indexLane, Health: rt.liveHealth,
		Options: &UnifiedSemanticWatcherOptions{Tick: make(chan time.Time)},
	}, false)
	t.Cleanup(func() { cancel(); indexLane.Close(); w.stopValidationTimer(); w.stopWatchHub(); w.closeDerived() })
	require.NoError(t, rt.Cache().EnsureReady(ctx))
	// Read-side cache warmup can consume the initial discovery diff before the
	// watcher runs. Leader boot work and polling are disabled in this fixture,
	// so explicitly request the first structural publication from a warm cache.
	w.retainWatchEvents(nil, true)
	handle := w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	require.NoError(t, handle.Err())
	ready := assert.EventuallyWithT(t, func(collect *assert.CollectT) {
		var currentPaths int
		err := rt.IntelStore().DB().QueryRowContext(t.Context(), `SELECT count(DISTINCT s.note_path) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id JOIN intel_embeddings e ON e.chunk_id=s.chunk_id JOIN intel_chunks c ON c.chunk_id=e.chunk_id JOIN intel_embeddings_vec_d8 v ON v.chunk_id=c.id WHERE s.node_structure_fingerprint=n.structural_fingerprint`).Scan(&currentPaths)
		if assert.NoError(collect, err) {
			assert.Equal(collect, 16, currentPaths, "initial fixture vectors must be current")
		}
		var pending int
		err = rt.IntelStore().DB().QueryRowContext(t.Context(), `SELECT count(*) FROM derived_work`).Scan(&pending)
		if assert.NoError(collect, err) {
			assert.Zero(collect, pending, "initial derived work must finish")
		}
	}, 30*time.Second, 25*time.Millisecond, "initial fixture must settle before provider control changes")
	if !ready {
		diagnosticCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		var nodes, states int
		statsErr := rt.IntelStore().DB().QueryRowContext(diagnosticCtx, `SELECT (SELECT count(*) FROM ontology_nodes), (SELECT count(*) FROM ontology_node_embedding_state)`).Scan(&nodes, &states)
		var work string
		workErr := rt.IntelStore().DB().QueryRowContext(diagnosticCtx, `SELECT json_group_array(json_object('kind',kind,'path',path,'ready',ready,'generation',generation,'attempt',attempt,'retry_at',retry_at)) FROM derived_work`).Scan(&work)
		t.Logf("startup nodes=%d states=%d stats_error=%v derived_work=%s work_error=%v", nodes, states, statsErr, work, workErr)
		t.Logf("startup lane=%+v scheduler=%+v provider_calls=%d failures=%d cancellations=%d", indexLane.Status(), w.schedulerHealth(), provider.calls.Load(), provider.failures.Load(), provider.canceled.Load())
		t.FailNow()
	}
	return rt, w, provider
}

func TestLiveWorkloadBlockedProviderMeasurements(t *testing.T) {
	runLiveBlockedWorkload(t, false)
}

func TestLiveWorkloadSecondEditVisibleWhileProviderBlocked(t *testing.T) {
	runLiveBlockedWorkload(t, true)
}

func runLiveBlockedWorkload(t *testing.T, assertSecond bool) {
	t.Helper()
	rt, w, provider := liveWorkloadWatcher(t)
	provider.block.Store(true)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(provider.release) }) }
	t.Cleanup(release)
	path := "notes/project-0000.md"
	before, err := rt.IntelStore().CurrentNoteMetadataRowsByPaths(t.Context(), []string{path, "notes/project-0001.md"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Changed")), 0o600))
	start := time.Now()
	rt.Cache().MarkDirty(path, cache.DirtyModified)
	first := w.processOwnershipBatch()
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("embedding call never started")
	}
	rows, err := rt.IntelStore().CurrentNoteMetadataRowsByPaths(t.Context(), []string{path})
	require.NoError(t, err)
	require.NotEqual(t, before[path].ContentHash, rows[path].ContentHash)
	nodes, err := rt.IntelStore().OntologyNodesByPaths(t.Context(), []string{path})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	fields, err := rt.IntelStore().OntologyNodeFieldValuesByNodeIDs(t.Context(), []string{nodes[0].NodeID}, []string{"name"})
	require.NoError(t, err)
	require.Len(t, fields, 1)
	require.Equal(t, "Project 0000 Changed", fields[0].ValueText)
	t.Logf("first structural visibility=%s while provider remains blocked", time.Since(start))
	// A second edit arrives while the first embedding call is still in flight.
	secondPath := "notes/project-0001.md"
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, secondPath), []byte(indexingworkload.Note(1, 16, "Changed")), 0o600))
	rt.Cache().MarkDirty(secondPath, cache.DirtyModified)
	second := w.processOwnershipBatch()
	secondStart := time.Now()
	if assertSecond {
		// The provider returns only after release below, so a second batch that
		// waited for the blocked call would never finish; the bound limits only
		// how long a failing run takes. Waiting on the batch itself also reports
		// its error instead of an anonymous polling timeout.
		watcherJobTerminal(t, second)
		require.NoError(t, second.Err())
		rows, err := rt.IntelStore().CurrentNoteMetadataRowsByPaths(t.Context(), []string{secondPath})
		require.NoError(t, err)
		require.NotEqual(t, before[secondPath].ContentHash, rows[secondPath].ContentHash, "a second structural edit must publish while the first embedding call remains blocked")
		require.Zero(t, provider.canceled.Load(), "the second batch must not cancel the blocked embedding call")
	} else {
		time.Sleep(100 * time.Millisecond)
	}
	secondRows, err := rt.IntelStore().CurrentNoteMetadataRowsByPaths(t.Context(), []string{secondPath})
	require.NoError(t, err)
	t.Logf("second edit visible after %s=%v, first job complete=%v", time.Since(secondStart), secondRows[secondPath].ContentHash != before[secondPath].ContentHash, workloadJobDone(first))
	provider.block.Store(false)
	release()
	watcherJobTerminal(t, first)
	require.NoError(t, first.Err())
	retry := w.processOwnershipBatch()
	watcherJobTerminal(t, retry)
	require.NoError(t, retry.Err())
	secondRows, err = rt.IntelStore().CurrentNoteMetadataRowsByPaths(t.Context(), []string{secondPath})
	require.NoError(t, err)
	require.NotEqual(t, before[secondPath].ContentHash, secondRows[secondPath].ContentHash)
	t.Logf("both edits converged=%s", time.Since(start))
}

func workloadJobDone(handle lane.Handle) bool {
	select {
	case <-handle.Done():
		return true
	default:
		return false
	}
}

func TestLiveWorkloadQuietTickRetriesFailedEmbedding(t *testing.T) {
	rt, w, provider := liveWorkloadWatcher(t)
	provider.fail.Store(true)
	path := "notes/project-0000.md"
	var oldSourceHash string
	require.NoError(t, rt.IntelStore().DB().QueryRowContext(t.Context(), `SELECT chunk_text_hash FROM ontology_node_embedding_state WHERE note_path=? LIMIT 1`, path).Scan(&oldSourceHash))
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Changed")), 0o600))
	rt.Cache().MarkDirty(path, cache.DirtyModified)
	failed := w.processOwnershipBatch()
	watcherJobTerminal(t, failed)
	require.NoError(t, failed.Err(), "optional provider failure must preserve structural success")
	require.Eventually(t, func() bool { return provider.failures.Load() > 0 }, 5*time.Second, 5*time.Millisecond, "controlled outage must be observed before recovery")
	before := provider.calls.Load()
	provider.fail.Store(false)
	start := time.Now()
	quiet := w.processOwnershipBatch()
	watcherJobTerminal(t, quiet)
	require.NoError(t, quiet.Err())
	t.Logf("quiet retry elapsed=%s providerCallsBefore=%d providerCallsAfter=%d", time.Since(start), before, provider.calls.Load())
	require.Eventually(t, func() bool { return provider.calls.Load() > before }, time.Second, 5*time.Millisecond, "a quiet tick must retry semantic debt without another source event")
	require.Eventually(t, func() bool {
		var currentVectors int
		err := rt.IntelStore().DB().QueryRowContext(t.Context(), `SELECT count(*) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id JOIN intel_embeddings e ON e.chunk_id=s.chunk_id JOIN intel_chunks c ON c.chunk_id=e.chunk_id JOIN intel_embeddings_vec_d8 v ON v.chunk_id=c.id WHERE s.note_path=? AND s.node_structure_fingerprint=n.structural_fingerprint AND s.chunk_text_hash != ?`, path, oldSourceHash).Scan(&currentVectors)
		return err == nil && currentVectors > 0
	}, time.Second, 5*time.Millisecond, "quiet provider recovery must publish current durable vectors")
}
