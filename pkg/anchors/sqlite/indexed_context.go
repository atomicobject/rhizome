package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
)

const maxIndexedContextLinks = 100
const maxIndexedContextRationale = 100
const maxIndexedContextEdges = 100
const maxIndexedOntologyTypes = 100
const maxIndexedOntologyExamples = 10
const maxIndexedGraphCommunities = 20

// WHY: SPEC-0082.US10-US11 make indexing the sole discovery owner. These
// methods therefore expose only bounded persisted reads; widening them into
// graph scans would put repository-size work back on agent startup.
// See docs/specs/technical/agent-start-performance.md.
//
// IndexedCodeNoteLinksForFile returns only code-to-note relationships for one
// normalized code path. The SQL limit is mandatory so startup readers cannot
// accidentally materialize the repository-wide coderef graph.
func (s *Store) IndexedCodeNoteLinksForFile(ctx context.Context, codePath string, limit int) ([]codeanchor.IndexedCodeNoteLink, error) {
	normalized, err := normalizeIndexedContextPath(codePath)
	if err != nil {
		return nil, err
	}
	limit = boundedIndexedContextLimit(limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path, COALESCE(lang, ''), COALESCE(label, ''),
		       COALESCE(snippet, ''), updated_at
		FROM doc_links
		WHERE src_type = 'code' AND dst_kind = 'note' AND src_path = ?
		ORDER BY src_path, dst_path, COALESCE(label, ''), COALESCE(snippet, '')
		LIMIT ?
	`, normalized, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIndexedCodeNoteLinks(rows)
}

// IndexedCodeNoteLinksForSubtree returns code-to-note relationships when
// either indexed endpoint is the subtree itself or lies below its directory
// boundary. Scope is applied in SQL before the result limit.
func (s *Store) IndexedCodeNoteLinksForSubtree(ctx context.Context, subtree string, limit int) ([]codeanchor.IndexedCodeNoteLink, error) {
	normalized, err := normalizeIndexedContextSubtree(subtree)
	if err != nil {
		return nil, err
	}
	limit = boundedIndexedContextLimit(limit)
	if normalized == "" {
		rows, err := s.db.QueryContext(ctx, `
			SELECT src_path, dst_path, COALESCE(lang, ''), COALESCE(label, ''),
			       COALESCE(snippet, ''), updated_at
			FROM doc_links
			WHERE src_type = 'code' AND dst_kind = 'note'
			ORDER BY src_path, dst_path, COALESCE(label, ''), COALESCE(snippet, '')
			LIMIT ?
		`, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanIndexedCodeNoteLinks(rows)
	}
	pattern := escapeLike(normalized) + "/%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path, COALESCE(lang, ''), COALESCE(label, ''),
		       COALESCE(snippet, ''), updated_at
		FROM doc_links
		WHERE src_type = 'code' AND dst_kind = 'note'
		  AND (
		    src_path = ? OR src_path LIKE ? ESCAPE '\'
		    OR dst_path = ? OR dst_path LIKE ? ESCAPE '\'
		  )
		ORDER BY src_path, dst_path, COALESCE(label, ''), COALESCE(snippet, '')
		LIMIT ?
	`, normalized, pattern, normalized, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIndexedCodeNoteLinks(rows)
}

// IndexedNoteMetadataForPath returns one persisted raw-note identity. It does
// not read Markdown content or widen to aliases/fuzzy path matching.
func (s *Store) IndexedNoteMetadataForPath(ctx context.Context, notePath string) (*codeanchor.IndexedNoteMetadata, error) {
	normalized, err := normalizeIndexedContextPath(notePath)
	if err != nil {
		return nil, err
	}
	var note codeanchor.IndexedNoteMetadata
	err = s.db.QueryRowContext(ctx, `
		SELECT path, COALESCE(title, ''), mtime, size, indexed_at
		FROM notes
		WHERE path = ? AND indexed_at > 0
		LIMIT 1
	`, normalized).Scan(&note.Path, &note.Title, &note.Mtime, &note.Size, &note.IndexedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &note, nil
}

// IndexedGraphSummary aggregates inside SQLite and returns only a bounded
// community list; startup never materializes graph score rows or edges.
func (s *Store) IndexedGraphSummary(ctx context.Context, communityLimit int) (codeanchor.IndexedGraphSummary, error) {
	communityLimit = boundedPositive(communityLimit, maxIndexedGraphCommunities)
	var summary codeanchor.IndexedGraphSummary
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN doc_type = 'note' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN inbound = 0 AND outbound = 0 THEN 1 ELSE 0 END), 0)
		FROM graph_doc_scores
	`).Scan(&summary.DocumentCount, &summary.NoteCount, &summary.OrphanCount)
	if err != nil {
		return codeanchor.IndexedGraphSummary{}, err
	}
	if summary.DocumentCount == 0 {
		return codeanchor.IndexedGraphSummary{}, codeanchor.ErrIndexedGraphSummaryMissing
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH ranked AS (
			SELECT community, doc_path, doc_type,
			       ROW_NUMBER() OVER (
			         PARTITION BY community
			         ORDER BY authority DESC, doc_path
			       ) AS ordinal
			FROM graph_doc_scores
			WHERE community <> ''
		)
		SELECT community,
		       COUNT(*),
		       SUM(CASE WHEN doc_type = 'note' THEN 1 ELSE 0 END),
		       MAX(CASE WHEN ordinal = 1 THEN doc_path ELSE '' END)
		FROM ranked
		GROUP BY community
		ORDER BY COUNT(*) DESC, community
		LIMIT ?
	`, communityLimit)
	if err != nil {
		return codeanchor.IndexedGraphSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var community codeanchor.IndexedGraphCommunity
		if err := rows.Scan(&community.ID, &community.DocumentCount, &community.NoteCount, &community.TopPath); err != nil {
			return codeanchor.IndexedGraphSummary{}, err
		}
		summary.Communities = append(summary.Communities, community)
	}
	return summary, rows.Err()
}

