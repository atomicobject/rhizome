package bootstrap

import (
	"context"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// SetGlobalEventSink installs the local broker callback. Bootstrap stays
// independent of the web package; serve wires the concrete SSE broker after
// server construction.
func (rt *LiveRuntime) SetGlobalEventSink(fn GlobalEventSink) {
	if rt == nil || fn == nil {
		return
	}
	rt.globalEventSink.Store(fn)
}

// PublishGlobalEvent emits a vault-wide freshness event on this process.
func (rt *LiveRuntime) PublishGlobalEvent(kind string, data any) {
	if rt == nil {
		return
	}
	fn, _ := rt.globalEventSink.Load().(GlobalEventSink)
	if fn != nil {
		fn(kind, data)
	}
}

// WaitForSearch blocks until the search capability is ready or context is cancelled.
// Search capability includes: cache and watchhub.
func (rt *LiveRuntime) WaitForSearch(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rt.searchReady:
		if err := rt.searchErr.Load(); err != nil {
			return *err
		}
		return nil
	}
}

// WaitForSemantic blocks until the semantic capability is ready or context is cancelled.
// Semantic capability includes: note embeddings index and provider.
func (rt *LiveRuntime) WaitForSemantic(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rt.semanticReady:
		if err := rt.semanticErr.Load(); err != nil {
			return *err
		}
		return nil
	}
}

// WaitForCodeIndex blocks until the code index capability is ready or context is cancelled.
// Code index capability includes: code anchor service and intel store.
func (rt *LiveRuntime) WaitForCodeIndex(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rt.codeReady:
		if err := rt.codeErr.Load(); err != nil {
			return *err
		}
		return nil
	}
}

// WaitForLeader blocks until this runtime becomes leader or context is cancelled.
func (rt *LiveRuntime) WaitForLeader(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-rt.leaderCh:
		return true
	}
}

// Snapshot returns the currently available phase states and dependencies
// without waiting for initialization.
func (rt *LiveRuntime) Snapshot() runtimeview.Snapshot {
	if rt == nil {
		return runtimeview.Snapshot{}
	}
	rt.ownershipConfigMu.Lock()
	vaultDefinition := rt.currentOwnershipVaultDefinition()
	configuration := rt.Configuration()
	rt.ownershipConfigMu.Unlock()
	notePath := ""
	if cfg := rt.embCfg.Load(); cfg != nil {
		notePath = cfg.IndexPath
	}
	codeIndex, codeProvider := rt.CodeEmbeddings()
	var noteReader obsidian.NoteReader
	if cacheService := rt.cache.Load(); cacheService != nil {
		noteReader = cache.NewNoteAdapter(cacheService, &obsidian.Note{})
	}
	return runtimeview.Snapshot{
		VaultDefinition:   vaultDefinition,
		Configuration:     &configuration,
		Search:            capabilityState(rt.searchReady, rt.searchErr.Load()),
		Semantic:          capabilityState(rt.semanticReady, rt.semanticErr.Load()),
		Code:              capabilityState(rt.codeReady, rt.codeErr.Load()),
		Leader:            rt.leaderFlag.Load(),
		WatchHub:          rt.hub.Load(),
		NoteReader:        noteReader,
		NoteIndex:         rt.noteIndex.Load(),
		NoteProvider:      rt.NoteProvider(),
		NoteIndexPath:     notePath,
		CodeIndex:         codeIndex,
		CodeProvider:      codeProvider,
		CodeAnchorService: rt.codeAnchorSvc.Load(),
		IntelStore:        rt.intelStore.Load(),
		SessionStore:      rt.SessionDedupeStore(),
	}
}

func capabilityState(done <-chan struct{}, errPtr *error) runtimeview.CapabilityState {
	state := runtimeview.CapabilityState{Err: nil}
	select {
	case <-done:
		state.Done = true
	default:
		return state
	}
	if errPtr != nil {
		state.Err = *errPtr
		return state
	}
	state.Ready = true
	return state
}

// SearchReady returns true if the search capability is ready.
func (rt *LiveRuntime) SearchReady() bool {
	select {
	case <-rt.searchReady:
		return rt.searchErr.Load() == nil
	default:
		return false
	}
}

// SemanticReady returns whether semantic capability is ready and any error.
func (rt *LiveRuntime) SemanticReady() (bool, error) {
	select {
	case <-rt.semanticReady:
		if err := rt.semanticErr.Load(); err != nil {
			return false, *err
		}
		return true, nil
	default:
		return false, nil
	}
}

// CodeIndexReady returns whether code index capability is ready and any error.
func (rt *LiveRuntime) CodeIndexReady() (bool, error) {
	select {
	case <-rt.codeReady:
		if err := rt.codeErr.Load(); err != nil {
			return false, *err
		}
		return true, nil
	default:
		return false, nil
	}
}

