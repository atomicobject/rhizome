package sqlite

import (
	"context"
	"database/sql"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// ReplaceDocLinksForPath replaces doc_links rows for a given source path.
func (s *Store) ReplaceDocLinksForPath(ctx context.Context, srcPath string, links []codeanchor.DocLink) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_doc_links")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = ?`, srcPath); err != nil {
			return err
		}
		if len(links) == 0 {
			return nil
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO doc_links (src_type, src_path, src_id, dst_kind, dst_id, dst_path, lang, label, snippet, meta_json, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, l := range links {
			if _, err := stmt.ExecContext(ctx, l.SrcType, l.SrcPath, l.SrcID, l.DstKind, l.DstID, l.DstPath, l.Lang, l.Label, l.Snippet, l.MetaJSON, l.UpdatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceDocLinksForPathsBatch replaces doc_links rows for multiple source paths in a single transaction.
func (s *Store) ReplaceDocLinksForPathsBatch(ctx context.Context, batches []codeanchor.DocLinksBatch) error {
	if len(batches) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_doc_links")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO doc_links (src_type, src_path, src_id, dst_kind, dst_id, dst_path, lang, label, snippet, meta_json, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, batch := range batches {
			if _, err := tx.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = ?`, batch.SrcPath); err != nil {
				return err
			}
			for _, l := range batch.Links {
				if _, err := stmt.ExecContext(ctx, l.SrcType, l.SrcPath, l.SrcID, l.DstKind, l.DstID, l.DstPath, l.Lang, l.Label, l.Snippet, l.MetaJSON, l.UpdatedAt); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// DocLinksForAnchor returns links pointing to an anchor.
func (s *Store) DocLinksForAnchor(ctx context.Context, anchorID string, limit int) ([]codeanchor.DocLink, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_type, src_path, src_id, dst_kind, dst_id, dst_path, lang, label, snippet, meta_json, updated_at
		FROM doc_links
		WHERE dst_kind = 'anchor' AND dst_id = ?
		ORDER BY updated_at DESC, src_type, src_path
		LIMIT ?
	`, anchorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return readDocLinks(rows)
}

// DocLinksForNote returns links pointing to a note (typically from code files via coderefs).
func (s *Store) DocLinksForNote(ctx context.Context, notePath string, limit int) ([]codeanchor.DocLink, error) {
	if limit <= 0 {
		limit = 10
	}
	notePath = normalizeNoteLookupPath(notePath)
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_type, src_path, src_id, dst_kind, dst_id, dst_path, lang, label, snippet, meta_json, updated_at
		FROM doc_links
		WHERE dst_kind = 'note' AND dst_path = ?
		ORDER BY updated_at DESC, src_type, src_path
		LIMIT ?
	`, notePath, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return readDocLinks(rows)
}

// DocLinksForNotes returns links pointing to multiple notes (dst_kind = 'note'),
// grouped by dst_path. Results are ordered by updated_at desc per note and capped
// to limitPerNote for each note path.
func (s *Store) DocLinksForNotes(ctx context.Context, notePaths []string, limitPerNote int) (map[string][]codeanchor.DocLink, error) {
	out := make(map[string][]codeanchor.DocLink, len(notePaths))
	if len(notePaths) == 0 {
		return out, nil
	}
	if limitPerNote <= 0 {
		limitPerNote = 10
	}

	seen := make(map[string]struct{}, len(notePaths))
	normalized := make([]string, 0, len(notePaths))
	for _, p := range notePaths {
		p = normalizeNoteLookupPath(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		normalized = append(normalized, p)
	}
	if len(normalized) == 0 {
		return out, nil
	}

	for batch := range slices.Chunk(normalized, 400) {
		holders, args := placeholders(batch)
		query := `
			SELECT src_type, src_path, src_id, dst_kind, dst_id, dst_path, lang, label, snippet, meta_json, updated_at
			FROM doc_links
			WHERE dst_kind = 'note' AND dst_path IN (` + holders + `)
			ORDER BY updated_at DESC, src_type, src_path
		`
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var l codeanchor.DocLink
			if err := rows.Scan(&l.SrcType, &l.SrcPath, &l.SrcID, &l.DstKind, &l.DstID, &l.DstPath, &l.Lang, &l.Label, &l.Snippet, &l.MetaJSON, &l.UpdatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if len(out[l.DstPath]) >= limitPerNote {
				continue
			}
			out[l.DstPath] = append(out[l.DstPath], l)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}

	return out, nil
}

// DocLinksFromCodePath returns links originating from a code file (typically coderefs) to notes/anchors.
func (s *Store) DocLinksFromCodePath(ctx context.Context, srcPath string, limit int) ([]codeanchor.DocLink, error) {
	if limit <= 0 {
		limit = 10
	}
	srcPath = normalizeCodeLookupPath(srcPath)
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_type, src_path, src_id, dst_kind, dst_id, dst_path, lang, label, snippet, meta_json, updated_at
		FROM doc_links
		WHERE src_type = 'code' AND src_path = ?
		ORDER BY updated_at DESC, dst_kind, dst_path
		LIMIT ?
	`, srcPath, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return readDocLinks(rows)
}

// DocLinksForAnchors returns links pointing to multiple anchors (dst_kind = 'anchor'),
// grouped by dst_id. Results are ordered by updated_at desc per anchor and capped
// to limitPerAnchor for each anchor ID.
func (s *Store) DocLinksForAnchors(ctx context.Context, anchorIDs []string, limitPerAnchor int) (map[string][]codeanchor.DocLink, error) {
	out := make(map[string][]codeanchor.DocLink, len(anchorIDs))
	if len(anchorIDs) == 0 {
		return out, nil
	}
	if limitPerAnchor <= 0 {
		limitPerAnchor = 10
	}

	seen := make(map[string]struct{}, len(anchorIDs))
	normalized := make([]string, 0, len(anchorIDs))
	for _, id := range anchorIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	if len(normalized) == 0 {
		return out, nil
	}

	for batch := range slices.Chunk(normalized, 400) {
		holders, args := placeholders(batch)
		query := `
			SELECT src_type, src_path, src_id, dst_kind, dst_id, dst_path, lang, label, snippet, meta_json, updated_at
			FROM doc_links
			WHERE dst_kind = 'anchor' AND dst_id IN (` + holders + `)
			ORDER BY updated_at DESC, src_type, src_path
		`
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var l codeanchor.DocLink
			if err := rows.Scan(&l.SrcType, &l.SrcPath, &l.SrcID, &l.DstKind, &l.DstID, &l.DstPath, &l.Lang, &l.Label, &l.Snippet, &l.MetaJSON, &l.UpdatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if len(out[l.DstID]) >= limitPerAnchor {
				continue
			}
			out[l.DstID] = append(out[l.DstID], l)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}

	return out, nil
}

// DeleteDocLinksByPath deletes links for a source path.
func (s *Store) DeleteDocLinksByPath(ctx context.Context, srcPath string) error {
	// The caller owns source classification. This neutral delete must not infer
	// note ownership from a filename extension.
	srcPath = normalizeLookupPath(srcPath)
	ctx = indexingperf.WithOp(ctx, "intel.delete_doc_links")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = ?`, srcPath)
		return err
	})
}

func readDocLinks(rows *sql.Rows) ([]codeanchor.DocLink, error) {
	var out []codeanchor.DocLink
	for rows.Next() {
		var l codeanchor.DocLink
		if err := rows.Scan(&l.SrcType, &l.SrcPath, &l.SrcID, &l.DstKind, &l.DstID, &l.DstPath, &l.Lang, &l.Label, &l.Snippet, &l.MetaJSON, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
