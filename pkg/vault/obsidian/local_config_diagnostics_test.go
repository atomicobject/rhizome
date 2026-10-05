package obsidian

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalDiagnosticsConfigRoundTrips(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("diagnostics:\n  enabled: false\n  retentionDays: 5\n  maxBytes: 8388608\n  level: warn\n"), 0o644))
	cfg, err := LoadLocalConfig(root)
	require.NoError(t, err)
	require.Empty(t, cfg.Warnings)
	require.NotNil(t, cfg.Diagnostics)
	require.False(t, *cfg.Diagnostics.Enabled)
	require.Equal(t, 5, *cfg.Diagnostics.RetentionDays)
	require.Equal(t, int64(8388608), *cfg.Diagnostics.MaxBytes)
	require.Equal(t, "warn", cfg.Diagnostics.Level)
	require.NoError(t, SaveLocalConfig(root, *cfg))
	reloaded, err := LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, cfg.Diagnostics, reloaded.Diagnostics)
}
