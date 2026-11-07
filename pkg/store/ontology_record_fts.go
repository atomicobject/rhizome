package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

// ontologyRecordFTSType is the Intel FTS item type for embedded records. Its
// item_id is the record's ontology node_id, so rows follow node replacement.
const ontologyRecordFTSType = "ontology_record"

func deleteAllOntologyRecordFTSTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM intel_fts WHERE rowid IN (SELECT fts_rowid FROM intel_fts_rowid WHERE item_type = ?)`, ontologyRecordFTSType); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM intel_fts_rowid WHERE item_type = ?`, ontologyRecordFTSType)
	return err
}

// deleteOntologyRecordFTSForPathsTx removes the record rows of the persisted
// nodes on notePaths. Call it before those nodes are replaced or deleted.
func deleteOntologyRecordFTSForPathsTx(ctx context.Context, tx *sql.Tx, notePaths []string) error {
	for batch := range slices.Chunk(normalizeNonEmptyStrings(notePaths), 400) {
		holders, args := placeholders(batch)
		ids := `SELECT node_id FROM ontology_nodes WHERE note_path IN (` + holders + `)`
		args = append([]any{ontologyRecordFTSType}, args...)
		if _, err := tx.ExecContext(ctx, `DELETE FROM intel_fts WHERE rowid IN (SELECT fts_rowid FROM intel_fts_rowid WHERE item_type = ? AND item_id IN (`+ids+`))`, args...); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM intel_fts_rowid WHERE item_type = ? AND item_id IN (`+ids+`)`, args...); err != nil {
			return err
		}
	}
	return nil
}

func insertOntologyRecordFTSTx(ctx context.Context, tx *sql.Tx, texts []readmodel.RecordSearchText) error {
	if len(texts) == 0 {
		return nil
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO intel_fts (item_type, item_id, path, title, body) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	mapRow, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO intel_fts_rowid (item_type, item_id, fts_rowid) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer mapRow.Close()
	seen := make(map[string]bool, len(texts))
	for _, text := range texts {
		nodeID := strings.TrimSpace(text.NodeID)
		if nodeID == "" || seen[nodeID] {
			continue
		}
		seen[nodeID] = true
		result, err := insert.ExecContext(ctx, ontologyRecordFTSType, nodeID, strings.TrimSpace(text.NotePath), text.Title, text.Body)
		if err != nil {
			return err
		}
		rowID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("record fts rowid: %w", err)
		}
		if _, err := mapRow.ExecContext(ctx, ontologyRecordFTSType, nodeID, rowID); err != nil {
			return err
		}
	}
	return nil
}
