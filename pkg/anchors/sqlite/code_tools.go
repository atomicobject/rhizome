package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// CodeSymbolLookupOptions scopes an exact/suffix symbol lookup over indexed code anchors.
type CodeSymbolLookupOptions struct {
	Symbol string
	Lang   codeanchor.Lang
	Path   string
	Limit  int
}

// CodeSymbolCandidates returns indexed code anchors matching a symbol exactly, by FQN suffix,
// or by short symbol name. Results are ordered so exact FQN matches win before suffix/name hits.
func (s *Store) CodeSymbolCandidates(ctx context.Context, opts CodeSymbolLookupOptions) ([]codeanchor.IntelAnchor, error) {
	symbol := strings.TrimSpace(opts.Symbol)
	if symbol == "" {
		return nil, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	clauses := []string{`(fqn = ? OR fqn LIKE ? OR symbol = ?)`}
	args := []any{symbol, "%." + symbol, symbol}
	lang := strings.TrimSpace(string(opts.Lang))
	if lang != "" {
		clauses = append(clauses, `lang = ?`)
		args = append(args, lang)
	}
	path := strings.TrimSpace(opts.Path)
	if path != "" {
		path = paths.NormalizeCode(path).String()
		clauses = append(clauses, `(path = ? OR path LIKE ?)`)
		args = append(args, path, strings.TrimRight(path, "/")+"/%")
	}
	args = append(args, symbol, "%."+symbol, symbol, limit)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
		       start_byte, end_byte, start_line, end_line, fingerprint, updated_at
		FROM intel_code_anchors
		WHERE %s
		ORDER BY
		  CASE
		    WHEN fqn = ? THEN 0
		    WHEN fqn LIKE ? THEN 1
		    WHEN symbol = ? THEN 2
		    ELSE 3
		  END,
		  LENGTH(COALESCE(fqn, '')),
		  path,
		  anchor_id
		LIMIT ?
	`, strings.Join(clauses, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCodeToolAnchors(rows)
}

// CodeCallers returns caller anchors that have indexed calls edges into the supplied FQN.
func (s *Store) CodeCallers(ctx context.Context, lang codeanchor.Lang, fqn string, limit int) ([]codeanchor.IntelAnchor, error) {
	return s.codeEdgeNeighbors(ctx, lang, fqn, limit, true)
}

// CodeCallees returns callee anchors reached by indexed calls edges from the supplied FQN.
func (s *Store) CodeCallees(ctx context.Context, lang codeanchor.Lang, fqn string, limit int) ([]codeanchor.IntelAnchor, error) {
	return s.codeEdgeNeighbors(ctx, lang, fqn, limit, false)
}

func (s *Store) codeEdgeNeighbors(ctx context.Context, lang codeanchor.Lang, fqn string, limit int, callers bool) ([]codeanchor.IntelAnchor, error) {
	fqn = strings.TrimSpace(fqn)
	if fqn == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	sourceAlias := "caller"
	targetAlias := "callee"
	selectedAlias := "callee"
	matchedAlias := "caller"
	if callers {
		selectedAlias = "caller"
		matchedAlias = "callee"
	}

	clauses := []string{
		`e.kind = 'calls'`,
		`e.src_type = 'anchor'`,
		`e.dst_type = 'anchor'`,
		fmt.Sprintf(`%s.fqn = ?`, matchedAlias),
	}
	args := []any{fqn}
	if strings.TrimSpace(string(lang)) != "" {
		clauses = append(clauses, fmt.Sprintf(`%s.lang = ?`, matchedAlias))
		args = append(args, string(lang))
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT
		       %s.anchor_id, %s.lang, %s.kind, %s.path, %s.symbol, %s.fqn, %s.signature, %s.doc_comment,
		       %s.start_byte, %s.end_byte, %s.start_line, %s.end_line, %s.fingerprint, %s.updated_at
		FROM intel_edges e
		JOIN intel_code_anchors %s ON %s.id = e.src_row_id
		JOIN intel_code_anchors %s ON %s.id = e.dst_row_id
		WHERE %s
		ORDER BY %s.path, %s.fqn, %s.anchor_id
		LIMIT ?
	`,
		selectedAlias, selectedAlias, selectedAlias, selectedAlias, selectedAlias, selectedAlias, selectedAlias, selectedAlias,
		selectedAlias, selectedAlias, selectedAlias, selectedAlias, selectedAlias, selectedAlias,
		sourceAlias, sourceAlias, targetAlias, targetAlias, strings.Join(clauses, " AND "),
		selectedAlias, selectedAlias, selectedAlias), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCodeToolAnchors(rows)
}

func scanCodeToolAnchors(rows *sql.Rows) ([]codeanchor.IntelAnchor, error) {
	var anchors []codeanchor.IntelAnchor
	for rows.Next() {
		var a codeanchor.IntelAnchor
		if err := rows.Scan(
			&a.AnchorID,
			&a.Lang,
			&a.Kind,
			&a.Path,
			&a.Symbol,
			&a.FQN,
			&a.Signature,
			&a.DocComment,
			&a.StartByte,
			&a.EndByte,
			&a.StartLine,
			&a.EndLine,
			&a.Fingerprint,
			&a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		anchors = append(anchors, a)
	}
	return anchors, rows.Err()
}
