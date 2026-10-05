//go:build unix

package validationproduct

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunLiveDoesNotReadUnselectedRecipeSources(t *testing.T) {
	root := writeProjectionVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: ['notes/**/*.md']\n"), 0o644))
	path := filepath.Join(root, ".agents", "skills", "example", "references", "query-recipes.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, syscall.Mkfifo(path, 0o600))
	hold, err := os.OpenFile(path, os.O_RDWR, 0o600)
	require.NoError(t, err)
	indexer := testProjectionNoteMetadata(t)
	finished := make(chan struct{})
	var run AuthoritativeRun
	var runErr error
	go func() {
		run, runErr = RunLive(context.Background(), indexer, obsidian.VaultDefinition{Path: root}, 100)
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
		require.True(t, run.Result.OK)
		require.Zero(t, run.Result.IssueCount)
		for _, outcome := range run.Result.Outcomes {
			require.NotEqual(t, "query-recipes", outcome.Check)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unselected live validation waited for a recipe source read")
	}
}
