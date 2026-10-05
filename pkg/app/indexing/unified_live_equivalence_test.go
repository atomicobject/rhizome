package indexing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// This compares durable logical data, omitting timestamps and database-local
// row IDs. It exercises public runtime boot and its real watcher scheduling.
func TestLiveWorkloadMatchesFreshBatchAfterRenameDeleteSchemaAndIgnore(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	liveRoot, freshRoot := t.TempDir(), t.TempDir()
	for _, root := range []string{liveRoot, freshRoot} {
		indexingworkload.Write(t, root, 4, 2)
	}
	run := func(root string) {
		definition, err := obsidian.LoadDefinitionFromPath(root)
		require.NoError(t, err)
		require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{
			VaultPath: root, VaultDef: definition, NoteMetadata: testNoteMetadataIndexer(t), SkipConfigPersistence: true,
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
		fresh, closeFresh, err := obsidian.OpenIntelStore(freshRoot, false)
		require.NoError(t, err)
		want, err := canonicalWorkloadSnapshot(ctx, fresh)
		closeFresh()
		require.NoError(t, err)
		require.Len(t, want["notes"], expectedNotes)
		require.Len(t, want["nodes"], expectedNotes)
		require.GreaterOrEqual(t, len(want["vectors"]), expectedNotes)
		require.Len(t, want["code"], expectedCode)
		require.Contains(t, strings.Join(want["fields"], "\n"), "Project 0002 Changed")
		for path, kind := range dirty {
			rt.Cache().MarkDirty(path, kind)
		}
		var got map[string][]string
		var snapshotErr error
		var debt int
		converged := pollWorkloadCondition(ctx, 15*time.Second, 20*time.Millisecond, func() bool {
			got, snapshotErr = canonicalWorkloadSnapshot(ctx, rt.IntelStore())
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
		require.NoError(t, os.WriteFile(filepath.Join(root, "notes/project-0002.md"), []byte(indexingworkload.Note(2, 4, "Changed")), 0o600))
	}, map[string]cache.DirtyKind{"notes/project-0002.md": cache.DirtyModified})
	compare("rename and delete topology", 3, 2, func(root string) {
		require.NoError(t, os.Rename(filepath.Join(root, "notes/project-0000.md"), filepath.Join(root, "notes/renamed.md")))
		require.NoError(t, os.Rename(filepath.Join(root, "notes/project-0001.md"), filepath.Join(root, "deleted.fixture")))
	}, map[string]cache.DirtyKind{
		"notes/project-0000.md": cache.DirtyRemoved, "notes/renamed.md": cache.DirtyCreated,
		"notes/project-0001.md": cache.DirtyRemoved,
	})
	compare("schema and ownership retirement", 3, 1, func(root string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ontology/schema.graphql"), []byte("type Project @node(paths: [\"notes/*.md\"]) { name: String! status: String }\n"), 0o600))
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

func canonicalWorkloadSnapshot(ctx context.Context, store *semdb.Store) (map[string][]string, error) {
	queries := map[string]string{
		"notes":         `SELECT path, title, format_id, content_hash FROM notes WHERE indexed_at > 0`,
		"nodes":         `SELECT note_path, node_id, node_kind, type_name, source_locator, structural_fingerprint, schema_hash FROM ontology_nodes`,
		"fields":        `SELECT note_path, node_id, field_name, field_kind, value_kind, value_text, value_norm, value_int, value_bool, target_note_path, list_ordinal, schema_hash FROM ontology_node_field_values`,
		"links":         `SELECT src_path, dst_path, kind FROM graph_doc_edges`,
		"vectors":       `SELECT s.note_path, s.type_name, s.node_kind, s.provider, s.model, s.source_content_hash, s.node_structure_fingerprint, s.chunk_text_hash, hex(v.embedding) FROM ontology_node_embedding_state s LEFT JOIN intel_embeddings e ON e.chunk_id=s.chunk_id LEFT JOIN intel_chunks c ON c.chunk_id=e.chunk_id LEFT JOIN intel_embeddings_vec_d8 v ON v.chunk_id=c.id`,
		"chunk-vectors": `SELECT c.owner_type, c.owner_id, c.granularity, c.content_hash, hex(v.embedding) FROM intel_chunks c LEFT JOIN intel_embeddings_vec_d8 v ON v.chunk_id=c.id`,
	}
	snapshot := make(map[string][]string)
	for name, query := range queries {
		rows, err := store.DB().QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		snapshot[name] = []string{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				return nil, err
			}
			for i, value := range values {
				if bytes, ok := value.([]byte); ok {
					values[i] = string(bytes)
				}
			}
			encoded, err := json.Marshal(values)
			if err != nil {
				rows.Close()
				return nil, err
			}
			snapshot[name] = append(snapshot[name], string(encoded))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		sort.Strings(snapshot[name])
	}
	codePaths, err := store.IndexedFilePaths(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(codePaths)
	snapshot["code"] = codePaths
	return snapshot, nil
}
