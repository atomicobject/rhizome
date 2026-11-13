package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestWatcherRecoveryDiagnosticsSurviveRuntimeRestart(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("noteEmbeddings:\n  enabled: false\ncodeEmbeddings:\n  enabled: false\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Original\n"), 0o600))
	start := func() (*unifiedSemanticWatcher, func()) {
		recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: io.Discard})
		require.NoError(t, err)
		ctx := diagnostics.WithOperation(diagnostics.WithRecorder(t.Context(), recorder), diagnostics.NewOperation("runtime", "serve"))
		rt, err := NewLiveRuntime(ctx, LiveOptions{
			VaultName: root, DisableLeaderWork: true, DisableWatchHub: true, DisableSessionStore: true,
			Requirements: RequireRuntimeCapabilities(RuntimeCapabilitySearch, RuntimeCapabilityCodeIndex, RuntimeCapabilityCodeAnchorWarmup),
		})
		require.NoError(t, err)
		require.NoError(t, rt.WaitForSearch(ctx))
		require.NoError(t, rt.WaitForCodeIndex(ctx))
		require.NoError(t, rt.Cache().EnsureReady(ctx))
		indexLane := lane.New(lane.Options{LockPath: repoconfig.IndexLockPath(root), PriorityPoll: 20 * time.Millisecond})
		w := &unifiedSemanticWatcher{
			runCtx: ctx, vaultPath: root, vaultDef: rt.VaultDef, noteRuntime: rt.noteFormats,
			codeCfg: *rt.codeCfg.Load(), noteMetadataIndexer: rt.noteMetadataIndexer,
			cacheService: rt.Cache(), noteSvc: rt.CodeAnchorService(), intelStore: rt.IntelStore(),
			lane: indexLane, health: rt.liveHealth, opts: UnifiedSemanticWatcherOptions{}.normalize(),
		}
		stop := func() {
			indexLane.Close()
			require.NoError(t, rt.Close())
			require.NoError(t, recorder.Close())
		}
		t.Cleanup(stop)
		return w, stop
	}
	w, stop := start()
	w.retainWatchEvents(nil, true)
	runOwnershipBatch(t, w)
	for _, cause := range []string{"external_priority", "explicit_index"} {
		var explicit lane.Handle
		w.afterPreparedOwnershipCommit = func(ctx context.Context) error {
			if cause == "external_priority" {
				priority, err := indexlock.RequestPriority(repoconfig.IndexLockPath(root))
				if err != nil {
					return err
				}
				defer priority.Close()
			} else {
				var err error
				explicit, _, err = w.lane.Submit(w.runCtx, lane.Request{Kind: lane.KindExplicitIndex})
				if err != nil {
					return err
				}
			}
			<-ctx.Done()
			return ctx.Err()
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Changed by "+cause+"\n"), 0o600))
		w.cacheService.MarkDirty("note.md", cache.DirtyModified)
		interrupted := w.processOwnershipBatch()
		watcherJobTerminal(t, interrupted)
		require.ErrorIs(t, interrupted.Err(), context.Canceled)
		if explicit != nil {
			watcherJobTerminal(t, explicit)
			require.ErrorIs(t, interrupted.Err(), lane.ErrPreempted)
		}
		w.afterPreparedOwnershipCommit = nil
		recovery := w.processOwnershipBatch()
		watcherJobTerminal(t, recovery)
		require.NoError(t, recovery.Err())
	}
	before, err := diagnostics.ReadEvents(root, diagnostics.Filter{Limit: 1000})
	require.NoError(t, err)
	require.False(t, before.Coverage.Truncated)
	var yielded, preempted, restored, full, unready int
	for _, event := range before.Events {
		switch event.Name {
		case "job.yielded":
			require.Equal(t, "external_priority", event.Attributes["reason_code"])
			yielded++
		case "job.preemption_requested":
			preempted++
		case "batch.restored":
			restored++
		case "discovery.full":
			require.Contains(t, []string{"resync", "pending_reconciliation"}, event.Attributes["reason_code"])
			full++
		case "batch.inputs":
			if event.Attributes["trigger_unready_derived_work"] == float64(1) {
				unready++
			}
		}
	}
	require.Equal(t, 1, yielded)
	require.Equal(t, 1, preempted)
	require.Equal(t, 2, restored)
	require.GreaterOrEqual(t, full, 3)
	require.Equal(t, 2, unready, "unready recovery is retained as a trigger on the same operation as full discovery")
	stop()
	_, _ = start()
	after, err := diagnostics.ReadEvents(root, diagnostics.Filter{Limit: 1000})
	require.NoError(t, err)
	require.False(t, after.Coverage.Truncated)
	require.Empty(t, after.Coverage.Warnings)
	retained := make(map[string]diagnostics.Event)
	processes := make(map[string]bool)
	for _, event := range after.Events {
		retained[fmt.Sprintf("%s/%d", event.ProcessID, event.Sequence)] = event
		processes[event.ProcessID] = true
	}
	require.Len(t, processes, 2, "restart must open a new recorder session")
	for _, event := range before.Events {
		require.Equal(t, event, retained[fmt.Sprintf("%s/%d", event.ProcessID, event.Sequence)], "event must survive reopening runtime and recorder")
	}
	reports, err := diagnostics.ReadReports(root, diagnostics.Filter{Kind: "indexing-job", Limit: 100})
	require.NoError(t, err)
	var external, explicit int
	for _, report := range reports.Reports {
		switch report.ReasonCode {
		case "external_priority":
			external++
		case "explicit_index":
			explicit++
		}
	}
	require.Equal(t, 1, external)
	require.Equal(t, 1, explicit)
}
