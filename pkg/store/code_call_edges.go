package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// UpsertIntelCallEdgesForPath replaces call/import edges for anchors in the given path.
// It deletes existing code-ref edges whose source anchor belongs to this path.
// then inserts the new edges. This is used during the second pass of batch indexing when
// callee/imported anchors are guaranteed to exist.
func (s *Store) UpsertIntelCallEdgesForPath(ctx context.Context, path string, edges []codeanchor.IntelEdge) error {
	path = normalizeCodeLookupPath(path)
	if path == "" {
		return nil
	}
	return s.UpsertIntelCallEdgesForPathsBatch(ctx, []codeanchor.CallEdgesBatch{{Path: path, Edges: edges}})
}

// UpsertIntelCallEdgesForPathsBatch upserts call/import/type_ref/member_ref edges for multiple paths in a single transaction.
func (s *Store) UpsertIntelCallEdgesForPathsBatch(ctx context.Context, batches []codeanchor.CallEdgesBatch) error {
	if len(batches) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_call_edges")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		deleteStmt, err := tx.PrepareContext(ctx, `
			DELETE FROM intel_edges
			WHERE kind IN ('calls', 'tests', 'imports', 'type_ref', 'member_ref')
			  AND src_type = 'anchor'
			  AND src_row_id IN (SELECT id FROM intel_code_anchors WHERE path = ?)
		`)
		if err != nil {
			return err
		}
		defer deleteStmt.Close()

		insertStmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO intel_edges (src_type, src_row_id, dst_type, dst_row_id, kind, meta_json)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer insertStmt.Close()

		anchorRowIDs := map[string]int64{}
		resolveRowID := func(anchorID string) (int64, error) {
			if rowID, ok := anchorRowIDs[anchorID]; ok {
				return rowID, nil
			}
			rowID, err := lookupIntelAnchorRowIDTx(ctx, tx, anchorID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return 0, err
			}
			anchorRowIDs[anchorID] = rowID
			return rowID, nil
		}
		clearPaths := make([]string, 0, len(batches))
		for _, batch := range batches {
			path := normalizeCodeLookupPath(batch.Path)
			if path == "" {
				continue
			}
			if _, err := deleteStmt.ExecContext(ctx, path); err != nil {
				return err
			}
			for _, e := range batch.Edges {
				if !codeanchor.IsCodeRefEdge(e.Kind) {
					continue
				}
				srcRowID, err := resolveRowID(e.SrcID)
				if err != nil {
					return err
				}
				dstRowID, err := resolveRowID(e.DstID)
				if err != nil {
					return err
				}
				if srcRowID <= 0 || dstRowID <= 0 {
					continue
				}
				if _, err := insertStmt.ExecContext(ctx, "anchor", srcRowID, "anchor", dstRowID, e.Kind, e.MetaJSON); err != nil {
					return err
				}
			}
			clearPaths = append(clearPaths, path)
		}
		if len(clearPaths) > 0 {
			holders, args := placeholders(clearPaths)
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE files SET call_edges_stale = 0 WHERE path IN (%s)`, holders), args...); err != nil {
				return err
			}
		}
		return nil
	})
}
