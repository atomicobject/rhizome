package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

func (s *Store) selectIntelAnchorIDsByPathTx(ctx context.Context, tx *sql.Tx, path string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT anchor_id FROM intel_code_anchors WHERE path = ?`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) selectIntelAnchorRowIDsByPathTx(ctx context.Context, tx *sql.Tx, path string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM intel_code_anchors WHERE path = ?`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func lookupIntelAnchorRowIDTx(ctx context.Context, tx *sql.Tx, anchorID string) (int64, error) {
	var rowID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM intel_code_anchors WHERE anchor_id = ?`, anchorID).Scan(&rowID)
	return rowID, err
}

func selectIntelAnchorRowIDsByAnchorIDsTx(ctx context.Context, tx *sql.Tx, anchorIDs []string) (map[string]int64, error) {
	anchorIDs = dedupeNonEmptyStrings(anchorIDs)
	if len(anchorIDs) == 0 {
		return map[string]int64{}, nil
	}
	out := make(map[string]int64, len(anchorIDs))
	const batchSize = 400
	for batch := range slices.Chunk(anchorIDs, batchSize) {
		holders, args := placeholders(batch)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(
			`SELECT anchor_id, id FROM intel_code_anchors WHERE anchor_id IN (%s)`,
			holders,
		), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID string
			var rowID int64
			if err := rows.Scan(&anchorID, &rowID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[anchorID] = rowID
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

func lookupIntelSectionRowIDTx(ctx context.Context, tx *sql.Tx, sectionID string) (int64, error) {
	var rowID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM intel_doc_sections WHERE section_id = ?`, sectionID).Scan(&rowID)
	return rowID, err
}

func lookupOntologyNodeRowIDTx(ctx context.Context, tx *sql.Tx, nodeID string) (int64, error) {
	var rowID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM ontology_nodes WHERE node_id = ?`, nodeID).Scan(&rowID)
	return rowID, err
}

type intelOwnerRef struct {
	rowID     int64
	ownerType string
}

func lookupIntelOwnerRefTx(ctx context.Context, tx *sql.Tx, ownerID string) (intelOwnerRef, error) {
	if rowID, err := lookupIntelAnchorRowIDTx(ctx, tx, ownerID); err == nil {
		return intelOwnerRef{rowID: rowID, ownerType: "anchor"}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return intelOwnerRef{}, err
	}
	if rowID, err := lookupIntelSectionRowIDTx(ctx, tx, ownerID); err == nil {
		return intelOwnerRef{rowID: rowID, ownerType: "doc_section"}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return intelOwnerRef{}, err
	}
	rowID, err := lookupOntologyNodeRowIDTx(ctx, tx, ownerID)
	if err != nil {
		return intelOwnerRef{}, err
	}
	return intelOwnerRef{rowID: rowID, ownerType: "ontology_node"}, nil
}

// Resolve one bounded embedding publication batch inside its write transaction.
func lookupIntelChunkRowIDsTx(ctx context.Context, tx *sql.Tx, chunkIDs []string) (map[string]int64, error) {
	holders, args := placeholders(chunkIDs)
	rows, err := tx.QueryContext(ctx, `SELECT chunk_id, id FROM intel_chunks WHERE chunk_id IN (`+holders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rowIDs := make(map[string]int64, len(chunkIDs))
	for rows.Next() {
		var chunkID string
		var rowID int64
		if err := rows.Scan(&chunkID, &rowID); err != nil {
			return nil, err
		}
		rowIDs[chunkID] = rowID
	}
	return rowIDs, rows.Err()
}
