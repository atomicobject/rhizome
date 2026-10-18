package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

// IntelAnchorsBySymbol returns intel anchors matching a symbol, across all languages/paths.
// This is intended for lightweight resolution of doc→code mentions.
func (s *Store) IntelAnchorsBySymbol(ctx context.Context, symbol string, limit int) ([]codeanchor.IntelAnchor, error) {
	if limit <= 0 {
		limit = 250
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
		       start_byte, end_byte, start_line, end_line, fingerprint, updated_at
		FROM intel_code_anchors
		WHERE symbol = ?
		ORDER BY path, fqn, anchor_id
		LIMIT ?
	`, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var anchors []codeanchor.IntelAnchor
	for rows.Next() {
		var a codeanchor.IntelAnchor
		if err := rows.Scan(&a.AnchorID, &a.Lang, &a.Kind, &a.Path, &a.Symbol, &a.FQN, &a.Signature, &a.DocComment, &a.StartByte, &a.EndByte, &a.StartLine, &a.EndLine, &a.Fingerprint, &a.UpdatedAt); err != nil {
			return nil, err
		}
		anchors = append(anchors, a)
	}
	return anchors, rows.Err()
}

// IntelAnchorByID returns a single stored intel anchor by anchor_id.
func (s *Store) IntelAnchorByID(ctx context.Context, anchorID string) (codeanchor.IntelAnchor, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
		       start_byte, end_byte, start_line, end_line, fingerprint, updated_at
		FROM intel_code_anchors
		WHERE anchor_id = ?
	`, anchorID)

	var a codeanchor.IntelAnchor
	if err := row.Scan(&a.AnchorID, &a.Lang, &a.Kind, &a.Path, &a.Symbol, &a.FQN, &a.Signature, &a.DocComment, &a.StartByte, &a.EndByte, &a.StartLine, &a.EndLine, &a.Fingerprint, &a.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return codeanchor.IntelAnchor{}, false, nil
		}
		return codeanchor.IntelAnchor{}, false, err
	}
	return a, true, nil
}

// IntelAnchorsByIDs returns intel anchors for a batch of anchor IDs.
func (s *Store) IntelAnchorsByIDs(ctx context.Context, anchorIDs []string) (map[string]codeanchor.IntelAnchor, error) {
	ids := normalizeNonEmptyStrings(anchorIDs)
	if len(ids) == 0 {
		return map[string]codeanchor.IntelAnchor{}, nil
	}
	const batchSize = 400
	out := make(map[string]codeanchor.IntelAnchor, len(ids))
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		holders := strings.Repeat("?,", len(batch))
		holders = strings.TrimSuffix(holders, ",")
		query := fmt.Sprintf(`
			SELECT anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
			       start_byte, end_byte, start_line, end_line, fingerprint, updated_at
			FROM intel_code_anchors
			WHERE anchor_id IN (%s)
		`, holders)
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var a codeanchor.IntelAnchor
			if err := rows.Scan(&a.AnchorID, &a.Lang, &a.Kind, &a.Path, &a.Symbol, &a.FQN, &a.Signature, &a.DocComment, &a.StartByte, &a.EndByte, &a.StartLine, &a.EndLine, &a.Fingerprint, &a.UpdatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[a.AnchorID] = a
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}
