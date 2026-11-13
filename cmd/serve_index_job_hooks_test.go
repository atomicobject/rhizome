package cmd

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	appserve "github.com/atomicobject/rhizome/pkg/app/cli/serve"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestBootCatchUpUsesAppliedSelectionAndPreservesPendingConfiguration(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "public.md"), []byte("# Public\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "private.md"), []byte("# Private\n"), 0o600))
	configPath := filepath.Join(root, ".rhizome/config.yml")
	config := "notes:\n  includes: [\"*.md\"]\nnoteEmbeddings: {enabled: false}\ncodeEmbeddings: {enabled: false}\n"
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	rt, err := bootstrap.NewLiveRuntime(ctx, bootstrap.LiveOptions{
		VaultName: root, DisableWatchHub: true, DisableSessionStore: true,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, rt.Close()) }()
	require.NoError(t, rt.WaitForSearch(ctx))
	require.NoError(t, rt.WaitForCodeIndex(ctx))
	settle := func(want []string) {
		t.Helper()
		require.Eventually(t, func() bool {
			paths, err := rt.IntelStore().NotePaths(ctx)
			sort.Strings(paths)
			last := rt.LiveHealth().LastCompletedEpoch
			return err == nil && slices.Equal(paths, want) && last != nil && last.Status == "completed" && !rt.Lane().Status().Busy
		}, 10*time.Second, 10*time.Millisecond)
	}
	settle([]string{"private.md", "public.md"})
	startup := rt.Snapshot()
	var catchUpPaths []string
	coordinator := appserve.NewReadinessCoordinator(func(ctx context.Context, lease *indexlock.Lease, noteMetadata notemeta.Indexer, vaultPath string, snapshot runtimeview.Snapshot, debug bool) error {
		if err := runBackgroundIndex(ctx, lease, noteMetadata, vaultPath, snapshot, debug, "test"); err != nil {
			return err
		}
		paths, err := rt.IntelStore().NotePaths(ctx)
		catchUpPaths = paths
		return err
	})
	defer func() {
		cancel()
		coordinator.Drain()
	}()
	done := make(chan error, 1)
	go func() {
		done <- coordinator.BackgroundIndexer(ctx, rt.NoteMetadataIndexer(), root, startup.VaultDefinition, false)
	}()

	excluded := "notes:\n  includes: [\"*.md\"]\n  excludes: [private.md]\nnoteEmbeddings: {enabled: false}\ncodeEmbeddings: {enabled: false}\n"
	require.NoError(t, os.WriteFile(configPath, []byte(excluded), 0o600))
	rt.Cache().MarkDirty(".rhizome/config.yml", cache.DirtyModified)
	settle([]string{"public.md"})
	applied := rt.Snapshot()
	require.Equal(t, []string{"private.md"}, applied.VaultDefinition.Excludes)
	pending := "# Unapplied edit\n" + config
	require.NoError(t, os.WriteFile(configPath, []byte(pending), 0o600))
	coordinator.BindLive(ctx, rt)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Equal(t, []string{"public.md"}, catchUpPaths, "catch-up must preserve the selection before validation refreshes it")
	stored, present, err := rt.IntelStore().GetScopeConfigHash(ctx)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, applied.Configuration.Local.ScopeConfigHash(), stored)
	disk, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, pending, string(disk))
}

func TestExplicitIndexUsesAppliedSelectionAndPreservesPendingConfiguration(t *testing.T) {
	for _, addExclude := range []bool{true, false} {
		name := "exclude removed live"
		if addExclude {
			name = "exclude added live"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			write := func(path, content string) {
				t.Helper()
				abs := filepath.Join(root, path)
				require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
				require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
			}
			config := func(excluded bool) string {
				text := "notes:\n  includes: [\"**/*.md\"]\n"
				if excluded {
					text += "  excludes: [private/**]\n"
				}
				return text + "noteEmbeddings:\n  enabled: false\ncodeEmbeddings:\n  enabled: false\n"
			}
			write(".rhizome/config.yml", config(!addExclude))
			write("public.md", "# Public\n")
			write("private/note.md", "# Private\n")
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			rt, err := bootstrap.NewLiveRuntime(ctx, bootstrap.LiveOptions{
				VaultName: root, DisableWatchHub: true, DisableSessionStore: true,
			})
			require.NoError(t, err)
			defer func() { require.NoError(t, rt.Close()) }()
			require.NoError(t, rt.WaitForSearch(ctx))
			require.NoError(t, rt.WaitForCodeIndex(ctx))
			settle := func(excluded bool) []string {
				t.Helper()
				want := []string{"private/note.md", "public.md"}
				if excluded {
					want = []string{"public.md"}
				}
				require.Eventually(t, func() bool {
					paths, err := rt.IntelStore().NotePaths(ctx)
					sort.Strings(paths)
					last := rt.LiveHealth().LastCompletedEpoch
					return err == nil && len(rt.Snapshot().VaultDefinition.Excludes) > 0 == excluded &&
						last != nil && last.Status == "completed" && slices.Equal(paths, want)
				}, 10*time.Second, 10*time.Millisecond)
				return want
			}
			settle(!addExclude)
			write(".rhizome/config.yml", config(addExclude))
			rt.Cache().MarkDirty(".rhizome/config.yml", cache.DirtyModified)
			want := settle(addExclude)
			applied := rt.Snapshot()
			// This provider edit requires a restart and must survive indexing.
			pending := "notes:\n  includes: [public.md]\nnoteEmbeddings: {enabled: true, provider: test, model: test, dimensions: 8}\n"
			write(".rhizome/config.yml", pending)
			var hooks appserve.ControlHooks
			(&vaultCommandOptions{}).registerIndexJobHooks(&hooks, rt)
			handle, _, err := hooks.SubmitIndex(ctx)
			require.NoError(t, err)
			select {
			case <-handle.Done():
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.NoError(t, handle.Err())
			paths, err := rt.IntelStore().NotePaths(ctx)
			require.NoError(t, err)
			sort.Strings(paths)
			require.Equal(t, want, paths)
			stored, present, err := rt.IntelStore().GetScopeConfigHash(ctx)
			require.NoError(t, err)
			require.True(t, present)
			require.Equal(t, applied.Configuration.Local.ScopeConfigHash(), stored)
			disk, err := os.ReadFile(filepath.Join(root, ".rhizome/config.yml"))
			require.NoError(t, err)
			require.Equal(t, pending, string(disk))
		})
	}
}