// Cache returns the cache service (nil until search ready).
func (rt *LiveRuntime) Cache() *cache.Service {
	return rt.cache.Load()
}

// Hub returns the watchhub (nil until search ready, may be nil if unavailable).
func (rt *LiveRuntime) Hub() *watchhub.Hub {
	return rt.hub.Load()
}

// NoteIndex returns the note embeddings index (nil until semantic ready).
func (rt *LiveRuntime) NoteIndex() *embsqlite.NoteStore {
	return rt.noteIndex.Load()
}

// NoteProvider returns the note embeddings provider (nil until semantic ready).
func (rt *LiveRuntime) NoteProvider() embeddings.Provider {
	if p := rt.noteProvider.Load(); p != nil {
		return *p
	}
	return nil
}

// CodeAnchorService returns the code anchor service (nil until code ready).
func (rt *LiveRuntime) CodeAnchorService() *codeanchor.Service {
	return rt.codeAnchorSvc.Load()
}

// IntelStore returns the current unified intel read handle. A validated,
// existing read-only handle may be published during search initialization;
// phase 3 replaces it with the full code-index handle.
func (rt *LiveRuntime) IntelStore() *semdb.Store {
	return rt.intelStore.Load()
}

// NoteMetadataIndexer returns the immutable provider-aware metadata indexer
// composed for this runtime before asynchronous initialization begins.
func (rt *LiveRuntime) NoteMetadataIndexer() notemeta.Indexer {
	if rt == nil {
		return notemeta.Indexer{}
	}
	return rt.noteMetadataIndexer
}

// SessionDedupeStore returns the narrow session persistence capability.
func (rt *LiveRuntime) SessionDedupeStore() semdb.SessionDedupeStore {
	holder := rt.sessionStore.Load()
	if holder == nil {
		return nil
	}
	return holder.store
}

// WaitForSession waits only for the optional existing-only session writer.
func (rt *LiveRuntime) WaitForSession(ctx context.Context) error {
	select {
	case <-rt.sessionReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LiveHealth reports current live watcher status.
func (rt *LiveRuntime) LiveHealth() LiveHealth {
	if rt == nil {
		return LiveHealth{Role: "unknown", WatcherMode: "unavailable"}
	}
	role := "leader"
	if !rt.leaderFlag.Load() {
		role = "unavailable"
	}
	mode := "single-process"
	var current, last *LiveEpoch
	if rt.liveHealth != nil {
		current, last = rt.liveHealth.snapshot()
	}
	cacheService := rt.cache.Load()
	pendingDirty := 0
	if cacheService != nil {
		pendingDirty = len(cacheService.DirtySnapshot())
	}
	var scheduler LiveSchedulerHealth
	if watcher := rt.liveWatcher.Load(); watcher != nil {
		scheduler = watcher.schedulerHealth()
	}
	health := LiveHealth{
		Role:               role,
		WatcherMode:        mode,
		PendingDirtyCount:  pendingDirty,
		Scheduler:          scheduler,
		CurrentEpoch:       current,
		LastCompletedEpoch: last,
	}
	if last != nil {
		if last.Status == "failed" {
			health.FailedPhase = last.Phase
			health.Error = last.Error
		}
		health.DegradedReasons = append([]string(nil), last.DegradedReasons...)
	}
	return health
}

// EmbeddingsConfig returns the note embeddings configuration (nil until semantic ready).
func (rt *LiveRuntime) EmbeddingsConfig() *embeddings.Config {
	return rt.embCfg.Load()
}

// CodeEmbeddingsConfig returns the code embeddings configuration (nil until code ready).
func (rt *LiveRuntime) CodeEmbeddingsConfig() *embeddings.Config {
	return rt.codeEmbCfg.Load()
}

// CodeEmbeddings returns the code embeddings store and provider (both nil until code ready).
func (rt *LiveRuntime) CodeEmbeddings() (*embsqlite.CodeStore, embeddings.Provider) {
	store := rt.codeEmbStore.Load()
	if p := rt.codeEmbProvider.Load(); p != nil {
		return store, *p
	}
	return store, nil
}

// NoteSyncer returns the note syncer for embeddings (nil until leader work starts).
func (rt *LiveRuntime) NoteSyncer() *embedindex.NoteSyncer {
	return rt.noteSyncer.Load()
}

// CodeSyncer returns the code syncer for embeddings (nil until leader work starts).
func (rt *LiveRuntime) CodeSyncer() *embedindex.Syncer {
	return rt.codeSyncer.Load()
}

// Configuration returns the current explicit repository configuration snapshot.
func (rt *LiveRuntime) Configuration() repoconfig.Resolved {
	if current := rt.resolvedConfig.Load(); current != nil {
		return *current
	}
	return repoconfig.FromLocal(rt.VaultPath, rt.LocalCfg)
}
