package init

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestInitPreservesDiagnosticsConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".rhizome", "config.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("rhizome:\n  version: v0.9.0\ndiagnostics:\n  enabled: false\n  retentionDays: 5\n  maxBytes: 8388608\n  level: warn\n"), 0o644))
	layout := DetectedLayout{ProjectRoot: root}
	require.NoError(t, loadExistingConfig(&layout))
	require.Empty(t, layout.ExistingLocal.Warnings)
	require.NotNil(t, layout.ExistingLocal.Diagnostics)
	cfg := layout.ExistingLocal
	cfg.Rhizome.Version = "v1.2.3"
	require.NoError(t, writeConfigPatched(root, cfg, changeSet{sectionRhizome: true}))
	reloaded, err := obsidian.LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, cfg.Diagnostics, reloaded.Diagnostics)
	newRoot := t.TempDir()
	require.NoError(t, writeConfigNew(newRoot, cfg))
	fresh, err := obsidian.LoadLocalConfig(newRoot)
	require.NoError(t, err)
	require.Equal(t, cfg.Diagnostics, fresh.Diagnostics)
}
