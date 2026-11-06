package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// Phase 3: Code index capability (code anchor service + intel store)
func (rt *LiveRuntime) initPhase3Code() {
	defer rt.observeCapabilityPhase("code")()
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("code index init panic: %v", r)
			rt.codeErr.Store(&err)
			close(rt.codeReady)
			// Continue to leader phase so leader work can still proceed
			rt.initPhase4Leader()
		}
	}()
	if rt.isClosed() {
		err := context.Canceled
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}
	if !rt.requirements.Includes(RuntimeCapabilityCodeIndex) {
		err := capabilityNotRequested(RuntimeCapabilityCodeIndex)
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}

	codeCfg, err := rt.publishPhase3OwnershipConfiguration()
	if err != nil {
		err = fmt.Errorf("compile cache ownership selection: %w", err)
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}

	recoveryWindow := codeIndexOpenRecoveryWindow
	if rt.readOnlyCodeIndex {
		// One-shot indexed reads report unavailable evidence promptly; they do
		// not own the long-lived runtime's startup recovery window.
		recoveryWindow = 0
	}
	store, cleanup, err := openIntelStoreWithRetry(rt.ctx, recoveryWindow, func() (*semdb.Store, func(), error) {
		return rt.openIntelStoreForServe(codeCfg)
	})
	if err != nil {
		err = fmt.Errorf("code index unavailable at %s: %w", codeCfg.IndexPath, err)
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}
	if store == nil {
		err = errors.New("code index not available")
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}
	if rt.isClosed() {
		cleanup()
		err := context.Canceled
		rt.codeErr.Store(&err)
		close(rt.codeReady)
		rt.initPhase4Leader()
		return
	}

	store.SetWriteMu(&rt.sharedWriteMu)
	if rt.queryProvidersOnly {
		provider := rt.queryNoteProvider.Load()
		meta := rt.queryNoteMeta.Load()
		if provider != nil && meta != nil {
			err = embsqlite.ValidateExisting(rt.ctx, embsqlite.Borrowed(store.DB()), embsqlite.Notes, *meta)
		}
		if err != nil {
			cleanup()
			err = fmt.Errorf("note embedding index incompatible: %w", err)
			rt.codeErr.Store(&err)
			close(rt.codeReady)
			rt.initPhase4Leader()
			return
		}
		if provider != nil {
			rt.noteProvider.Store(provider)
		}
	}
	rt.intelStore.Store(store)
	rt.addCloser(func() {
		if diagnostics.FromContext(rt.ctx) != nil {
			rt.recordDiagnostic(slog.LevelInfo, "intel_store.closed", nil)
		}
		cleanup()
	})

	var sessionStore semdb.SessionDedupeHandle
	if !rt.disableSessionStore && !rt.readOnlyCodeIndex {
		sessionStore = store
	}
	if !rt.disableSessionStore && rt.readOnlyCodeIndex && !rt.sessionOnlyExisting {
		// Indexed retrieval remains query-only. Session dedupe gets a separate,
		// existing-only validated handle so repeated one-shot commands can
		// persist reservations without enabling index mutation or migrations.
		doneSessionStore := indexingperf.StartSpan(
			indexingperf.WithPhase(rt.ctx, indexingperf.AgentStartPhaseStoreWarmOpen),
			indexingperf.AgentStartPhaseStoreWarmOpen,
		)
		sessionStore, err = semdb.OpenSessionStoreExisting(codeCfg.IndexPath, rt.ctx, sessionSQLitePoolOptions())
		doneSessionStore(err)
		if err != nil {
			sessionStore = nil
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelInfo, "session_store.unavailable", err)
			}
		} else {
			rt.addCloser(func() {
				if err := sessionStore.Close(); err != nil {
					rt.recordDiagnostic(slog.LevelWarn, "session_store.close_failed", err)
				}
			})
		}
	}
	if sessionStore != nil {
		rt.sessionStore.Store(&sessionStoreHolder{store: sessionStore})
	}
	if rt.ownsSessionCleanup() {
		// Only write-capable runtimes with session persistence own cleanup.
		rt.startSessionCleanup(store)
	}

	// Create code anchor service
	var warmCtx context.Context
	includeIndexers := codeCfg.Enabled && rt.requirements.Includes(RuntimeCapabilityCodeAnchorWarmup)
	if includeIndexers {
		warmCtx = rt.ctx
	}
	svc, _ := codeintel.NewCodeAnchorService(codeintel.CodeAnchorServiceConfig{
		VaultPath:        rt.VaultPath,
		CodeCfg:          codeCfg,
		Store:            store,
		WarmCacheContext: warmCtx,
		IncludeIndexers:  includeIndexers,
		WriteAccess:      includeIndexers,
	})
	if svc != nil {
		rt.codeAnchorSvc.Store(svc)
	}

	// Initialize code embeddings (optional, failures don't block code capability)
	if rt.requirements.Includes(RuntimeCapabilityCodeEmbeddings) {
		rt.initCodeEmbeddings(store)
	}

	// Create syncers for leader work
	if rt.requirements.Includes(RuntimeCapabilityLeaderSyncers) {
		rt.createSyncers(store)
	}

	// Code capability is ready
	close(rt.codeReady)

	// Continue to phase 4
	rt.initPhase4Leader()
}

func (rt *LiveRuntime) openIntelStoreForServe(codeCfg codeanchor.Config) (*semdb.Store, func(), error) {
	doneStore := indexingperf.StartSpan(indexingperf.WithPhase(rt.ctx, indexingperf.AgentStartPhaseStoreWarmOpen), indexingperf.AgentStartPhaseStoreWarmOpen)
	if rt.readOnlyCodeIndex {
		store, err := semdb.OpenReadOnlyExisting(codeCfg.IndexPath, rt.ctx, liveSQLitePoolOptions())
		doneStore(err)
		if err != nil {
			return nil, nil, err
		}
		return store, func() { _ = store.Close() }, nil
	}
	store, cleanup, err := indexstore.Open(rt.VaultPath, indexstore.Options{Access: indexstore.ReadWrite, Config: &codeCfg, Create: true, Context: rt.ctx,
		Pool: liveSQLitePoolOptions()},
	)
	doneStore(err)
	return store, cleanup, err
}

func (rt *LiveRuntime) publishWarmIntelStoreForRead(codeCfg codeanchor.Config) {
	if rt == nil || rt.intelStore.Load() != nil || strings.TrimSpace(codeCfg.IndexPath) == "" {
		return
	}
	store, err := semdb.OpenReadOnlyExisting(codeCfg.IndexPath, rt.ctx, liveSQLitePoolOptions())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			rt.recordDiagnostic(slog.LevelInfo, "intel_store.warm_open_failed", err)
		}
		return
	}
	if !rt.intelStore.CompareAndSwap(nil, store) {
		_ = store.Close()
		return
	}
	rt.addCloser(func() {
		if err := store.Close(); err != nil {
			rt.recordDiagnostic(slog.LevelWarn, "intel_store.warm_close_failed", err)
		}
	})
}
