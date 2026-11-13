package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	codeembsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
	"github.com/stretchr/testify/require"
)

func TestLiveParserConfigurationReparsesUnchangedPython(t *testing.T) {
	for _, tc := range []struct {
		name, before, after, want string
	}{
		{"changed roots", "python:\n    roots: [src]", "python:\n    roots: [src/pkg]", "util.answer"},
		{"empty language block", "go:\n    roots: [src]", "go:\n    roots: [src]\n  python: {}", "src.pkg.util.answer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "src/pkg"), 0755))
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "src/pkg/util.py"), []byte("def answer():\n    return 42\n"), 0600))
			configPath := filepath.Join(root, ".rhizome/config.yml")
			writeConfig := func(code string) {
				t.Helper()
				require.NoError(t, os.WriteFile(configPath, []byte("code:\n  enabled: true\n  "+code+"\n"), 0600))
			}
			writeConfig(tc.before)
			rt, w := parserConfigurationTestWatcher(t, root)
			store := w.intelStore
			run := func() {
				t.Helper()
				handle := w.processOwnershipBatch()
				watcherJobTerminal(t, handle)
				require.NoError(t, handle.Err())
			}
			w.retainWatchEvents(nil, true)
			run()
			var fqn string
			require.NoError(t, store.DB().QueryRow(`SELECT fqn FROM symbols WHERE file='src/pkg/util.py' AND kind='func'`).Scan(&fqn))
			require.Equal(t, "pkg.util.answer", fqn)
			writeConfig(tc.after)
			w.retainWatchEvents(map[string]cache.DirtyKind{".rhizome/config.yml": cache.DirtyModified}, false)
			run()
			require.NoError(t, store.DB().QueryRow(`SELECT fqn FROM symbols WHERE file='src/pkg/util.py' AND kind='func'`).Scan(&fqn))
			require.Equal(t, tc.want, fqn)
			require.Same(t, rt.CodeAnchorService(), w.noteSvc)
			hash, present, err := store.GetScopeConfigHash(context.Background())
			require.NoError(t, err)
			require.True(t, present)
			require.Equal(t, w.configuration.Local.ScopeConfigHash(), hash)
		})
	}
}

func parserConfigurationTestWatcher(t *testing.T, root string) (*LiveRuntime, *unifiedSemanticWatcher) {
	t.Helper()
	resolved, err := repoconfig.Load(root)
	require.NoError(t, err)
	definition := repoconfig.LocalConfigToDefinition(root, resolved.Local)
	w, service, store := newLiveOwnershipTestWatcher(t, root, definition)
	t.Cleanup(func() { require.NoError(t, service.Close()); require.NoError(t, store.Close()) })
	w.codeCfg = resolved.Code
	w.noteSvc, _ = codeintel.NewCodeAnchorService(codeintel.CodeAnchorServiceConfig{VaultPath: root, CodeCfg: resolved.Code, Store: store, IncludeIndexers: true, WriteAccess: true})
	rt := &LiveRuntime{ctx: t.Context(), VaultPath: root, VaultDef: definition, noteFormats: w.noteRuntime}
	rt.resolvedConfig.Store(&resolved)
	rt.cache.Store(service)
	rt.intelStore.Store(store)
	rt.codeAnchorSvc.Store(w.noteSvc)
	w.configuration = &resolved
	w.onConfigurationChanged = rt.publishOwnershipConfiguration
	return rt, w
}

func TestLiveCodeConfigurationRegistersAndRemovesWatchRoots(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".rhizome", "old", "new"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0755))
	}
	configPath := filepath.Join(root, ".rhizome/config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("code:\n  python:\n    roots: [old]\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new/mod.py"), []byte("def initial():\n    return 0\n"), 0600))
	rt, w := parserConfigurationTestWatcher(t, root)
	hub, err := watchhub.NewHub(root, watchhub.Options{ForceFSNotify: true, Debounce: time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, hub.Close()) })
	hub.Start(t.Context())
	rt.hub.Store(hub)
	w.watchHub = hub
	w.subscribeWatchHub()
	t.Cleanup(w.stopWatchHub)
	require.NoError(t, hub.AddRoot(filepath.Join(root, "old"), watchhub.RootOptions{Kind: watchhub.RootCode}))
	events := make(chan watchhub.WatchEvent, 32)
	unsub := hub.Subscribe("root-test", watchhub.Filter{IncludeFiles: true}, func(_ context.Context, batch []watchhub.WatchEvent) {
		for _, event := range batch {
			events <- event
		}
	}, nil)
	t.Cleanup(unsub)
	require.NoError(t, os.WriteFile(configPath, []byte("code:\n  python:\n    roots: [new]\n"), 0600))
	w.retainWatchEvents(map[string]cache.DirtyKind{".rhizome/config.yml": cache.DirtyModified}, false)
	handle := w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	require.NoError(t, handle.Err())
	require.NoError(t, os.WriteFile(filepath.Join(root, "new/mod.py"), []byte("def added():\n    return 1\n"), 0600))
	require.Eventually(t, func() bool { return w.events("new/mod.py") > 0 }, 5*time.Second, time.Millisecond)
	handle = w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	require.NoError(t, handle.Err())
	meta, err := w.intelStore.SymbolMetaForFQNs(t.Context(), "py", []string{"mod.added"})
	require.NoError(t, err)
	require.Contains(t, meta, "mod.added")
	require.NoError(t, os.WriteFile(filepath.Join(root, "old/mod.py"), []byte("def retired():\n    pass\n"), 0600))
	for found := false; !found; {
		select {
		case event := <-events:
			if event.RelPath == "old/mod.py" {
				// The native watch can still report events shared with other roots.
				require.Empty(t, event.Root, "the removed root must leave the registry")
				found = true
			}
		case <-time.After(5 * time.Second):
			t.Fatal("removed-root observation did not arrive")
		}
	}
}

