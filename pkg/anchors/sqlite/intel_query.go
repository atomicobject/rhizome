package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
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

// CallerAnchorsByCalleeIDs returns caller anchors for the provided callee anchor IDs.
// Results are keyed by callee anchor ID and capped by perCalleeLimit.
func (s *Store) CallerAnchorsByCalleeIDs(ctx context.Context, calleeIDs []string, perCalleeLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	return s.callerAnchorsByCalleeIDs(ctx, calleeIDs, nil, perCalleeLimit, false)
}

// CallerTestAnchorsByCalleeIDs returns caller anchors from recognized test
// paths. Filtering happens before the per-target cap so production callers
// cannot crowd test relationship evidence out of a bounded result.
func (s *Store) CallerTestAnchorsByCalleeIDs(ctx context.Context, calleeIDs, pathPrefixes []string, perCalleeLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	return s.callerAnchorsByCalleeIDs(ctx, calleeIDs, pathPrefixes, perCalleeLimit, true)
}

func (s *Store) callerAnchorsByCalleeIDs(ctx context.Context, calleeIDs, pathPrefixes []string, perCalleeLimit int, testsOnly bool) (map[string][]codeanchor.IntelAnchor, error) {
	if perCalleeLimit <= 0 {
		perCalleeLimit = 20
	}
	normalized := normalizeNonEmptyStrings(calleeIDs)
	if len(normalized) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}

	holders := make([]string, len(normalized))
	args := make([]any, 0, len(normalized))
	for i, id := range normalized {
		holders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		WITH requested AS (
			SELECT anchor_id, fqn FROM intel_code_anchors WHERE anchor_id IN (%s)
		), candidates AS (
		SELECT DISTINCT
			callee.anchor_id AS callee_id,
			caller.anchor_id, caller.lang, caller.kind, caller.path, caller.symbol, caller.fqn,
			caller.signature, caller.doc_comment, caller.start_byte, caller.end_byte,
			caller.start_line, caller.end_line, caller.fingerprint, caller.updated_at
		FROM intel_edges e
		JOIN intel_code_anchors caller ON caller.id = e.src_row_id
		JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
		WHERE e.kind = 'calls' AND e.src_type = 'anchor' AND e.dst_type = 'anchor' AND callee.anchor_id IN (SELECT anchor_id FROM requested)
		UNION
		SELECT requested.anchor_id,
			caller.anchor_id, caller.lang, caller.kind, caller.path, caller.symbol, caller.fqn,
			caller.signature, caller.doc_comment, caller.start_byte, caller.end_byte,
			caller.start_line, caller.end_line, caller.fingerprint, caller.updated_at
		FROM requested
		JOIN intel_go_derived_relationships r ON r.kind='calls' AND r.target_fqn=requested.fqn
		JOIN intel_go_package_relationship_state st ON st.package_key=r.package_key AND st.valid=1
		JOIN intel_code_anchors caller ON caller.fqn=r.source_fqn AND caller.path=r.source_path
		)
		SELECT * FROM candidates
		ORDER BY callee_id, fqn, anchor_id
	`, strings.Join(holders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.IntelAnchor, len(normalized))
	counts := make(map[string]int, len(normalized))
	for rows.Next() {
		var calleeID string
		var anchor codeanchor.IntelAnchor
		if err := rows.Scan(
			&calleeID,
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
		if testsOnly && (!codeanchor.IsTestPath(anchor.Path) || !intelPathAllowed(anchor.Path, pathPrefixes)) {
			continue
		}
		if counts[calleeID] >= perCalleeLimit {
			continue
		}
		out[calleeID] = append(out[calleeID], anchor)
		counts[calleeID]++
	}
	return out, rows.Err()
}

func intelPathAllowed(value string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	value = filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	for _, prefix := range prefixes {
		prefix = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(strings.TrimSpace(prefix))), "/")
		if prefix == "" || prefix == "." || prefix == "/" || value == prefix || strings.HasPrefix(value, prefix+"/") {
			return true
		}
	}
	return false
}

// ImplementerAnchorsByTargetIDs returns exact local Go implementers whose
// package proof is still valid. Results are keyed by the requested interface
// anchor ID and capped after deterministic ordering.
func (s *Store) ImplementerAnchorsByTargetIDs(ctx context.Context, targetIDs []string, perTargetLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	return s.implementerAnchorsByTargetIDs(ctx, targetIDs, nil, perTargetLimit)
}

// ImplementerAnchorsByTargetIDsFiltered applies path eligibility before the
// per-target cap so an excluded implementer cannot crowd out an eligible one.
func (s *Store) ImplementerAnchorsByTargetIDsFiltered(ctx context.Context, targetIDs, pathPrefixes []string, perTargetLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	return s.implementerAnchorsByTargetIDs(ctx, targetIDs, pathPrefixes, perTargetLimit)
}

func (s *Store) implementerAnchorsByTargetIDs(ctx context.Context, targetIDs, pathPrefixes []string, perTargetLimit int) (map[string][]codeanchor.IntelAnchor, error) {
	if perTargetLimit <= 0 {
		perTargetLimit = 20
	}
	targetIDs = normalizeNonEmptyStrings(targetIDs)
	if len(targetIDs) == 0 {
		return map[string][]codeanchor.IntelAnchor{}, nil
	}
	holders := make([]string, len(targetIDs))
	args := make([]any, len(targetIDs))
	for i, id := range targetIDs {
		holders[i], args[i] = "?", id
	}
	pathPredicate := ""
	for _, prefix := range normalizeNonEmptyStrings(pathPrefixes) {
		prefix = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(prefix)), "/")
		if prefix == "" || prefix == "." || prefix == "/" {
			pathPredicate = ""
			args = args[:len(targetIDs)]
			break
		}
		if pathPredicate != "" {
			pathPredicate += " OR "
		}
		pathPredicate += `(impl.path=? OR impl.path LIKE ? ESCAPE '\')`
		args = append(args, prefix, escapeLike(prefix)+"/%")
	}
	if pathPredicate != "" {
		pathPredicate = " AND (" + pathPredicate + ")"
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT target.anchor_id,
			impl.anchor_id, impl.lang, impl.kind, impl.path, impl.symbol, impl.fqn,
			impl.signature, impl.doc_comment, impl.start_byte, impl.end_byte,
			impl.start_line, impl.end_line, impl.fingerprint, impl.updated_at
		FROM intel_code_anchors target
		JOIN intel_go_derived_relationships r ON r.kind='implements' AND r.target_fqn=target.fqn
		JOIN intel_go_package_relationship_state st ON st.package_key=r.package_key AND st.valid=1
		JOIN intel_code_anchors impl ON impl.fqn=r.source_fqn AND impl.path=r.source_path
		WHERE target.anchor_id IN (%s)%s
		ORDER BY target.anchor_id, impl.fqn, impl.anchor_id
	`, strings.Join(holders, ","), pathPredicate), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]codeanchor.IntelAnchor, len(targetIDs))
	counts := make(map[string]int, len(targetIDs))
	for rows.Next() {
		var targetID string
		var anchor codeanchor.IntelAnchor
		if err := rows.Scan(
			&targetID, &anchor.AnchorID, &anchor.Lang, &anchor.Kind, &anchor.Path,
			&anchor.Symbol, &anchor.FQN, &anchor.Signature, &anchor.DocComment,
			&anchor.StartByte, &anchor.EndByte, &anchor.StartLine, &anchor.EndLine,
			&anchor.Fingerprint, &anchor.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if counts[targetID] >= perTargetLimit {
			continue
		}
		out[targetID] = append(out[targetID], anchor)
		counts[targetID]++
	}
	return out, rows.Err()
}

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

// IntelDocSections returns all stored intel doc sections.
func (s *Store) IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error) {
	rows, err := s.db.QueryContext(ctx, `
                SELECT section_id, path, title, level, start_byte, end_byte, content, fingerprint, updated_at
                FROM intel_doc_sections
                ORDER BY path, start_byte, section_id
        `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sections []codeanchor.IntelDocSection
	for rows.Next() {
		var sct codeanchor.IntelDocSection
		if err := rows.Scan(&sct.SectionID, &sct.Path, &sct.Title, &sct.Level, &sct.StartByte, &sct.EndByte, &sct.Content, &sct.Fingerprint, &sct.UpdatedAt); err != nil {
			return nil, err
		}
		sections = append(sections, sct)
	}
	return sections, rows.Err()
}

// IntelDocSectionMetas returns lightweight intel doc section metadata without content.
func (s *Store) IntelDocSectionMetas(ctx context.Context) ([]codeanchor.IntelDocSectionMeta, error) {
	rows, err := s.db.QueryContext(ctx, `
                SELECT section_id, path, title, level, start_byte, end_byte, fingerprint, updated_at
                FROM intel_doc_sections
                ORDER BY path, start_byte, section_id
        `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sections []codeanchor.IntelDocSectionMeta
	for rows.Next() {
		var sct codeanchor.IntelDocSectionMeta
		if err := rows.Scan(&sct.SectionID, &sct.Path, &sct.Title, &sct.Level, &sct.StartByte, &sct.EndByte, &sct.Fingerprint, &sct.UpdatedAt); err != nil {
			return nil, err
		}
		sections = append(sections, sct)
	}
	return sections, rows.Err()
}

// IntelNotePaths returns distinct note paths that have indexed doc sections.
func (s *Store) IntelNotePaths(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT path
		FROM intel_doc_sections
		ORDER BY path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		if strings.TrimSpace(path) == "" {
			continue
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

// IntelNoteMtimes returns stored note mtimes (unix seconds) per indexed note path.
// Used for mtime-based change detection during incremental indexing.
func (s *Store) IntelNoteMtimes(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, mtime
		FROM notes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var path string
		var mtime int64
		if err := rows.Scan(&path, &mtime); err != nil {
			return nil, err
		}
		if strings.TrimSpace(path) == "" {
			continue
		}
		out[path] = mtime
	}
	return out, rows.Err()
}

// IntelNoteIndexMeta returns note index metadata for incremental note indexing decisions.
func (s *Store) IntelNoteIndexMeta(ctx context.Context) (map[string]codeanchor.NoteIndexMeta, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, content_hash, indexer_version, mtime
		FROM notes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]codeanchor.NoteIndexMeta)
	for rows.Next() {
		var path string
		var hash string
		var version string
		var mtime int64
		if err := rows.Scan(&path, &hash, &version, &mtime); err != nil {
			return nil, err
		}
		if strings.TrimSpace(path) == "" {
			continue
		}
		out[path] = codeanchor.NoteIndexMeta{
			ContentHash:    hash,
			IndexerVersion: version,
			Mtime:          mtime,
		}
	}
	return out, rows.Err()
}

// IntelCodeMtimes returns the latest updated_at (unix seconds) per indexed code file path.
// Used for mtime-based change detection during incremental code indexing.
func (s *Store) IntelCodeMtimes(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, MAX(updated_at)
		FROM intel_code_anchors
		GROUP BY path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var path string
		var mtime int64
		if err := rows.Scan(&path, &mtime); err != nil {
			return nil, err
		}
		if strings.TrimSpace(path) == "" {
			continue
		}
		out[path] = mtime
	}
	return out, rows.Err()
}

// IntelDocSectionIDsByPath returns section IDs in (start_byte, section_id) order
// without loading section bodies or other metadata.
func (s *Store) IntelDocSectionIDsByPath(ctx context.Context, path string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT section_id
		FROM intel_doc_sections
		WHERE path = ?
		ORDER BY start_byte, section_id
	`, path)
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

// IntelDocSectionsByPath returns stored intel doc sections for a single note path.
func (s *Store) IntelDocSectionsByPath(ctx context.Context, path string) ([]codeanchor.IntelDocSection, error) {
	rows, err := s.db.QueryContext(ctx, `
                SELECT section_id, path, title, level, start_byte, end_byte, content, fingerprint, updated_at
                FROM intel_doc_sections
                WHERE path = ?
                ORDER BY start_byte, section_id
        `, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sections []codeanchor.IntelDocSection
	for rows.Next() {
		var sct codeanchor.IntelDocSection
		if err := rows.Scan(&sct.SectionID, &sct.Path, &sct.Title, &sct.Level, &sct.StartByte, &sct.EndByte, &sct.Content, &sct.Fingerprint, &sct.UpdatedAt); err != nil {
			return nil, err
		}
		sections = append(sections, sct)
	}
	return sections, rows.Err()
}

// EmbeddingsByChunkIDs returns embeddings keyed by chunk ID.
func (s *Store) EmbeddingsByChunkIDs(ctx context.Context, chunkIDs []string) (map[string]embeddings.Embedding, error) {
	ids := normalizeNonEmptyStrings(chunkIDs)
	if len(ids) == 0 {
		return map[string]embeddings.Embedding{}, nil
	}
	if err := s.requireIntelVecPrimary(ctx); err != nil {
		return nil, err
	}

	const batchSize = 400
	out := make(map[string]embeddings.Embedding, len(ids))

	type chunkMeta struct {
		rowID int64
		dims  int
	}
	metaByID := make(map[string]chunkMeta, len(ids))
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		holders := strings.Repeat("?,", len(batch))
		holders = strings.TrimSuffix(holders, ",")
		query := fmt.Sprintf(`
			SELECT chunk_id, chunk_row_id, dimensions
			FROM intel_embeddings
			WHERE chunk_id IN (%s)
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
			var id string
			var rowID int64
			var dims int
			if err := rows.Scan(&id, &rowID, &dims); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if dims <= 0 {
				continue
			}
			metaByID[id] = chunkMeta{rowID: rowID, dims: dims}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}

	grouped := make(map[int]map[int64]string)
	for id, meta := range metaByID {
		if grouped[meta.dims] == nil {
			grouped[meta.dims] = make(map[int64]string)
		}
		grouped[meta.dims][meta.rowID] = id
	}

	for dims, groupedIDs := range grouped {
		table := intelVecTableName(dims)
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		rowIDs := make([]int64, 0, len(groupedIDs))
		for rowID := range groupedIDs {
			rowIDs = append(rowIDs, rowID)
		}
		sort.Slice(rowIDs, func(i, j int) bool { return rowIDs[i] < rowIDs[j] })
		for start := 0; start < len(rowIDs); start += batchSize {
			end := start + batchSize
			if end > len(rowIDs) {
				end = len(rowIDs)
			}
			batch := rowIDs[start:end]
			holders := strings.Repeat("?,", len(batch))
			holders = strings.TrimSuffix(holders, ",")
			query := fmt.Sprintf(`
				SELECT chunk_id, embedding
				FROM %s
				WHERE chunk_id IN (%s)
			`, table, holders)
			args := make([]any, 0, len(batch))
			for _, rowID := range batch {
				args = append(args, rowID)
			}
			rows, err := s.db.QueryContext(ctx, query, args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var rowID int64
				var blob []byte
				if err := rows.Scan(&rowID, &blob); err != nil {
					_ = rows.Close()
					return nil, err
				}
				if len(blob) == 0 {
					continue
				}
				if id, ok := groupedIDs[rowID]; ok {
					out[id] = bytesToEmbed(blob)
				}
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return nil, err
			}
			_ = rows.Close()
		}
	}
	return out, nil
}

// IntelChunkEmbeddingStates returns chunk hashes for anchors that have embeddings, keyed by owner ID.
func (s *Store) IntelChunkEmbeddingStates(ctx context.Context, ownerIDs []string) (map[string]map[int]string, error) {
	out := make(map[string]map[int]string, len(ownerIDs))
	ids := normalizeNonEmptyStrings(ownerIDs)
	if len(ids) == 0 {
		return out, nil
	}
	const batchSize = 400
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		holders := strings.Repeat("?,", len(batch))
		holders = strings.TrimSuffix(holders, ",")
		query := fmt.Sprintf(`
			SELECT c.owner_id, c.ord, c.content_hash
			FROM intel_chunks c
			JOIN intel_embeddings e ON e.chunk_row_id = c.id
			WHERE c.owner_type = 'anchor' AND c.owner_id IN (%s)
			ORDER BY c.owner_id, c.ord
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
			var ownerID, hash string
			var ord int
			if err := rows.Scan(&ownerID, &ord, &hash); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if out[ownerID] == nil {
				out[ownerID] = make(map[int]string)
			}
			if _, ok := out[ownerID][ord]; ok {
				continue
			}
			out[ownerID][ord] = hash
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// EmbeddingStats returns the total embedding count and distinct dimensions.
func (s *Store) EmbeddingStats(ctx context.Context) (int, []int, error) {
	if s == nil {
		return 0, nil, nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_embeddings`).Scan(&count); err != nil {
		return 0, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT dimensions FROM intel_embeddings ORDER BY dimensions`)
	if err != nil {
		return count, nil, err
	}
	defer rows.Close()
	var dims []int
	for rows.Next() {
		var d int
		if err := rows.Scan(&d); err != nil {
			return count, dims, err
		}
		dims = append(dims, d)
	}
	if err := rows.Err(); err != nil {
		return count, dims, err
	}
	return count, dims, nil
}

// IntelEdges returns all stored intel edges.
func (s *Store) IntelEdges(ctx context.Context) ([]codeanchor.IntelEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
                SELECT
                        CASE WHEN e.src_type = 'anchor' THEN src.anchor_id ELSE srcs.section_id END AS src_id,
                        CASE WHEN e.dst_type = 'anchor' THEN dst.anchor_id ELSE dsts.section_id END AS dst_id,
                        e.kind,
                        e.meta_json
                FROM intel_edges e
                LEFT JOIN intel_code_anchors src ON src.id = e.src_row_id AND e.src_type = 'anchor'
                LEFT JOIN intel_doc_sections srcs ON srcs.id = e.src_row_id AND e.src_type = 'doc_section'
                LEFT JOIN intel_code_anchors dst ON dst.id = e.dst_row_id AND e.dst_type = 'anchor'
                LEFT JOIN intel_doc_sections dsts ON dsts.id = e.dst_row_id AND e.dst_type = 'doc_section'
                ORDER BY src_id, dst_id
        `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []codeanchor.IntelEdge
	for rows.Next() {
		var e codeanchor.IntelEdge
		if err := rows.Scan(&e.SrcID, &e.DstID, &e.Kind, &e.MetaJSON); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}
