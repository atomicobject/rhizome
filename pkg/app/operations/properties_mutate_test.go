package operations

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestSetPropertyOnFilesWithWorkers(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	dir := t.TempDir()
	notePath := filepath.Join(dir, "note.md")
	require.NoError(t, os.WriteFile(notePath, []byte("# Note"), 0644))

	vault := repoconfig.Vault{Env: env, Name: dir}
	note := obsidian.Note{}

	summary, err := SetPropertyOnFilesWithWorkers(t.Context(), &vault, &note, "status", "open", []string{"note.md"}, false, false, 1)
	require.NoError(t, err)
	require.Equal(t, 1, summary.NotesTouched)
	require.Equal(t, 1, summary.PropertyChanges["status"])

	data, err := os.ReadFile(notePath)
	require.NoError(t, err)
	require.Contains(t, string(data), "status: open")
}

func TestDeletePropertiesAcrossVault(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	dir := t.TempDir()
	notePath := filepath.Join(dir, "note.md")
	require.NoError(t, os.WriteFile(notePath, []byte("---\nstatus: open\n---\nbody"), 0644))

	vault := repoconfig.Vault{Env: env, Name: dir}
	note := obsidian.Note{}

	summary, err := DeletePropertiesWithWorkers(t.Context(), &vault, &note, []string{"status"}, nil, false, 1)
	require.NoError(t, err)
	require.Equal(t, 1, summary.NotesTouched)
	require.Equal(t, 1, summary.PropertyChanges["status"])

	data, err := os.ReadFile(notePath)
	require.NoError(t, err)
	require.NotContains(t, string(data), "status")
}

func TestRenamePropertiesMerge(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	dir := t.TempDir()
	notePath := filepath.Join(dir, "note.md")
	require.NoError(t, os.WriteFile(notePath, []byte("---\nold: a\nnew:\n  - b\n---\nbody"), 0644))

	vault := repoconfig.Vault{Env: env, Name: dir}
	note := obsidian.Note{}

	summary, err := RenamePropertiesWithWorkers(t.Context(), &vault, &note, []string{"old"}, "new", true, nil, false, 1)
	require.NoError(t, err)
	require.Equal(t, 1, summary.NotesTouched)
	require.Equal(t, 1, summary.PropertyChanges["old"])
	require.Equal(t, 1, summary.PropertyChanges["new"])

	data, err := os.ReadFile(notePath)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "new:")
	require.Contains(t, content, "- a")
	require.Contains(t, content, "- b")
	require.NotContains(t, content, "old:")
}
