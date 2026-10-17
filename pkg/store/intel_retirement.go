package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// Internal helpers for intel persistence.
func (s *Store) deleteIntelCodeByPathTx(ctx context.Context, tx *sql.Tx, path string) error {
	anchorIDs, err := s.selectIntelAnchorIDsByPathTx(ctx, tx, path)
	if err != nil {
		return err
	}
	anchorRowIDs, err := s.selectIntelAnchorRowIDsByPathTx(ctx, tx, path)
	if err != nil {
		return err
	}
	if len(anchorIDs) > 0 {
		if err := s.deleteIntelChunksByOwnerRowIDsTx(ctx, tx, "anchor", anchorRowIDs); err != nil {
			return err
		}
		if err := s.deleteIntelEdgesByRowIDsTx(ctx, tx, "anchor", anchorRowIDs); err != nil {
			return err
		}
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, "anchor", anchorIDs); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intel_code_anchors WHERE path = ?`, path); err != nil {
		return err
	}
	return nil
}

func (s *Store) deleteIntelDocSectionsByPathTx(ctx context.Context, tx *sql.Tx, path string) error {
	sectionIDs, err := s.selectIntelSectionIDsByPathTx(ctx, tx, path)
	if err != nil {
		return err
	}
	sectionRowIDs, err := s.selectIntelSectionRowIDsByPathTx(ctx, tx, path)
	if err != nil {
		return err
	}
	if len(sectionIDs) > 0 {
		if err := s.deleteIntelChunksByOwnerRowIDsTx(ctx, tx, "doc_section", sectionRowIDs); err != nil {
			return err
		}
		if err := s.deleteIntelEdgesByRowIDsTx(ctx, tx, "doc_section", sectionRowIDs); err != nil {
			return err
		}
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, "doc_section", sectionIDs); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intel_doc_sections WHERE path = ?`, path); err != nil {
		return err
	}
	return nil
}

func (s *Store) deleteIntelChunksByOwnerRowIDsTx(ctx context.Context, tx *sql.Tx, ownerType string, ownerRowIDs []int64) error {
	if len(ownerRowIDs) == 0 {
		return nil
	}
	for batch := range slices.Chunk(ownerRowIDs, 400) {
		holders := make([]string, len(batch))
		args := make([]any, 0, len(batch)+1)
		args = append(args, ownerType)
		for i, id := range batch {
			holders[i] = "?"
			args = append(args, id)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM intel_chunks WHERE owner_type = ? AND owner_row_id IN (%s)`,
			strings.Join(holders, ","),
		), args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) deleteIntelEdgesByRowIDsTx(ctx context.Context, tx *sql.Tx, nodeType string, nodeRowIDs []int64) error {
	if len(nodeRowIDs) == 0 {
		return nil
	}

	if len(nodeRowIDs) <= 900 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(nodeRowIDs)), ",")
		args := make([]any, 0, len(nodeRowIDs)*2+2)
		args = append(args, nodeType)
		for _, id := range nodeRowIDs {
			args = append(args, id)
		}
		args = append(args, nodeType)
		for _, id := range nodeRowIDs {
			args = append(args, id)
		}
		stmt := fmt.Sprintf(`DELETE FROM intel_edges WHERE (src_type = ? AND src_row_id IN (%s)) OR (dst_type = ? AND dst_row_id IN (%s))`, placeholders, placeholders)
		_, err := tx.ExecContext(ctx, stmt, args...)
		return err
	}

	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_intel_edge_ids`)
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_delete_intel_edge_ids (id INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_delete_intel_edge_ids(id) VALUES (?)`)
	if err != nil {
		return err
	}
	for _, id := range nodeRowIDs {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()

	_, err = tx.ExecContext(ctx, `
		DELETE FROM intel_edges
		WHERE (src_type = ? AND src_row_id IN (SELECT id FROM temp_delete_intel_edge_ids))
		   OR (dst_type = ? AND dst_row_id IN (SELECT id FROM temp_delete_intel_edge_ids))
	`, nodeType, nodeType)
	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_intel_edge_ids`)
	return err
}
