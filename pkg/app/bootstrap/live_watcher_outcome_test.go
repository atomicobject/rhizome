package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLiveRuntimeWatcherRequiredDestinationFailureReportsTruthAndRetries(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\ncode:\n  enabled: true\nnoteEmbeddings:\n  enabled: false\ncodeEmbeddings:\n  enabled: false\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Destination fixture\n\nMust be indexed after retry.\n"), 0o644))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	rt, err := NewLiveRuntime(ctx, LiveOptions{VaultName: root, DisableWatchHub: true, SkipCacheWarmup: true, DisableSessionStore: true, Requirements: RequireRuntimeCapabilities(RuntimeCapabilitySearch, RuntimeCapabilityCodeIndex)})
	require.NoError(t, err)
	defer func() { require.NoError(t, rt.Close()) }()
	require.NoError(t, rt.WaitForSearch(ctx))
	require.NoError(t, rt.WaitForCodeIndex(ctx))
	require.Eventually(t, func() bool { return rt.liveWatcher.Load() != nil }, 5*time.Second, 5*time.Millisecond)
	w := rt.liveWatcher.Load()
	require.NoError(t, rt.Cache().EnsureReady(ctx))
	store := rt.IntelStore()
	require.NotNil(t, store)
	// ensureNoteRow inserts an empty hash only at the required anchor destination,
	// after ownership has committed the source with its nonempty hash.
	_, err = store.DB().ExecContext(ctx, `CREATE TRIGGER watcher_destination_failure BEFORE INSERT ON notes WHEN NEW.content_hash = '' BEGIN SELECT RAISE(ABORT, 'synthetic destination write denied'); END`)
	require.NoError(t, err)
	rt.Cache().MarkDirty("note.md", cache.DirtyModified)
	// Kick only the scheduler tick deterministically; use the elected public
	// LiveRuntime, its actual lane/lease and all real reconciliation destinations.
	handle := w.processOwnershipBatch()
	terminal := watcherJobTerminal(t, handle)
	jobErr := handle.Err()
	status := rt.Lane().Status()
	health := rt.LiveHealth()
	generation, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	t.Logf("after real SQLite destination failure: job err=%v terminal=%+v lane lastError=%v liveHealth=%+v generation=%d pending=%v dirty=%v", jobErr, terminal, status.LastError, health, generation, pending, rt.Cache().DirtySnapshot())
	require.NotNil(t, health.LastCompletedEpoch)
	require.Equal(t, "failed", health.LastCompletedEpoch.Status)
	require.Contains(t, health.Error, "synthetic destination write denied")
	require.True(t, pending)
	require.Contains(t, rt.Cache().DirtySnapshot(), "note.md")
	require.ErrorContains(t, jobErr, "synthetic destination write denied")
	require.Equal(t, lane.OutcomeFailed, terminal.Outcome)
	require.False(t, terminal.OK)
	require.Equal(t, jobErr.Error(), terminal.Error)
	require.ErrorIs(t, status.LastError, jobErr)
	_, err = store.DB().ExecContext(ctx, `DROP TRIGGER watcher_destination_failure`)
	require.NoError(t, err)
	// Retry without another filesystem event.
	select {
	case <-time.After(time.Nanosecond + 50*time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// No new path event: the retained batch/debt must drive the retry.
	retry := w.processOwnershipBatch()
	retryTerminal := watcherJobTerminal(t, retry)
	require.NoError(t, retry.Err())
	require.Equal(t, lane.OutcomeOK, retryTerminal.Outcome)
	require.True(t, retryTerminal.OK)
	require.Empty(t, retryTerminal.Error)
	require.NoError(t, rt.Lane().Status().LastError)
	_, pending, err = store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.False(t, pending)
	sections, err := store.IntelDocSections(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, sections)
	live := rt.LiveHealth()
	require.Nil(t, live.CurrentEpoch)
	require.NotNil(t, live.LastCompletedEpoch)
	require.Equal(t, "completed", live.LastCompletedEpoch.Status)
	require.Empty(t, live.Error)
	t.Logf("quiet retry: sections=%d debtPending=%v health=%+v laneLastError=%v", len(sections), pending, rt.LiveHealth(), rt.Lane().Status().LastError)
}

func watcherJobTerminal(t *testing.T, handle lane.Handle) lane.Event {
	t.Helper()
	require.NotNil(t, handle)
	select {
	case <-handle.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("watcher job did not complete")
	}
	events, unsubscribe := handle.Subscribe()
	defer unsubscribe()
	var terminals []lane.Event
	for event := range events {
		if event.Type == lane.EventDone {
			terminals = append(terminals, event)
		}
	}
	require.Len(t, terminals, 1)
	require.Equal(t, handle.ID(), terminals[0].JobID)
	require.Equal(t, handle.Kind(), terminals[0].Kind)
	return terminals[0]
}

func TestWatcherEarlyReadFailuresReachLane(t *testing.T) {
	for _, boundary := range []string{"cache-refresh", "pending-generation"} {
		t.Run(boundary, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644))
			w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
			defer cacheService.Close()
			defer store.Close()
			fault := errors.New("synthetic discovery failure")
			failDiscovery := true
			if boundary == "cache-refresh" {
				cold, err := cache.NewService(root, cache.Options{DiscoverFiles: func() ([]string, error) {
					if failDiscovery {
						return nil, fault
					}
					return []string{"note.md"}, nil
				}})
				require.NoError(t, err)
				defer cold.Close()
				w.cacheService = cold
			} else {
				// Keep a real open store and make only its debt query unavailable.
				_, err := store.DB().Exec(`ALTER TABLE index_metadata RENAME TO hidden_index_metadata`)
				require.NoError(t, err)
				cacheService.MarkDirty("note.md", cache.DirtyModified)
			}
			handle := w.processOwnershipBatch()
			terminal := watcherJobTerminal(t, handle)
			require.Error(t, handle.Err())
			require.Equal(t, lane.OutcomeFailed, terminal.Outcome)
			require.False(t, terminal.OK)
			require.Equal(t, handle.Err().Error(), terminal.Error)
			require.ErrorIs(t, w.lane.Status().LastError, handle.Err())
			current, last := w.health.snapshot()
			require.Nil(t, current, "early errors do not invent an ownership epoch")
			require.Nil(t, last)
			if boundary == "cache-refresh" {
				require.ErrorIs(t, handle.Err(), fault)
				failDiscovery = false
			} else {
				require.ErrorContains(t, handle.Err(), "index_metadata")
				require.Contains(t, cacheService.DirtySnapshot(), "note.md")
				_, err := store.DB().Exec(`ALTER TABLE hidden_index_metadata RENAME TO index_metadata`)
				require.NoError(t, err)
			}
			retry := w.processOwnershipBatch()
			require.Equal(t, lane.OutcomeOK, watcherJobTerminal(t, retry).Outcome)
			require.NoError(t, retry.Err())
			require.NoError(t, w.lane.Status().LastError)
			sections, err := store.IntelDocSections(t.Context())
			require.NoError(t, err)
			require.NotEmpty(t, sections)
		})
	}
}

