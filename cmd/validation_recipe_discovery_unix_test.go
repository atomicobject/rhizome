//go:build unix

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNamedValidationDoesNotReadUnselectedRecipeSources(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/config.yml": "notes:\n  includes: ['people/**/*.md']\nvalidation:\n  default:\n    add: [query-recipes]\n",
		"people/alice.md":     "# Alice\n",
	})
	path := filepath.Join(vault.path, ".agents", "skills", "example", "references", "query-recipes.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, syscall.Mkfifo(path, 0o600))
	hold, err := os.OpenFile(path, os.O_RDWR, 0o600)
	require.NoError(t, err)
	command := newValidateProductCmdWithRunner(newProductionValidationRunner())
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"broken-links", "--vault", vault.name})
	finished := make(chan struct{})
	var runErr error
	go func() {
		runErr = command.Execute()
		close(finished)
	}()
	t.Cleanup(func() {
		_ = hold.Close()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("validation did not finish after the recipe reader was released")
		}
	})
	select {
	case <-finished:
		require.NoError(t, runErr)
		require.Empty(t, stderr.String())
		require.Contains(t, stdout.String(), "no broken links")
	case <-time.After(2 * time.Second):
		t.Fatal("unrelated named validation waited for a recipe source read")
	}
}
