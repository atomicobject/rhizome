package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

func BenchmarkApplyOwnershipTransitions1000(b *testing.B) {
	const pathCount = 1000
	ctx := context.Background()
	noteSource := NoteSourceState{
		FormatID:          "markdown",
		ContentHash:       "same",
		ProviderVersion:   "md-v1",
		ProjectionVersion: "root-v1",
		Status:            NoteProjectionStatusCurrent,
		ObservedAt:        1,
	}
	noteTransitions := make([]OwnershipTransition, pathCount)
	retireTransitions := make([]OwnershipTransition, pathCount)
	for i := 0; i < pathCount; i++ {
		path := fmt.Sprintf("notes/bench-%04d.md", i)
		noteTransitions[i] = OwnershipTransition{Path: path, Target: OwnershipTargetNote, Note: &noteSource}
		retireTransitions[i] = OwnershipTransition{Path: path, Target: OwnershipTargetUnowned}
	}

	b.Run("no_op", func(b *testing.B) {
		store := newOwnershipBenchmarkStore(b)
		require.NoError(b, seedOwnershipBenchmarkNotes(ctx, store, pathCount))
		collector := indexingperf.New()
		measuredCtx := indexingperf.WithCollector(ctx, collector)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result, err := store.ApplyOwnershipTransitions(measuredCtx, noteTransitions)
			require.NoError(b, err)
			require.Empty(b, result.TransitionedPaths)
		}
		b.StopTimer()
		b.Logf("measured ownership operations=%d; cumulative writer metrics:\n%s", b.N, collector.RenderSummary())
	})

	b.Run("cold_populated_reverse_index", func(b *testing.B) {
		b.StopTimer()
		store := newOwnershipBenchmarkStore(b)
		collector := indexingperf.New()
		measuredCtx := indexingperf.WithCollector(ctx, collector)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			require.NoError(b, seedOwnershipBenchmarkReverseIndex(ctx, store, pathCount))
			b.StartTimer()
			result, err := store.ApplyOwnershipTransitions(measuredCtx, retireTransitions)
			require.NoError(b, err)
			require.Len(b, result.TransitionedPaths, pathCount)
		}
		b.StopTimer()
		b.Logf("measured ownership operations=%d; cumulative writer metrics:\n%s", b.N, collector.RenderSummary())
	})
}

func newOwnershipBenchmarkStore(b *testing.B) *Store {
	b.Helper()
	store, err := Open(filepath.Join(b.TempDir(), "ownership-transition-bench.db"))
	require.NoError(b, err)
	b.Cleanup(func() { _ = store.Close() })
	return store
}

func seedOwnershipBenchmarkNotes(ctx context.Context, store *Store, count int) error {
	return store.withWriteTx(ctx, func(tx *sql.Tx) error {
		for i := 0; i < count; i++ {
			path := fmt.Sprintf("notes/bench-%04d.md", i)
			result, err := tx.ExecContext(ctx, `
				INSERT INTO notes(path, title, content_hash, indexer_version, mtime, size, indexed_at, note_key_full, note_key_base, path_len, first_segment_norm, format_id)
				VALUES (?, '', 'same', '', 0, 0, 1, ?, ?, ?, 'notes', 'markdown')
			`, path, path, path, len(path))
			if err != nil {
				return err
			}
			noteID, err := result.LastInsertId()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO note_projection_state(note_id, provider_version, projection_version, source_content_hash, status, diagnostic_code, diagnostic_detail, updated_at)
				VALUES (?, 'md-v1', 'root-v1', 'same', 'current', '', '', 1)
			`, noteID); err != nil {
				return err
			}
		}
		return nil
	})
}

func seedOwnershipBenchmarkReverseIndex(ctx context.Context, store *Store, count int) error {
	return store.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, statement := range []string{
			`DELETE FROM intel_symbol_refs`,
			`DELETE FROM intel_symbol_ref_files`,
			`DELETE FROM intel_symbol_ref_targets`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		for i := 0; i < count; i++ {
			path := fmt.Sprintf("notes/bench-%04d.md", i)
			result, err := tx.ExecContext(ctx, `INSERT INTO intel_symbol_ref_files(path) VALUES (?)`, path)
			if err != nil {
				return err
			}
			fileID, err := result.LastInsertId()
			if err != nil {
				return err
			}
			result, err = tx.ExecContext(ctx, `
				INSERT INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn)
				VALUES ('go', 'bench', ?, ?)
			`, fmt.Sprintf("Target%04d", i), fmt.Sprintf("bench.Target%04d", i))
			if err != nil {
				return err
			}
			targetID, err := result.LastInsertId()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO intel_symbol_refs(src_file_id, owner_fqn, ref_kind, dst_target_id)
				VALUES (?, ?, 'calls', ?)
			`, fileID, fmt.Sprintf("bench.Owner%04d", i), targetID); err != nil {
				return err
			}
		}
		return nil
	})
}
