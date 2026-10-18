package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

// IntelAnchors returns all stored intel anchors.
func (s *Store) IntelAnchors(ctx context.Context) ([]codeanchor.IntelAnchor, error) {
	rows, err := s.db.QueryContext(ctx, `
                SELECT anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
                       start_byte, end_byte, start_line, end_line, fingerprint, updated_at
                FROM intel_code_anchors
                ORDER BY path, fqn, anchor_id
        `)
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

// IntelAnchorMetas returns lightweight intel anchor metadata without doc/comment payloads.
func (s *Store) IntelAnchorMetas(ctx context.Context) ([]codeanchor.IntelAnchorMeta, error) {
	rows, err := s.db.QueryContext(ctx, `
                SELECT anchor_id, lang, kind, path, symbol, fqn, fingerprint, updated_at
                FROM intel_code_anchors
                ORDER BY path, fqn, anchor_id
        `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var anchors []codeanchor.IntelAnchorMeta
	for rows.Next() {
		var a codeanchor.IntelAnchorMeta
		if err := rows.Scan(&a.AnchorID, &a.Lang, &a.Kind, &a.Path, &a.Symbol, &a.FQN, &a.Fingerprint, &a.UpdatedAt); err != nil {
			return nil, err
		}
		anchors = append(anchors, a)
	}
	return anchors, rows.Err()
}

// IntelAnchorsByPath returns intel anchors for a single code path.
func (s *Store) IntelAnchorsByPath(ctx context.Context, path string) ([]codeanchor.IntelAnchor, error) {
	anchorsByPath, err := s.IntelAnchorsByPaths(ctx, []string{path})
	if err != nil {
		return nil, err
	}
	path = normalizeCodeLookupPath(path)
	return anchorsByPath[path], nil
}

// IntelAnchorsByPaths returns intel anchors grouped by code path.
func (s *Store) IntelAnchorsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.IntelAnchor, error) {
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		path = normalizeCodeLookupPath(path)
		if path == "" {
			continue
		}
		normalized = append(normalized, path)
	}
	paths = normalizeNonEmptyStrings(normalized)
	if len(paths) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}

	holders := make([]string, len(paths))
	args := make([]any, 0, len(paths))
	for i, path := range paths {
		holders[i] = "?"
		args = append(args, path)
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
		       start_byte, end_byte, start_line, end_line, fingerprint, updated_at
		FROM intel_code_anchors
		WHERE path IN (%s)
		ORDER BY path, fqn, anchor_id
	`, strings.Join(holders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	anchorsByPath := make(map[string][]codeanchor.IntelAnchor, len(paths))
	for rows.Next() {
		var a codeanchor.IntelAnchor
		if err := rows.Scan(&a.AnchorID, &a.Lang, &a.Kind, &a.Path, &a.Symbol, &a.FQN, &a.Signature, &a.DocComment, &a.StartByte, &a.EndByte, &a.StartLine, &a.EndLine, &a.Fingerprint, &a.UpdatedAt); err != nil {
			return nil, err
		}
		anchorsByPath[a.Path] = append(anchorsByPath[a.Path], a)
	}
	return anchorsByPath, rows.Err()
}

// CallAnchorsByPaths returns callee anchors for calls originating from the provided file paths.
// Results are keyed by caller path and capped by perPathLimit and perFQNLimit.
func (s *Store) CallAnchorsByPaths(ctx context.Context, paths []string, perPathLimit int, perFQNLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	if perPathLimit <= 0 {
		perPathLimit = 50
	}
	if perFQNLimit <= 0 {
		perFQNLimit = 3
	}
	normalized := make([]string, 0, len(paths))
	for _, p := range paths {
		p = normalizeCodeLookupPath(p)
		if p == "" {
			continue
		}
		normalized = append(normalized, p)
	}
	paths = normalizeNonEmptyStrings(normalized)
	if len(paths) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}

	args := make([]any, len(paths))
	for i, p := range paths {
		args[i] = p
	}

	valueRows := strings.TrimSuffix(strings.Repeat("(?),", len(paths)), ",")
	query := fmt.Sprintf(`
		WITH requested(path) AS (VALUES %s), candidates AS (
		SELECT DISTINCT
			caller.path AS caller_path,
			callee.anchor_id, callee.lang, callee.kind, callee.path, callee.symbol, callee.fqn,
			callee.signature, callee.doc_comment, callee.start_byte, callee.end_byte,
			callee.start_line, callee.end_line, callee.fingerprint, callee.updated_at
		FROM intel_edges e
		JOIN intel_code_anchors caller ON caller.id = e.src_row_id
		JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
		WHERE e.kind = 'calls' AND e.src_type = 'anchor' AND e.dst_type = 'anchor' AND caller.path IN (SELECT path FROM requested)
		UNION
		SELECT r.source_path,
			callee.anchor_id, callee.lang, callee.kind, callee.path, callee.symbol, callee.fqn,
			callee.signature, callee.doc_comment, callee.start_byte, callee.end_byte,
			callee.start_line, callee.end_line, callee.fingerprint, callee.updated_at
		FROM intel_go_derived_relationships r
		JOIN intel_go_package_relationship_state st ON st.package_key=r.package_key AND st.valid=1
		JOIN intel_code_anchors callee ON callee.fqn=r.target_fqn
		WHERE r.kind='calls' AND r.source_path IN (SELECT path FROM requested)
		)
		SELECT * FROM candidates ORDER BY caller_path, fqn, anchor_id
	`, valueRows)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return readCallAnchors(rows, perPathLimit, perFQNLimit)
}

// CallAnchorsByCallerIDs returns callee anchors for calls originating from the provided caller anchor IDs.
// Results are keyed by caller anchor ID and capped by perCallerLimit and perFQNLimit.
func (s *Store) CallAnchorsByCallerIDs(ctx context.Context, callerIDs []string, perCallerLimit int, perFQNLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	if perCallerLimit <= 0 {
		perCallerLimit = 50
	}
	if perFQNLimit <= 0 {
		perFQNLimit = 3
	}
	normalized := normalizeNonEmptyStrings(callerIDs)
	if len(normalized) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}

	holders, args := placeholders(normalized)

	query := fmt.Sprintf(`
		WITH requested AS (
			SELECT anchor_id, path, fqn FROM intel_code_anchors WHERE anchor_id IN (%s)
		), candidates AS (
		SELECT DISTINCT
			caller.anchor_id AS caller_id,
			callee.anchor_id, callee.lang, callee.kind, callee.path, callee.symbol, callee.fqn,
			callee.signature, callee.doc_comment, callee.start_byte, callee.end_byte,
			callee.start_line, callee.end_line, callee.fingerprint, callee.updated_at
		FROM intel_edges e
		JOIN intel_code_anchors caller ON caller.id = e.src_row_id
		JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
		WHERE e.kind = 'calls' AND e.src_type = 'anchor' AND e.dst_type = 'anchor' AND caller.anchor_id IN (SELECT anchor_id FROM requested)
		UNION
		SELECT requested.anchor_id,
			callee.anchor_id, callee.lang, callee.kind, callee.path, callee.symbol, callee.fqn,
			callee.signature, callee.doc_comment, callee.start_byte, callee.end_byte,
			callee.start_line, callee.end_line, callee.fingerprint, callee.updated_at
		FROM requested
		JOIN intel_go_derived_relationships r ON r.kind='calls' AND r.source_fqn=requested.fqn AND r.source_path=requested.path
		JOIN intel_go_package_relationship_state st ON st.package_key=r.package_key AND st.valid=1
		JOIN intel_code_anchors callee ON callee.fqn=r.target_fqn
		)
		SELECT * FROM candidates ORDER BY caller_id, fqn, anchor_id
	`, holders)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return readCallAnchors(rows, perCallerLimit, perFQNLimit)
}

func readCallAnchors(rows *sql.Rows, perCallerLimit, perFQNLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	out := make(map[string][]codeanchor.IntelAnchor)
	fqnCounts := make(map[string]map[string]int)
	for rows.Next() {
		var caller string
		var anchor codeanchor.IntelAnchor
		if err := rows.Scan(
			&caller,
			&anchor.AnchorID,
			&anchor.Lang,
			&anchor.Kind,
			&anchor.Path,
			&anchor.Symbol,
			&anchor.FQN,
			&anchor.Signature,
			&anchor.DocComment,
			&anchor.StartByte,
			&anchor.EndByte,
			&anchor.StartLine,
			&anchor.EndLine,
			&anchor.Fingerprint,
			&anchor.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(out[caller]) >= perCallerLimit {
			continue
		}
		if _, ok := fqnCounts[caller]; !ok {
			fqnCounts[caller] = map[string]int{}
		}
		fqn := anchor.FQN
		if fqnCounts[caller][fqn] >= perFQNLimit {
			continue
		}
		out[caller] = append(out[caller], anchor)
		fqnCounts[caller][fqn]++
	}
	return out, rows.Err()
}
