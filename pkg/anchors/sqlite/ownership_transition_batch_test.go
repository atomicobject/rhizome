package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyOwnershipTransitions_BatchesCurrentOwnershipAcrossParameterBoundary(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const noteCount = 901
	require.NoError(t, seedOwnershipBenchmarkNotes(ctx, store, noteCount))
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime)
		VALUES ('notes/bench-0450.md', 'go', 'same', 'v1', 'ok', 1)
	`)
	require.NoError(t, err)

	source := &NoteSourceState{
		FormatID: "markdown", ContentHash: "same", ProviderVersion: "md-v1", ProjectionVersion: "root-v1",
		Status: NoteProjectionStatusCurrent, ObservedAt: 2,
	}
	transitions := make([]OwnershipTransition, 0, noteCount+2)
	for i := 0; i < noteCount; i++ {
		transitions = append(transitions, OwnershipTransition{
			Path: fmt.Sprintf("notes/bench-%04d.md", i), Target: OwnershipTargetNote, Note: source,
		})
	}
	transitions = append(transitions,
		OwnershipTransition{Path: "src/cold.go", Target: OwnershipTargetCode},
		OwnershipTransition{Path: "notes/missing.md", Target: OwnershipTargetUnowned},
	)

	result, err := store.ApplyOwnershipTransitions(ctx, transitions)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/bench-0450.md", "notes/missing.md", "src/cold.go"}, result.TransitionedPaths)
	require.EqualValues(t, 1, result.ReconciliationGeneration)
	var notes, codeOwners int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes`).Scan(&notes))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files`).Scan(&codeOwners))
	require.Equal(t, noteCount, notes)
	require.Zero(t, codeOwners)
}

func TestApplyOwnershipTransitions_CleansReverseIndexWithoutOwnerRootsAndPreservesSharedTarget(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const retiredPath = "src/missing-owner.go"
	const retainedPath = "src/retained.go"
	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, statement := range []string{
			`INSERT INTO intel_symbol_ref_files(file_id, path) VALUES (1, 'src/missing-owner.go'), (2, 'src/retained.go')`,
			`INSERT INTO intel_symbol_ref_targets(target_id, dst_lang, dst_pkg, dst_name, dst_fqn) VALUES
				(1, 'go', 'pkg', 'Shared', 'pkg.Shared'), (2, 'go', 'pkg', 'Orphan', 'pkg.Orphan')`,
			`INSERT INTO intel_symbol_refs(src_file_id, owner_fqn, ref_kind, dst_target_id) VALUES
				(1, 'missing.Owner', 'calls', 1), (1, 'missing.Owner', 'calls', 2), (2, 'retained.Owner', 'calls', 1)`,
			`INSERT INTO intel_import_refs(src_path, module) VALUES ('src/missing-owner.go', 'old')`,
			`INSERT INTO intel_module_defs(src_path, lang, module) VALUES ('src/missing-owner.go', 'go', 'old')`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}))

	result, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{Path: retiredPath, Target: OwnershipTargetUnowned}})
	require.NoError(t, err)
	require.Equal(t, []string{retiredPath}, result.TransitionedPaths)

	var retiredRefs, retainedRefs, targets, imports, modules int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_symbol_ref_files WHERE path = ?`, retiredPath).Scan(&retiredRefs))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_symbol_ref_files WHERE path = ?`, retainedPath).Scan(&retainedRefs))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_symbol_ref_targets`).Scan(&targets))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_import_refs WHERE src_path = ?`, retiredPath).Scan(&imports))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_module_defs WHERE src_path = ?`, retiredPath).Scan(&modules))
	require.Zero(t, retiredRefs)
	require.Equal(t, 1, retainedRefs)
	require.Equal(t, 1, targets, "the shared target remains while the unique target is pruned")
	require.Zero(t, imports)
	require.Zero(t, modules)
}

