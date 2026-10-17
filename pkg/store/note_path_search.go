package store

import (
	"context"
	"fmt"
	pathutil "github.com/atomicobject/rhizome/pkg/paths"
	"strings"
)

func (s *Store) CurrentNotePathsByPathPrefix(ctx context.Context, normalizedPath string) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	normalizedPath = string(pathutil.Normalize(strings.TrimSpace(normalizedPath)))
	if normalizedPath == "" {
		return []string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT path
		FROM notes
		WHERE indexed_at > 0 AND (path = ? OR path LIKE ?)
		ORDER BY path
	`, normalizedPath, normalizedPath+"/%")
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
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNotePathsByFirstSegmentPrefix(ctx context.Context, prefix string, exact bool) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return []string{}, nil
	}
	query := `
		SELECT path
		FROM notes
		WHERE indexed_at > 0 AND first_segment_norm = ?
		ORDER BY path
	`
	args := []any{prefix}
	if !exact {
		query = `
			SELECT path
			FROM notes
			WHERE indexed_at > 0 AND first_segment_norm LIKE ?
			ORDER BY path
		`
		args = []any{prefix + "%"}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
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
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNotePathsBySearchTokenPrefixes(ctx context.Context, scope NoteSearchScope, tokens []string, sameSegment bool) ([]string, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, err
	}
	tokens = trimNonEmptyStrings(tokens)
	if len(tokens) == 0 {
		return []string{}, nil
	}

	var sql strings.Builder
	sql.WriteString(`SELECT DISTINCT n.path FROM note_search_terms t0 JOIN notes n ON n.id = t0.note_id `)
	args := make([]any, 0, len(tokens)+2)
	for i := 1; i < len(tokens); i++ {
		prev := i - 1
		sql.WriteString(fmt.Sprintf(
			`JOIN note_search_terms t%d ON t%d.note_id = t0.note_id AND t%d.scope = t0.scope AND t%d.token_ordinal > t%d.token_ordinal `,
			i, i, i, i, prev,
		))
		if sameSegment {
			sql.WriteString(fmt.Sprintf(`AND t%d.segment_ordinal = t0.segment_ordinal `, i))
		}
		sql.WriteString(fmt.Sprintf(`AND t%d.token_text LIKE ? `, i))
		args = append(args, tokens[i]+"%")
	}
	sql.WriteString(`WHERE n.indexed_at > 0 AND t0.scope = ? AND t0.token_text LIKE ? ORDER BY n.path`)
	args = append(args, int(scope), tokens[0]+"%")
	rows, err := s.db.QueryContext(ctx, sql.String(), args...)
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
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) CurrentNotePathsByFindPattern(ctx context.Context, pattern string) ([]string, bool, error) {
	state, err := s.GetNoteMetadataState(ctx)
	if err != nil || !state.Ready || state.LoadedAt == 0 {
		return nil, false, err
	}
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return []string{}, false, nil
	}
	if strings.ContainsAny(pattern, "*?") || strings.Count(pattern, "/") > 1 {
		return nil, false, nil
	}
	// This returns a candidate set, not final truth. The planner still runs
	// obsidian.FuzzyMatch on these paths so SQL can stay conservative without
	// changing matcher semantics.
	if strings.Contains(pattern, "/") {
		dirPattern, contentPattern := splitFindDirectoryAndContent(pattern)
		if strings.TrimSpace(dirPattern) == "" {
			return nil, false, nil
		}
		dirPaths, err := s.CurrentNotePathsByFirstSegmentPrefix(ctx, dirPattern, len(dirPattern) > 1)
		if err != nil {
			return nil, false, err
		}
		if strings.TrimSpace(contentPattern) == "" {
			return dirPaths, true, nil
		}
		tokens := searchTokens(contentPattern)
		if len(tokens) == 0 {
			return nil, false, nil
		}
		contentPaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopeContent, tokens, false)
		if err != nil {
			return nil, false, err
		}
		return intersectSortedPaths(dirPaths, contentPaths), true, nil
	}

	tokens := searchTokens(pattern)
	if len(tokens) == 0 {
		return nil, false, nil
	}
	if strings.Contains(pattern, ".") {
		segmentPaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopeSegment, tokens, true)
		if err != nil {
			return nil, false, err
		}
		titlePaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopeTitle, tokens, false)
		if err != nil {
			return nil, false, err
		}
		return unionSortedPaths(segmentPaths, titlePaths), true, nil
	}

	pathPaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopePath, tokens, false)
	if err != nil {
		return nil, false, err
	}
	titlePaths, err := s.CurrentNotePathsBySearchTokenPrefixes(ctx, NoteSearchScopeTitle, tokens, false)
	if err != nil {
		return nil, false, err
	}
	return unionSortedPaths(pathPaths, titlePaths), true, nil
}
