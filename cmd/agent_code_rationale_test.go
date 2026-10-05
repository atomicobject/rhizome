package cmd

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAgentCodeRationaleCommandReturnsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"pkg/a.go":   "package main\n",
		"pkg/b.go":   "package main\n",
		"other/c.go": "package main\n",
	})

	store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer store.Close()

	require.NoError(t, store.ReplaceRationaleForPath(context.Background(), "pkg/a.go", []codeanchor.Rationale{
		{ID: "todo-a", Path: "pkg/a.go", Kind: codeanchor.RationaleTodo, Content: "TODO: keep this", StartLine: 2, EndLine: 2, Fingerprint: "fp-a"},
	}))
	require.NoError(t, store.ReplaceRationaleForPath(context.Background(), "pkg/b.go", []codeanchor.Rationale{
		{ID: "note-b", Path: "pkg/b.go", Kind: codeanchor.RationaleNote, Content: "NOTE: ignore this", StartLine: 3, EndLine: 3, Fingerprint: "fp-b"},
	}))
	require.NoError(t, store.ReplaceRationaleForPath(context.Background(), "other/c.go", []codeanchor.Rationale{
		{ID: "todo-c", Path: "other/c.go", Kind: codeanchor.RationaleTodo, Content: "TODO: other dir", StartLine: 4, EndLine: 4, Fingerprint: "fp-c"},
	}))
	localCfg, err := obsidian.LoadLocalConfig(vault.path)
	require.NoError(t, err)
	require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(context.Background(), localCfg.ScopeConfigHash()))

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "code-rationale", "--vault", vault.name, "--path", "pkg", "--kind", "todo"})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp agentCodeRationaleResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.Equal(t, 1, resp.Count)
	require.Len(t, resp.Rationale, 1)
	require.Equal(t, "pkg/a.go", resp.Rationale[0].Path)
	require.Equal(t, "todo", resp.Rationale[0].Kind)
	require.Equal(t, "TODO: keep this", resp.Rationale[0].Content)
}

func TestAgentCodeRationaleCommandPreservesStaleIndexRemediation(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"pkg/a.go": "package main\n"})
	store, err := semdb.Open(filepath.Join(vault.path, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.SetIndexerVersion(context.Background(), codeanchor.IndexerVersion))
	require.NoError(t, store.SetScopeConfigHash(context.Background(), "stale-scope"))
	require.NoError(t, store.Close())

	vaultName = vault.name
	_, stderr, err := runRootCLI(t, nil, []string{"agent", "code-rationale", "--vault", vault.name})
	require.Error(t, err)
	var envelope map[string]string
	require.NoError(t, json.Unmarshal([]byte(stderr), &envelope))
	require.Equal(t, "indexed-context-stale", envelope["code"])
	require.Equal(t, "rzm index", envelope["remediation"])
}
