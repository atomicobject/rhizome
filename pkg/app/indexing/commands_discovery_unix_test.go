//go:build !windows

package indexing

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestCodeCommands_PreserveNotesOnDiscoveryFailure(t *testing.T) {
	for _, command := range []string{"anchors", "index"} {
		t.Run(command, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
			locked := filepath.Join(root, "locked")
			require.NoError(t, os.Mkdir(locked, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(locked, "kept.md"), []byte("# Kept\n"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "visible.md"), []byte("# Visible\n"), 0o644))
			cfg := codeanchor.DefaultConfig(root)
			vault := obsidian.VaultDefinition{Path: root}
			var output bytes.Buffer
			run := func() error {
				output.Reset()
				if command == "anchors" {
					return RunCodeAnchorsCommand(ctx, CodeAnchorsCommandOptions{VaultPath: root, VaultDef: vault, CodeConfig: cfg, ErrWriter: &output})
				}
				return RunCodeIndexCommand(ctx, CodeIndexCommandOptions{VaultPath: root, VaultDef: vault, CodeConfig: cfg, ErrWriter: &output})
			}
			require.NoError(t, run())
			require.Contains(t, output.String(), "Ingested 2 notes")
			require.NoError(t, os.Chmod(locked, 0))
			t.Cleanup(func() { require.NoError(t, os.Chmod(locked, 0o755)) })
			_, err := os.ReadDir(locked)
			if err == nil {
				t.Skip("permission restriction is ineffective for this process")
			}
			require.ErrorIs(t, err, os.ErrPermission)
			require.ErrorIs(t, run(), os.ErrPermission)
			require.NotContains(t, output.String(), "Ingested")
			require.NotContains(t, output.String(), "Code anchors refreshed")
			require.NotContains(t, output.String(), "Code index updated")
			assertPaths := func() {
				t.Helper()
				store, err := semdb.Open(cfg.IndexPath)
				require.NoError(t, err)
				defer func() { require.NoError(t, store.Close()) }()
				paths, err := store.NotePaths(ctx)
				require.NoError(t, err)
				require.ElementsMatch(t, []string{"locked/kept.md", "visible.md"}, paths)
			}
			assertPaths()
			require.NoError(t, os.Chmod(locked, 0o755))
			require.NoError(t, run(), "ordinary retry must also acquire the released writer lease")
			require.Contains(t, output.String(), "Ingested 0 notes")
			assertPaths()
		})
	}
}
