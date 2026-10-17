package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
)

func prepareIntelAnchorUpsertStmt(ctx context.Context, db anchorUpsertPreparer) (*sql.Stmt, error) {
	hasStartByte, err := tableHasColumn(ctx, db, "intel_code_anchors", "start_byte")
	if err != nil {
		return nil, err
	}
	hasFingerprint, err := tableHasColumn(ctx, db, "intel_code_anchors", "fingerprint")
	if err != nil {
		return nil, err
	}
	hasUpdatedAt, err := tableHasColumn(ctx, db, "intel_code_anchors", "updated_at")
	if err != nil {
		return nil, err
	}

	cols := []string{"anchor_id", "lang", "kind", "path", "symbol", "fqn"}
	vals := []string{"?", "?", "?", "?", "?", "?"}
	updates := []string{
		"lang = excluded.lang",
		"kind = excluded.kind",
		"path = excluded.path",
		"symbol = excluded.symbol",
		"fqn = excluded.fqn",
	}
	if hasStartByte {
		cols = append(cols, "start_byte", "end_byte", "start_line", "end_line")
		vals = append(vals, "0", "0", "0", "0")
	}
	if hasFingerprint {
		cols = append(cols, "fingerprint")
		vals = append(vals, "'codeemb'")
	}
	if hasUpdatedAt {
		cols = append(cols, "updated_at")
		vals = append(vals, "0")
	}
	query := fmt.Sprintf(`
		INSERT INTO intel_code_anchors (%s)
		VALUES (%s)
		ON CONFLICT(anchor_id) DO UPDATE SET
			%s
	`, strings.Join(cols, ", "), strings.Join(vals, ", "), strings.Join(updates, ",\n\t\t\t"))
	return db.PrepareContext(ctx, query)
}

func selectCodeItemRowIDsByAnchorIDs(ctx context.Context, q anchorRowIDQueryer, anchorIDs []codeindex.AnchorID) (map[string]int64, error) {
	anchorIDs = dedupeAnchorIDs(anchorIDs)
	if len(anchorIDs) == 0 {
		return map[string]int64{}, nil
	}
	out := make(map[string]int64, len(anchorIDs))
	const batchSize = 50
	for batch := range slices.Chunk(anchorIDs, batchSize) {
		holders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, id := range batch {
			holders[i] = "?"
			args[i] = string(id)
		}
		rows, err := q.QueryContext(ctx, fmt.Sprintf(`
			SELECT a.anchor_id, i.id
			FROM `+tableItems+` i
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE a.anchor_id IN (%s)
		`, strings.Join(holders, ",")), args...)
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

func dedupeAnchorIDs(anchorIDs []codeindex.AnchorID) []codeindex.AnchorID {
	if len(anchorIDs) == 0 {
		return nil
	}
	seen := make(map[codeindex.AnchorID]struct{}, len(anchorIDs))
	out := make([]codeindex.AnchorID, 0, len(anchorIDs))
	for _, id := range anchorIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func bulkUpsertIntelAnchorsTx(ctx context.Context, tx *sql.Tx, items []codeindex.Item) error {
	if len(items) == 0 {
		return nil
	}
	hasStartByte, err := tableHasColumn(ctx, tx, "intel_code_anchors", "start_byte")
	if err != nil {
		return err
	}
	hasFingerprint, err := tableHasColumn(ctx, tx, "intel_code_anchors", "fingerprint")
	if err != nil {
		return err
	}
	hasUpdatedAt, err := tableHasColumn(ctx, tx, "intel_code_anchors", "updated_at")
	if err != nil {
		return err
	}

	cols := []string{"anchor_id", "lang", "kind", "path", "symbol", "fqn"}
	updates := []string{
		"lang = excluded.lang",
		"kind = excluded.kind",
		"path = excluded.path",
		"symbol = excluded.symbol",
		"fqn = excluded.fqn",
	}
	if hasStartByte {
		cols = append(cols, "start_byte", "end_byte", "start_line", "end_line")
	}
	if hasFingerprint {
		cols = append(cols, "fingerprint")
	}
	if hasUpdatedAt {
		cols = append(cols, "updated_at")
	}

	const batchSize = 100
	for batch := range slices.Chunk(items, batchSize) {
		valueSQL := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*6)
		for _, item := range batch {
			vals := []string{"?", "?", "?", "?", "?", "?"}
			args = append(args, string(item.AnchorID), item.Lang, item.Kind, item.Path, item.Symbol, item.FQN)
			if hasStartByte {
				vals = append(vals, "0", "0", "0", "0")
			}
			if hasFingerprint {
				vals = append(vals, "'codeemb'")
			}
			if hasUpdatedAt {
				vals = append(vals, "0")
			}
			valueSQL = append(valueSQL, "("+strings.Join(vals, ", ")+")")
		}
		stmt := fmt.Sprintf(`
			INSERT INTO intel_code_anchors (%s)
			VALUES %s
			ON CONFLICT(anchor_id) DO UPDATE SET
				%s
		`, strings.Join(cols, ", "), strings.Join(valueSQL, ","), strings.Join(updates, ",\n\t\t\t\t"))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

func bulkUpsertCodeItemsTx(ctx context.Context, tx *sql.Tx, items []codeindex.Item, now, gen int64) error {
	if len(items) == 0 {
		return nil
	}
	const batchSize = 200
	for batch := range slices.Chunk(items, batchSize) {
		valueSQL := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*4)
		for _, item := range batch {
			valueSQL = append(valueSQL, "(?, ?, ?, ?)")
			args = append(args, string(item.AnchorID), item.Fingerprint, now, gen)
		}
		stmt := fmt.Sprintf(`
			WITH input(anchor_id, fingerprint, updated_at, sync_generation) AS (VALUES %s)
			INSERT INTO `+tableItems+` (anchor_row_id, fingerprint, updated_at, sync_generation)
			SELECT a.id, i.fingerprint, i.updated_at, i.sync_generation
			FROM input i
			JOIN intel_code_anchors a ON a.anchor_id = i.anchor_id
			ON CONFLICT(anchor_row_id) DO UPDATE SET
				fingerprint = excluded.fingerprint,
				updated_at = excluded.updated_at,
				sync_generation = excluded.sync_generation
		`, strings.Join(valueSQL, ","))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}
