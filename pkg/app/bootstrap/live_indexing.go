package bootstrap

import (
	"context"
	"errors"
	"log"
	"os"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// startLeaderIndexing starts every indexing actor the vault owner runs. It
// first installs the serialized indexing lane: from here on, boot catch-up,
// watcher batches, and explicit index jobs are lane jobs, and the lane is the
// only component in this process that acquires .rhizome/index.lock
// (SPEC-0104 US3).
func (rt *LiveRuntime) startLeaderIndexing(
	vaultDef obsidian.VaultDefinition,
	codeCfg *codeanchor.Config,
	cacheService *cache.Service,
	noteSvc *codeanchor.Service,
	intelStore *semdb.Store,
	hub *watchhub.Hub,
	unifiedWatcherAvailable bool,
) {
	// The leader/follower hint log is deleted by SPEC-0104; the parameter stays
	// until the serve lane drops it from the call.

	indexLane := rt.Lane()
	if indexLane == nil {
		indexLane = lane.New(lane.Options{LockPath: obsidian.IndexLockPath(rt.VaultPath), Debug: rt.debug})
		rt.SetLane(indexLane)
		rt.addCloser(indexLane.Close)
	}

	// Boot catch-up picks up changes made before the watcher started.
	if rt.backgroundIndexer != nil {
		rt.startWorker(func() { rt.runBootCatchUp(vaultDef) })
	}

	// Ensure Ollama is ready only on the leader before embedding work begins.
	embCfg := rt.embCfg.Load()
	codeEmbCfg := rt.codeEmbCfg.Load()
	if embCfg != nil {
		if err := embeddings.EnsureOllamaForConfig(rt.ctx, *embCfg, os.Stderr); err != nil {
			log.Printf("semantic: %v", err)
		}
	}
	if codeEmbCfg != nil {
		if err := embeddings.EnsureOllamaForConfig(rt.ctx, *codeEmbCfg, os.Stderr); err != nil {
			log.Printf("code embeddings: %v", err)
		}
	}

	if !unifiedWatcherAvailable {
		return
	}

	noteSyncer := rt.noteSyncer.Load()
	var nodeSyncer *semantic.OntologyNodeSyncer
	if noteSyncer != nil && noteSyncer.Provider != nil {
		nodeSyncer = &semantic.OntologyNodeSyncer{
			Store:         intelStore,
			Provider:      noteSyncer.Provider,
			ProviderInfo:  noteSyncer.ProviderInfo,
			BatchSize:     noteSyncer.BatchSize,
			MaxConcurrent: noteSyncer.MaxConcurrent,
			EmbedGate:     noteSyncer.EmbedGate,
			EmbeddingNode: noteSyncer.EmbeddingNode,
		}
	}
	watcher := StartUnifiedSemanticWatcher(rt.ctx, rt.VaultPath, WatcherDeps{
		VaultDef:               vaultDef,
		NoteRuntime:            rt.noteFormats,
		CodeConfig:             *codeCfg,
		NoteMetadataIndexer:    rt.noteMetadataIndexer,
		CacheService:           cacheService,
		WatchHub:               hub,
		NoteSvc:                noteSvc,
		IntelStore:             intelStore,
		NoteSyncer:             noteSyncer,
		NodeSyncer:             nodeSyncer,
		CodeSyncer:             rt.codeSyncer.Load(),
		NoteSemanticStore:      embCfg != nil && embCfg.Enabled,
		CodeSemanticStore:      codeEmbCfg != nil && codeEmbCfg.Enabled,
		Lane:                   indexLane,
		Health:                 rt.liveHealth,
		PublishEvent:           rt.PublishGlobalEvent,
		OnNoteSelectionChanged: rt.setNoteSelectionPolicy,
	}, rt.debug)
	cacheService.MarkStale()
	rt.liveWatcher.Store(watcher)
	rt.addCloser(watcher.closeDerived)
	rt.addCloser(watcher.stopValidationTimer)
	rt.addCloser(watcher.stopWatchHub)
}

// runBootCatchUp runs the boot catch-up through the configured indexer. The
// indexer (the serve readiness coordinator) submits the index itself as a
// KindBootCatchUp lane job; its pre-checks and post-index validation refresh
// run outside the job because the refresh takes .rhizome/index.lock, which the
// job holds.
func (rt *LiveRuntime) runBootCatchUp(vaultDef obsidian.VaultDefinition) {
	if err := rt.backgroundIndexer(rt.ctx, rt.noteMetadataIndexer, rt.VaultPath, vaultDef, rt.debug); err != nil && rt.debug && !errors.Is(err, context.Canceled) {
		log.Printf("live: boot catch-up index: %v", err)
	}
}
