package obsidian

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSaveLocalConfigPreservesUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".rhizome", "config.yml")
	workflowPath := filepath.Join(dir, ".rhizome", "workflows.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte("# user configuration\nrhizome:\n  version: v0.50.1\nfutureConfig: retained\n"), 0o640))
	require.NoError(t, os.WriteFile(workflowPath, []byte("# user workflows\ntemplates: [spec-driven]\nfutureWorkflow: retained\n"), 0o640))
	cfg, err := LoadLocalConfig(dir)
	require.NoError(t, err)
	require.Equal(t, "v0.50.1", cfg.Rhizome.Version)
	require.Empty(t, cfg.Rhizome.BinaryPath)
	// The first save may normalize formatting; subsequent saves must leave files alone.
	require.NoError(t, SaveLocalConfig(dir, *cfg))
	before := make(map[string]os.FileInfo)
	contents := make(map[string][]byte)
	for _, path := range []string{configPath, workflowPath} {
		require.NoError(t, os.Chtimes(path, time.Unix(1000000000, 0), time.Unix(1000000000, 0)))
		before[path], err = os.Stat(path)
		require.NoError(t, err)
		contents[path], err = os.ReadFile(path)
		require.NoError(t, err)
	}
	require.Contains(t, string(contents[configPath]), "# user configuration")
	require.Contains(t, string(contents[configPath]), "futureConfig: retained")
	require.Contains(t, string(contents[workflowPath]), "# user workflows")
	require.Contains(t, string(contents[workflowPath]), "futureWorkflow: retained")
	require.NoError(t, SaveLocalConfig(dir, *cfg))
	reloaded, err := LoadLocalConfig(dir)
	require.NoError(t, err)
	require.Equal(t, "v0.50.1", reloaded.Rhizome.Version)
	require.Empty(t, reloaded.Rhizome.BinaryPath)
	for _, path := range []string{configPath, workflowPath} {
		after, err := os.Stat(path)
		require.NoError(t, err)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, contents[path], data)
		require.True(t, os.SameFile(before[path], after), "unchanged file replaced: %s", path)
		require.Equal(t, before[path].ModTime(), after.ModTime())
		require.Equal(t, before[path].Mode(), after.Mode())
	}

	// Skipping the main config must not skip a real workflow update.
	cfg.WorkflowTemplateManagement.UpdatePolicy.Docs = "never"
	require.NoError(t, SaveLocalConfig(dir, *cfg))
	afterConfig, err := os.Stat(configPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(before[configPath], afterConfig))
	require.Equal(t, before[configPath].ModTime(), afterConfig.ModTime())
	workflow, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	require.Contains(t, string(workflow), "docs: never")
	require.Contains(t, string(workflow), "futureWorkflow: retained")
	afterWorkflow, err := os.Stat(workflowPath)
	require.NoError(t, err)
	require.Equal(t, before[workflowPath].Mode(), afterWorkflow.Mode())
	require.False(t, before[workflowPath].ModTime().Equal(afterWorkflow.ModTime()))
}
