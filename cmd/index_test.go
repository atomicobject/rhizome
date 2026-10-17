package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func acquireRebuildTestLock(t *testing.T, vault string) string {
	t.Helper()
	lockPath := repoconfig.IndexLockPath(vault)
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { require.NoError(t, release()) })
	return lockPath
}

func TestRunResolvedIndexCommand_ComposesNoteMetadataBeforeSideEffects(t *testing.T) {
	commandOptions := newIndexOptionsForTest(t)

	vault := t.TempDir()
	dbPath := repoconfig.UnifiedIndexPath(vault, "")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	require.NoError(t, os.WriteFile(dbPath, []byte("preserve me"), 0o644))

	previousRebuild := commandOptions.indexRebuild
	previousStatus := commandOptions.indexStatus
	previousExplainPath := commandOptions.indexExplainPath
	t.Cleanup(func() {
		commandOptions.indexRebuild = previousRebuild
		commandOptions.indexStatus = previousStatus
		commandOptions.indexExplainPath = previousExplainPath
	})
	commandOptions.indexRebuild = true
	commandOptions.indexStatus = false
	commandOptions.indexExplainPath = ""

	wantErr := errors.New("note metadata composition failed")
	composed := false
	recorder, openErr := diagnostics.Open(vault, diagnostics.Options{Stderr: io.Discard})
	require.NoError(t, openErr)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	cmd := &cobra.Command{}
	cmd.SetContext(diagnostics.WithRecorder(context.Background(), recorder))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := commandOptions.runResolvedIndexCommand(cmd, obsidian.VaultDefinition{Path: vault}, func() (notemeta.Indexer, error) {
		composed = true
		_, lockErr := os.Stat(repoconfig.IndexLockPath(vault))
		require.ErrorIs(t, lockErr, os.ErrNotExist, "note metadata must be composed before acquiring the index lock")
		contents, readErr := os.ReadFile(dbPath)
		require.NoError(t, readErr)
		require.Equal(t, "preserve me", string(contents), "note metadata must be composed before rebuild preparation")
		return notemeta.Indexer{}, wantErr
	})

	require.True(t, composed)
	require.ErrorIs(t, err, wantErr)
	contents, readErr := os.ReadFile(dbPath)
	require.NoError(t, readErr)
	require.Equal(t, "preserve me", string(contents))
	report, reportErr := diagnostics.ReadLatest(vault, "index")
	require.NoError(t, reportErr)
	require.Equal(t, "rebuild", report.Trigger)
	require.Equal(t, "error", report.Status)
	require.Zero(t, report.LockWaitMS)
	require.Zero(t, report.ExecutionMS)
}

func TestPrepareIndexRebuild_ReplacesCorruptIndex(t *testing.T) {
	t.Parallel()

	vault := t.TempDir()
	dbPath := filepath.Join(vault, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	require.NoError(t, os.WriteFile(dbPath, []byte("not a sqlite db"), 0o644))

	cfg := repoconfig.LocalConfig{
		IndexPath: dbPath,
		NoteEmbeddings: &embeddings.Config{
			Enabled:  true,
			Provider: "openai",
			Model:    "text",
		},
		Code: repoconfig.LocalCodeConfig{
			Enabled: true,
			Go:      &repoconfig.LocalCodeLangConfig{Roots: []string{"."}},
		},
	}
	require.NoError(t, repoconfig.SaveLocalConfig(vault, cfg))
	lockPath := acquireRebuildTestLock(t, vault)

	require.NoError(t, indexing.PrepareFreshRebuild(vault, lockPath))

	_, err := os.Stat(dbPath)
	require.ErrorIs(t, err, os.ErrNotExist, "explicit rebuild must clobber a corrupt index")
}

func TestPrepareIndexRebuild_FullRebuildClobbersUnifiedDB(t *testing.T) {
	t.Parallel()

	vault := t.TempDir()
	dbPath := filepath.Join(vault, ".rhizome", "db.sqlite")

	cfg := repoconfig.LocalConfig{
		IndexPath: dbPath,
		NoteEmbeddings: &embeddings.Config{
			Enabled:  true,
			Provider: "openai",
			Model:    "text",
		},
		Code: repoconfig.LocalCodeConfig{
			Enabled: true,
			Go:      &repoconfig.LocalCodeLangConfig{Roots: []string{"."}},
		},
	}
	require.NoError(t, repoconfig.SaveLocalConfig(vault, cfg))
	lockPath := acquireRebuildTestLock(t, vault)

	codeStore, err := semdb.Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, codeStore.Close())

	embStore, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(dbPath), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 3})
	require.NoError(t, err)
	require.NoError(t, embStore.Close())

	require.NoError(t, indexing.PrepareFreshRebuild(vault, lockPath))
	require.NoError(t, err)

	_, err = os.Stat(dbPath)
	require.ErrorIs(t, err, os.ErrNotExist, "explicit full rebuild must clobber the unified DB")
}

func TestIndexHelpOmitsEmbeddingOverrideFlags(t *testing.T) {

	stdout, stderr, err := runCLI(t, []string{"index", "--help"}, "")
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(stderr))
	require.Contains(t, stdout, "Clobber and recreate indexes from scratch")
	require.NotContains(t, stdout, "--code")
	require.NotContains(t, stdout, "--semantic")
	require.NotContains(t, stdout, "--provider")
	require.NotContains(t, stdout, "--model")
	require.NotContains(t, stdout, "--api-key")
}

func TestIndexRejectsRetiredModeFlagsBeforeIndexing(t *testing.T) {

	for _, flag := range []string{"--code", "--semantic"} {
		t.Run(flag, func(t *testing.T) {
			vault := t.TempDir()
			_, _, err := runCLI(t, []string{"index", flag, "--vault", vault}, "")
			require.ErrorContains(t, err, "unknown flag: "+flag)
			_, statErr := os.Stat(repoconfig.UnifiedIndexPath(vault, ""))
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}
