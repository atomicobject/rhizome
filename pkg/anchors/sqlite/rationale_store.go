package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// RationaleSearchRow is a rationale FTS hit with the metadata needed to map it
// back to an anchor or file candidate.
type RationaleSearchRow struct {
	ID        string
	Path      string
	SymbolFQN string
	Kind      string
	Content   string
	Snippet   string
	StartLine int64
	EndLine   int64
	Score     float64
}

// ReplaceRationaleForPath deletes all rationale records for path and inserts fresh ones.
func (s *Store) ReplaceRationaleForPath(ctx context.Context, path string, rationales []codeanchor.Rationale) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if err := s.deleteRationaleByPathTx(ctx, tx, path); err != nil {
			return err
		}
		if len(rationales) == 0 {
			return nil
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO intel_rationale
				(rationale_id, path, symbol_fqn, kind, content, start_line, end_line, fingerprint, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		now := time.Now().Unix()
		for _, r := range rationales {
			symbolFQN := sql.NullString{String: r.SymbolFQN, Valid: r.SymbolFQN != ""}
			if _, err := stmt.ExecContext(ctx,
				r.ID, r.Path, symbolFQN, string(r.Kind), r.Content,
				r.StartLine, r.EndLine, r.Fingerprint, now,
			); err != nil {
				return err
			}
		}
		if err := s.replaceRationaleFTSForPathTx(ctx, tx, path, rationales); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) deleteRationaleByPathTx(ctx context.Context, tx *sql.Tx, path string) error {
	if err := s.deleteRationaleFTSByPathTx(ctx, tx, path); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM intel_rationale WHERE path = ?`, path)
	return err
}

// SearchRationaleFTS searches rationale comments as supporting evidence.
// It is intentionally separate from intel_fts so rationale can boost an
// already-addressable file/anchor without competing as a broad result class.
func (s *Store) SearchRationaleFTS(ctx context.Context, query string, kinds []string, limit int) ([]RationaleSearchRow, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	if limit <= 0 {
		limit = 25
	}
	ftsQuery := sqliteutil.FTS5QueryFromText(query)
	if ftsQuery == "" {
		return nil, nil
	}

	kindSet := normalizeRationaleKinds(kinds)
	args := []any{ftsQuery}
	kindClause := ""
	if len(kindSet) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(kindSet)), ",")
		kindClause = ` AND r.kind IN (` + placeholders + `)`
		for _, k := range kindSet {
			args = append(args, k)
		}
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.rationale_id,
			r.path,
			COALESCE(r.symbol_fqn, '') AS symbol_fqn,
			r.kind,
			r.content,
			snippet(intel_rationale_fts, 4, '[', ']', '…', 18) AS snippet,
			r.start_line,
			r.end_line,
			bm25(intel_rationale_fts, 0.0, 0.0, 0.2, 0.8, 2.4) AS score
		FROM intel_rationale_fts f
		JOIN intel_rationale_fts_rowid m ON m.fts_rowid = f.rowid
		JOIN intel_rationale r ON r.rationale_id = m.rationale_id
		WHERE intel_rationale_fts MATCH ?`+kindClause+`
		ORDER BY score, r.path, r.start_line
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RationaleSearchRow
	for rows.Next() {
		var r RationaleSearchRow
		if err := rows.Scan(&r.ID, &r.Path, &r.SymbolFQN, &r.Kind, &r.Content, &r.Snippet, &r.StartLine, &r.EndLine, &r.Score); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RationaleForPath returns all rationale records for a given file path.
func (s *Store) RationaleForPath(ctx context.Context, path string) ([]codeanchor.Rationale, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT rationale_id, path, symbol_fqn, kind, content, start_line, end_line, fingerprint
		FROM intel_rationale
		WHERE path = ?
		ORDER BY start_line
	`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRationale(rows)
}

// RationaleForSymbol returns rationale records associated with a specific FQN.
func (s *Store) RationaleForSymbol(ctx context.Context, fqn string) ([]codeanchor.Rationale, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT rationale_id, path, symbol_fqn, kind, content, start_line, end_line, fingerprint
		FROM intel_rationale
		WHERE symbol_fqn = ?
		ORDER BY path, start_line
	`, fqn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRationale(rows)
}

// RationaleByKind returns all rationale records of the given kinds (empty = all).
func (s *Store) RationaleByKind(ctx context.Context, kinds []string) ([]codeanchor.Rationale, error) {
	var rows *sql.Rows
	var err error
	if len(kinds) == 0 {
		rows, err = s.db.QueryContext(ctx, `
			SELECT rationale_id, path, symbol_fqn, kind, content, start_line, end_line, fingerprint
			FROM intel_rationale
			ORDER BY path, start_line
		`)
	} else {
		placeholders := make([]byte, 0, len(kinds)*2)
		args := make([]any, len(kinds))
		for i, k := range kinds {
			if i > 0 {
				placeholders = append(placeholders, ',')
			}
			placeholders = append(placeholders, '?')
			args[i] = k
		}
		rows, err = s.db.QueryContext(ctx,
			`SELECT rationale_id, path, symbol_fqn, kind, content, start_line, end_line, fingerprint
			FROM intel_rationale WHERE kind IN (`+string(placeholders)+`) ORDER BY path, start_line`,
			args...,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRationale(rows)
}

// RationaleForPathPrefix returns all rationale records for files under a path prefix.
func (s *Store) RationaleForPathPrefix(ctx context.Context, prefix string, kinds []string) ([]codeanchor.Rationale, error) {
	likePattern := prefix + "%"
	var rows *sql.Rows
	var err error
	if len(kinds) == 0 {
		rows, err = s.db.QueryContext(ctx, `
			SELECT rationale_id, path, symbol_fqn, kind, content, start_line, end_line, fingerprint
			FROM intel_rationale
			WHERE path LIKE ?
			ORDER BY path, start_line
		`, likePattern)
	} else {
		placeholders := make([]byte, 0, len(kinds)*2)
		args := make([]any, 0, len(kinds)+1)
		args = append(args, likePattern)
		for i, k := range kinds {
			if i > 0 {
				placeholders = append(placeholders, ',')
			}
			placeholders = append(placeholders, '?')
			args = append(args, k)
		}
		rows, err = s.db.QueryContext(ctx,
			`SELECT rationale_id, path, symbol_fqn, kind, content, start_line, end_line, fingerprint
			FROM intel_rationale WHERE path LIKE ? AND kind IN (`+string(placeholders)+`) ORDER BY path, start_line`,
			args...,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRationale(rows)
}

func scanRationale(rows *sql.Rows) ([]codeanchor.Rationale, error) {
	var out []codeanchor.Rationale
	for rows.Next() {
		var r codeanchor.Rationale
		var symbolFQN sql.NullString
		if err := rows.Scan(&r.ID, &r.Path, &symbolFQN, (*string)(&r.Kind), &r.Content,
			&r.StartLine, &r.EndLine, &r.Fingerprint); err != nil {
			return nil, err
		}
		if symbolFQN.Valid {
			r.SymbolFQN = symbolFQN.String
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const rationaleFTSTableDDL = `CREATE VIRTUAL TABLE IF NOT EXISTS intel_rationale_fts USING fts5(
			rationale_id UNINDEXED,
			path,
			symbol_fqn,
			kind,
			content,
			tokenize='porter'
		);`

func ensureRationaleFTSSchema(ctx context.Context, db execer) error {
	stmts := []string{
		rationaleFTSTableDDL,
		`CREATE TABLE IF NOT EXISTS intel_rationale_fts_rowid (
			rationale_id TEXT PRIMARY KEY,
			fts_rowid INTEGER NOT NULL CHECK (fts_rowid > 0)
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_rationale_fts_rowid ON intel_rationale_fts_rowid(fts_rowid);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func validateRationaleFTSSchema(ctx context.Context, q queryRower) error {
	var ddl string
	if err := q.QueryRowContext(ctx, `
		SELECT COALESCE(sql, '')
		FROM sqlite_master
		WHERE type = 'table' AND name = 'intel_rationale_fts'
		LIMIT 1
	`).Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return intelSchemaDrift("missing required table intel_rationale_fts")
		}
		return fmt.Errorf("load intel_rationale_fts schema: %w", err)
	}
	normalize := func(sql string) string {
		normalized := strings.Join(strings.Fields(strings.ToLower(sql)), "")
		normalized = strings.TrimSuffix(normalized, ";")
		return strings.Replace(normalized, "ifnotexists", "", 1)
	}
	if normalize(ddl) != normalize(rationaleFTSTableDDL) {
		return intelSchemaDrift("malformed required table intel_rationale_fts")
	}
	return nil
}

func (s *Store) replaceRationaleFTSForPathTx(ctx context.Context, tx *sql.Tx, path string, rationales []codeanchor.Rationale) error {
	if err := s.deleteRationaleFTSByPathTx(ctx, tx, path); err != nil {
		return err
	}
	if len(rationales) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO intel_rationale_fts(rationale_id, path, symbol_fqn, kind, content)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rationales {
		if _, err := stmt.ExecContext(ctx, r.ID, r.Path, r.SymbolFQN, string(r.Kind), r.Content); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO intel_rationale_fts_rowid(rationale_id, fts_rowid)
			VALUES (?, last_insert_rowid())
		`, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) deleteRationaleFTSByPathTx(ctx context.Context, tx *sql.Tx, path string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT fts_rowid
		FROM intel_rationale_fts_rowid
		WHERE rationale_id IN (SELECT rationale_id FROM intel_rationale WHERE path = ?)
	`, path)
	if err != nil {
		return err
	}
	var rowIDs []any
	for rows.Next() {
		var rowid int64
		if err := rows.Scan(&rowid); err != nil {
			_ = rows.Close()
			return err
		}
		rowIDs = append(rowIDs, rowid)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(rowIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(rowIDs)), ",")
		if _, err := tx.ExecContext(ctx, `DELETE FROM intel_rationale_fts WHERE rowid IN (`+placeholders+`)`, rowIDs...); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `
		DELETE FROM intel_rationale_fts_rowid
		WHERE rationale_id IN (SELECT rationale_id FROM intel_rationale WHERE path = ?)
	`, path)
	return err
}

func normalizeRationaleKinds(kinds []string) []string {
	seen := make(map[string]struct{}, len(kinds))
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}
