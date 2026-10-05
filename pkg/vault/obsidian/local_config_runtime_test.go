package obsidian

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalRuntimeConfigRoundTrips(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("notes: {}\nruntime:\n  autostart: false\n  idleTimeout: 15m\n"), 0o644))

	cfg, err := LoadLocalConfig(dir)
	require.NoError(t, err)
	require.Empty(t, cfg.Warnings, "runtime is a recognized block")
	require.NotNil(t, cfg.Runtime)
	require.NotNil(t, cfg.Runtime.Autostart)
	assert.False(t, *cfg.Runtime.Autostart)
	assert.Equal(t, "15m", cfg.Runtime.IdleTimeout)
	require.NoError(t, SaveLocalConfig(dir, *cfg))
	reloaded, err := LoadLocalConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, reloaded.Runtime)
	require.NotNil(t, reloaded.Runtime.Autostart)
	assert.False(t, *reloaded.Runtime.Autostart)
	assert.Equal(t, "15m", reloaded.Runtime.IdleTimeout)
}
