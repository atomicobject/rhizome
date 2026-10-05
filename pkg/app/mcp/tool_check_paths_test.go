package mcp

import (
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPathsUsesNestedRulesAndRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.tmp\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested/.gitignore"), []byte("!keep.tmp\nsecret.md\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ignore"), []byte("private/\n"), 0644))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "outside")))
	inputs := []string{"ok.go", "x.tmp", "nested/keep.tmp", "nested/secret.md", "private/a.md", "excluded.md", "../escape", "outside/new.md"}
	entries := []map[string]any{}
	for _, p := range inputs {
		entries = append(entries, map[string]any{"path": p, "isDir": false})
	}
	raw, err := json.Marshal(map[string]any{"paths": entries})
	require.NoError(t, err)
	result, err := CheckPathsTool(Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Excludes: []string{"excluded.md"}}})(t.Context(), evaluationCall(t, string(raw)))
	require.NoError(t, err)
	require.False(t, result.IsError)
	var payload struct {
		Paths []PathCheck `json:"paths"`
	}
	decodeToolResult(t, result, &payload)
	require.Len(t, payload.Paths, len(inputs))
	for i, status := range []string{"allowed", "ignored", "allowed", "ignored", "ignored", "ignored", "invalid", "invalid"} {
		require.Equal(t, inputs[i], payload.Paths[i].Input)
		require.Equal(t, status, payload.Paths[i].Status, inputs[i])
	}
	require.NotNil(t, payload.Paths[1].Rule)
}
