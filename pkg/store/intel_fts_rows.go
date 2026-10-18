package store

import (
	"context"
	"database/sql"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
	"time"
)

func (s *Store) deleteIntelFTSByIDsTx(ctx context.Context, tx *sql.Tx, itemType string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// Avoid SQLite bound-parameter limits for large batch deletes.
	if len(ids) <= 900 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, 0, len(ids)+1)
		args = append(args, itemType)
		for _, id := range ids {
			args = append(args, id)
		}

		stmt := fmt.Sprintf(`SELECT fts_rowid FROM intel_fts_rowid WHERE item_type = ? AND item_id IN (%s)`, placeholders)
		rows, err := tx.QueryContext(ctx, stmt, args...)
		if err != nil {
			return err
		}
		var rowIDs []int64
		for rows.Next() {
			var rowid int64
			if err := rows.Scan(&rowid); err != nil {
				_ = rows.Close()
				return err
			}
			rowIDs = append(rowIDs, rowid)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(rowIDs) > 0 {
			delPH := strings.TrimRight(strings.Repeat("?,", len(rowIDs)), ",")
			delArgs := make([]any, len(rowIDs))
			for i, id := range rowIDs {
				delArgs[i] = id
			}
			delStmt := fmt.Sprintf(`DELETE FROM intel_fts WHERE rowid IN (%s)`, delPH)
			if _, err := tx.ExecContext(ctx, delStmt, delArgs...); err != nil {
				return err
			}
		}
		delMap := fmt.Sprintf(`DELETE FROM intel_fts_rowid WHERE item_type = ? AND item_id IN (%s)`, placeholders)
		if _, err := tx.ExecContext(ctx, delMap, args...); err != nil {
			return err
		}
		return nil
	}

	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_intel_fts_ids`)
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_delete_intel_fts_ids (id TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_delete_intel_fts_ids(id) VALUES (?)`)
	if err != nil {
		return err
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM intel_fts
		WHERE rowid IN (
			SELECT r.fts_rowid
			FROM intel_fts_rowid r
			JOIN temp_delete_intel_fts_ids d ON d.id = r.item_id
			WHERE r.item_type = ?
		)
	`, itemType); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM intel_fts_rowid
		WHERE item_type = ?
		  AND item_id IN (SELECT id FROM temp_delete_intel_fts_ids)
	`, itemType); err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_intel_fts_ids`)
	return nil
}

func (s *Store) upsertIntelFTSRowsTx(ctx context.Context, tx *sql.Tx, rows []codeanchor.IntelFTSRow) error {
	if len(rows) == 0 {
		return nil
	}

	idsByType := make(map[string][]string)
	for _, r := range rows {
		if r.ItemType == "" || r.ItemID == "" {
			continue
		}
		idsByType[r.ItemType] = append(idsByType[r.ItemType], r.ItemID)
	}
	ftsDeleteStarted := time.Now()
	for itemType, ids := range idsByType {
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, itemType, ids); err != nil {
			return err
		}
	}
	indexingperf.ObserveLatency(ctx, indexingperf.CodepersistIntelFtsDelete, time.Since(ftsDeleteStarted))
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_intel_fts_rows`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE temp_intel_fts_rows (
			item_type TEXT NOT NULL,
			item_id TEXT NOT NULL,
			path TEXT,
			title TEXT,
			body TEXT,
			PRIMARY KEY (item_type, item_id)
		)
	`); err != nil {
		return err
	}
	defer func() {
		_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_fts_rows`)
	}()
	tempStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO temp_intel_fts_rows (item_type, item_id, path, title, body)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer tempStmt.Close()
	for _, r := range rows {
		if r.ItemType == "" || r.ItemID == "" {
			continue
		}
		if _, err := tempStmt.ExecContext(ctx, r.ItemType, r.ItemID, r.Path, r.Title, r.Body); err != nil {
			return err
		}
	}
	ftsInsertStarted := time.Now()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO intel_fts (item_type, item_id, path, title, body)
		SELECT item_type, item_id, path, title, body
		FROM temp_intel_fts_rows
	`); err != nil {
		return err
	}
	indexingperf.ObserveLatency(ctx, indexingperf.CodepersistIntelFtsInsert, time.Since(ftsInsertStarted))
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO intel_fts_rowid (item_type, item_id, fts_rowid)
		SELECT t.item_type, t.item_id, f.rowid
		FROM temp_intel_fts_rows t
		JOIN intel_fts f
		  ON f.item_type = t.item_type
		 AND f.item_id = t.item_id
	`); err != nil {
		return err
	}
	return nil
}