func TestApplyOwnershipTransitions_BatchesEffectiveReverseIndexCleanupAcrossParameterBoundary(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const pathCount = 901
	require.NoError(t, seedOwnershipBenchmarkReverseIndex(ctx, store, pathCount))
	var sharedTargetID int64
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT target_id FROM intel_symbol_ref_targets WHERE dst_fqn = 'bench.Target0000'
	`).Scan(&sharedTargetID))
	err := store.db.QueryRowContext(ctx, `
		INSERT INTO intel_symbol_ref_files(path) VALUES ('src/non-transitioned.go')
		RETURNING file_id
	`).Scan(new(int64))
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_symbol_refs(src_file_id, owner_fqn, ref_kind, dst_target_id)
		SELECT file_id, 'retained.Owner', 'calls', ?
		FROM intel_symbol_ref_files WHERE path = 'src/non-transitioned.go'
	`, sharedTargetID)
	require.NoError(t, err)

	transitions := make([]OwnershipTransition, pathCount)
	for i := range transitions {
		transitions[i] = OwnershipTransition{
			Path: fmt.Sprintf("notes/bench-%04d.md", i), Target: OwnershipTargetUnowned,
		}
	}
	result, err := store.ApplyOwnershipTransitions(ctx, transitions)
	require.NoError(t, err)
	require.Len(t, result.TransitionedPaths, pathCount)

	var transitionedFiles, transitionedRefs, remainingTargets, retainedRefs int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM intel_symbol_ref_files WHERE path LIKE 'notes/bench-%'
	`).Scan(&transitionedFiles))
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_symbol_refs r
		JOIN intel_symbol_ref_files f ON f.file_id = r.src_file_id
		WHERE f.path LIKE 'notes/bench-%'
	`).Scan(&transitionedRefs))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_symbol_ref_targets`).Scan(&remainingTargets))
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_symbol_refs r
		JOIN intel_symbol_ref_files f ON f.file_id = r.src_file_id
		WHERE f.path = 'src/non-transitioned.go' AND r.dst_target_id = ?
	`, sharedTargetID).Scan(&retainedRefs))
	require.Zero(t, transitionedFiles)
	require.Zero(t, transitionedRefs)
	require.Equal(t, 1, remainingTargets)
	require.Equal(t, 1, retainedRefs)
}

func TestApplyOwnershipTransitions_ReverseIndexCleanupRollsBackWithPendingGeneration(t *testing.T) {
	ctx := context.Background()
	store := newOwnershipTransitionStore(t)
	const path = "src/rollback-missing-owner.go"
	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, statement := range []string{
			`INSERT INTO intel_symbol_ref_files(file_id, path) VALUES (1, 'src/rollback-missing-owner.go')`,
			`INSERT INTO intel_symbol_ref_targets(target_id, dst_lang, dst_pkg, dst_name, dst_fqn) VALUES (1, 'go', 'pkg', 'Rollback', 'pkg.Rollback')`,
			`INSERT INTO intel_symbol_refs(src_file_id, owner_fqn, ref_kind, dst_target_id) VALUES (1, 'rollback.Owner', 'calls', 1)`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, createOwnershipAbortTrigger(ctx, store, "before_batched_cleanup_generation", `
		CREATE TRIGGER before_batched_cleanup_generation
		BEFORE INSERT ON index_metadata
		WHEN NEW.key = 'ownership_reconciliation_generation'
		BEGIN SELECT RAISE(ABORT, 'injected batched cleanup rollback'); END;
	`))

	_, err := store.ApplyOwnershipTransitions(ctx, []OwnershipTransition{{Path: path, Target: OwnershipTargetUnowned}})
	require.ErrorContains(t, err, "injected batched cleanup rollback")
	for _, table := range []string{"intel_symbol_ref_files", "intel_symbol_ref_targets", "intel_symbol_refs"} {
		var count int
		require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		require.Equal(t, 1, count, table)
	}
	generation, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.Zero(t, generation)
	require.False(t, pending)
}
