package indexing

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLiveWorkloadMatchesFreshBatchAfterRenameDeleteSchemaAndIgnore(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	liveRoot, freshRoot := t.TempDir(), t.TempDir()
	for _, root := range []string{liveRoot, freshRoot} {
		writeIndexingParityWorkload(t, root, 4, 2)
	}
	run := func(root string) {
		definition, err := repoconfig.LoadDefinitionFromPath(root)
		require.NoError(t, err)
		require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{
			VaultPath: root, VaultDef: definition, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true,
		}))
	}
	run(liveRoot)
	rt, err := bootstrap.NewLiveRuntime(ctx, bootstrap.LiveOptions{
		VaultName: liveRoot, DisableWatchHub: true, DisableSessionStore: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	require.NoError(t, rt.WaitForSearch(ctx))
	require.NoError(t, rt.WaitForCodeIndex(ctx))
	var warmDebt int
	var warmErr error
	warm := pollWorkloadCondition(ctx, 10*time.Second, 10*time.Millisecond, func() bool {
		last := rt.LiveHealth().LastCompletedEpoch
		warmDebt, warmErr = workloadDerivedDebt(ctx, rt.IntelStore())
		return warmErr == nil && warmDebt == 0 && last != nil && last.Status == "completed"
	})
	require.NoError(t, warmErr)
	require.True(t, warm, "initial live publication and derived work did not finish; durable debt=%d", warmDebt)
	compare := func(stage string, expectedNotes, expectedCode int, mutate func(string), dirty map[string]cache.DirtyKind) {
		t.Helper()
		mutate(liveRoot)
		mutate(freshRoot)
		run(freshRoot)
		fresh, closeFresh, err := indexstore.Open(freshRoot, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
		require.NoError(t, err)
		want, err := canonicalWorkloadSnapshot(t, ctx, fresh, freshRoot)
		closeFresh()
		require.NoError(t, err)
		require.Len(t, want["notes"], expectedNotes)
		require.Len(t, want["nodes"], expectedNotes)
		require.GreaterOrEqual(t, len(want["vectors"]), expectedNotes)
		require.Len(t, want["code"], expectedCode)
		require.Len(t, want["assessments"], expectedNotes)
		require.NotEmpty(t, want["ontology-edges"])
		require.NotEmpty(t, want["dependencies"])
		require.NotEmpty(t, want["code-anchors"])
		requireParitySnapshotCoverage(t, want)
		if expectedCode == 2 {
			require.NotEmpty(t, want["go-relationships"])
		}
		require.Contains(t, strings.Join(want["fields"], "\n"), "Project 0002 Changed")
		for path, kind := range dirty {
			rt.Cache().MarkDirty(path, kind)
		}
		var got map[string][]string
		var snapshotErr error
		var debt int
		converged := pollWorkloadCondition(ctx, 15*time.Second, 20*time.Millisecond, func() bool {
			got, snapshotErr = canonicalWorkloadSnapshot(t, ctx, rt.IntelStore(), liveRoot)
			if snapshotErr != nil {
				return false
			}
			debt, snapshotErr = workloadDerivedDebt(ctx, rt.IntelStore())
			return snapshotErr == nil && debt == 0 && reflect.DeepEqual(want, got)
		})
		if !converged {
			t.Logf("%s: final durable derived debt=%d", stage, debt)
			require.NoError(t, snapshotErr)
			require.Equal(t, want, got, "%s: live and full batch did not converge", stage)
			require.True(t, converged, "%s: snapshots match but durable derived debt did not drain: %d", stage, debt)
		}
		population, err := rt.IntelStore().IndexedPopulation(ctx)
		require.NoError(t, err)
		require.Zero(t, population.UnresolvedChunkOwner, "removed semantic owners must not retain chunks")
		paths, err := rt.IntelStore().CurrentNoteMetadataPaths(ctx)
		require.NoError(t, err)
		current, err := rt.IntelStore().CurrentNoteMetadataRowsByPaths(ctx, paths)
		require.NoError(t, err)
		require.Len(t, current, expectedNotes)
		for _, row := range current {
			require.Equal(t, semdb.NoteProjectionStatusCurrent, row.Projection.Status)
		}
		if expectedNotes == 3 {
			var retiredNoteSemantics int
			require.NoError(t, rt.IntelStore().DB().QueryRowContext(ctx, `SELECT count(*) FROM ontology_node_embedding_state WHERE note_path IN ('notes/project-0000.md','notes/project-0001.md')`).Scan(&retiredNoteSemantics))
			require.Zero(t, retiredNoteSemantics)
		}
		if expectedCode == 1 {
			require.Equal(t, []string{"src/unit_0001.go"}, got["code"])
		}
		t.Logf("%s: %d current notes/nodes, %d code paths and canonical vectors match", stage, expectedNotes, expectedCode)
	}
	compare("scoped content edit", 4, 2, func(root string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, "notes/project-0002.md"), []byte(indexingParityNote(2, 4, "Changed")), 0o600))
	}, map[string]cache.DirtyKind{"notes/project-0002.md": cache.DirtyModified})
	compare("rename and delete topology", 3, 2, func(root string) {
		require.NoError(t, os.Rename(filepath.Join(root, "notes/project-0000.md"), filepath.Join(root, "notes/renamed.md")))
		require.NoError(t, os.Rename(filepath.Join(root, "notes/project-0001.md"), filepath.Join(root, "deleted.fixture")))
	}, map[string]cache.DirtyKind{
		"notes/project-0000.md": cache.DirtyRemoved, "notes/renamed.md": cache.DirtyCreated,
		"notes/project-0001.md": cache.DirtyRemoved,
	})
	compare("schema and ownership retirement", 3, 1, func(root string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ontology/schema.graphql"), []byte(indexingParitySchema("status: String")), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ignore"), []byte("src/unit_0000.go\n"), 0o600))
	}, map[string]cache.DirtyKind{
		".rhizome/ontology/schema.graphql": cache.DirtyModified, ".rhizome/ignore": cache.DirtyModified,
	})
}

// Poll on the test goroutine so a timed-out callback cannot keep mutating the
// diagnostic snapshot while the test reads it or closes the runtime.
func pollWorkloadCondition(ctx context.Context, timeout, interval time.Duration, condition func() bool) bool {
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if condition() {
			return true
		}
		timer := time.NewTimer(interval)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func workloadDerivedDebt(ctx context.Context, store *semdb.Store) (int, error) {
	var present int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='derived_work'`).Scan(&present); err != nil {
		return 0, err
	}
	// The same fixture also runs against the pre-refactor baseline schema.
	if present == 0 {
		return 0, nil
	}
	var pending int
	err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM derived_work`).Scan(&pending)
	return pending, err
}
