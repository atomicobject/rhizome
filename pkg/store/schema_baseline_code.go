package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

func intelBaselineStep00(context.Context, execer) error { return nil }

func intelBaselineStep01(ctx context.Context, db execer) error { // v1 -> v2
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_anchor_scopes_symbol ON anchor_scopes(symbol_fqn);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_scopes_call_file ON anchor_scopes(call_file);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep02(ctx context.Context, db execer) error { // v2 -> v3
	stmts := []string{
		`ALTER TABLE anchors ADD COLUMN path_prefix TEXT;`,
		`CREATE INDEX IF NOT EXISTS idx_anchors_path_prefix ON anchors(path_prefix);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			// SQLite doesn't support IF NOT EXISTS for ADD COLUMN; ignore if already present.
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

func intelBaselineStep03(ctx context.Context, db execer) error { // v3 -> v4
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS anchor_globs (
				anchor_id INTEGER NOT NULL,
				pattern TEXT NOT NULL,
				UNIQUE(anchor_id, pattern),
				FOREIGN KEY(anchor_id) REFERENCES anchors(id) ON DELETE CASCADE
			);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_globs_anchor ON anchor_globs(anchor_id);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep05(ctx context.Context, db execer) error { // v5 -> v6
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS doc_links (
				src_type TEXT NOT NULL,
				src_path TEXT NOT NULL,
				src_id   TEXT,
				dst_kind TEXT NOT NULL,
				dst_id   TEXT,
				dst_path TEXT NOT NULL,
				lang     TEXT,
				label    TEXT,
				snippet  TEXT,
				meta_json TEXT,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (src_type, src_path, dst_kind, dst_path, dst_id)
			);`,
		`CREATE INDEX IF NOT EXISTS idx_doc_links_dst ON doc_links(dst_kind, dst_id);`,
		`CREATE INDEX IF NOT EXISTS idx_doc_links_src ON doc_links(src_type, src_path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep06(ctx context.Context, db execer) error { // v6 -> v7
	// The calls table was removed in v15->v16. For fresh DBs (or DBs that never had calls),
	// these statements would fail. Use IF EXISTS pattern to make this migration idempotent.
	// Note: SQLite doesn't support ALTER TABLE ... IF EXISTS, so we first check if the table exists.
	var tableExists int
	row := db.(interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	}).QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='calls'`)
	if err := row.Scan(&tableExists); err != nil {
		return err
	}
	if tableExists == 0 {
		return nil // Table doesn't exist, nothing to migrate
	}
	stmts := []string{
		`ALTER TABLE calls ADD COLUMN owner_fqn TEXT;`,
		`CREATE INDEX IF NOT EXISTS idx_calls_owner ON calls(owner_fqn);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			// SQLite doesn't support IF NOT EXISTS for ADD COLUMN; ignore if already present.
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

func intelBaselineStep08(ctx context.Context, db execer) error { // v8 -> v9
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_symbol ON intel_code_anchors(symbol);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_path_symbol ON intel_code_anchors(path, symbol);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep12(ctx context.Context, db execer) error { // v12 -> v13
	// Track per-file indexer version so interrupted runs can resume without re-indexing
	// unchanged files, while still forcing re-index when IndexerVersion bumps.
	if _, err := db.ExecContext(ctx, `ALTER TABLE files ADD COLUMN indexer_version TEXT;`); err != nil {
		// SQLite reports "duplicate column name" if already present.
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return err
		}
	}
	// Backfill per-file versions from the global indexer version metadata when present.
	// This preserves incremental indexing behavior across the schema change.
	if _, err := db.ExecContext(ctx, `
			UPDATE files
			SET indexer_version = (SELECT value FROM index_metadata WHERE key = 'indexer_version')
			WHERE (indexer_version IS NULL OR indexer_version = '')
			  AND EXISTS (
				SELECT 1 FROM index_metadata
				WHERE key = 'indexer_version'
				  AND value IS NOT NULL
				  AND value != ''
			  )
		`); err != nil {
		return err
	}
	return nil
}

func intelBaselineStep15(ctx context.Context, db execer) error { // v15 -> v16
	stmts := []string{
		`DROP INDEX IF EXISTS idx_calls_file;`,
		`DROP INDEX IF EXISTS idx_calls_callee;`,
		`DROP INDEX IF EXISTS idx_calls_owner;`,
		`DROP TABLE IF EXISTS calls;`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep19(ctx context.Context, db execer) error { // v19 -> v20
	// Add fqn_reversed column for suffix matching via reversed string index.
	stmts := []string{
		`ALTER TABLE symbols ADD COLUMN fqn_reversed TEXT;`,
		`CREATE INDEX IF NOT EXISTS idx_symbols_fqn_reversed ON symbols(fqn_reversed);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}

	// Backfill fqn_reversed for existing symbols.
	// We need QueryContext which *sql.Tx supports but execer doesn't expose.
	tx, ok := db.(*sql.Tx)
	if !ok {
		// If not a transaction (shouldn't happen in normal flow), skip backfill.
		// The column will be populated on next index rebuild.
		return nil
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, fqn FROM symbols WHERE fqn_reversed IS NULL`)
	if err != nil {
		return fmt.Errorf("query symbols for backfill: %w", err)
	}

	type symbolRow struct {
		id  int64
		fqn string
	}
	var toUpdate []symbolRow
	for rows.Next() {
		var r symbolRow
		if err := rows.Scan(&r.id, &r.fqn); err != nil {
			rows.Close()
			return fmt.Errorf("scan symbol: %w", err)
		}
		toUpdate = append(toUpdate, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate symbols: %w", err)
	}

	// Update each symbol with its reversed FQN.
	for _, r := range toUpdate {
		reversed := codeanchor.ReverseString(r.fqn)
		if _, err := tx.ExecContext(ctx, `UPDATE symbols SET fqn_reversed = ? WHERE id = ?`, reversed, r.id); err != nil {
			return fmt.Errorf("update symbol %d: %w", r.id, err)
		}
	}

	return nil
}

func intelBaselineStep21(ctx context.Context, db execer) error { // v21 -> v22
	// Record parse health per file so we never treat parse errors/timeouts as skippable.
	_, err := db.ExecContext(ctx, `ALTER TABLE files ADD COLUMN parse_status TEXT NOT NULL DEFAULT 'ok';`)
	if err != nil && strings.Contains(err.Error(), "duplicate column name") {
		return nil
	}
	return err
}

func intelBaselineStep22(ctx context.Context, db execer) error { // v22 -> v23
	// Track whether call edges need a second-pass rebuild for this file.
	_, err := db.ExecContext(ctx, `ALTER TABLE files ADD COLUMN call_edges_stale INTEGER NOT NULL DEFAULT 0;`)
	if err != nil && strings.Contains(err.Error(), "duplicate column name") {
		return nil
	}
	return err
}

func intelBaselineStep30(ctx context.Context, db execer) error { // v30 -> v31
	tx, ok := db.(*sql.Tx)
	if !ok {
		return errors.New("note metadata link resolver migration requires sql transaction")
	}
	stmts := []string{
		`ALTER TABLE notes ADD COLUMN note_key_full TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE notes ADD COLUMN note_key_base TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE notes ADD COLUMN path_len INTEGER NOT NULL DEFAULT 0;`,
		`CREATE INDEX IF NOT EXISTS idx_notes_note_key_full ON notes(note_key_full);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_note_key_base ON notes(note_key_base, path_len, path);`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return err
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, path FROM notes`)
	if err != nil {
		return err
	}
	type noteKeyRow struct {
		id   int64
		path string
	}
	var updates []noteKeyRow
	for rows.Next() {
		var row noteKeyRow
		if err := rows.Scan(&row.id, &row.path); err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	stmt, err := tx.PrepareContext(ctx, `UPDATE notes SET note_key_full = ?, note_key_base = ?, path_len = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range updates {
		fullKey, baseKey, pathLen := noteLookupKeys(row.path)
		if _, err := stmt.ExecContext(ctx, fullKey, baseKey, pathLen, row.id); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep35(ctx context.Context, db execer) error { // v35 -> v36
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS intel_rationale (
				rationale_id TEXT PRIMARY KEY,
				path TEXT NOT NULL,
				symbol_fqn TEXT,
				kind TEXT NOT NULL,
				content TEXT NOT NULL,
				start_line INTEGER NOT NULL,
				end_line INTEGER NOT NULL,
				fingerprint TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			) STRICT, WITHOUT ROWID;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_rationale_path ON intel_rationale(path);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_rationale_symbol ON intel_rationale(symbol_fqn) WHERE symbol_fqn IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_rationale_kind ON intel_rationale(kind);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep38(ctx context.Context, db execer) error { // v38 -> v39
	stmts := []string{
		`DROP TABLE IF EXISTS intel_edges;`,
		`CREATE TABLE IF NOT EXISTS intel_edges (
					src_type TEXT NOT NULL CHECK (src_type IN ('anchor', 'doc_section')),
					src_row_id INTEGER NOT NULL CHECK (src_row_id > 0),
					dst_type TEXT NOT NULL CHECK (dst_type IN ('anchor', 'doc_section')),
					dst_row_id INTEGER NOT NULL CHECK (dst_row_id > 0),
					kind TEXT NOT NULL CHECK (kind != ''),
					meta_json TEXT,
					PRIMARY KEY (src_type, src_row_id, dst_type, dst_row_id, kind)
				) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_dst ON intel_edges(kind, dst_row_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_src ON intel_edges(kind, src_row_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_mentions_dst ON intel_edges(dst_row_id) WHERE kind = 'mentions' AND dst_type = 'anchor';`,
		`UPDATE files SET hash = NULL;`,
		`UPDATE notes SET mtime = 0, content_hash = '', indexer_version = '';`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep39(ctx context.Context, db execer) error { // v39 -> v40
	_, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_intel_module_defs_src;`)
	return err
}

func intelBaselineStep40(ctx context.Context, db execer) error { // v40 -> v41
	_, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_symbols_lang_fqn_reversed ON symbols(lang, fqn_reversed);`)
	return err
}

func intelBaselineStep41(ctx context.Context, db execer) error { // v41 -> v42
	tx, ok := db.(*sql.Tx)
	if !ok {
		return errors.New("note search schema migration requires sql transaction")
	}
	stmts := []string{
		`ALTER TABLE notes ADD COLUMN first_segment_norm TEXT NOT NULL DEFAULT '';`,
		`CREATE INDEX IF NOT EXISTS idx_notes_first_segment_norm ON notes(first_segment_norm, path);`,
		`CREATE TABLE IF NOT EXISTS note_search_terms (
				note_id INTEGER NOT NULL,
				scope INTEGER NOT NULL,
				segment_ordinal INTEGER NOT NULL,
				token_ordinal INTEGER NOT NULL,
				token_text TEXT NOT NULL,
				PRIMARY KEY (note_id, scope, segment_ordinal, token_ordinal, token_text),
				FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_search_terms_lookup ON note_search_terms(scope, token_text, note_id, segment_ordinal, token_ordinal);`,
		`CREATE INDEX IF NOT EXISTS idx_note_search_terms_note ON note_search_terms(note_id, scope, segment_ordinal, token_ordinal);`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return err
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, path FROM notes`)
	if err != nil {
		return err
	}
	type searchSeed struct {
		id   int64
		path string
	}
	var seeds []searchSeed
	for rows.Next() {
		var row searchSeed
		if err := rows.Scan(&row.id, &row.path); err != nil {
			rows.Close()
			return err
		}
		seeds = append(seeds, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	stmt, err := tx.PrepareContext(ctx, `UPDATE notes SET first_segment_norm = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range seeds {
		if _, err := stmt.ExecContext(ctx, firstSegmentNorm(row.path), row.id); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM note_search_terms`); err != nil {
		return err
	}
	noteRows, err := tx.QueryContext(ctx, `SELECT id, path, COALESCE(title, '') FROM notes WHERE indexed_at > 0`)
	if err != nil {
		return err
	}
	var termValues [][]any
	for noteRows.Next() {
		var noteID int64
		var path string
		var title string
		if err := noteRows.Scan(&noteID, &path, &title); err != nil {
			noteRows.Close()
			return err
		}
		for _, row := range buildNoteSearchTermRowsForNote(noteID, path, title) {
			termValues = append(termValues, []any{row.NoteID, row.Scope, row.SegmentOrdinal, row.TokenOrdinal, row.TokenText})
		}
	}
	if err := noteRows.Err(); err != nil {
		noteRows.Close()
		return err
	}
	noteRows.Close()
	return execValuesBatch(ctx, tx, `
			INSERT OR IGNORE INTO note_search_terms(note_id, scope, segment_ordinal, token_ordinal, token_text)
			VALUES
		`, termValues, 5, "")
}

func intelBaselineStep43(context.Context, execer) error { return nil }

func intelBaselineStep45(context.Context, execer) error { return nil }

func intelBaselineStep54(ctx context.Context, db execer) error { // v54 -> v55
	// Persist SymbolRef.Member on anchors so PHP class members (`Class::method`)
	// can be distinguished from namespace-only symbols (`Namespace\name`) after
	// loading. Without this column, BaseSym.Member resets to false on read and
	// normalizeSymbol produces `\` for everything, breaking method-anchor scope
	// strings stored in anchor_scopes.
	stmts := []string{
		`ALTER TABLE anchors ADD COLUMN base_member INTEGER NOT NULL DEFAULT 0;`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}
