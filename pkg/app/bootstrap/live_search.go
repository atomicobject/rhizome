package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// Phase 1: Basic search capability (cache + watchhub)
func (rt *LiveRuntime) initPhase1Search() {
	defer rt.observeCapabilityPhase("search")()
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("search init panic: %v", r)
			rt.searchErr.Store(&err)
			close(rt.searchReady)
			// Continue to next phases so WaitFor* methods don't hang forever
			rt.startWorker(rt.initPhase2Semantic)
		}
	}()

	if !rt.requirements.Includes(RuntimeCapabilitySearch) {
		err := capabilityNotRequested(RuntimeCapabilitySearch)
		rt.searchErr.Store(&err)
		close(rt.searchReady)
		rt.startWorker(rt.initPhase2Semantic)
		return
	}

	// Initialize cache service from the immutable ownership selector. Raw watch
	// events remain available even when this Markdown projection rejects them.
	cacheCodeCfg := rt.Configuration().Code
	rt.codeCfg.Store(&cacheCodeCfg)
	selectionPolicy, err := liveCacheSelectionPolicy(rt.VaultDef, rt.noteFormats, cacheCodeCfg)
	if err != nil {
		err = fmt.Errorf("compile cache ownership selection: %w", err)
		rt.searchErr.Store(&err)
		close(rt.searchReady)
		rt.startWorker(rt.initPhase2Semantic)
		return
	}
	rt.setNoteSelectionPolicy(selectionPolicy)
	rt.publishWarmIntelStoreForRead(cacheCodeCfg)
	cacheOpts := cache.Options{
		DiscoverFiles: selectionPolicy.DiscoverFiles,
		AdmitNote:     selectionPolicy.Admit,
		UserExcludes:  selectionPolicy.UserExcludes,
		NoteRuntime:   &rt.noteFormats,
	}
	var watchRoots []string
	if rt.VaultDef.IsCollection() {
		watchRoots = rt.VaultDef.WatchRoots()
	}

	// Add coderef config if available
	if rt.requirements.Includes(RuntimeCapabilityCodeRefDiscovery) && rt.LocalCfg != nil {
		if inc, exc := repoconfig.NormalizeCodeRefPatterns(*rt.LocalCfg); len(inc) > 0 {
			cacheOpts.CodeRefConfig = coderefs.NewConfig(true, inc, exc)
		}
	}

	cacheService, err := cache.NewService(rt.VaultPath, cacheOpts)
	if err != nil {
		err = fmt.Errorf("cache init failed: %w", err)
		rt.searchErr.Store(&err)
		close(rt.searchReady)
		rt.startWorker(rt.initPhase2Semantic)
		return
	}
	rt.cache.Store(cacheService)
	rt.addCloser(func() { _ = cacheService.Close() })

	if !rt.disableWatchHub {
		// Initialize watchhub
		watchPolicy := embedindex.PolicyFor(embedindex.IndexRequest{
			Source:       embedindex.IndexSourceWatcher,
			LatencyClass: embedindex.LatencyAuto,
		})
		hubOpts := watchhub.Options{
			UserExcludes:  rt.VaultDef.Excludes,
			Debug:         rt.debug,
			Debounce:      watchPolicy.Debounce,
			ForceFSNotify: rt.forceFSNotify,
		}
		hub, err := watchhub.NewHub(rt.VaultPath, hubOpts)
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "watchhub.unavailable", err)
			}
		} else {
			rt.hub.Store(hub)
			hub.Start(rt.ctx)
			rt.addCloser(func() {
				if err := hub.Close(); err != nil {
					rt.recordDiagnostic(slog.LevelWarn, "watchhub.close_failed", err)
				}
			})

			_ = hub.AddRoot(rt.VaultPath, watchhub.RootOptions{Kind: watchhub.RootNotes, WatchForIgnore: true})
			_ = hub.AddRoot(filepath.Join(rt.VaultPath, ".rhizome"), watchhub.RootOptions{Kind: watchhub.RootNotes, IncludeHidden: true})
			for _, root := range watchRoots {
				_ = hub.AddRoot(root, watchhub.RootOptions{Kind: watchhub.RootNotes})
			}
			cache.SubscribeWatchHub(hub, cacheService)

			rt.startWorker(hub.AddWatchRoots)
		}
	}

	if !rt.skipCacheWarmup {
		// Warm the note cache without delaying search readiness.
		rt.startWorker(func() {
			if err := cacheService.EnsureReady(rt.ctx); err != nil && !errors.Is(err, context.Canceled) {
				rt.recordDiagnostic(slog.LevelWarn, "cache.warmup_failed", err)
			}
		})
	}

	// Search capability is ready
	close(rt.searchReady)

	// Continue to phase 2
	rt.startWorker(rt.initPhase2Semantic)
}
