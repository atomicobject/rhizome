package store

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

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
