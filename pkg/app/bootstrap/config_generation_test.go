package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// Every configuration publication, from a config.yml or an ignore-file reload,
// changes the snapshot's config generation (SPEC-0136 R4).
func TestConfigGenerationChangesOnEveryConfigurationPublication(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	configPath := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	definition, err := repoconfig.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, definition)
	defer store.Close()
	defer cacheService.Close()
	rt := &LiveRuntime{VaultDef: definition, VaultPath: root, noteFormats: w.noteRuntime}
	w.onConfigurationChanged = rt.publishOwnershipConfiguration
	rt.cache.Store(cacheService)

	seen := map[string]bool{rt.Snapshot().ConfigGeneration: true}
	publish := func(step string) {
		require.NoError(t, w.reloadOwnershipSelection(), step)
		generation := rt.Snapshot().ConfigGeneration
		require.NotEmpty(t, generation, step)
		require.False(t, seen[generation], "%s must change the config generation", step)
		seen[generation] = true
	}

	publish("first publication")
	require.NoError(t, os.WriteFile(configPath, []byte("notes:\n  includes: [\"**/*.md\"]\n  excludes: [\"private/**\"]\n"), 0o644))
	publish("configuration change")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ignore"), []byte("drafts/\n"), 0o644))
	publish("ignore change")
	publish("unchanged reload")
}

func TestConfigGenerationFollowsOwnershipWithoutPublication(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	rt := &LiveRuntime{VaultPath: root}
	before := rt.Snapshot().ConfigGeneration
	path := filepath.Join(root, ".rhizome", "source-ownership.yml")
	require.NoError(t, os.WriteFile(path, []byte("sources: []\n"), 0o644))
	require.NotEqual(t, before, rt.Snapshot().ConfigGeneration)
	require.NoError(t, os.Remove(path))
	require.Equal(t, before, rt.Snapshot().ConfigGeneration)
}
