//go:build !windows

package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIngestNotesForVault_PreservesNotesOnDiscoveryFailure(t *testing.T) {
	for _, kind := range []string{"file", "subtree", "root"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			locked := filepath.Join(root, "locked")
			require.NoError(t, os.Mkdir(locked, 0o755))
			kept := filepath.Join(locked, "kept.md")
			require.NoError(t, os.WriteFile(kept, []byte("# Kept\n"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "visible.md"), []byte("# Visible\n"), 0o644))
			store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			service := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))
			vault := obsidian.VaultDefinition{Path: root}
			run := func() (NoteIngestResult, error) {
				return IngestNotesForVault(ctx, service, vault, ignore.NewMatcher(nil), nil)
			}
			seed, err := run()
			require.NoError(t, err)
			require.Equal(t, 2, seed.Count)
			// Force the readable-file guard to enter the worker on the next run.
			// The file control proves read tolerance, rather than just an mtime skip.
			require.NoError(t, os.WriteFile(kept, []byte("# Changed\n"), 0o644))
			future := time.Now().Add(2 * time.Second)
			require.NoError(t, os.Chtimes(kept, future, future))
			blocked, mode := locked, os.FileMode(0o755)
			if kind == "root" {
				blocked = root
			} else if kind == "file" {
				blocked, mode = kept, 0o644
			}
			require.NoError(t, os.Chmod(blocked, 0))
			t.Cleanup(func() { require.NoError(t, os.Chmod(blocked, mode)) })
			if kind == "file" {
				_, err = os.ReadFile(blocked)
			} else {
				_, err = os.ReadDir(blocked)
			}
			if err == nil {
				t.Skip("permission restriction is ineffective for this process")
			}
			require.ErrorIs(t, err, os.ErrPermission, "real permission failure is required")
			result, runErr := run()
			if kind == "file" {
				require.NoError(t, runErr, "legacy individual-file reads remain tolerant")
			} else {
				require.ErrorIs(t, runErr, os.ErrPermission)
			}
			require.Zero(t, result.Deleted)
			paths, err := store.NotePaths(ctx)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"locked/kept.md", "visible.md"}, paths)
			require.NoError(t, os.Chmod(blocked, mode))
			retry, err := run()
			require.NoError(t, err)
			require.Equal(t, 1, retry.Count, "restored discovery must index changed bytes")
			require.Zero(t, retry.Deleted)
			paths, err = store.NotePaths(ctx)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"locked/kept.md", "visible.md"}, paths)
		})
	}
}
