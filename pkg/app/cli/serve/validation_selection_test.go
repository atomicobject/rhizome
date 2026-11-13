package serve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestValidationAndRepairProjectionFollowReloadedSelection(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "public.md"), []byte("# Public\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "private.md"), []byte("# Private\n"), 0o600))
	configPath := filepath.Join(root, ".rhizome/config.yml")
	config := "notes:\n  includes: [\"*.md\"]\nnoteEmbeddings: {enabled: false}\ncodeEmbeddings: {enabled: false}\n"
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	live, err := bootstrap.NewLiveRuntime(ctx, bootstrap.LiveOptions{VaultName: root, DisableWatchHub: true, DisableSessionStore: true})
	require.NoError(t, err)
	defer func() { require.NoError(t, live.Close()) }()
	require.NoError(t, live.WaitForSearch(ctx))
	require.NoError(t, live.WaitForCodeIndex(ctx))
	settle := func(count int) {
		t.Helper()
		require.Eventually(t, func() bool {
			paths, err := live.IntelStore().NotePaths(ctx)
			last := live.LiveHealth().LastCompletedEpoch
			return err == nil && len(paths) == count && last != nil && last.Status == "completed" && !live.Lane().Status().Busy
		}, 10*time.Second, 10*time.Millisecond)
	}
	settle(2)
	coordinator := NewReadinessCoordinator(nil)
	coordinator.indexAttemptDoneOnce.Do(func() { close(coordinator.indexAttemptDone) })
	coordinator.refreshValidation(ctx, live)
	for _, excluded := range []bool{true, false} {
		text, count := config, 2
		if excluded {
			text = "notes:\n  includes: [\"*.md\"]\n  excludes: [private.md]\nnoteEmbeddings: {enabled: false}\ncodeEmbeddings: {enabled: false}\n"
			count = 1
		}
		require.NoError(t, os.WriteFile(configPath, []byte(text), 0o600))
		live.Cache().MarkDirty(".rhizome/config.yml", cache.DirtyModified)
		settle(count)
		coordinator.refreshValidation(ctx, live)
		state, err := live.IntelStore().GetValidationState(ctx)
		require.NoError(t, err)
		require.Equal(t, semdb.ValidationStatusOK, state.Status, state.Error)
		paths, err := live.IntelStore().NotePaths(ctx)
		require.NoError(t, err)
		require.Len(t, paths, count, "validation must preserve the live selection")
		handle, _, err := live.Lane().Submit(ctx, lane.Request{Kind: lane.KindValidationRefresh,
			Run: func(ctx context.Context, lease *indexlock.Lease, _ lane.Reporter) error {
				result, err := (liveValidationProjectionRefresher{live}).Refresh(ctx, lease, []string{"private.md"}, nil, nil)
				if err != nil {
					return err
				}
				return result.Close()
			}})
		require.NoError(t, err)
		select {
		case <-handle.Done():
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		require.NoError(t, handle.Err())
		paths, err = live.IntelStore().NotePaths(ctx)
		require.NoError(t, err)
		require.Len(t, paths, count, "repair projection must preserve the live selection")
	}
}