// IndexedRationaleForFile returns rationale already extracted for one code
// file. The SQL limit prevents large comment inventories from being loaded
// before the renderer applies its byte budget.
func (s *Store) IndexedRationaleForFile(ctx context.Context, codePath string, limit int) ([]codeanchor.IndexedRationale, error) {
	normalized, err := normalizeIndexedContextPath(codePath)
	if err != nil {
		return nil, err
	}
	limit = boundedPositive(limit, maxIndexedContextRationale)
	rows, err := s.db.QueryContext(ctx, `
		SELECT rationale_id, path, COALESCE(symbol_fqn, ''), kind, content,
		       start_line, end_line
		FROM intel_rationale
		WHERE path = ?
		ORDER BY path, start_line, rationale_id
		LIMIT ?
	`, normalized, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIndexedRationale(rows)
}

// IndexedRationaleForSubtree applies an exact directory boundary in SQL
// before limiting; similarly prefixed siblings are not part of the result.
func (s *Store) IndexedRationaleForSubtree(ctx context.Context, subtree string, limit int) ([]codeanchor.IndexedRationale, error) {
	normalized, err := normalizeIndexedContextSubtree(subtree)
	if err != nil {
		return nil, err
	}
	limit = boundedPositive(limit, maxIndexedContextRationale)
	if normalized == "" {
		rows, err := s.db.QueryContext(ctx, `
			SELECT rationale_id, path, COALESCE(symbol_fqn, ''), kind, content,
			       start_line, end_line
			FROM intel_rationale
			ORDER BY path, start_line, rationale_id
			LIMIT ?
		`, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanIndexedRationale(rows)
	}
	pattern := escapeLike(normalized) + "/%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT rationale_id, path, COALESCE(symbol_fqn, ''), kind, content,
		       start_line, end_line
		FROM intel_rationale
		WHERE path = ? OR path LIKE ? ESCAPE '\'
		ORDER BY path, start_line, rationale_id
		LIMIT ?
	`, normalized, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIndexedRationale(rows)
}

// IndexedCodeEdgesForFile returns resolved code relationships incident to one
// file, aggregated before the mandatory SQL limit.
func (s *Store) IndexedCodeEdgesForFile(ctx context.Context, codePath string, limit int) ([]codeanchor.IndexedCodeEdge, error) {
	normalized, err := normalizeIndexedContextPath(codePath)
	if err != nil {
		return nil, err
	}
	return s.indexedCodeEdges(ctx, `(src.path = ? OR dst.path = ?)`,
		[]any{normalized, normalized}, boundedPositive(limit, maxIndexedContextEdges))
}

// IndexedCodeEdgesForSubtree returns resolved code relationships when either
// endpoint lies on the requested directory boundary.
func (s *Store) IndexedCodeEdgesForSubtree(ctx context.Context, subtree string, limit int) ([]codeanchor.IndexedCodeEdge, error) {
	normalized, err := normalizeIndexedContextSubtree(subtree)
	if err != nil {
		return nil, err
	}
	if normalized == "" {
		return s.indexedCodeEdges(ctx, `1 = 1`, nil, boundedPositive(limit, maxIndexedContextEdges))
	}
	pattern := escapeLike(normalized) + "/%"
	return s.indexedCodeEdges(ctx, `(
		src.path = ? OR src.path LIKE ? ESCAPE '\'
		OR dst.path = ? OR dst.path LIKE ? ESCAPE '\'
	)`, []any{normalized, pattern, normalized, pattern}, boundedPositive(limit, maxIndexedContextEdges))
}

func (s *Store) indexedCodeEdges(ctx context.Context, scope string, args []any, limit int) ([]codeanchor.IndexedCodeEdge, error) {
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT src.path, dst.path, e.kind, COUNT(*)
		FROM intel_edges e
		JOIN intel_code_anchors src
		  ON e.src_type = 'anchor' AND src.id = e.src_row_id
		JOIN intel_code_anchors dst
		  ON e.dst_type = 'anchor' AND dst.id = e.dst_row_id
		WHERE e.kind NOT IN ('defines', 'mentions') AND `+scope+`
		GROUP BY src.path, dst.path, e.kind
		ORDER BY src.path, dst.path, e.kind
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIndexedCodeEdges(rows)
}

// IndexedOntologySummary reads only persisted note/type/schema projections.
// Public types come from ontology_note_types, whose indexing contract contains
// authored resolved note types only. Unlike ontology_nodes it contains no
// internal fallback catalog nodes, and no type name is hard-coded here.
func (s *Store) IndexedOntologySummary(ctx context.Context, maxTypes, maxExamples int) (codeanchor.IndexedOntologySummary, error) {
	maxTypes = boundedPositive(maxTypes, maxIndexedOntologyTypes)
	maxExamples = boundedPositive(maxExamples, maxIndexedOntologyExamples)

	state, err := s.GetOntologySchemaState(ctx)
	if err != nil {
		return codeanchor.IndexedOntologySummary{}, err
	}
	summary := codeanchor.IndexedOntologySummary{
		Available:              state.SchemaHash != "" || state.LoadedAt != 0 || state.Ready,
		Ready:                  state.Ready,
		SchemaHash:             state.SchemaHash,
		MaterializationVersion: state.MaterializationVersion,
		LoadedAt:               state.LoadedAt,
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes`).Scan(&summary.TotalNotes); err != nil {
		return codeanchor.IndexedOntologySummary{}, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT note_path)
		FROM ontology_note_types
	`).Scan(&summary.TypedNotes); err != nil {
		return codeanchor.IndexedOntologySummary{}, err
	}
	summary.UntypedNotes = summary.TotalNotes - summary.TypedNotes
	if summary.UntypedNotes < 0 {
		summary.UntypedNotes = 0
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT type_name, COUNT(DISTINCT note_path)
		FROM ontology_note_types
		GROUP BY type_name
		ORDER BY type_name
		LIMIT ?
	`, maxTypes)
	if err != nil {
		return codeanchor.IndexedOntologySummary{}, err
	}
	typeIndexes := make(map[string]int)
	for rows.Next() {
		var count codeanchor.IndexedOntologyTypeCount
		if err := rows.Scan(&count.TypeName, &count.Count); err != nil {
			_ = rows.Close()
			return codeanchor.IndexedOntologySummary{}, err
		}
		typeIndexes[count.TypeName] = len(summary.TypeCounts)
		summary.TypeCounts = append(summary.TypeCounts, count)
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil || len(summary.TypeCounts) == 0 {
		return summary, rowsErr
	}

	typeNames := make([]string, 0, len(summary.TypeCounts))
	for _, count := range summary.TypeCounts {
		typeNames = append(typeNames, count.TypeName)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(typeNames)), ",")
	args := make([]any, 0, len(typeNames)+1)
	for _, typeName := range typeNames {
		args = append(args, typeName)
	}
	args = append(args, maxExamples)
	exampleRows, err := s.db.QueryContext(ctx, `
		WITH ranked AS (
			SELECT nt.type_name, nt.note_path,
			       ROW_NUMBER() OVER (PARTITION BY nt.type_name ORDER BY nt.note_path) AS ordinal
			FROM ontology_note_types nt
			WHERE nt.type_name IN (`+placeholders+`)
		)
		SELECT type_name, note_path
		FROM ranked
		WHERE ordinal <= ?
		ORDER BY type_name, ordinal
	`, args...)
	if err != nil {
		return codeanchor.IndexedOntologySummary{}, err
	}
	defer exampleRows.Close()
	for exampleRows.Next() {
		var typeName, notePath string
		if err := exampleRows.Scan(&typeName, &notePath); err != nil {
			return codeanchor.IndexedOntologySummary{}, err
		}
		index, ok := typeIndexes[typeName]
		if ok {
			summary.TypeCounts[index].Examples = append(summary.TypeCounts[index].Examples, notePath)
		}
	}
	return summary, exampleRows.Err()
}

type indexedCodeNoteRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

type indexedRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanIndexedCodeNoteLinks(rows indexedCodeNoteRows) ([]codeanchor.IndexedCodeNoteLink, error) {
	out := make([]codeanchor.IndexedCodeNoteLink, 0)
	for rows.Next() {
		var link codeanchor.IndexedCodeNoteLink
		if err := rows.Scan(&link.CodePath, &link.NotePath, &link.Lang, &link.Label, &link.Snippet, &link.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

func scanIndexedRationale(rows indexedRows) ([]codeanchor.IndexedRationale, error) {
	out := make([]codeanchor.IndexedRationale, 0)
	for rows.Next() {
		var rationale codeanchor.IndexedRationale
		if err := rows.Scan(
			&rationale.ID,
			&rationale.CodePath,
			&rationale.SymbolFQN,
			(*string)(&rationale.Kind),
			&rationale.Content,
			&rationale.StartLine,
			&rationale.EndLine,
		); err != nil {
			return nil, err
		}
		out = append(out, rationale)
	}
	return out, rows.Err()
}

func scanIndexedCodeEdges(rows indexedRows) ([]codeanchor.IndexedCodeEdge, error) {
	out := make([]codeanchor.IndexedCodeEdge, 0)
	for rows.Next() {
		var edge codeanchor.IndexedCodeEdge
		if err := rows.Scan(&edge.SourcePath, &edge.TargetPath, &edge.Kind, &edge.Weight); err != nil {
			return nil, err
		}
		out = append(out, edge)
	}
	return out, rows.Err()
}

func normalizeIndexedContextPath(raw string) (string, error) {
	normalized, err := paths.CleanRelPath(raw)
	if err != nil {
		return "", fmt.Errorf("normalize indexed context path: %w", err)
	}
	if normalized.String() == "" {
		return "", fmt.Errorf("normalize indexed context path: path is required")
	}
	return normalized.String(), nil
}

func normalizeIndexedContextSubtree(raw string) (string, error) {
	normalized, err := paths.CleanRelPath(raw)
	if err != nil {
		return "", fmt.Errorf("normalize indexed context subtree: %w", err)
	}
	return normalized.String(), nil
}

func boundedIndexedContextLimit(limit int) int {
	return boundedPositive(limit, maxIndexedContextLinks)
}

func boundedPositive(value, maximum int) int {
	if value <= 0 {
		return 1
	}
	if value > maximum {
		return maximum
	}
	return value
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