func TestWatcherNoWorkAndRepeatedEditsReportSuccess(t *testing.T) {
	root := t.TempDir()
	notePath := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(notePath, []byte("# Before\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer cacheService.Close()
	defer store.Close()
	now := time.Now()
	w.opts.Now = func() time.Time { return now }
	processed := 0
	w.onProcessedPath = func(string) { processed++ }
	success := func() {
		t.Helper()
		handle := w.processOwnershipBatch()
		terminal := watcherJobTerminal(t, handle)
		require.NoError(t, handle.Err())
		require.True(t, terminal.OK)
		require.Equal(t, lane.OutcomeOK, terminal.Outcome)
		require.Empty(t, terminal.Error)
		require.NoError(t, w.lane.Status().LastError)
	}
	cacheService.MarkDirty("note.md", cache.DirtyModified)
	success()
	require.Equal(t, 1, processed)
	before, err := store.IntelDocSections(t.Context())
	require.NoError(t, err)
	require.Len(t, before, 1)
	success() // No dirty input, resync, or debt.
	require.Equal(t, 1, processed)
	require.NoError(t, os.WriteFile(notePath, []byte("# After\n"), 0o644))
	cacheService.MarkDirty("note.md", cache.DirtyModified)
	success() // A repeated edit publishes without a structural cooldown.
	require.Equal(t, 2, processed)
	after, err := store.IntelDocSections(t.Context())
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Contains(t, after[0].Content, "After")
}