func TestLiveParserRebuildRespectsRequestedCapabilities(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	path := filepath.Join(root, ".rhizome/config.yml")
	require.NoError(t, os.WriteFile(path, []byte("code:\n  python:\n    roots: [src]\n"), 0600))
	rt, w := parserConfigurationTestWatcher(t, root)
	rt.requirements = RequireRuntimeCapabilities(RuntimeCapabilityCodeIndex)
	require.NoError(t, os.WriteFile(path, []byte("code:\n  python: {}\n"), 0600))
	require.NoError(t, w.reloadOwnershipSelection())
	require.Same(t, rt.CodeAnchorService(), w.noteSvc)
	require.False(t, w.noteSvc.HasIndexer(codeanchor.LangPy))
}

func TestLiveForcedCodeReparseReusesEmbeddings(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src/util.py"), []byte("def answer():\n    return 42\n"), 0600))
	path := filepath.Join(root, ".rhizome/config.yml")
	require.NoError(t, os.WriteFile(path, []byte("code:\n  python:\n    roots: [src]\n"), 0600))
	_, w := parserConfigurationTestWatcher(t, root)
	provider := &recordingEmbeddingProvider{}
	index, err := codeembsqlite.OpenCode(t.Context(), codeembsqlite.File(filepath.Join(root, "code.sqlite")), codeembsqlite.Options{Access: codeembsqlite.WriteIndex, Dimensions: provider.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, index.Close()) })
	w.codeSyncer = &embedindex.Syncer{Index: index, Provider: provider, Intel: w.intelStore, Root: root}
	w.codeSemanticStore = true
	drain := func() {
		t.Helper()
		handle := w.processOwnershipBatch()
		watcherJobTerminal(t, handle)
		require.NoError(t, handle.Err())
		tickets, err := w.intelStore.PendingDerivedWork(t.Context(), time.Now(), 32)
		require.NoError(t, err)
		for _, ticket := range tickets {
			if ticket.Kind == codeanchor.DerivedCode {
				require.NoError(t, (&derivedScheduler{watcher: w, ctx: t.Context()}).execute(ticket))
			}
		}
	}
	w.retainWatchEvents(nil, true)
	drain()
	require.NotEmpty(t, provider.texts)
	provider.texts = nil
	var changed []string
	w.publishEvent = func(kind string, data any) {
		if kind == appruntime.GlobalEventNodeChanged {
			changed = append(changed, data.(map[string]any)["paths"].([]string)...)
		}
	}
	require.NoError(t, os.WriteFile(path, []byte("code:\n  tsParseTimeout: 2s\n  python:\n    roots: [src]\n"), 0600))
	w.retainWatchEvents(map[string]cache.DirtyKind{".rhizome/config.yml": cache.DirtyModified}, false)
	drain()
	require.Empty(t, provider.texts, "unchanged semantic inputs reuse their vectors after a forced parse")
	require.Equal(t, []string{"src/util.py"}, changed, "the unchanged code was actually reparsed")
	_, last := w.health.snapshot()
	require.NotNil(t, last)
	require.Equal(t, "completed", last.Status)
}

func TestLiveSelectionChangeStampsAppliedScopeHash(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	path := filepath.Join(root, ".rhizome/config.yml")
	require.NoError(t, os.WriteFile(path, []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0600))
	_, w := parserConfigurationTestWatcher(t, root)
	store := w.intelStore
	w.retainWatchEvents(nil, true)
	handle := w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	require.NoError(t, handle.Err())
	previous, present, err := store.GetScopeConfigHash(context.Background())
	require.NoError(t, err)
	require.True(t, present)
	require.NoError(t, os.WriteFile(path, []byte("notes:\n  includes: [\"**/*.md\"]\n  excludes: [private/**]\n"), 0600))
	w.retainWatchEvents(map[string]cache.DirtyKind{".rhizome/config.yml": cache.DirtyModified}, false)
	w.afterPreparedOwnershipCommit = func(context.Context) error { return context.Canceled }
	handle = w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	require.ErrorIs(t, handle.Err(), context.Canceled)
	retained, present, err := store.GetScopeConfigHash(context.Background())
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, previous, retained, "a failed convergence cannot certify the new scope")
	w.afterPreparedOwnershipCommit = nil
	handle = w.processOwnershipBatch()
	watcherJobTerminal(t, handle)
	require.NoError(t, handle.Err())
	hash, present, err := store.GetScopeConfigHash(context.Background())
	require.NoError(t, err)
	require.True(t, present)
	require.NotEqual(t, previous, hash)
	require.Equal(t, w.configuration.Local.ScopeConfigHash(), hash)
}
