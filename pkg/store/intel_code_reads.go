package store

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

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
                SELECT section_id, path, title, level, start_byte, end_byte, content, fingerprint, updated_at, source_node_id
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
		if err := rows.Scan(&sct.SectionID, &sct.Path, &sct.Title, &sct.Level, &sct.StartByte, &sct.EndByte, &sct.Content, &sct.Fingerprint, &sct.UpdatedAt, &sct.SourceNodeID); err != nil {
			return nil, err
		}
		sections = append(sections, sct)
	}
	return sections, rows.Err()
}
