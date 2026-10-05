package obsidian

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/stretchr/testify/require"
)

func TestLocalConfigPublicationWithOpenReaders(t *testing.T) {
	for _, tc := range []struct{ name, root string }{
		{"short path", t.TempDir()},
		{"long path", filepath.Join(t.TempDir(), strings.Repeat("nested", 20), strings.Repeat("vault", 24))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath := rhizomeConfigPath(tc.root)
			workflowPath := localWorkflowPath(tc.root)
			require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
			oldConfig := []byte("# user config\nrhizome:\n  version: v0.50.1\nfutureConfig: retained\n")
			oldWorkflow := []byte("# user workflows\ntemplates: [spec-driven]\nfutureWorkflow: retained\n")
			require.NoError(t, os.WriteFile(configPath, oldConfig, 0o600))
			require.NoError(t, os.WriteFile(workflowPath, oldWorkflow, 0o600))
			cfg, err := LoadLocalConfig(tc.root)
			require.NoError(t, err)

			configReader, err := fileio.OpenRead(configPath)
			require.NoError(t, err)
			t.Cleanup(func() { _ = configReader.Close() })
			workflowReader, err := fileio.OpenRead(workflowPath)
			require.NoError(t, err)
			t.Cleanup(func() { _ = workflowReader.Close() })
			cfg.Rhizome.Version = "v0.50.2"
			cfg.WorkflowTemplateManagement.UpdatePolicy.Docs = "never"
			require.NoError(t, SaveLocalConfig(tc.root, *cfg), "cooperative readers must allow config publication")

			for _, snapshot := range []struct {
				path   string
				reader *os.File
				old    []byte
				kept   string
			}{
				{configPath, configReader, oldConfig, "futureConfig: retained"},
				{workflowPath, workflowReader, oldWorkflow, "futureWorkflow: retained"},
			} {
				old, err := io.ReadAll(snapshot.reader)
				require.NoError(t, err)
				require.Equal(t, snapshot.old, old, "existing readers retain the complete old snapshot")
				current, err := fileio.ReadFile(snapshot.path)
				require.NoError(t, err)
				require.NotEqual(t, snapshot.old, current)
				require.Contains(t, string(current), snapshot.kept)
				require.Contains(t, string(current), "# user")
			}
			current, err := LoadLocalConfig(tc.root)
			require.NoError(t, err)
			require.Equal(t, "v0.50.2", current.Rhizome.Version)
			require.Equal(t, "never", current.WorkflowTemplateManagement.UpdatePolicy.Docs)
			entries, err := os.ReadDir(filepath.Dir(configPath))
			require.NoError(t, err)
			require.Len(t, entries, 2, "successful publication leaves only config and workflow files")
		})
	}
}

func TestLocalConfigPublicationBlockedByNoncooperatingReader(t *testing.T) {
	root := t.TempDir()
	path := rhizomeConfigPath(root)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	old := []byte("# user config\nrhizome:\n  version: v0.50.1\nfutureConfig: retained\n")
	require.NoError(t, os.WriteFile(path, old, 0o600))
	cfg, err := LoadLocalConfig(root)
	require.NoError(t, err)
	reader, err := os.Open(path) // Ordinary Windows readers do not share deletion.
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })
	cfg.Rhizome.Version = "v0.50.2"
	err = SaveLocalConfig(root, *cfg)
	require.Error(t, err, "a noncooperating reader must report the denied replacement")
	var renameErr *os.LinkError
	require.ErrorAs(t, err, &renameErr)
	require.Equal(t, "rename", renameErr.Op)
	require.Equal(t, path, renameErr.New)
	unchanged, err := fileio.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, old, unchanged, "failed replacement retains the published config")
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "failed replacement must not leak a temporary sibling")
	require.Equal(t, filepath.Base(path), entries[0].Name())

	require.NoError(t, reader.Close())
	require.NoError(t, SaveLocalConfig(root, *cfg), "a new save succeeds after the blocking reader closes")
	current, err := LoadLocalConfig(root)
	require.NoError(t, err)
	require.Equal(t, "v0.50.2", current.Rhizome.Version)
}
