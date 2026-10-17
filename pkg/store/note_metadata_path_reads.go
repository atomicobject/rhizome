package store

import (
	"context"
	"fmt"
	pathutil "github.com/atomicobject/rhizome/pkg/paths"
	"sort"
	"strings"
)

func (s *Store) noteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]NoteMetadataRow, error) {
	return noteMetadataRowsByPaths(ctx, s.db, paths)
}

func noteMetadataRowsByPaths(ctx context.Context, queryer noteMetadataQueryer, paths []string) (map[string]NoteMetadataRow, error) {
	if len(paths) == 0 {
		return map[string]NoteMetadataRow{}, nil
	}
	out := make(map[string]NoteMetadataRow, len(paths))
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := queryer.QueryContext(ctx, fmt.Sprintf(`
			SELECT n.id, n.path, COALESCE(n.title, ''), COALESCE(n.content_hash, ''), COALESCE(n.indexer_version, ''),
				n.mtime, n.size, n.indexed_at, COALESCE(n.format_id, 'markdown'),
				COALESCE(p.provider_version, ''), COALESCE(p.projection_version, ''), COALESCE(p.source_content_hash, ''),
				COALESCE(p.status, 'stale'), COALESCE(p.diagnostic_code, ''), COALESCE(p.diagnostic_detail, ''), COALESCE(p.updated_at, 0)
			FROM notes n
			LEFT JOIN note_projection_state p ON p.note_id = n.id
			WHERE n.indexed_at > 0 AND n.path IN (%s)
		`, placeholders), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row NoteMetadataRow
			if err := scanNoteMetadataRow(rows, &row); err != nil {
				rows.Close()
				return nil, err
			}
			out[row.Path] = row
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func canonicalNoteMetadataPaths(rawPaths []string) ([]string, error) {
	if len(rawPaths) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(rawPaths))
	seen := make(map[string]struct{}, len(rawPaths))
	for _, rawPath := range rawPaths {
		canonical, err := pathutil.CleanNotePath(rawPath)
		if err != nil || canonical.String() != rawPath {
			return nil, fmt.Errorf("note metadata path %q must be canonical and vault-relative", rawPath)
		}
		if _, duplicate := seen[canonical.String()]; duplicate {
			return nil, fmt.Errorf("note metadata path %q is repeated", canonical)
		}
		seen[canonical.String()] = struct{}{}
		paths = append(paths, canonical.String())
	}
	sort.Strings(paths)
	return paths, nil
}
