package bootstrap

import (
	"context"
	"log/slog"
	"path/filepath"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// Phase 4: Leader work (runs continuously in background)
func (rt *LiveRuntime) initPhase4Leader() {
	defer rt.observeCapabilityPhase("leader")()
	if rt.disableLeaderWork {
		return
	}
	rt.startWorker(func() {
		select {
		case <-rt.ctx.Done():
			return
		case <-rt.leaderCh:
			rt.startLeaderWork()
		}
	})
}

func (rt *LiveRuntime) startLeaderWork() {
	cacheService := rt.cache.Load()
	noteSvc := rt.codeAnchorSvc.Load()
	intelStore := rt.intelStore.Load()
	hub := rt.hub.Load()
	configurationView := rt.Snapshot()
	vaultDef, leaderCodeCfg := configurationView.VaultDefinition, configurationView.Configuration.Code
	codeCfg := &leaderCodeCfg
	unifiedWatcherAvailable := cacheService != nil && noteSvc != nil && intelStore != nil

	// subscriberReady is closed after WatchHub roots are registered and any
	// fallback codeanchor subscriber is installed, allowing us to safely enable
	// FSNotify without losing events.
	subscriberReady := make(chan struct{})

	if codeCfg.Enabled && noteSvc != nil {
		// Register WatchHub roots for note/code events. When the unified semantic
		// watcher is available, it is the live owner for durable note/code ingest;
		// the legacy codeanchor subscriber stays off so it cannot also full-rescan,
		// ingest notes, index code, or recompute scopes for the same events.
		noteRoots := []string{rt.VaultPath}
		codeRoots := codeintel.ResolveRoots(rt.VaultPath, codeCfg.CodeRoots())
		ignoreGlobs := append([]string{}, codeCfg.PythonIgnore...)
		ignoreGlobs = append(ignoreGlobs, codeCfg.GoIgnore...)
		ignoreGlobs = append(ignoreGlobs, codeCfg.TSIgnore...)
		ignoreGlobs = append(ignoreGlobs, codeCfg.CSharpIgnore...)
		ignoreGlobs = append(ignoreGlobs, codeCfg.PHPIgnore...)

		rt.startWorker(func() {
			defer close(subscriberReady)
			if hub == nil {
				rt.recordDiagnostic(slog.LevelWarn, "watch_roots.skipped", nil, slog.String("reason_code", "watch_hub_unavailable"))
				return
			}
			for _, root := range noteRoots {
				_ = hub.AddRoot(root, watchhub.RootOptions{Kind: watchhub.RootNotes})
			}
			for _, root := range codeRoots {
				if err := hub.AddRoot(root, watchhub.RootOptions{Kind: watchhub.RootCode}); err != nil {
					rt.recordDiagnostic(slog.LevelWarn, "watch_root.registration_failed", err)
				} else {
					rt.recordDiagnostic(slog.LevelInfo, "watch_root.registered", nil)
				}
			}
			if !shouldStartLegacyCodeAnchorSubscriber(unifiedWatcherAvailable) {
				if diagnostics.FromContext(rt.ctx) != nil {
					rt.recordDiagnostic(slog.LevelInfo, "code_subscriber.skipped", nil, slog.String("reason_code", "unified_watcher_owns_ingest"))
				}
				hub.AddWatchRoots()
				return
			}
			watcher, err := codeanchor.NewHubSubscriber(
				noteSvc,
				noteRoots,
				codeRoots,
				func(path string, content []byte, mtime int64) (codeanchor.NoteSource, error) {
					return rt.noteMetadataIndexer.ProjectNoteSource(path, string(content), mtime)
				},
				codeanchor.WithWatcherExcludeGlobs(ignoreGlobs),
				codeanchor.WithWatcherIgnoreMatcher(hub.IgnoreMatcher()),
				codeanchor.WithWatcherDebounce(0),
			)
			if err != nil {
				rt.recordDiagnostic(slog.LevelWarn, "code_subscriber.unavailable", err)
				return
			}
			rt.addCloser(func() {
				if err := watcher.Close(); err != nil {
					rt.recordDiagnostic(slog.LevelWarn, "code_subscriber.close_failed", err)
				}
			})
			if err := watcher.Start(rt.ctx); err != nil {
				rt.recordDiagnostic(slog.LevelWarn, "code_subscriber.start_failed", err)
			} else {
				rt.recordDiagnostic(slog.LevelInfo, "code_subscriber.started", nil, slog.Int("note_roots", len(noteRoots)), slog.Int("code_roots", len(codeRoots)))
			}
			vaultPaths, _ := paths.NewVaultPaths(rt.VaultPath)
			prefixes := make([]string, 0, len(noteRoots)+len(codeRoots))
			for _, root := range append(append([]string{}, noteRoots...), codeRoots...) {
				if rel, err := vaultPaths.RelStrict(root); err == nil {
					if rel.String() == "" {
						prefixes = nil
						break
					}
					prefixes = append(prefixes, filepath.ToSlash(rel.String()))
				}
			}
			onStale := func(ctx context.Context, ev watchhub.StaleEvent) {
				if ev.Reason == watchhub.StaleIgnoreChanged {
					watcher.SetIgnoreMatcher(hub.IgnoreMatcher())
				}
				watcher.HandleWatchStale(ctx, ev)
			}
			unsub := hub.Subscribe("codeanchor", watchhub.Filter{
				Prefixes:      prefixes,
				IncludeDirs:   true,
				IncludeFiles:  true,
				IncludeHidden: false,
			}, watcher.HandleWatchEvents, onStale)
			rt.addCloser(unsub)
			hub.AddWatchRoots()
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelInfo, "watcher.ready", nil, slog.Int("note_roots", len(noteRoots)), slog.Int("code_roots", len(codeRoots)))
			}
		})
	} else {
		// No codeanchor subscriber needed; signal ready immediately.
		close(subscriberReady)
	}

	rt.startLeaderIndexing(vaultDef, codeCfg, cacheService, noteSvc, intelStore, hub, unifiedWatcherAvailable)
}

func shouldStartLegacyCodeAnchorSubscriber(unifiedWatcherAvailable bool) bool {
	return !unifiedWatcherAvailable
}
