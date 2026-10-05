package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

type IntelSearchRow struct {
	Type     string
	ID       string
	Path     string
	Title    string
	Snippet  string
	TitleHit bool
	BodyHit  bool
	Score    float64
	Lang     string
	Kind     string
	FQN      string
}

// IntelSearchFilters limits FTS candidates before the ranked query LIMIT is
// applied. NoteTypes are resolved types of the owning note, so they include
// doc sections belonging to that note.
type IntelSearchFilters struct {
	Types        []string
	PathPrefixes []string
	NoteTypes    []string
	ExactSymbols []string
	TestsOnly    bool
	ExcludeTests bool
}

// SearchIntelFTS performs a BM25-ranked text search over intel anchors, doc
// sections, and provider-owned root regions.
// Results are deterministic: ordered by score, then path, then title.
func (s *Store) SearchIntelFTS(ctx context.Context, query string, limit int) ([]IntelSearchRow, error) {
	return s.SearchIntelFTSFiltered(ctx, query, limit, IntelSearchFilters{})
}

// SearchIntelFTSFiltered performs the same search as SearchIntelFTS while
// applying path/type eligibility inside the FTS CTE, before candidate limits.
func (s *Store) SearchIntelFTSFiltered(ctx context.Context, query string, limit int, filters IntelSearchFilters) ([]IntelSearchRow, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	if limit <= 0 {
		limit = 25
	}
	if err := s.ensureAnalyticsTables(ctx); err != nil {
		return nil, err
	}

	ftsQuery := sqliteutil.FTS5QueryFromText(query)
	if ftsQuery == "" {
		return nil, nil
	}

	whereClauses := []string{"intel_fts MATCH ?"}
	args := []any{ftsQuery}
	if len(filters.PathPrefixes) > 0 {
		var parts []string
		for _, prefix := range filters.PathPrefixes {
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				continue
			}
			prefix = strings.TrimSuffix(strings.ReplaceAll(prefix, "\\", "/"), "/")
			if prefix == "" || prefix == "." || prefix == "/" {
				continue
			}
			parts = append(parts, caseSensitivePathPrefixSQL("path"))
			args = append(args, prefix, prefix, prefix)
		}
		if len(parts) > 0 {
			whereClauses = append(whereClauses, "("+strings.Join(parts, " OR ")+")")
		}
	}
	if len(filters.Types) > 0 {
		var parts []string
		for _, typ := range filters.Types {
			switch strings.ToLower(strings.TrimSpace(typ)) {
			case "note", "notes", "doc_section":
				parts = append(parts, "?", "?", "?")
				args = append(args, "doc_section", noteRegionFTSVisible, noteRegionFTSSupplemental)
			case "code", "anchor":
				parts = append(parts, "?")
				args = append(args, "anchor")
			}
		}
		if len(parts) > 0 {
			whereClauses = append(whereClauses, "item_type IN ("+strings.Join(parts, ",")+")")
		}
	}
	if len(filters.NoteTypes) > 0 {
		var parts []string
		for _, typeName := range filters.NoteTypes {
			typeName = strings.TrimSpace(typeName)
			if typeName == "" {
				continue
			}
			parts = append(parts, "?")
			args = append(args, typeName)
		}
		if len(parts) > 0 {
			whereClauses = append(whereClauses, "item_type IN ('doc_section', 'note_region_visible', 'note_region_supplemental') AND EXISTS (SELECT 1 FROM ontology_note_types nt WHERE nt.note_path = intel_fts.path AND nt.type_name IN ("+strings.Join(parts, ",")+"))")
		}
	}
	if predicate, symbolArgs := exactSymbolFilterSQL(filters.ExactSymbols, "a.symbol", "a.fqn"); predicate != "" {
		whereClauses = append(whereClauses, "item_type = 'anchor' AND EXISTS (SELECT 1 FROM intel_code_anchors a WHERE a.anchor_id = intel_fts.item_id AND "+predicate+")")
		args = append(args, symbolArgs...)
	}
	if filters.TestsOnly {
		whereClauses = append(whereClauses, testPathSQL("path"))
	}
	if filters.ExcludeTests {
		whereClauses = append(whereClauses, "NOT "+testPathSQL("path"))
	}

	statement := `
		WITH raw_hits AS (
			SELECT
				rowid,
				item_type,
				item_id,
				path,
				title,
				snippet(intel_fts, 3, '[', ']', '…', 10) AS title_snippet,
				snippet(intel_fts, 4, '[', ']', '…', 15) AS body_snippet,
				-- Column weights (item_type, item_id, path, title, body).
				-- We intentionally downweight path hits so searches like "chunker" don't return
				-- every symbol in files named chunker*.go ahead of doc/comment matches.
				bm25(intel_fts, 0.0, 0.0, 0.15, 1.2, 2.5) AS raw_score
			FROM intel_fts
			WHERE ` + strings.Join(whereClauses, " AND ") + `
		), hits AS (
			SELECT *,
				CASE item_type
					WHEN 'note_region_supplemental' THEN raw_score * 0.55
					ELSE raw_score
				END AS score
			FROM raw_hits
			ORDER BY score, path, title, item_type, item_id
			LIMIT ?
		)
		SELECT
			h.item_type,
			h.item_id,
			h.path,
			COALESCE(a.symbol, d.title, h.title) AS title,
			COALESCE(h.body_snippet, '') AS snippet,
			(instr(COALESCE(h.title_snippet, ''), '[') > 0) AS title_hit,
			(instr(COALESCE(h.body_snippet, ''), '[') > 0) AS body_hit,
			h.score,
			COALESCE(a.lang, '') AS lang,
			COALESCE(a.kind, h.item_type) AS kind,
			COALESCE(a.fqn, '') AS fqn
		FROM hits h
		LEFT JOIN intel_code_anchors a ON a.anchor_id = h.item_id
		LEFT JOIN intel_doc_sections d ON d.section_id = h.item_id
		ORDER BY h.score, h.path, title
		LIMIT ?
	`
	args = append(args, limit, limit)
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IntelSearchRow
	for rows.Next() {
		var r IntelSearchRow
		if err := rows.Scan(&r.Type, &r.ID, &r.Path, &r.Title, &r.Snippet, &r.TitleHit, &r.BodyHit, &r.Score, &r.Lang, &r.Kind, &r.FQN); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
