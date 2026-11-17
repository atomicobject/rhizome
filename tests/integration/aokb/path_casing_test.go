//go:build integration

package aokb

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestAOKBIndexAcceptsFilesystemCaseAliases(t *testing.T) {
	kb := &aoKB{t: t, root: filepath.Join(t.TempDir(), "vault"), home: t.TempDir()}
	require.NoError(t, os.CopyFS(kb.root, os.DirFS(filepath.Join(fixture.RepoRoot(t), "testdata/ao-kb-legacy"))))
	original := filepath.Join(kb.root, "MOCs")
	alias := filepath.Join(kb.root, "mocs")
	originalInfo, err := os.Stat(original)
	require.NoError(t, err)
	aliasInfo, err := os.Stat(alias)
	if err != nil || !os.SameFile(originalInfo, aliasInfo) {
		t.Skip("fixture requires a filesystem that resolves case aliases")
	}
	require.NoError(t, os.Rename(original, filepath.Join(kb.root, "temporary-mocs")))
	require.NoError(t, os.Rename(filepath.Join(kb.root, "temporary-mocs"), alias))
	kb.write(".rhizome/config.yml", "rhizome:\n  binaryManager: external\nnotes:\n  includes: [\"**/*.md\", \".kb/curation/*.md\"]\n")
	output, err := kb.rzm("init", "--add-addon", "ao-kb", "--agents", "none", "--search", "off")
	require.NoError(t, err, output)
	t.Cleanup(func() { fixture.StopRuntime(t, kb.root) })
	for _, operation := range []string{"refresh-bindings", "index"} {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		args := []string{".rhizome/modules/ao-kb/bindings/cli.mjs", operation}
		if operation == "refresh-bindings" {
			args = append(args, "--single")
		}
		command := exec.CommandContext(ctx, "node", args...)
		command.Dir = kb.root
		command.Env = append(kb.env(), "RZM="+rzmBinary(t))
		output, err := command.CombinedOutput()
		cancel()
		require.NoError(t, err, "%s: %s", operation, output)
		var report map[string]any
		require.NoError(t, json.Unmarshal(output, &report), "%s", output)
		require.Equal(t, true, report["bindingsReady"], "%s", output)
		if operation == "index" {
			require.Equal(t, true, report["indexReady"], "%s", output)
		}
	}
	require.DirExists(t, alias)
	entries, err := os.ReadDir(kb.root)
	require.NoError(t, err)
	for _, entry := range entries {
		require.NotEqual(t, "MOCs", entry.Name(), "setup must preserve the actual directory spelling")
	}
}
