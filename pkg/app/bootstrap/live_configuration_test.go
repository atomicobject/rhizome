package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/mcp"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestLiveRuntimeStartupHonorsCapturedConfiguration(t *testing.T) {
	for _, later := range []string{"indexPath: later.sqlite\nbudgetChars: 22222\nnotes:\n  excludes: [later/**]\n", "broken: [yaml"} {
		t.Run(later, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
			path := filepath.Join(root, ".rhizome", "config.yml")
			require.NoError(t, os.WriteFile(path, []byte("indexPath: captured.sqlite\nbudgetChars: 11111\nnotes:\n  excludes: [captured/**]\n"), 0644))
			captured, err := repoconfig.Load(root)
			require.NoError(t, err)
			store, err := semdb.Open(captured.Code.IndexPath)
			require.NoError(t, err)
			require.NoError(t, store.Close())
			require.NoError(t, os.WriteFile(path, []byte(later), 0644))
			ctx := vaultconfig.WithEnv(context.Background(), vaultconfig.Env{WorkingDir: root})
			rt, err := NewLiveRuntime(ctx, LiveOptions{VaultName: root, Config: &captured, DisableLeaderWork: true, DisableWatchHub: true, SkipCacheWarmup: true, SessionOnlyExisting: true, Requirements: RequireRuntimeCapabilities(RuntimeCapabilitySearch)})
			require.NoError(t, err)
			t.Cleanup(func() { _ = rt.Close() })
			select {
			case <-rt.sessionReady:
			case <-time.After(5 * time.Second):
				t.Fatal("session resource did not finish opening")
			}
			require.NotNil(t, rt.SessionDedupeStore(), "runtime must open the index named by its captured snapshot")
			code := rt.Snapshot().Configuration.Code
			require.Equal(t, filepath.Join(captured.VaultPath, "captured.sqlite"), code.IndexPath)
			view := rt.Snapshot()
			require.Equal(t, []string{"captured/**"}, view.VaultDefinition.Excludes)
			require.Equal(t, 11111, view.Configuration.BudgetChars)
			require.Equal(t, filepath.Join(captured.VaultPath, "captured.sqlite"), view.Configuration.Code.IndexPath)
			_, err = os.Stat(filepath.Join(root, "later.sqlite"))
			require.True(t, os.IsNotExist(err))
		})
	}
}

func TestLiveConfigurationReloadPublishesSafeChangesAndPreservesStateOnFailure(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "private"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "private", "note.md"), []byte("# A note\n"), 0644))
	path := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.WriteFile(path, []byte("budgetChars: 11111\nnotes:\n  excludes: [private/**]\n"), 0644))
	captured, err := repoconfig.Load(root)
	require.NoError(t, err)
	definition := repoconfig.LocalConfigToDefinition(root, captured.Local)
	watcher, service, store := newLiveOwnershipTestWatcher(t, root, definition)
	t.Cleanup(func() { _ = service.Close(); _ = store.Close() })
	rt := &LiveRuntime{VaultPath: root, VaultDef: definition, noteFormats: watcher.noteRuntime}
	rt.resolvedConfig.Store(&captured)
	rt.ownershipVaultDef.Store(&definition)
	rt.cache.Store(service)
	watcher.configuration = &captured
	watcher.onConfigurationChanged = rt.publishOwnershipConfiguration
	require.NoError(t, watcher.reloadOwnershipSelection())
	require.False(t, rt.NotePathOwned("private/note.md"))
	require.NoError(t, os.WriteFile(path, []byte("budgetChars: 33333\nfileContext:\n  docPatterns: [GUIDE.md]\nnotes:\n  includes: [\"**/*.md\"]\n"), 0644))
	require.NoError(t, watcher.reloadOwnershipSelection())
	view := rt.Snapshot()
	require.Equal(t, 33333, view.Configuration.BudgetChars)
	require.Equal(t, []string{"GUIDE.md"}, view.Configuration.FileContext.DocPatterns)
	require.Empty(t, view.VaultDefinition.Excludes)
	require.True(t, rt.NotePathOwned("private/note.md"))
	// Search's actual cached reader and MCP budget consumer use the same new pair.
	var notes []string
	// Selection changes schedule a cache recrawl; read through the real adapter until it publishes.
	require.Eventually(t, func() bool {
		notes, err = view.NoteReader.GetNotesList(view.VaultDefinition)
		return err == nil && len(notes) == 1 && notes[0] == "private/note.md"
	}, 3*time.Second, 5*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, []string{"private/note.md"}, notes)
	require.Equal(t, 33333, (mcp.Config{RepositoryConfig: view.Configuration}).BudgetChars())
	for _, invalid := range []string{"indexPath: replacement.sqlite\nnotes:\n  excludes: [private/**]\n", "noteEmbeddings:\n  enabled: true\n  provider: voyage\n", "broken: [yaml"} {
		require.NoError(t, os.WriteFile(path, []byte(invalid), 0644))
		require.Error(t, watcher.reloadOwnershipSelection())
		unchanged := rt.Snapshot()
		require.Equal(t, 33333, unchanged.Configuration.BudgetChars)
		require.Equal(t, []string{"GUIDE.md"}, unchanged.Configuration.FileContext.DocPatterns)
		require.Empty(t, unchanged.VaultDefinition.Excludes)
		require.True(t, rt.NotePathOwned("private/note.md"))
		require.True(t, service.AdmitsNotePath("private/note.md"))
	}
}
