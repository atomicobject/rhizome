package sqlite

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - matching + scopes](docs/reference/analysis/code-anchors-matching-scopes.md)
// - [Code Index (Hub)](docs/hubs/Code Index (Hub).md)
// - [Code Intel - Data model](docs/reference/domain/Code Intel - Data model (anchors, sections, edges).md)
// - [Code Index - Unified SQLite DB](docs/reference/analysis/Code Index - Unified SQLite DB.md)

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration/domains"
	"github.com/bmatcuk/doublestar/v4"
)

// Store persists semantic note data in SQLite.
type Store struct {
	db        *sql.DB
	closeFn   func() error
	writeMu   sync.Mutex
	sharedMu  *sync.Mutex
	writeInfo writeStats
	vecReady  sync.Map // map[int]struct{} keyed by embedding dimensions
	readOnly  bool
}

type writeStats struct {
	label     string
	mu        sync.Mutex
	count     int64
	totalWait time.Duration
	totalHold time.Duration
	lastLog   time.Time
}

const (
	writeStatsLogEvery    = 200
	writeStatsLogInterval = 10 * time.Second
)

func (w *writeStats) record(wait, hold time.Duration) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.count++
	w.totalWait += wait
	w.totalHold += hold
	shouldLog := w.count%writeStatsLogEvery == 0
	now := time.Now()
	if shouldLog && now.Sub(w.lastLog) >= writeStatsLogInterval {
		avgWait := time.Duration(0)
		avgHold := time.Duration(0)
		if w.count > 0 {
			avgWait = w.totalWait / time.Duration(w.count)
			avgHold = w.totalHold / time.Duration(w.count)
		}
		log.Printf("[index] %s writes=%d wait=%s avg_wait=%s hold=%s avg_hold=%s",
			w.label,
			w.count,
			w.totalWait.Truncate(time.Millisecond),
			avgWait.Truncate(time.Millisecond),
			w.totalHold.Truncate(time.Millisecond),
			avgHold.Truncate(time.Millisecond),
		)
		w.lastLog = now
	}
	w.mu.Unlock()
}

func (s *Store) writeLock() *sync.Mutex {
	if s != nil && s.sharedMu != nil {
		return s.sharedMu
	}
	return &s.writeMu
}

// DB returns the underlying sqlite handle (shared by unified indexes).
func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// SetWriteMu overrides the default write mutex (used to serialize writes across stores).
func (s *Store) SetWriteMu(mu *sync.Mutex) {
	if s == nil {
		return
	}
	s.sharedMu = mu
}

func (s *Store) withWrite(ctx context.Context, fn func(ctx context.Context, db *sql.DB) error) error {
	mu := s.writeLock()
	waitStart := time.Now()
	mu.Lock()
	wait := time.Since(waitStart)
	holdStart := time.Now()
	err := fn(ctx, s.db)
	hold := time.Since(holdStart)
	mu.Unlock()
	s.writeInfo.record(wait, hold)
	indexingperf.ObserveDBWrite(ctx, s.writeInfo.label, wait, hold)
	return err
}

func (s *Store) withWriteTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	mu := s.writeLock()
	waitStart := time.Now()
	mu.Lock()
	wait := time.Since(waitStart)
	holdStart := time.Now()
	err := sqliteutil.ExecTxWithRetry(ctx, s.db, fn)
	hold := time.Since(holdStart)
	mu.Unlock()
	s.writeInfo.record(wait, hold)
	indexingperf.ObserveDBWrite(ctx, s.writeInfo.label, wait, hold)
	return err
}

const (
	intelBaselineVersion          = 56
	currentSchemaVersion          = 72
	intelVecTablePrefix           = "intel_embeddings_vec_d"
	currentIntelSchemaFingerprint = "intel-schema-v72-2026-10-08-link-text"
)

// forwardSchemaMigrations contains only migrations added after the consolidated
// v56 baseline. Append one entry and increment currentSchemaVersion together.
var forwardSchemaMigrations = []domains.TxStep{
	func(ctx context.Context, tx *sql.Tx) error { // v56 -> v57
		return createNoteFragmentTargetsSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v57 -> v58
		return addOntologyMaterializationVersionSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v58 -> v59
		return createExternalTargetSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v59 -> v60
		// v0.50.2 and main independently assigned different schemas to v59.
		// Reapply both idempotent definitions so either historical v59 shape
		// upgrades without losing indexed or session state.
		if err := createExternalTargetSchema(ctx, tx); err != nil {
			return err
		}
		return createSessionReservationSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v60 -> v61
		return createNoteProjectionStateSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v61 -> v62
		return createGraphRevisionSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v62 -> v63
		// v62 bumped the graph revision on every `UPDATE notes`, including
		// indexing metadata the web graph never reads. Drop the unscoped
		// trigger so the column-scoped definition is recreated.
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS graph_web_revision_notes_update`); err != nil {
			return err
		}
		return createGraphRevisionSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v63 -> v64
		return addOntologyAssessmentFlagsSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v64 -> v65
		return migrateNotePublicationFactsSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v65 -> v66
		return migrateValidationDiagnosticSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v66 -> v67
		return migrateIntelVecOwnerPartitions(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v67 -> v68
		return createGoRelationshipSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v68 -> v69
		return addSymbolRefTargetMemberSchema(ctx, tx)
	},
	func(ctx context.Context, tx *sql.Tx) error { // v69 -> v70
		return addValidationDiagnosticVariantSchema(ctx, tx)
	},
	createDerivedWorkSchema,       // v70 -> v71
	addGraphDocEdgeLinkTextSchema, // v71 -> v72
}

// addGraphDocEdgeLinkTextSchema stores each note link's label and line on its
// edge row so search can score a note by the words other notes use for it.
// Existing rows stay empty until notemeta rederives links.
func addGraphDocEdgeLinkTextSchema(ctx context.Context, tx *sql.Tx) error {
	hasColumn, err := tableHasColumnQuery(ctx, tx, "graph_doc_edges", "link_text")
	if err != nil || hasColumn {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE graph_doc_edges ADD COLUMN link_text TEXT NOT NULL DEFAULT ''`)
	return err
}

// addValidationDiagnosticVariantSchema adds the optional issue variant to
// published diagnostics. Existing rows default to no variant until the next
// validation run publishes a new generation. The index lives here, not in the
// baseline statements, because ResetDomain keeps validation tables and a
// surviving pre-v70 table lacks the columns until this step runs.
func addValidationDiagnosticVariantSchema(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []string{"variant_key", "variant_label"} {
		hasColumn, err := tableHasColumnQuery(ctx, tx, "validation_diagnostics", column)
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE validation_diagnostics ADD COLUMN `+column+` TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_validation_diagnostics_variant ON validation_diagnostics(generation, code, variant_key)`)
	return err
}

func addSymbolRefTargetMemberSchema(ctx context.Context, tx *sql.Tx) error {
	hasMember, err := tableHasColumnQuery(ctx, tx, "intel_symbol_ref_targets", "dst_member")
	if err != nil {
		return err
	}
	memberProjection := `CASE WHEN lower(dst_lang) = 'php' AND instr(dst_fqn, '::') > 0 THEN 1 ELSE 0 END`
	if hasMember {
		memberProjection = "dst_member"
	}
	execAll := func(stmts ...string) error {
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	}
	// External mappings and symbol evidence reference both tables being rebuilt.
	// Move them first, then recreate and copy them before dropping the v68 graph.
	if err := execAll(
		`ALTER TABLE intel_external_symbol_evidence RENAME TO intel_external_symbol_evidence_v68`,
		`ALTER TABLE intel_external_target_map RENAME TO intel_external_target_map_v68`,
		`ALTER TABLE intel_symbol_refs RENAME TO intel_symbol_refs_v68`,
		`ALTER TABLE intel_symbol_ref_targets RENAME TO intel_symbol_ref_targets_v68`,
		`CREATE TABLE intel_symbol_ref_targets (
			target_id INTEGER PRIMARY KEY,
			dst_lang TEXT NOT NULL,
			dst_pkg TEXT NOT NULL,
			dst_name TEXT NOT NULL,
			dst_fqn TEXT NOT NULL,
			dst_member INTEGER NOT NULL DEFAULT 0 CHECK (dst_member IN (0, 1)),
			UNIQUE (dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
		) STRICT`,
		`CREATE TABLE intel_symbol_refs (
			src_file_id INTEGER NOT NULL,
			owner_symbol_id INTEGER,
			owner_fqn TEXT NOT NULL,
			ref_kind TEXT NOT NULL,
			dst_target_id INTEGER NOT NULL,
			PRIMARY KEY (src_file_id, owner_fqn, ref_kind, dst_target_id),
			FOREIGN KEY(dst_target_id) REFERENCES intel_symbol_ref_targets(target_id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT`,
		`INSERT INTO intel_symbol_ref_targets(target_id, dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
		 SELECT target_id, dst_lang, dst_pkg, dst_name, dst_fqn, `+memberProjection+`
		 FROM intel_symbol_ref_targets_v68`,
		`INSERT INTO intel_symbol_refs(src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id)
		 SELECT src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id
		 FROM intel_symbol_refs_v68`,
	); err != nil {
		return err
	}
	if err := createExternalTargetSchema(ctx, tx); err != nil {
		return err
	}
	if err := execAll(
		`INSERT INTO intel_external_target_map(raw_target_id, external_id)
		 SELECT raw_target_id, external_id FROM intel_external_target_map_v68`,
		`INSERT INTO intel_external_symbol_evidence(
			src_file_id, owner_fqn, ref_kind, raw_target_id, external_id,
			evidence_kind, confidence, imported_name, local_name, manifest_path, declared_range, version_scope
		 ) SELECT src_file_id, owner_fqn, ref_kind, raw_target_id, external_id,
		          evidence_kind, confidence, imported_name, local_name, manifest_path, declared_range, version_scope
		   FROM intel_external_symbol_evidence_v68`,
		`DROP TABLE intel_external_symbol_evidence_v68`,
		`DROP TABLE intel_external_target_map_v68`,
		`DROP TABLE intel_symbol_refs_v68`,
		`DROP TABLE intel_symbol_ref_targets_v68`,
		`CREATE INDEX idx_intel_symbol_ref_targets_name ON intel_symbol_ref_targets(dst_lang, dst_name)`,
		`CREATE INDEX idx_intel_symbol_ref_targets_fqn ON intel_symbol_ref_targets(dst_lang, dst_fqn)`,
		`CREATE INDEX idx_intel_symbol_refs_dst_target ON intel_symbol_refs(dst_target_id, src_file_id, owner_symbol_id)`,
		`CREATE INDEX idx_intel_symbol_refs_owner ON intel_symbol_refs(owner_symbol_id, src_file_id, dst_target_id) WHERE owner_symbol_id IS NOT NULL`,
	); err != nil {
		return err
	}
	return createExternalTargetSchema(ctx, tx)
}

func createNoteProjectionStateSchema(ctx context.Context, db execer) error {
	query, ok := db.(queryRower)
	if !ok {
		return errors.New("note projection migration requires schema query support")
	}
	hasFormatID, err := tableHasColumnQuery(ctx, query, "notes", "format_id")
	if err != nil {
		return err
	}
	if !hasFormatID {
		if _, err := db.ExecContext(ctx, `ALTER TABLE notes ADD COLUMN format_id TEXT NOT NULL DEFAULT 'markdown' CHECK (format_id != '' AND format_id = lower(trim(format_id)))`); err != nil {
			return err
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS note_projection_state (
			note_id INTEGER PRIMARY KEY,
			provider_version TEXT NOT NULL DEFAULT '',
			projection_version TEXT NOT NULL DEFAULT '',
			source_content_hash TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'stale' CHECK (status IN ('stale', 'current', 'fatal')),
			diagnostic_code TEXT NOT NULL DEFAULT '',
			diagnostic_detail TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
			CHECK (
				(status = 'current'
					AND length(trim(provider_version)) > 0
					AND length(trim(projection_version)) > 0
					AND length(trim(source_content_hash)) > 0
					AND diagnostic_code = '' AND diagnostic_detail = '')
				OR (status = 'fatal'
					AND length(trim(provider_version)) > 0
					AND length(trim(projection_version)) > 0
					AND length(trim(diagnostic_code)) > 0
					AND length(trim(diagnostic_detail)) > 0)
				OR (status = 'stale' AND diagnostic_code = '' AND diagnostic_detail = '')
			),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_projection_state_status
			ON note_projection_state(status, note_id);`,
		`INSERT OR IGNORE INTO note_projection_state(
			note_id, provider_version, projection_version, source_content_hash,
			status, diagnostic_code, diagnostic_detail, updated_at
		)
		SELECT id, '', '', '', 'stale', '', '', indexed_at
		FROM notes;`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func createSessionReservationSchema(ctx context.Context, db execer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS mcp_session_item_reservations (
			session_id TEXT NOT NULL CHECK (session_id != ''),
			item_key TEXT NOT NULL CHECK (item_key != ''),
			fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
			reservation_id TEXT NOT NULL CHECK (reservation_id != ''),
			reserved_at INTEGER NOT NULL CHECK (reserved_at >= 0),
			PRIMARY KEY (session_id, item_key)
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_session_item_reservations_owner
			ON mcp_session_item_reservations(session_id, reservation_id);`,
		`CREATE TABLE IF NOT EXISTS mcp_session_maintenance (
			maintenance_key TEXT PRIMARY KEY CHECK (maintenance_key != ''),
			next_due_at INTEGER NOT NULL CHECK (next_due_at >= 0)
		) WITHOUT ROWID, STRICT;`,
		`INSERT INTO mcp_session_maintenance (maintenance_key, next_due_at)
			VALUES ('cleanup', 0) ON CONFLICT(maintenance_key) DO NOTHING;`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

var schemaMigrations = []func(context.Context, execer) error{
	func(context.Context, execer) error { return nil }, // v0 -> v1 (initial schema)
	func(ctx context.Context, db execer) error { // v1 -> v2
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
	},
	func(ctx context.Context, db execer) error { // v2 -> v3
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
	},
	func(ctx context.Context, db execer) error { // v3 -> v4
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
	},
	func(ctx context.Context, db execer) error { // v4 -> v5
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS intel_code_anchors (
				anchor_id TEXT PRIMARY KEY,
				lang TEXT NOT NULL,
				kind TEXT NOT NULL,
				path TEXT NOT NULL,
				symbol TEXT NOT NULL,
				fqn TEXT,
				signature TEXT,
				doc_comment TEXT,
				start_byte INTEGER NOT NULL,
				end_byte INTEGER NOT NULL,
				start_line INTEGER NOT NULL,
				end_line INTEGER NOT NULL,
				fingerprint TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS intel_doc_sections (
				section_id TEXT PRIMARY KEY,
				path TEXT NOT NULL,
				title TEXT NOT NULL,
				level INTEGER NOT NULL,
				start_byte INTEGER NOT NULL,
				end_byte INTEGER NOT NULL,
				content TEXT NOT NULL,
				fingerprint TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS intel_edges (
				src_id TEXT NOT NULL,
				dst_id TEXT NOT NULL,
				kind TEXT NOT NULL,
				meta_json TEXT,
				PRIMARY KEY (src_id, dst_id, kind)
			);`,
			`CREATE VIRTUAL TABLE IF NOT EXISTS intel_fts USING fts5(
				item_type,
				item_id,
				path,
				title,
				body,
				tokenize='porter'
			);`,
			`CREATE TABLE IF NOT EXISTS intel_fts_rowid (
				item_type TEXT NOT NULL,
				item_id TEXT NOT NULL,
				fts_rowid INTEGER NOT NULL,
				PRIMARY KEY (item_type, item_id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_path ON intel_code_anchors(path);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_fqn ON intel_code_anchors(fqn);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_edges_dst_kind ON intel_edges(dst_id, kind);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_edges_src_kind ON intel_edges(src_id, kind);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_doc_sections_path ON intel_doc_sections(path);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_fts_rowid_item ON intel_fts_rowid(item_type, item_id);`,
			// Force a one-time reindex after the intel schema is introduced.
			// Existing hashes would otherwise cause IndexCodeFile to skip unchanged files,
			// leaving intel tables empty/stale until a rebuild.
			`UPDATE files SET hash = NULL;`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v5 -> v6
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
	},
	func(ctx context.Context, db execer) error { // v6 -> v7
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
	},
	func(ctx context.Context, db execer) error { // v7 -> v8
		// Intel schema upgrade:
		// - Use modern SQLite table features (STRICT + WITHOUT ROWID + basic invariants).
		// - Add traversal-oriented indexes (kind-first + hot partial index for mentions).
		// - Force a one-time reindex so intel/FTS is rebuilt after the structural change.
		//
		// The intel tables are derived and safe to drop/rebuild; correctness is ensured by reindex.
		stmts := []string{
			// Drop (derived) intel tables. This intentionally clears intel + FTS data.
			`DROP TABLE IF EXISTS intel_fts_rowid;`,
			`DROP TABLE IF EXISTS intel_fts;`,
			`DROP TABLE IF EXISTS intel_edges;`,
			`DROP TABLE IF EXISTS intel_doc_sections;`,
			`DROP TABLE IF EXISTS intel_code_anchors;`,

			`CREATE TABLE IF NOT EXISTS intel_code_anchors (
					anchor_id TEXT PRIMARY KEY CHECK (anchor_id != ''),
					lang TEXT NOT NULL CHECK (lang != ''),
					kind TEXT NOT NULL CHECK (kind != ''),
					path TEXT NOT NULL CHECK (path != ''),
					symbol TEXT NOT NULL CHECK (symbol != ''),
					fqn TEXT,
					signature TEXT,
					doc_comment TEXT,
					start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
					end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
					start_line INTEGER NOT NULL CHECK (start_line >= 0),
					end_line INTEGER NOT NULL CHECK (end_line >= start_line),
					fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
					updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
				) WITHOUT ROWID, STRICT;`,
			`CREATE TABLE IF NOT EXISTS intel_doc_sections (
					section_id TEXT PRIMARY KEY CHECK (section_id != ''),
					path TEXT NOT NULL CHECK (path != ''),
					title TEXT NOT NULL,
					level INTEGER NOT NULL CHECK (level >= 1 AND level <= 6),
					start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
					end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
					content TEXT NOT NULL,
					fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
					updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
				) WITHOUT ROWID, STRICT;`,
			`CREATE TABLE IF NOT EXISTS intel_edges (
					src_id TEXT NOT NULL CHECK (src_id != ''),
					dst_id TEXT NOT NULL CHECK (dst_id != ''),
					kind TEXT NOT NULL CHECK (kind != ''),
					meta_json TEXT,
					PRIMARY KEY (src_id, dst_id, kind)
				) WITHOUT ROWID, STRICT;`,
			`CREATE VIRTUAL TABLE IF NOT EXISTS intel_fts USING fts5(
					item_type,
					item_id,
					path,
					title,
					body,
					tokenize='porter'
				);`,
			`CREATE TABLE IF NOT EXISTS intel_fts_rowid (
					item_type TEXT NOT NULL CHECK (item_type != ''),
					item_id TEXT NOT NULL CHECK (item_id != ''),
					fts_rowid INTEGER NOT NULL CHECK (fts_rowid > 0),
					PRIMARY KEY (item_type, item_id)
				) WITHOUT ROWID, STRICT;`,

			// Indexes tuned for common lookups + traversal.
			`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_path ON intel_code_anchors(path);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_lang_fqn ON intel_code_anchors(lang, fqn) WHERE fqn IS NOT NULL;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_doc_sections_path ON intel_doc_sections(path);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_dst ON intel_edges(kind, dst_id);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_src ON intel_edges(kind, src_id);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_edges_mentions_dst ON intel_edges(dst_id) WHERE kind = 'mentions';`,

			// Force a one-time reindex after the intel schema changes.
			`UPDATE files SET hash = NULL;`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v8 -> v9
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
	},
	func(ctx context.Context, db execer) error { // v9 -> v10
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS graph_doc_scores (
				doc_path TEXT PRIMARY KEY CHECK (doc_path != ''),
				doc_type TEXT NOT NULL CHECK (doc_type IN ('note', 'code')),
				hub REAL NOT NULL,
				authority REAL NOT NULL,
				community TEXT NOT NULL,
				inbound INTEGER NOT NULL,
				outbound INTEGER NOT NULL,
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_graph_doc_scores_authority ON graph_doc_scores(authority DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_graph_doc_scores_community ON graph_doc_scores(community);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v10 -> v11
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS graph_doc_edges (
				src_path TEXT NOT NULL CHECK (src_path != ''),
				dst_path TEXT NOT NULL CHECK (dst_path != ''),
				kind TEXT NOT NULL CHECK (kind != ''),
				PRIMARY KEY (src_path, dst_path, kind)
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_src ON graph_doc_edges(src_path);`,
			`CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_dst ON graph_doc_edges(dst_path);`,
			`CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_kind_src ON graph_doc_edges(kind, src_path);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v11 -> v12
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS graph_anchor_scores (
				anchor_id TEXT PRIMARY KEY CHECK (anchor_id != ''),
				pagerank REAL NOT NULL,
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_graph_anchor_scores_rank ON graph_anchor_scores(pagerank DESC);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v12 -> v13
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
	},
	func(ctx context.Context, db execer) error { // v13 -> v14
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS mcp_sessions (
				session_id TEXT PRIMARY KEY CHECK (session_id != ''),
				created_at INTEGER NOT NULL CHECK (created_at >= 0),
				last_seen_at INTEGER NOT NULL CHECK (last_seen_at >= 0)
			) WITHOUT ROWID, STRICT;`,
			`CREATE TABLE IF NOT EXISTS mcp_session_items (
				session_id TEXT NOT NULL CHECK (session_id != ''),
				item_key TEXT NOT NULL CHECK (item_key != ''),
				fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
				sent_at INTEGER NOT NULL CHECK (sent_at >= 0),
				PRIMARY KEY (session_id, item_key)
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_mcp_sessions_last_seen ON mcp_sessions(last_seen_at);`,
			`CREATE INDEX IF NOT EXISTS idx_mcp_session_items_session ON mcp_session_items(session_id);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v14 -> v15
		stmts := []string{
			`CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_kind_dst ON graph_doc_edges(kind, dst_path);`,
			`CREATE INDEX IF NOT EXISTS idx_doc_links_dst_path ON doc_links(dst_kind, dst_path);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v15 -> v16
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
	},
	func(ctx context.Context, db execer) error { // v16 -> v17
		// Intel Spine: add intel_chunks table for explicit chunk ordering + provenance.
		// Also add pack metadata entries for deterministic builds.
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS intel_chunks (
				chunk_id TEXT PRIMARY KEY CHECK (chunk_id != ''),
				owner_id TEXT NOT NULL CHECK (owner_id != ''),
				owner_type TEXT NOT NULL CHECK (owner_type IN ('anchor', 'doc_section')),
				ord INTEGER NOT NULL CHECK (ord >= 0),
				granularity TEXT NOT NULL CHECK (granularity != ''),
				breadcrumb TEXT,
				heading TEXT,
				content_hash TEXT NOT NULL CHECK (content_hash != ''),
				start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
				end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
				UNIQUE(owner_id, ord, granularity)
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner ON intel_chunks(owner_id);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v17 -> v18
		// Intel Spine: add intel_embeddings table to unify note and code vector storage.
		// Vectors are keyed by chunk_id from intel_chunks, enabling unified search.
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS intel_embeddings (
				chunk_id TEXT PRIMARY KEY CHECK (chunk_id != ''),
				embedding BLOB NOT NULL,
				norm REAL NOT NULL DEFAULT 0,
				dimensions INTEGER NOT NULL CHECK (dimensions > 0),
				created_at INTEGER NOT NULL CHECK (created_at >= 0),
				FOREIGN KEY(chunk_id) REFERENCES intel_chunks(chunk_id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT;`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v18 -> v19
		// Migrate legacy embeddings into intel_embeddings where matching intel_chunks exist.
		// This is a best-effort migration; embeddings without matching chunks are skipped.
		// After this migration, legacy tables can eventually be dropped.
		hasEmbeddingBlob := true
		hasCodeItemsAnchorRowID := false
		hasCodeItemsAnchorID := false
		hasIntelCodeAnchorsID := false
		if q, ok := db.(queryRower); ok {
			has, err := tableHasColumnQuery(ctx, q, "intel_embeddings", "embedding")
			if err != nil {
				return err
			}
			hasEmbeddingBlob = has
			hasCodeItemsAnchorRowID, err = tableHasColumnQuery(ctx, q, "code_items", "anchor_row_id")
			if err != nil && !strings.Contains(err.Error(), "no such table") {
				return err
			}
			hasCodeItemsAnchorID, err = tableHasColumnQuery(ctx, q, "code_items", "anchor_id")
			if err != nil && !strings.Contains(err.Error(), "no such table") {
				return err
			}
			hasIntelCodeAnchorsID, err = tableHasColumnQuery(ctx, q, "intel_code_anchors", "id")
			if err != nil && !strings.Contains(err.Error(), "no such table") {
				return err
			}
		}

		// Migrate code embeddings: code_chunk_embeddings -> intel_embeddings
		// Match current schema via code_items.anchor_row_id -> intel_code_anchors.anchor_id.
		// Fall back to the legacy code_items.anchor_id column for older DBs.
		codeMigration := ""
		switch {
		case hasCodeItemsAnchorRowID && hasIntelCodeAnchorsID:
			codeMigration = `
				INSERT OR IGNORE INTO intel_embeddings (chunk_id, embedding, norm, dimensions, created_at)
				SELECT c.chunk_id, ce.embedding, ce.norm, ce.dimensions, CAST(strftime('%s', 'now') AS INTEGER)
				FROM code_chunk_embeddings ce
				JOIN code_items ci ON ci.id = ce.item_row_id
				JOIN intel_code_anchors a ON a.id = ci.anchor_row_id
				JOIN intel_chunks c ON c.owner_id = a.anchor_id AND c.ord = ce.chunk_index AND c.owner_type = 'anchor'
				WHERE NOT EXISTS (SELECT 1 FROM intel_embeddings WHERE chunk_id = c.chunk_id)
			`
		case hasCodeItemsAnchorRowID:
			codeMigration = `
				INSERT OR IGNORE INTO intel_embeddings (chunk_id, embedding, norm, dimensions, created_at)
				SELECT c.chunk_id, ce.embedding, ce.norm, ce.dimensions, CAST(strftime('%s', 'now') AS INTEGER)
				FROM code_chunk_embeddings ce
				JOIN code_items ci ON ci.id = ce.item_row_id
				JOIN intel_code_anchors a ON a.rowid = ci.anchor_row_id
				JOIN intel_chunks c ON c.owner_id = a.anchor_id AND c.ord = ce.chunk_index AND c.owner_type = 'anchor'
				WHERE NOT EXISTS (SELECT 1 FROM intel_embeddings WHERE chunk_id = c.chunk_id)
			`
		case hasCodeItemsAnchorID:
			codeMigration = `
				INSERT OR IGNORE INTO intel_embeddings (chunk_id, embedding, norm, dimensions, created_at)
				SELECT c.chunk_id, ce.embedding, ce.norm, ce.dimensions, CAST(strftime('%s', 'now') AS INTEGER)
				FROM code_chunk_embeddings ce
				JOIN code_items ci ON ci.id = ce.item_row_id
				JOIN intel_chunks c ON c.owner_id = ci.anchor_id AND c.ord = ce.chunk_index AND c.owner_type = 'anchor'
				WHERE NOT EXISTS (SELECT 1 FROM intel_embeddings WHERE chunk_id = c.chunk_id)
			`
		}
		if hasEmbeddingBlob && codeMigration != "" {
			if _, err := db.ExecContext(ctx, codeMigration); err != nil {
				// Table might not exist; that's fine
				if !strings.Contains(err.Error(), "no such table") && !strings.Contains(err.Error(), "no such column: a.rowid") {
					return fmt.Errorf("migrate code embeddings: %w", err)
				}
			}
		}

		// Migrate note embeddings: emb_chunk_embeddings -> intel_embeddings
		// Match via: emb_notes.note_id + intel_doc_sections matching by path
		// Note: This is approximate since note_id is the title but intel uses section_id by path.
		// We match by note path and first section (ord=0), which covers single-section notes.
		noteMigration := `
			INSERT OR IGNORE INTO intel_embeddings (chunk_id, embedding, norm, dimensions, created_at)
			SELECT c.chunk_id, ne.embedding, ne.norm, ne.dimensions, CAST(strftime('%s', 'now') AS INTEGER)
			FROM emb_chunk_embeddings ne
			JOIN emb_notes n ON n.id = ne.note_row_id
			JOIN intel_doc_sections s ON s.path = n.path
			JOIN intel_chunks c ON c.owner_id = s.section_id AND c.ord = ne.chunk_index AND c.owner_type = 'doc_section'
			WHERE NOT EXISTS (SELECT 1 FROM intel_embeddings WHERE chunk_id = c.chunk_id)
		`
		if hasEmbeddingBlob {
			if _, err := db.ExecContext(ctx, noteMigration); err != nil {
				if !strings.Contains(err.Error(), "no such table") {
					return fmt.Errorf("migrate note embeddings: %w", err)
				}
			}
		}

		// Clean up orphaned doc_links entries where source files no longer exist.
		// This can happen when files are excluded via .gitignore after being indexed.
		orphanCleanup := `
			DELETE FROM doc_links
			WHERE src_type = 'code' AND src_path NOT IN (SELECT path FROM files)
		`
		if _, err := db.ExecContext(ctx, orphanCleanup); err != nil {
			if !strings.Contains(err.Error(), "no such table") {
				return fmt.Errorf("cleanup orphan doc_links: %w", err)
			}
		}

		return nil
	},
	func(ctx context.Context, db execer) error { // v19 -> v20
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
	},
	func(ctx context.Context, db execer) error { // v20 -> v21
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS intent_embeddings (
				intent TEXT NOT NULL,
				exemplar TEXT NOT NULL,
				embedding BLOB NOT NULL,
				norm REAL NOT NULL DEFAULT 0,
				dimensions INTEGER NOT NULL CHECK (dimensions > 0),
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
				PRIMARY KEY (intent, exemplar)
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_intent_embeddings_intent ON intent_embeddings(intent);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v21 -> v22
		// Record parse health per file so we never treat parse errors/timeouts as skippable.
		_, err := db.ExecContext(ctx, `ALTER TABLE files ADD COLUMN parse_status TEXT NOT NULL DEFAULT 'ok';`)
		if err != nil && strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return err
	},
	func(ctx context.Context, db execer) error { // v22 -> v23
		// Track whether call edges need a second-pass rebuild for this file.
		_, err := db.ExecContext(ctx, `ALTER TABLE files ADD COLUMN call_edges_stale INTEGER NOT NULL DEFAULT 0;`)
		if err != nil && strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return err
	},
	func(ctx context.Context, db execer) error { // v23 -> v24
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS intel_symbol_ref_files (
				file_id INTEGER PRIMARY KEY,
				path    TEXT NOT NULL UNIQUE
			) STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_files_path ON intel_symbol_ref_files(path);`,
			`CREATE TABLE IF NOT EXISTS intel_symbol_ref_targets (
				target_id INTEGER PRIMARY KEY,
				dst_lang  TEXT NOT NULL,
				dst_pkg   TEXT NOT NULL,
				dst_name  TEXT NOT NULL,
				dst_fqn   TEXT NOT NULL,
				dst_member INTEGER NOT NULL DEFAULT 0 CHECK (dst_member IN (0, 1)),
				UNIQUE (dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
			) STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_targets_name ON intel_symbol_ref_targets(dst_lang, dst_name);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_targets_fqn ON intel_symbol_ref_targets(dst_lang, dst_fqn);`,
			`CREATE TABLE IF NOT EXISTS intel_symbol_refs (
				src_file_id    INTEGER NOT NULL,
				owner_symbol_id INTEGER,
				owner_fqn      TEXT NOT NULL,
				ref_kind       TEXT NOT NULL,
				dst_target_id  INTEGER NOT NULL,
				PRIMARY KEY (src_file_id, owner_fqn, ref_kind, dst_target_id),
				FOREIGN KEY(dst_target_id) REFERENCES intel_symbol_ref_targets(target_id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_symbol_refs_dst_target ON intel_symbol_refs(dst_target_id, src_file_id, owner_symbol_id);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_symbol_refs_owner ON intel_symbol_refs(owner_symbol_id, src_file_id, dst_target_id) WHERE owner_symbol_id IS NOT NULL;`,
			`CREATE TABLE IF NOT EXISTS intel_import_refs (
				src_path TEXT NOT NULL,
				module   TEXT NOT NULL,
				PRIMARY KEY (src_path, module)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_import_refs_module ON intel_import_refs(module, src_path);`,
			`CREATE TABLE IF NOT EXISTS intel_module_defs (
				src_path TEXT NOT NULL,
				lang     TEXT NOT NULL,
				module   TEXT NOT NULL,
				PRIMARY KEY (src_path, lang, module)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_module_defs_src ON intel_module_defs(src_path);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_module_defs_module ON intel_module_defs(lang, module, src_path);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v24 -> v25
		stmts := []string{
			`ALTER TABLE notes ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE notes ADD COLUMN indexer_version TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE notes ADD COLUMN mtime INTEGER NOT NULL DEFAULT 0;`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				if strings.Contains(err.Error(), "duplicate column name") {
					continue
				}
				return err
			}
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE notes
			SET mtime = COALESCE(
				(SELECT MAX(updated_at) FROM intel_doc_sections WHERE path = notes.path),
				mtime,
				0
			)
			WHERE COALESCE(mtime, 0) = 0
		`); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v25 -> v26
		tx, ok := db.(*sql.Tx)
		if !ok {
			return errors.New("intel vec migration requires sql transaction")
		}
		_, err := migrateIntelEmbeddingsToVecPrimaryTx(ctx, tx)
		return err
	},
	func(ctx context.Context, db execer) error { // v26 -> v27
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS ontology_note_types (
				note_path TEXT NOT NULL,
				type_name TEXT NOT NULL,
				schema_hash TEXT NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (note_path, type_name)
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_note_types_type ON ontology_note_types(type_name, note_path);`,
			`CREATE TABLE IF NOT EXISTS ontology_edges (
				src_path TEXT NOT NULL,
				src_node_id TEXT NOT NULL DEFAULT '',
				relation_name TEXT NOT NULL,
				dst_path TEXT NOT NULL,
				dst_node_id TEXT NOT NULL DEFAULT '',
				dst_type TEXT NOT NULL,
				provenance TEXT NOT NULL,
				structural INTEGER NOT NULL,
				schema_hash TEXT NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (src_path, src_node_id, relation_name, dst_path, dst_node_id, provenance)
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_edges_src ON ontology_edges(src_path, src_node_id, structural, relation_name);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_edges_dst ON ontology_edges(dst_path, dst_node_id, structural, relation_name);`,
			`CREATE TABLE IF NOT EXISTS ontology_schema_state (
				schema_hash TEXT NOT NULL,
				notes_hash TEXT NOT NULL,
				materialization_version INTEGER NOT NULL DEFAULT 0 CHECK (materialization_version >= 0),
				loaded_at INTEGER NOT NULL,
				ready INTEGER NOT NULL,
				error_json TEXT,
				PRIMARY KEY (schema_hash, notes_hash)
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_schema_state_loaded ON ontology_schema_state(loaded_at DESC);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v27 -> v28
		stmts := []string{
			`CREATE INDEX IF NOT EXISTS idx_ontology_edges_src_query ON ontology_edges(src_path, provenance, relation_name, dst_type, dst_path);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_edges_dst_query ON ontology_edges(dst_path, provenance, relation_name, src_path);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v28 -> v29
		stmts := []string{
			`ALTER TABLE notes ADD COLUMN size INTEGER NOT NULL DEFAULT 0;`,
			`ALTER TABLE notes ADD COLUMN indexed_at INTEGER NOT NULL DEFAULT 0;`,
			`CREATE TABLE IF NOT EXISTS property_keys (
				property_id INTEGER PRIMARY KEY,
				property_name TEXT NOT NULL UNIQUE,
				updated_at INTEGER NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS note_property_values (
				note_id INTEGER NOT NULL,
				property_id INTEGER NOT NULL,
				source INTEGER NOT NULL CHECK (source IN (1, 2)),
				value_text TEXT NOT NULL,
				value_norm TEXT NOT NULL,
				value_kind INTEGER NOT NULL,
				is_list INTEGER NOT NULL,
				list_ordinal INTEGER NOT NULL,
				PRIMARY KEY (note_id, property_id, source, list_ordinal, value_norm),
				FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE,
				FOREIGN KEY(property_id) REFERENCES property_keys(property_id) ON DELETE CASCADE
			);`,
			`CREATE TABLE IF NOT EXISTS note_tags (
				note_id INTEGER NOT NULL,
				tag_norm TEXT NOT NULL,
				PRIMARY KEY (note_id, tag_norm),
				FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
			);`,
			`CREATE TABLE IF NOT EXISTS note_metadata_state (
				notes_hash TEXT NOT NULL,
				raw_notes_hash TEXT NOT NULL DEFAULT '',
				loaded_at INTEGER NOT NULL,
				ready INTEGER NOT NULL DEFAULT 0
			);`,
			`CREATE INDEX IF NOT EXISTS idx_property_keys_name ON property_keys(property_name);`,
			`CREATE INDEX IF NOT EXISTS idx_note_property_values_lookup ON note_property_values(property_id, value_norm, note_id);`,
			`CREATE INDEX IF NOT EXISTS idx_note_property_values_note ON note_property_values(note_id, property_id, source);`,
			`CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags(tag_norm, note_id);`,
			`CREATE INDEX IF NOT EXISTS idx_note_metadata_state_loaded ON note_metadata_state(loaded_at DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_notes_indexed_at ON notes(indexed_at, path);`,
			`CREATE INDEX IF NOT EXISTS idx_files_call_edges_stale ON files(call_edges_stale, path);`,
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
	},
	func(ctx context.Context, db execer) error { // v29 -> v30
		_, err := db.ExecContext(ctx, `ALTER TABLE note_metadata_state ADD COLUMN raw_notes_hash TEXT NOT NULL DEFAULT '';`)
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return nil
		}
		return err
	},
	func(ctx context.Context, db execer) error { // v30 -> v31
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
	},
	func(ctx context.Context, db execer) error { // v31 -> v32
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS ontology_note_assessments (
				note_path TEXT NOT NULL PRIMARY KEY,
				declared_type TEXT NOT NULL,
				resolved_type TEXT NOT NULL,
				assessment_json TEXT NOT NULL,
				schema_hash TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_note_assessments_resolved ON ontology_note_assessments(resolved_type, note_path);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v32 -> v33
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS ontology_note_state (
				note_path TEXT NOT NULL PRIMARY KEY,
				input_fingerprint TEXT NOT NULL,
				schema_hash TEXT NOT NULL,
				resolved_type TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_note_state_schema ON ontology_note_state(schema_hash, note_path);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v33 -> v34
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS ontology_type_policies (
					type_name TEXT NOT NULL PRIMARY KEY,
					policy_json TEXT NOT NULL,
					schema_hash TEXT NOT NULL,
					updated_at INTEGER NOT NULL
				) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_type_policies_schema ON ontology_type_policies(schema_hash, type_name);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v34 -> v35
		// Add confidence columns to graph_doc_edges. Guard each ALTER with a column-existence
		// check so this migration is idempotent (safe if re-run after manual state resets).
		type colAdd struct {
			col  string
			stmt string
		}
		cols := []colAdd{
			{"confidence", `ALTER TABLE graph_doc_edges ADD COLUMN confidence TEXT NOT NULL DEFAULT 'extracted'`},
			{"confidence_score", `ALTER TABLE graph_doc_edges ADD COLUMN confidence_score REAL NOT NULL DEFAULT 1.0`},
			{"source_location", `ALTER TABLE graph_doc_edges ADD COLUMN source_location TEXT NOT NULL DEFAULT ''`},
		}
		if q, ok := db.(queryRower); ok {
			for _, c := range cols {
				has, err := tableHasColumnQuery(ctx, q, "graph_doc_edges", c.col)
				if err != nil {
					return err
				}
				if has {
					continue
				}
				if _, err := db.ExecContext(ctx, c.stmt); err != nil {
					return err
				}
			}
		} else {
			for _, c := range cols {
				if _, err := db.ExecContext(ctx, c.stmt); err != nil {
					return err
				}
			}
		}
		if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_graph_doc_edges_confidence ON graph_doc_edges(confidence)`); err != nil {
			return err
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v35 -> v36
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
	},
	func(ctx context.Context, db execer) error { // v36 -> v37
		tx, ok := db.(*sql.Tx)
		if !ok {
			return errors.New("intel schema migration v37 requires sql transaction")
		}
		stmts := []string{
			`CREATE INDEX IF NOT EXISTS idx_files_call_edges_stale ON files(call_edges_stale, path);`,
			`DROP INDEX IF EXISTS idx_intel_doc_sections_level;`,
			`DROP INDEX IF EXISTS idx_intel_chunks_owner_type;`,
			`DROP INDEX IF EXISTS idx_intel_embeddings_dimensions;`,
			`DROP TABLE IF EXISTS file_symbols;`,
			`CREATE TABLE IF NOT EXISTS intel_symbol_ref_targets (
				target_id INTEGER PRIMARY KEY,
				dst_lang  TEXT NOT NULL,
				dst_pkg   TEXT NOT NULL,
				dst_name  TEXT NOT NULL,
				dst_fqn   TEXT NOT NULL,
				dst_member INTEGER NOT NULL DEFAULT 0 CHECK (dst_member IN (0, 1)),
				UNIQUE (dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
			) STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_targets_name ON intel_symbol_ref_targets(dst_lang, dst_name);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_targets_fqn ON intel_symbol_ref_targets(dst_lang, dst_fqn);`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}

		hasRefsTable, err := tableExistsQuery(ctx, tx, "intel_symbol_refs")
		if err != nil {
			return err
		}
		hasSrcPath := false
		if hasRefsTable {
			hasSrcPath, err = tableHasColumnQuery(ctx, tx, "intel_symbol_refs", "src_path")
			if err != nil {
				return err
			}
		}
		if hasSrcPath {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE intel_symbol_refs RENAME TO intel_symbol_refs_legacy`); err != nil {
				return err
			}
		} else if hasRefsTable {
			if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS intel_symbol_refs`); err != nil {
				return err
			}
		}

		createRefs := []string{
			`CREATE TABLE IF NOT EXISTS intel_symbol_refs (
				src_file_id INTEGER NOT NULL,
				owner_symbol_id INTEGER,
				owner_fqn TEXT NOT NULL,
				ref_kind TEXT NOT NULL,
				dst_target_id INTEGER NOT NULL,
				PRIMARY KEY (src_file_id, owner_fqn, ref_kind, dst_target_id),
				FOREIGN KEY(dst_target_id) REFERENCES intel_symbol_ref_targets(target_id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_symbol_refs_dst_target ON intel_symbol_refs(dst_target_id, src_file_id, owner_symbol_id);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_symbol_refs_owner ON intel_symbol_refs(owner_symbol_id, src_file_id, dst_target_id) WHERE owner_symbol_id IS NOT NULL;`,
		}
		for _, stmt := range createRefs {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}

		exists, err := tableExistsQuery(ctx, tx, "intel_symbol_refs_legacy")
		if err != nil {
			return err
		}
		if exists {
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO intel_symbol_ref_files(path)
				SELECT DISTINCT src_path
				FROM intel_symbol_refs_legacy
			`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn)
				SELECT DISTINCT dst_lang, dst_pkg, dst_name, dst_fqn
				FROM intel_symbol_refs_legacy
			`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO intel_symbol_refs(src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id)
				SELECT sf.file_id, s.id, l.owner_fqn, l.ref_kind, t.target_id
				FROM intel_symbol_refs_legacy l
				JOIN intel_symbol_ref_files sf ON sf.path = l.src_path
				LEFT JOIN symbols s ON s.fqn = l.owner_fqn
				JOIN intel_symbol_ref_targets t
				  ON t.dst_lang = l.dst_lang
				 AND t.dst_pkg = l.dst_pkg
				 AND t.dst_name = l.dst_name
				 AND t.dst_fqn = l.dst_fqn
			`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DROP TABLE intel_symbol_refs_legacy`); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v37 -> v38
		tx, ok := db.(*sql.Tx)
		if ok {
			if err := dropVecTablesByPrefixTx(ctx, tx, intelVecTablePrefix); err != nil {
				return err
			}
		}
		stmts := []string{
			`DROP TABLE IF EXISTS intel_fts_rowid;`,
			`DROP TABLE IF EXISTS intel_fts;`,
			`DROP TABLE IF EXISTS intel_embeddings;`,
			`DROP TABLE IF EXISTS intel_chunks;`,
			`DROP TABLE IF EXISTS intel_edges;`,
			`DROP TABLE IF EXISTS intel_doc_sections;`,
			`DROP TABLE IF EXISTS intel_code_anchors;`,
			`CREATE TABLE IF NOT EXISTS intel_code_anchors (
					id INTEGER PRIMARY KEY,
					anchor_id TEXT NOT NULL UNIQUE CHECK (anchor_id != ''),
					lang TEXT NOT NULL CHECK (lang != ''),
					kind TEXT NOT NULL CHECK (kind != ''),
					path TEXT NOT NULL CHECK (path != ''),
					symbol TEXT NOT NULL CHECK (symbol != ''),
					fqn TEXT,
					signature TEXT,
					doc_comment TEXT,
					start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
					end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
					start_line INTEGER NOT NULL CHECK (start_line >= 0),
					end_line INTEGER NOT NULL CHECK (end_line >= start_line),
					fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
					updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
				) STRICT;`,
			`CREATE TABLE IF NOT EXISTS intel_doc_sections (
					id INTEGER PRIMARY KEY,
					section_id TEXT NOT NULL UNIQUE CHECK (section_id != ''),
					path TEXT NOT NULL CHECK (path != ''),
					title TEXT NOT NULL,
					level INTEGER NOT NULL CHECK (level >= 1 AND level <= 6),
					start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
					end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
					content TEXT NOT NULL,
					fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
					updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
				) STRICT;`,
			`CREATE TABLE IF NOT EXISTS intel_edges (
					src_type TEXT NOT NULL DEFAULT 'anchor' CHECK (src_type IN ('anchor', 'doc_section')),
					src_row_id INTEGER,
					dst_type TEXT NOT NULL DEFAULT 'anchor' CHECK (dst_type IN ('anchor', 'doc_section')),
					dst_row_id INTEGER,
					kind TEXT NOT NULL CHECK (kind != ''),
					meta_json TEXT,
					src_id TEXT NOT NULL CHECK (src_id != ''),
					dst_id TEXT NOT NULL CHECK (dst_id != ''),
					PRIMARY KEY (src_id, dst_id, kind)
				) WITHOUT ROWID, STRICT;`,
			`CREATE TABLE IF NOT EXISTS intel_chunks (
					id INTEGER PRIMARY KEY,
					chunk_id TEXT NOT NULL UNIQUE CHECK (chunk_id != ''),
					owner_id TEXT NOT NULL CHECK (owner_id != ''),
					owner_row_id INTEGER,
					owner_type TEXT NOT NULL CHECK (owner_type IN ('anchor', 'doc_section')),
					chunk_family TEXT NOT NULL DEFAULT 'default' CHECK (chunk_family != ''),
					ord INTEGER NOT NULL CHECK (ord >= 0),
					granularity TEXT NOT NULL CHECK (granularity != ''),
					breadcrumb TEXT,
					heading TEXT,
					content_hash TEXT NOT NULL CHECK (content_hash != ''),
					start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
					end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
					updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
					UNIQUE(owner_type, owner_id, ord, granularity)
				) STRICT;`,
			`CREATE TABLE IF NOT EXISTS intel_embeddings (
					chunk_row_id INTEGER PRIMARY KEY,
					chunk_id TEXT NOT NULL UNIQUE,
					norm REAL NOT NULL DEFAULT 0,
					dimensions INTEGER NOT NULL CHECK (dimensions > 0),
					created_at INTEGER NOT NULL CHECK (created_at >= 0),
					FOREIGN KEY(chunk_row_id) REFERENCES intel_chunks(id) ON DELETE CASCADE
				) STRICT;`,
			`CREATE VIRTUAL TABLE IF NOT EXISTS intel_fts USING fts5(
					item_type,
					item_id,
					path,
					title,
					body,
					tokenize='porter'
				);`,
			`CREATE TABLE IF NOT EXISTS intel_fts_rowid (
					item_type TEXT NOT NULL CHECK (item_type != ''),
					item_id TEXT NOT NULL CHECK (item_id != ''),
					fts_rowid INTEGER NOT NULL CHECK (fts_rowid > 0),
					PRIMARY KEY (item_type, item_id)
				) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_path ON intel_code_anchors(path);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_symbol ON intel_code_anchors(symbol);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_path_symbol ON intel_code_anchors(path, symbol);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_lang_fqn ON intel_code_anchors(lang, fqn) WHERE fqn IS NOT NULL;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_doc_sections_path ON intel_doc_sections(path);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_dst ON intel_edges(kind, dst_type, dst_row_id);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_src ON intel_edges(kind, src_type, src_row_id);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_edges_mentions_dst ON intel_edges(dst_row_id) WHERE kind = 'mentions' AND dst_type = 'anchor' AND dst_row_id IS NOT NULL;`,
			`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner ON intel_chunks(owner_type, owner_id, ord);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner_id ON intel_chunks(owner_id);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_chunks_family_owner ON intel_chunks(chunk_family, owner_type, owner_id, ord);`,
			`CREATE INDEX IF NOT EXISTS idx_intel_fts_rowid_item ON intel_fts_rowid(item_type, item_id);`,
			`UPDATE files SET hash = NULL;`,
			`UPDATE notes SET mtime = 0, content_hash = '', indexer_version = '';`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v38 -> v39
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
	},
	func(ctx context.Context, db execer) error { // v39 -> v40
		_, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_intel_module_defs_src;`)
		return err
	},
	func(ctx context.Context, db execer) error { // v40 -> v41
		_, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_symbols_lang_fqn_reversed ON symbols(lang, fqn_reversed);`)
		return err
	},
	func(ctx context.Context, db execer) error { // v41 -> v42
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
	},
	func(ctx context.Context, db execer) error { // v42 -> v43
		_, err := db.ExecContext(ctx, `ALTER TABLE ontology_schema_state ADD COLUMN broken_links_json TEXT;`)
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return nil
		}
		return err
	},
	func(context.Context, execer) error { return nil }, // v43 -> v44 (retired generated-card schema)
	func(ctx context.Context, db execer) error { // v44 -> v45
		stmts := []string{
			`ALTER TABLE intel_chunks ADD COLUMN chunk_family TEXT NOT NULL DEFAULT 'default' CHECK (chunk_family != '');`,
			`CREATE INDEX IF NOT EXISTS idx_intel_chunks_family_owner ON intel_chunks(chunk_family, owner_type, owner_id, ord);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
					continue
				}
				return err
			}
		}
		return nil
	},
	func(context.Context, execer) error { return nil }, // v45 -> v46 (retired generated-card schema)
	func(ctx context.Context, db execer) error { // v46 -> v47
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS ontology_nodes (
				id INTEGER PRIMARY KEY,
				node_id TEXT NOT NULL UNIQUE CHECK (node_id != ''),
				note_path TEXT NOT NULL CHECK (note_path != ''),
				node_ref_json TEXT NOT NULL CHECK (node_ref_json != ''),
				node_kind TEXT NOT NULL CHECK (node_kind != ''),
				type_name TEXT,
				parent_node_id TEXT,
				parent_type_name TEXT,
				title TEXT,
				source_locator TEXT NOT NULL DEFAULT '',
				fragment TEXT NOT NULL DEFAULT '',
				block_id TEXT NOT NULL DEFAULT '',
				display_label TEXT NOT NULL DEFAULT '',
				locator_status TEXT NOT NULL DEFAULT '',
				start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
				end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
				structural_fingerprint TEXT,
				schema_hash TEXT,
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
			) STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_path ON ontology_nodes(note_path);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_type ON ontology_nodes(type_name);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_source_locator ON ontology_nodes(source_locator);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_fragment ON ontology_nodes(note_path, fragment);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_block ON ontology_nodes(note_path, block_id);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if err := recreateIntelChunksWithOntologyNode(ctx, db); err != nil {
			return err
		}
		sidecarStmts := []string{
			`CREATE TABLE IF NOT EXISTS ontology_node_embedding_state (
				chunk_id TEXT PRIMARY KEY CHECK (chunk_id != ''),
				node_id TEXT NOT NULL CHECK (node_id != ''),
				note_path TEXT NOT NULL CHECK (note_path != ''),
				type_name TEXT,
				node_kind TEXT NOT NULL CHECK (node_kind != ''),
				embedding_schema_signature TEXT NOT NULL,
				node_structure_fingerprint TEXT NOT NULL,
				source_content_hash TEXT NOT NULL,
				chunk_text_hash TEXT NOT NULL,
				chunk_granularity TEXT NOT NULL,
				provider TEXT,
				model TEXT,
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
				FOREIGN KEY(chunk_id) REFERENCES intel_chunks(chunk_id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT;`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_node_embedding_state_node ON ontology_node_embedding_state(node_id);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_node_embedding_state_note ON ontology_node_embedding_state(note_path);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_node_embedding_state_type ON ontology_node_embedding_state(type_name);`,
		}
		for _, stmt := range sidecarStmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v47 -> v48
		stmts := []string{
			`ALTER TABLE ontology_edges RENAME TO ontology_edges_legacy_node_scope`,
			`CREATE TABLE ontology_edges (
				src_path TEXT NOT NULL,
				src_node_id TEXT NOT NULL DEFAULT '',
				relation_name TEXT NOT NULL,
				dst_path TEXT NOT NULL,
				dst_node_id TEXT NOT NULL DEFAULT '',
				dst_type TEXT NOT NULL,
				provenance TEXT NOT NULL,
				structural INTEGER NOT NULL,
				schema_hash TEXT NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (src_path, src_node_id, relation_name, dst_path, dst_node_id, provenance)
			) WITHOUT ROWID, STRICT;`,
			`INSERT OR REPLACE INTO ontology_edges (src_path, src_node_id, relation_name, dst_path, dst_node_id, dst_type, provenance, structural, schema_hash, updated_at)
			 SELECT src_path, '', relation_name, dst_path, '', dst_type, provenance, structural, schema_hash, updated_at
			 FROM ontology_edges_legacy_node_scope`,
			`DROP TABLE ontology_edges_legacy_node_scope`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_edges_src ON ontology_edges(src_path, src_node_id, structural, relation_name);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_edges_dst ON ontology_edges(dst_path, dst_node_id, structural, relation_name);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_edges_src_query ON ontology_edges(src_path, src_node_id, provenance, relation_name, dst_type, dst_path);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_edges_dst_query ON ontology_edges(dst_path, dst_node_id, provenance, relation_name, src_path);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "no such table: ontology_edges") ||
					strings.Contains(strings.ToLower(err.Error()), "there is already another table or index with this name") {
					continue
				}
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v48 -> v49
		stmts := []string{
			`ALTER TABLE ontology_nodes ADD COLUMN source_locator TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE ontology_nodes ADD COLUMN fragment TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE ontology_nodes ADD COLUMN block_id TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE ontology_nodes ADD COLUMN display_label TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE ontology_nodes ADD COLUMN locator_status TEXT NOT NULL DEFAULT '';`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_source_locator ON ontology_nodes(source_locator);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_fragment ON ontology_nodes(note_path, fragment);`,
			`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_block ON ontology_nodes(note_path, block_id);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
					continue
				}
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v49 -> v50
		for _, stmt := range ontologyNodeFieldValueSchemaStatements() {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v50 -> v51
		for _, stmt := range ontologyNodeFieldValueDependencySchemaStatements() {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v51 -> v52
		// v49/v50 created ontology_node_field_values{,_dependencies} empty.
		// EnsureIndex's fast path skips when ontology_schema_state.SchemaHash and
		// NotesHash are unchanged, so an upgrade against an existing vault would
		// leave the new field tables empty and indexed reads would be wrong.
		// Clear the schema state so the next index/serve recomputes the read
		// model and populates the field/dependency rows.
		if _, err := db.ExecContext(ctx, `DELETE FROM ontology_schema_state`); err != nil {
			return err
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v52 -> v53
		if err := ensureValidationAndRationaleSchema(ctx, db); err != nil {
			return err
		}
		return nil
	},
	func(ctx context.Context, db execer) error { // v53 -> v54
		return ensureValidationAndRationaleSchema(ctx, db)
	},
	func(ctx context.Context, db execer) error { // v54 -> v55
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
	},
	func(ctx context.Context, db execer) error { // v55 -> v56
		// Generated answer cards were retired in favor of enriched primary
		// chunks. Remove their durable rows so embeddings and invalidation state
		// cascade away instead of lingering as unreachable index data.
		if _, err := db.ExecContext(ctx, `
			DELETE FROM intel_chunks
			WHERE chunk_family = 'ontology_card'
			   OR granularity IN ('card', 'node_card')`); err != nil {
			return err
		}
		for _, table := range []string{
			"indexed_card_context_diagnostics",
			"indexed_card_context_targets",
			"indexed_card_facts",
			"indexed_cards",
			"indexed_card_specs",
		} {
			if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
				return err
			}
		}
		// The primary node-body format changed with card removal. Clearing the
		// cached schema state makes the next index rebuild unchanged projections,
		// replacing old ordinals and preserving bodyless-node retrieval.
		if _, err := db.ExecContext(ctx, `DELETE FROM ontology_schema_state`); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM ontology_node_embedding_state`); err != nil {
			return err
		}
		return nil
	},
}

func ensureValidationAndRationaleSchema(ctx context.Context, db execer) error {
	if err := ensureRationaleFTSSchema(ctx, db); err != nil {
		return err
	}
	for _, stmt := range validationStateSchemaStatements() {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// OpenOptions controls SQLite store open behavior.
type OpenOptions struct {
	// Context carries diagnostics and cancellation through schema initialization.
	// Nil uses context.Background for compatibility with path-only callers.
	Context context.Context
	// TxLockMode sets sqlite _txlock mode (supported: immediate, exclusive).
	TxLockMode string
	// Pool controls SQLite connection pooling.
	Pool sqliteutil.Options
	// SkipIntegrityCheck omits PRAGMA quick_check for latency-sensitive callers
	// that do not claim database integrity. The default remains fail-safe.
	SkipIntegrityCheck bool
}

func ontologyNodeFieldValueSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS ontology_node_field_values (
			id INTEGER PRIMARY KEY,
			node_id TEXT NOT NULL CHECK (node_id != ''),
			note_path TEXT NOT NULL CHECK (note_path != ''),
			type_name TEXT NOT NULL DEFAULT '',
			field_name TEXT NOT NULL CHECK (field_name != ''),
			field_kind TEXT NOT NULL DEFAULT '',
			source_kind TEXT NOT NULL DEFAULT '',
			value_kind TEXT NOT NULL DEFAULT '',
			value_text TEXT NOT NULL DEFAULT '',
			value_norm TEXT NOT NULL DEFAULT '',
			value_bool INTEGER,
			value_int INTEGER,
			value_real REAL,
			value_date TEXT,
			value_datetime TEXT,
			target_node_id TEXT NOT NULL DEFAULT '',
			target_ref_json TEXT NOT NULL DEFAULT '',
			target_note_path TEXT NOT NULL DEFAULT '',
			target_type_name TEXT NOT NULL DEFAULT '',
			target_source_locator TEXT NOT NULL DEFAULT '',
			list_ordinal INTEGER NOT NULL DEFAULT 0,
			schema_hash TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
			FOREIGN KEY(node_id) REFERENCES ontology_nodes(node_id) ON DELETE CASCADE
		) STRICT;`,
		// IMPORTANT: this generic sidecar is node-scoped, not note-scoped like
		// note_property_values. It supports typed ontology predicates without
		// hard-coding ActionItem or per-type columns. Docs: [[ontology-indexed-read-model-contract]]
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ontology_node_field_values_unique ON ontology_node_field_values(node_id, field_name, list_ordinal, value_norm, target_node_id);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_node ON ontology_node_field_values(node_id, field_name, list_ordinal);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_norm ON ontology_node_field_values(type_name, field_name, value_norm, node_id) WHERE value_norm != '';`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_bool ON ontology_node_field_values(type_name, field_name, value_bool, node_id) WHERE value_bool IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_date ON ontology_node_field_values(type_name, field_name, value_date, node_id) WHERE value_date IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_datetime ON ontology_node_field_values(type_name, field_name, value_datetime, node_id) WHERE value_datetime IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_real ON ontology_node_field_values(type_name, field_name, value_real, node_id) WHERE value_real IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_int ON ontology_node_field_values(type_name, field_name, value_int, node_id) WHERE value_int IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_target ON ontology_node_field_values(type_name, field_name, target_node_id, node_id) WHERE target_node_id != '';`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_target_note ON ontology_node_field_values(type_name, field_name, target_note_path, target_type_name, node_id) WHERE target_note_path != '';`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_note ON ontology_node_field_values(note_path, node_id);`,
	}
}

func ontologyNodeFieldValueDependencySchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS ontology_node_field_value_dependencies (
			id INTEGER PRIMARY KEY,
			source_note_path TEXT NOT NULL CHECK (source_note_path != ''),
			node_id TEXT NOT NULL DEFAULT '',
			type_name TEXT NOT NULL DEFAULT '',
			field_name TEXT NOT NULL CHECK (field_name != ''),
			target_input TEXT NOT NULL DEFAULT '',
			target_input_norm TEXT NOT NULL DEFAULT '',
			resolved_target_note_path TEXT NOT NULL DEFAULT '',
			resolved_target_type_name TEXT NOT NULL DEFAULT '',
			schema_hash TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
		) STRICT;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ontology_node_field_value_dependencies_unique ON ontology_node_field_value_dependencies(source_note_path, node_id, field_name, target_input_norm);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_value_dependencies_source ON ontology_node_field_value_dependencies(source_note_path);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_value_dependencies_target_input ON ontology_node_field_value_dependencies(target_input_norm, source_note_path);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_value_dependencies_resolved_target ON ontology_node_field_value_dependencies(resolved_target_note_path, source_note_path) WHERE resolved_target_note_path != '';`,
	}
}

func validationStateSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS validation_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			status TEXT NOT NULL DEFAULT 'never_ran' CHECK (status IN ('never_ran', 'running', 'ok', 'error')),
			result_json TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			generation INTEGER NOT NULL DEFAULT 0,
			published_generation INTEGER NOT NULL DEFAULT 0,
			started_at INTEGER,
			finished_at INTEGER,
			duration_ms INTEGER NOT NULL DEFAULT 0
		) STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_generations (
			generation INTEGER PRIMARY KEY,
			vault_identity TEXT NOT NULL,
			scope TEXT NOT NULL,
			selected_checks_json TEXT NOT NULL,
			schema_identity TEXT NOT NULL DEFAULT '',
			config_identity TEXT NOT NULL DEFAULT '',
			input_revision TEXT NOT NULL DEFAULT '',
			started_at INTEGER NOT NULL DEFAULT 0,
			finished_at INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
			completion TEXT NOT NULL CHECK (completion IN ('complete', 'incomplete')),
			stale_reason TEXT NOT NULL DEFAULT '',
			issue_count INTEGER NOT NULL DEFAULT 0 CHECK (issue_count >= 0),
			error_count INTEGER NOT NULL DEFAULT 0 CHECK (error_count >= 0),
			affected_file_count INTEGER NOT NULL DEFAULT 0 CHECK (affected_file_count >= 0),
			affected_note_count INTEGER NOT NULL DEFAULT 0 CHECK (affected_note_count >= 0),
			repair_action_count INTEGER NOT NULL DEFAULT 0 CHECK (repair_action_count >= 0),
			repair_plan_fingerprint TEXT NOT NULL DEFAULT ''
		) STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_checks (
			generation INTEGER NOT NULL,
			check_order INTEGER NOT NULL CHECK (check_order >= 0),
			check_name TEXT NOT NULL,
			outcome TEXT NOT NULL CHECK (outcome IN ('completed', 'failed', 'blocked', 'not_applicable', 'skipped')),
			issue_count INTEGER NOT NULL DEFAULT 0 CHECK (issue_count >= 0),
			summary TEXT NOT NULL DEFAULT '',
			notes_json TEXT NOT NULL DEFAULT '[]',
			error TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
			PRIMARY KEY (generation, check_name),
			FOREIGN KEY(generation) REFERENCES validation_generations(generation) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_diagnostics (
			generation INTEGER NOT NULL,
			diagnostic_order INTEGER NOT NULL CHECK (diagnostic_order >= 0),
			issue_key TEXT NOT NULL,
			check_name TEXT NOT NULL,
			code TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			evidence_json TEXT NOT NULL DEFAULT '',
			primary_path TEXT NOT NULL DEFAULT '',
			type_name TEXT NOT NULL DEFAULT '',
			field_name TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			target TEXT NOT NULL DEFAULT '',
			location_unit TEXT NOT NULL DEFAULT '' CHECK (location_unit IN ('', 'utf8_bytes', 'line')),
			location_start INTEGER NOT NULL DEFAULT 0 CHECK (location_start >= 0),
			location_end INTEGER NOT NULL DEFAULT 0 CHECK (location_end >= location_start),
			node_id TEXT NOT NULL DEFAULT '',
			location_field TEXT NOT NULL DEFAULT '',
			location_relation TEXT NOT NULL DEFAULT '',
			variant_key TEXT NOT NULL DEFAULT '',
			variant_label TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (generation, issue_key),
			FOREIGN KEY(generation) REFERENCES validation_generations(generation) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_diagnostic_paths (
			generation INTEGER NOT NULL,
			issue_key TEXT NOT NULL,
			path TEXT NOT NULL,
			membership_kind TEXT NOT NULL CHECK (membership_kind IN ('affected', 'note')),
			PRIMARY KEY (generation, issue_key, path, membership_kind),
			FOREIGN KEY(generation, issue_key) REFERENCES validation_diagnostics(generation, issue_key) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_diagnostic_scopes (
			generation INTEGER NOT NULL,
			issue_key TEXT NOT NULL,
			scope_kind TEXT NOT NULL CHECK (scope_kind IN ('node', 'type', 'interface')),
			scope_key TEXT NOT NULL,
			PRIMARY KEY (generation, issue_key, scope_kind, scope_key),
			FOREIGN KEY(generation, issue_key) REFERENCES validation_diagnostics(generation, issue_key) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_actions (
			generation INTEGER NOT NULL,
			action_id TEXT NOT NULL,
			check_name TEXT NOT NULL,
			issue_code TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			safety TEXT NOT NULL CHECK (safety IN ('safe', 'needs_confirmation', 'agent_required')),
			title TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			question TEXT NOT NULL DEFAULT '',
			instance_count INTEGER NOT NULL DEFAULT 0 CHECK (instance_count >= 0),
			candidate_paths_json TEXT NOT NULL DEFAULT '[]',
			PRIMARY KEY (generation, action_id),
			FOREIGN KEY(generation) REFERENCES validation_generations(generation) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_action_issues (
			generation INTEGER NOT NULL,
			action_id TEXT NOT NULL,
			issue_key TEXT NOT NULL,
			PRIMARY KEY (generation, action_id, issue_key),
			FOREIGN KEY(generation, action_id) REFERENCES validation_actions(generation, action_id) ON DELETE CASCADE,
			FOREIGN KEY(generation, issue_key) REFERENCES validation_diagnostics(generation, issue_key) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_action_paths (
			generation INTEGER NOT NULL,
			action_id TEXT NOT NULL,
			path TEXT NOT NULL,
			PRIMARY KEY (generation, action_id, path),
			FOREIGN KEY(generation, action_id) REFERENCES validation_actions(generation, action_id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_validation_checks_order ON validation_checks(generation, check_order);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_diagnostics_page ON validation_diagnostics(generation, diagnostic_order);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_diagnostics_check_code ON validation_diagnostics(generation, check_name, code, diagnostic_order);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_diagnostic_paths_scope ON validation_diagnostic_paths(generation, path, issue_key);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_diagnostic_scopes_scope ON validation_diagnostic_scopes(generation, scope_kind, scope_key, issue_key);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_action_issues_issue ON validation_action_issues(generation, issue_key, action_id);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_action_paths_scope ON validation_action_paths(generation, path, action_id);`,
	}
}

func createNoteFragmentTargetsSchema(ctx context.Context, db execer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS note_fragment_targets (
			note_id INTEGER NOT NULL,
			target_kind TEXT NOT NULL CHECK (target_kind IN ('heading', 'block', 'element_id', 'legacy_name')),
			target_text TEXT NOT NULL CHECK (target_text != ''),
			target_norm TEXT NOT NULL CHECK (target_norm != ''),
			ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_fragment_targets_note ON note_fragment_targets(note_id, target_kind, ordinal);`,
		`CREATE INDEX IF NOT EXISTS idx_note_fragment_targets_lookup ON note_fragment_targets(note_id, target_kind, target_norm COLLATE BINARY, ordinal);`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func migrateNotePublicationFactsSchema(ctx context.Context, tx *sql.Tx) error {
	var oldTargets int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'note_markdown_targets'`).Scan(&oldTargets); err != nil {
		return err
	}
	if err := createNoteFragmentTargetsSchema(ctx, tx); err != nil {
		return err
	}
	if oldTargets > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO note_fragment_targets(note_id, target_kind, target_text, target_norm, ordinal)
			SELECT note_id, target_kind, target_text, target_norm, ordinal FROM note_markdown_targets
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE note_markdown_targets`); err != nil {
			return err
		}
	}
	return createNoteProjectionFactsSchema(ctx, tx)
}

func migrateValidationDiagnosticSchema(ctx context.Context, tx *sql.Tx) error {
	hasPublished, err := tableHasColumnQuery(ctx, tx, "validation_state", "published_generation")
	if err != nil {
		return err
	}
	if !hasPublished {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE validation_state ADD COLUMN published_generation INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	for _, stmt := range validationStateSchemaStatements()[1:] {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// Legacy result_json was rendered with a per-check cap and cannot prove a
	// complete diagnostic identity set. Force an ordinary refresh instead of
	// promoting it into the generation tables.
	_, err = tx.ExecContext(ctx, `
		UPDATE validation_state
		SET status = 'never_ran', result_json = '', error = '', published_generation = 0,
			started_at = NULL, finished_at = NULL, duration_ms = 0
	`)
	return err
}

func createNoteProjectionFactsSchema(ctx context.Context, db execer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS note_projection_diagnostics (
			note_id INTEGER NOT NULL,
			ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
			code TEXT NOT NULL CHECK (code != ''),
			category TEXT NOT NULL CHECK (category != ''),
			message TEXT NOT NULL CHECK (message != ''),
			range_present INTEGER NOT NULL CHECK (range_present IN (0, 1)),
			start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
			end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
			blocking INTEGER NOT NULL CHECK (blocking IN (0, 1)),
			affected_operation TEXT NOT NULL CHECK (affected_operation != ''),
			PRIMARY KEY(note_id, ordinal),
			CHECK (range_present = 1 OR (start_byte = 0 AND end_byte = 0)),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_projection_diagnostics_operation ON note_projection_diagnostics(affected_operation, blocking, note_id);`,
		`CREATE TABLE IF NOT EXISTS note_search_regions (
			note_id INTEGER NOT NULL,
			ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
			origin TEXT NOT NULL CHECK (origin IN ('authored', 'derived')),
			region_kind TEXT NOT NULL CHECK (region_kind IN ('visible', 'supplemental')),
			region_text TEXT NOT NULL CHECK (region_text != ''),
			media_type TEXT NOT NULL CHECK (media_type != ''),
			range_present INTEGER NOT NULL CHECK (range_present IN (0, 1)),
			start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
			end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
			PRIMARY KEY(note_id, ordinal),
			CHECK (range_present = 1 OR (start_byte = 0 AND end_byte = 0)),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_search_regions_kind ON note_search_regions(region_kind, origin, note_id, ordinal);`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func addOntologyMaterializationVersionSchema(ctx context.Context, tx *sql.Tx) error {
	hasColumn, err := tableHasColumnQuery(ctx, tx, "ontology_schema_state", "materialization_version")
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}
	_, err = tx.ExecContext(ctx, `
		ALTER TABLE ontology_schema_state
		ADD COLUMN materialization_version INTEGER NOT NULL DEFAULT 0 CHECK (materialization_version >= 0)
	`)
	return err
}

// addOntologyAssessmentFlagsSchema adds the materialized inventory flags to
// ontology_note_assessments. The columns default to 0; the ontology
// materialization version bump, not this migration, populates them.
func addOntologyAssessmentFlagsSchema(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []string{"has_issues", "type_ambiguous"} {
		hasColumn, err := tableHasColumnQuery(ctx, tx, "ontology_note_assessments", column)
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		stmt := fmt.Sprintf(
			`ALTER TABLE ontology_note_assessments ADD COLUMN %s INTEGER NOT NULL DEFAULT 0 CHECK (%s IN (0, 1))`,
			column, column,
		)
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// Open opens or creates the SQLite store at the provided path.
func Open(path string) (*Store, error) {
	return OpenWithOptions(path, OpenOptions{})
}

// OpenWithOptions opens or creates the SQLite store at the provided path.
func OpenWithOptions(path string, opts OpenOptions) (*Store, error) {
	return openWithOptionsAtSchemaVersion(path, opts, currentSchemaVersion)
}

// OpenReadOnlyExisting opens a current, previously validated index without
// creating directories, migrating schema, repairing state, or permitting writes.
func OpenReadOnlyExisting(path string, ctx context.Context, pool sqliteutil.Options) (*Store, error) {
	return openValidatedExisting(path, ctx, pool, true)
}

// OpenSessionStoreExisting opens a current, previously validated index for
// session persistence without creating directories, migrating schema, repairing
// state, or running an integrity check. Callers must expose this handle only to
// the session-dedupe boundary; indexed retrieval uses OpenReadOnlyExisting.
func OpenSessionStoreExisting(path string, ctx context.Context, pool sqliteutil.Options) (SessionDedupeHandle, error) {
	store, err := openValidatedExisting(path, ctx, pool, false)
	if err != nil {
		return nil, err
	}
	return newBoundedSessionDedupeHandle(store), nil
}

func openValidatedExisting(path string, ctx context.Context, pool sqliteutil.Options, readOnly bool) (*Store, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{
		ReadOnly:      readOnly,
		ExistingWrite: !readOnly,
		// Managed readers must not wait inside SQLite, including during open
		// before context-aware validation. Session writes retain a short wait.
		NoBusyWait:    readOnly,
		BusyTimeoutMs: 100,
	}), pool)
	if err != nil {
		return nil, err
	}
	store := &Store{
		db:        db,
		closeFn:   db.Close,
		writeInfo: writeStats{label: "intel-store"},
		readOnly:  readOnly,
	}
	if err := store.validateExistingSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func openWithOptionsAtSchemaVersion(path string, opts OpenOptions, schemaVersion int) (*Store, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}
	openCtx := opts.Context
	if openCtx == nil {
		openCtx = context.Background()
	}
	// One creator or migrator per database file, held from before the first
	// connection (the WAL switch) through schema validation.
	releaseSchemaLock, err := sqliteutil.LockSchemaInit(openCtx, path)
	if err != nil {
		return nil, err
	}
	defer releaseSchemaLock()
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{
		TxLockMode: opts.TxLockMode,
	}), opts.Pool)
	if err != nil {
		return nil, err
	}

	// Quick integrity check for existing databases
	if _, statErr := os.Stat(path); statErr == nil && !opts.SkipIntegrityCheck {
		indexingperf.AddCount(openCtx, indexingperf.AgentStartOpIntegrityChecks, 1)
		if err := sqliteutil.QuickIntegrityCheck(db); err != nil {
			_ = db.Close()
			// Only wrap as "integrity check failed" if it's actual corruption.
			// Transient errors (busy, locked, timeout) should not be treated as corruption.
			var checkFailed sqliteutil.IntegrityCheckFailed
			if errors.As(err, &checkFailed) {
				return nil, fmt.Errorf("database integrity check failed: %w", err)
			}
			// For transient errors, return a different message that won't trigger
			// corruption recovery (which could delete the DB while another process uses it)
			return nil, fmt.Errorf("database unavailable: %w", err)
		}
	}

	store := &Store{
		db:        db,
		closeFn:   db.Close,
		writeInfo: writeStats{label: "intel-store"},
	}
	if err := store.ensureSchemaWithRecovery(openCtx, schemaVersion); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// OpenWithDB opens the store using an existing SQLite handle.
func OpenWithDB(db *sql.DB) (*Store, error) {
	return OpenWithDBContext(context.Background(), db)
}

// OpenWithDBContext opens the store using an existing SQLite handle while
// retaining caller diagnostics and cancellation during schema initialization.
func OpenWithDBContext(ctx context.Context, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("sqlite db is required")
	}
	store := &Store{
		db:        db,
		closeFn:   func() error { return nil },
		writeInfo: writeStats{label: "intel-store"},
	}
	if ctx == nil {
		ctx = context.Background()
	}
	releaseSchemaLock, err := sqliteutil.LockSchemaInitForDB(ctx, db)
	if err != nil {
		return nil, err
	}
	defer releaseSchemaLock()
	if err := store.ensureSchemaWithRecovery(ctx, currentSchemaVersion); err != nil {
		return nil, err
	}
	return store, nil
}

// Close releases resources.
func (s *Store) Close() error {
	if s == nil || s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

// Checkpoint flushes WAL writes to the main database file.
// Use truncate=true for aggressive checkpoint that truncates the WAL.
func (s *Store) Checkpoint(ctx context.Context, truncate bool) error {
	return sqliteutil.Checkpoint(ctx, s.db, truncate)
}

func (s *Store) ensureSchema(ctx context.Context, schemaVersion int) error {
	stmts := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS files (
			path TEXT PRIMARY KEY,
			lang TEXT NOT NULL,
			hash TEXT,
			indexer_version TEXT,
			parse_status TEXT NOT NULL DEFAULT 'ok',
			call_edges_stale INTEGER NOT NULL DEFAULT 0,
			mtime INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS symbols (
			id INTEGER PRIMARY KEY,
			lang TEXT NOT NULL,
			kind TEXT NOT NULL,
			file TEXT NOT NULL,
			pkg TEXT,
			name TEXT NOT NULL,
			fqn TEXT NOT NULL UNIQUE,
			fqn_reversed TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file);`,
		`CREATE INDEX IF NOT EXISTS idx_symbols_lang_pkg_name ON symbols(lang, pkg, name);`,
		`CREATE INDEX IF NOT EXISTS idx_symbols_fqn_reversed ON symbols(fqn_reversed);`,
		`CREATE INDEX IF NOT EXISTS idx_symbols_lang_fqn_reversed ON symbols(lang, fqn_reversed);`,
		`CREATE TABLE IF NOT EXISTS super_edges (
			child_fqn TEXT NOT NULL,
			parent_fqn TEXT NOT NULL,
			kind TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_super_child ON super_edges(child_fqn);`,
		`CREATE INDEX IF NOT EXISTS idx_super_parent ON super_edges(parent_fqn);`,
		`CREATE TABLE IF NOT EXISTS annotations (
			id INTEGER PRIMARY KEY,
			owner_fqn TEXT NOT NULL,
			ann_lang TEXT,
			ann_pkg TEXT,
			ann_name TEXT,
			args_json TEXT,
			confidence REAL DEFAULT 1.0
		);`,
		`CREATE INDEX IF NOT EXISTS idx_annotations_type ON annotations(ann_lang, ann_pkg, ann_name);`,
		`CREATE TABLE IF NOT EXISTS anchors (
			id INTEGER PRIMARY KEY,
			label TEXT NOT NULL UNIQUE,
			kind TEXT NOT NULL,
			lang TEXT,
			base_lang TEXT,
			base_pkg TEXT,
			base_name TEXT,
			base_member INTEGER NOT NULL DEFAULT 0,
			ann_lang TEXT,
			ann_pkg TEXT,
			ann_name TEXT,
			ann_args_json TEXT,
			path_prefix TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_anchors_callee ON anchors(kind, base_lang, base_name, base_pkg);`,
		`CREATE TABLE IF NOT EXISTS anchor_globs (
			anchor_id INTEGER NOT NULL,
			pattern TEXT NOT NULL,
			UNIQUE(anchor_id, pattern),
			FOREIGN KEY(anchor_id) REFERENCES anchors(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_globs_anchor ON anchor_globs(anchor_id);`,
		`CREATE TABLE IF NOT EXISTS notes (
			id INTEGER PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			title TEXT,
			content_hash TEXT NOT NULL DEFAULT '',
			indexer_version TEXT NOT NULL DEFAULT '',
			mtime INTEGER NOT NULL DEFAULT 0,
			size INTEGER NOT NULL DEFAULT 0,
			indexed_at INTEGER NOT NULL DEFAULT 0,
			note_key_full TEXT NOT NULL DEFAULT '',
			note_key_base TEXT NOT NULL DEFAULT '',
			path_len INTEGER NOT NULL DEFAULT 0,
			first_segment_norm TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_indexed_at ON notes(indexed_at, path);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_note_key_full ON notes(note_key_full);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_note_key_base ON notes(note_key_base, path_len, path);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_first_segment_norm ON notes(first_segment_norm, path);`,
		`CREATE TABLE IF NOT EXISTS property_keys (
			property_id INTEGER PRIMARY KEY,
			property_name TEXT NOT NULL UNIQUE,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_property_keys_name ON property_keys(property_name);`,
		`CREATE TABLE IF NOT EXISTS note_property_values (
			note_id INTEGER NOT NULL,
			property_id INTEGER NOT NULL,
			source INTEGER NOT NULL CHECK (source IN (1, 2)),
			value_text TEXT NOT NULL,
			value_norm TEXT NOT NULL,
			value_kind INTEGER NOT NULL,
			is_list INTEGER NOT NULL,
			list_ordinal INTEGER NOT NULL,
			PRIMARY KEY (note_id, property_id, source, list_ordinal, value_norm),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE,
			FOREIGN KEY(property_id) REFERENCES property_keys(property_id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_note_property_values_lookup ON note_property_values(property_id, value_norm, note_id);`,
		`CREATE INDEX IF NOT EXISTS idx_note_property_values_note ON note_property_values(note_id, property_id, source);`,
		`CREATE TABLE IF NOT EXISTS note_tags (
			note_id INTEGER NOT NULL,
			tag_norm TEXT NOT NULL,
			PRIMARY KEY (note_id, tag_norm),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags(tag_norm, note_id);`,
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
		`CREATE TABLE IF NOT EXISTS note_metadata_state (
			notes_hash TEXT NOT NULL,
			raw_notes_hash TEXT NOT NULL DEFAULT '',
			loaded_at INTEGER NOT NULL,
			ready INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE INDEX IF NOT EXISTS idx_note_metadata_state_loaded ON note_metadata_state(loaded_at DESC);`,
		`CREATE TABLE IF NOT EXISTS note_anchors (
			note_id INTEGER NOT NULL,
			anchor_id INTEGER NOT NULL,
			UNIQUE(note_id, anchor_id),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE,
			FOREIGN KEY(anchor_id) REFERENCES anchors(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS anchor_scopes (
			anchor_id INTEGER NOT NULL,
			symbol_fqn TEXT,
			call_file TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_scopes_anchor ON anchor_scopes(anchor_id);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_scopes_symbol ON anchor_scopes(symbol_fqn);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_scopes_call_file ON anchor_scopes(call_file);`,
		`CREATE TABLE IF NOT EXISTS index_metadata (
			key TEXT PRIMARY KEY,
			value TEXT
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.bootstrapIntelBaseline(ctx); err != nil {
		return err
	}
	// Create indices that depend on repaired columns.
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_annotations_owner ON annotations(owner_fqn);`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_annotations_type_owner ON annotations(ann_lang, ann_pkg, ann_name, owner_fqn);`); err != nil {
		return err
	}
	if err := s.ensureSchemaVersion(ctx, schemaVersion); err != nil {
		return err
	}
	return nil
}

// bootstrapIntelBaseline creates the consolidated v56 Intel schema for a fresh
// or reset domain. Historical schema steps are schema assembly details here;
// they are no longer replayed as independently committed upgrade migrations.
func (s *Store) bootstrapIntelBaseline(ctx context.Context) error {
	version, _, exists, err := s.intelSchemaState(ctx)
	if err != nil {
		return err
	}
	if exists && version >= intelBaselineVersion {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, build := range schemaMigrations {
			if err := build(ctx, tx); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schema_version`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES (?)`, intelBaselineVersion)
		return err
	})
}

func (s *Store) ensureSchemaWithRecovery(ctx context.Context, schemaVersion int) error {
	if plan, err := s.intelMigrationPlan(ctx, schemaVersion); err == nil {
		valid, observation, _ := migration.ProbeValidatedDomain(ctx, s.db, plan)
		if observation.Statements > 0 {
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
		}
		if valid {
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpSchemaStatements, int64(observation.Statements))
			return nil
		}
	}
	if reset, err := s.classifyIntelBaseline(ctx, schemaVersion); err != nil {
		return err
	} else if reset {
		if err := s.ResetDomain(ctx); err != nil {
			return err
		}
	}
	if err := s.ensureSchema(ctx, schemaVersion); err == nil {
		return nil
	} else {
		var drift *migration.ErrSchemaDrift
		if errors.As(err, &drift) {
			// Drift means declared version and actual schema shape diverged.
			// Intel tables are derived, so reset and rebuild them deterministically.
			if resetErr := s.ResetDomain(ctx); resetErr != nil {
				return resetErr
			}
			return s.ensureSchema(ctx, schemaVersion)
		}
		if !isSchemaError(err) {
			return err
		}
	}

	// Self-heal: drop and recreate code-intel/code-anchor tables, but intentionally preserve
	// embeddings tables (so unchanged code chunks can keep their existing embeddings).
	if err := s.ResetDomain(ctx); err != nil {
		return err
	}
	return s.ensureSchema(ctx, schemaVersion)
}

// classifyIntelBaseline inspects schema state before ensureSchema performs any
// writes. Intel is a disposable derived domain: pre-baseline and dirty schemas
// reset in place, while future schemas fail closed without mutation.
func (s *Store) classifyIntelBaseline(ctx context.Context, supportedVersion int) (bool, error) {
	version, dirty, exists, err := s.intelSchemaState(ctx)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	if version > supportedVersion {
		return false, &migration.ErrFutureSchema{
			Domain:    migration.DomainIntel,
			Current:   version,
			Supported: supportedVersion,
		}
	}
	return dirty || version < intelBaselineVersion, nil
}

func (s *Store) intelSchemaState(ctx context.Context) (version int, dirty bool, exists bool, err error) {
	var metadataExists int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'rzm_migration_state'
	`).Scan(&metadataExists); err != nil {
		return 0, false, false, err
	}
	if metadataExists > 0 {
		var dirtyInt int
		err := s.db.QueryRowContext(ctx, `
			SELECT version, dirty FROM rzm_migration_state WHERE domain = ?
		`, string(migration.DomainIntel)).Scan(&version, &dirtyInt)
		if err == nil {
			return version, dirtyInt != 0, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, false, false, err
		}
	}

	var legacyExists int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'schema_version'
	`).Scan(&legacyExists); err != nil {
		return 0, false, false, err
	}
	if legacyExists == 0 {
		return 0, false, false, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		return 0, false, false, err
	}
	return version, false, true, nil
}

// ResetDomain drops and recreates the code-index domain tables (code anchors + intel + doc_links).
// It does NOT touch semantic embeddings tables (`emb_*`) nor code embeddings tables (`code_*`).
func (s *Store) ResetDomain(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "intel.reset_domain")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
		defer func() {
			_, _ = db.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
		}()

		// Drop views first (a corrupted DB can have a view shadowing a table name).
		dropViews := []string{
			"schema_version",
			"files",
			"symbols",
			"super_edges",
			"annotations",
			"calls",
			"anchors",
			"anchor_globs",
			"notes",
			"property_keys",
			"note_property_values",
			"note_tags",
			"note_metadata_state",
			"note_projection_state",
			"note_fragment_targets",
			"note_projection_diagnostics",
			"note_search_regions",
			"note_anchors",
			"anchor_scopes",
			"index_metadata",
			"doc_links",
			"intel_code_anchors",
			"intel_doc_sections",
			"intel_edges",
			"intel_symbol_ref_files",
			"intel_symbol_ref_targets",
			"intel_symbol_refs",
			"intel_external_targets",
			"intel_external_target_map",
			"intel_external_symbol_evidence",
			"intel_external_import_evidence",
			"intel_import_refs",
			"intel_module_defs",
			"intel_go_package_relationship_state",
			"intel_go_package_files",
			"intel_go_derived_relationships",
			"intel_chunks",
			"intel_fts",
			"intel_fts_rowid",
			"intel_rationale",
			"intel_rationale_fts",
			"intel_rationale_fts_rowid",
			"graph_doc_scores",
			"graph_doc_edges",
			"graph_anchor_scores",
			"ontology_note_assessments",
			"ontology_note_state",
			"ontology_note_types",
			"ontology_edges",
			"ontology_type_policies",
			"ontology_schema_state",
			"ontology_nodes",
			"ontology_node_field_values",
			"ontology_node_field_value_dependencies",
			"ontology_node_embedding_state",
			"mcp_session_item_reservations",
			"mcp_session_maintenance",
		}
		for _, name := range dropViews {
			if err := dropViewIfExists(ctx, db, name); err != nil {
				return err
			}
		}

		// Indices (best-effort; dropping tables will remove their indices, but keep this resilient).
		dropIndexes := []string{
			"idx_symbols_file",
			"idx_symbols_lang_pkg_name",
			"idx_symbols_lang_fqn_reversed",
			"idx_super_child",
			"idx_super_parent",
			"idx_annotations_owner",
			"idx_annotations_type",
			"idx_annotations_type_owner",
			"idx_calls_file",
			"idx_calls_callee",
			"idx_calls_owner",
			"idx_anchors_callee",
			"idx_anchors_path_prefix",
			"idx_property_keys_name",
			"idx_note_property_values_lookup",
			"idx_note_property_values_note",
			"idx_note_tags_tag",
			"idx_note_metadata_state_loaded",
			"idx_note_projection_state_status",
			"idx_note_fragment_targets_note",
			"idx_note_fragment_targets_lookup",
			"idx_note_projection_diagnostics_operation",
			"idx_note_search_regions_kind",
			"idx_notes_indexed_at",
			"idx_anchor_globs_anchor",
			"idx_anchor_scopes_anchor",
			"idx_anchor_scopes_symbol",
			"idx_anchor_scopes_call_file",
			"idx_intel_code_anchors_path",
			"idx_intel_code_anchors_fqn",
			"idx_intel_code_anchors_lang_fqn",
			"idx_intel_edges_dst_kind",
			"idx_intel_edges_src_kind",
			"idx_intel_edges_kind_dst",
			"idx_intel_edges_kind_src",
			"idx_intel_edges_mentions_dst",
			"idx_intel_symbol_refs_dst_target",
			"idx_intel_symbol_refs_owner",
			"idx_intel_symbol_ref_files_path",
			"idx_intel_symbol_ref_targets_name",
			"idx_intel_symbol_ref_targets_fqn",
			"idx_intel_external_target_map_external",
			"idx_intel_external_symbol_evidence_external",
			"idx_intel_external_import_evidence_external",
			"idx_intel_import_refs_module",
			"idx_intel_module_defs_src",
			"idx_intel_module_defs_module",
			"idx_intel_go_package_files_package",
			"idx_intel_go_package_files_directory",
			"idx_intel_go_derived_relationship_target",
			"idx_intel_go_derived_relationship_source",
			"idx_intel_doc_sections_path",
			"idx_intel_fts_rowid_item",
			"idx_intel_rationale_path",
			"idx_intel_rationale_symbol",
			"idx_intel_rationale_kind",
			"idx_intel_rationale_fts_rowid",
			"idx_intel_chunks_owner",
			"idx_intel_chunks_owner_id",
			"idx_intent_embeddings_intent",
			"idx_doc_links_dst",
			"idx_doc_links_src",
			"idx_graph_doc_scores_authority",
			"idx_graph_doc_scores_community",
			"idx_graph_doc_edges_src",
			"idx_graph_doc_edges_dst",
			"idx_graph_doc_edges_kind_src",
			"idx_graph_anchor_scores_rank",
			"idx_files_call_edges_stale",
			"idx_ontology_note_assessments_resolved",
			"idx_ontology_note_state_schema",
			"idx_ontology_note_types_type",
			"idx_ontology_edges_src",
			"idx_ontology_edges_dst",
			"idx_ontology_edges_src_query",
			"idx_ontology_edges_dst_query",
			"idx_ontology_type_policies_schema",
			"idx_ontology_schema_state_loaded",
			"idx_ontology_nodes_note_path",
			"idx_ontology_nodes_type",
			"idx_ontology_nodes_source_locator",
			"idx_ontology_nodes_note_fragment",
			"idx_ontology_nodes_note_block",
			"idx_ontology_node_field_values_unique",
			"idx_ontology_node_field_values_node",
			"idx_ontology_node_field_values_norm",
			"idx_ontology_node_field_values_bool",
			"idx_ontology_node_field_values_date",
			"idx_ontology_node_field_values_datetime",
			"idx_ontology_node_field_values_real",
			"idx_ontology_node_field_values_int",
			"idx_ontology_node_field_values_target",
			"idx_ontology_node_field_values_target_note",
			"idx_ontology_node_field_values_note",
			"idx_ontology_node_field_value_dependencies_unique",
			"idx_ontology_node_field_value_dependencies_source",
			"idx_ontology_node_field_value_dependencies_target_input",
			"idx_ontology_node_field_value_dependencies_resolved_target",
			"idx_ontology_node_embedding_state_node",
			"idx_ontology_node_embedding_state_note",
			"idx_ontology_node_embedding_state_type",
			"idx_mcp_session_item_reservations_owner",
		}
		for _, name := range dropIndexes {
			if _, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
				return err
			}
		}

		// Tables (includes virtual tables).
		dropTables := []string{
			"graph_doc_scores",
			"graph_doc_edges",
			"graph_anchor_scores",
			"graph_web_revision",
			"ontology_note_assessments",
			"ontology_note_state",
			"ontology_note_types",
			"ontology_edges",
			"ontology_type_policies",
			"ontology_schema_state",
			"ontology_node_embedding_state",
			"ontology_node_field_value_dependencies",
			"ontology_node_field_values",
			"ontology_nodes",
			"note_fragment_targets",
			"note_projection_diagnostics",
			"note_search_regions",
			"note_projection_state",
			"note_metadata_state",
			"note_tags",
			"note_property_values",
			"property_keys",
			"intel_fts",
			"intel_fts_rowid",
			"intel_rationale",
			"intel_rationale_fts",
			"intel_rationale_fts_rowid",
			"intel_embeddings",
			"intel_chunks",
			"intel_edges",
			"intel_doc_sections",
			"intel_code_anchors",
			"intel_symbol_ref_files",
			"intel_external_symbol_evidence",
			"intel_external_import_evidence",
			"intel_external_target_map",
			"intel_external_targets",
			"intel_symbol_ref_targets",
			"intel_symbol_refs",
			"intel_import_refs",
			"intel_module_defs",
			"intel_go_derived_relationships",
			"intel_go_package_files",
			"intel_go_package_relationship_state",
			"doc_links",
			"intent_embeddings",
			"anchor_scopes",
			"note_anchors",
			"notes",
			"anchor_globs",
			"anchors",
			"calls",
			"annotations",
			"super_edges",
			"symbols",
			"files",
			"index_metadata",
			"mcp_session_item_reservations",
			"mcp_session_maintenance",
			"schema_version",
		}
		for _, name := range dropTables {
			if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
				return err
			}
		}
		if err := dropVecTablesByPrefix(ctx, db, intelVecTablePrefix); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainIntel)); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
				return err
			}
		}
		s.vecReady = sync.Map{}
		return nil
	})
}

func intelVecTableName(dims int) string {
	return fmt.Sprintf("%s%d", intelVecTablePrefix, dims)
}

func createIntelVecTable(ctx context.Context, db execer, dims int) error {
	if dims <= 0 {
		return fmt.Errorf("invalid vec dimensions: %d", dims)
	}
	table := intelVecTableName(dims)
	indexingperf.AddCount(ctx, indexingperf.AgentStartOpSchemaStatements, 1)
	_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(chunk_id integer primary key, owner_type text partition key, embedding float[%d] distance_metric=cosine);`, table, dims))
	return err
}

func migrateIntelVecOwnerPartitions(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT dimensions FROM intel_embeddings WHERE dimensions > 0 ORDER BY dimensions`)
	if err != nil {
		return err
	}
	var dimensions []int
	for rows.Next() {
		var dims int
		if err := rows.Scan(&dims); err != nil {
			_ = rows.Close()
			return err
		}
		dimensions = append(dimensions, dims)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, dims := range dimensions {
		table := intelVecTableName(dims)
		exists, err := tableExistsQuery(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("missing vector table %s during owner partition migration", table)
		}
		hasOwnerPartition, err := vecOwnerPartitionReadyTx(ctx, tx, table)
		if err != nil {
			return err
		}
		if hasOwnerPartition {
			continue
		}
		staging := table + "_owner_partition_v67_stage"
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+staging); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE `+staging+` (chunk_id INTEGER PRIMARY KEY, owner_type TEXT NOT NULL, embedding BLOB NOT NULL) STRICT`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s(chunk_id, owner_type, embedding)
			SELECT v.chunk_id, COALESCE(c.owner_type, ''), v.embedding
			FROM %s v
			LEFT JOIN intel_chunks c ON c.id = v.chunk_id
		`, staging, table)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+table); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`CREATE VIRTUAL TABLE %s USING vec0(chunk_id integer primary key, owner_type text partition key, embedding float[%d] distance_metric=cosine)`, table, dims)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s(chunk_id, owner_type, embedding) SELECT chunk_id, owner_type, embedding FROM %s`, table, staging)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+staging); err != nil {
			return err
		}
	}
	return nil
}

func tableHasColumnTx(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	var found int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM pragma_table_info(?)
		WHERE name = ?
		LIMIT 1
	`, table, column).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found == 1, nil
}

func dropTriggersByPrefixTx(ctx context.Context, tx *sql.Tx, prefix string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'trigger' AND name LIKE ?
	`, prefix+"%")
	if err != nil {
		return err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS `+name); err != nil {
			return err
		}
	}
	return nil
}

func dropVecTablesByPrefixTx(ctx context.Context, tx *sql.Tx, prefix string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name LIKE ?
	`, prefix+"%")
	if err != nil {
		return err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
			return err
		}
	}
	return nil
}

func migrateIntelEmbeddingsToVecPrimaryTx(ctx context.Context, tx *sql.Tx) ([]int, error) {
	hasEmbeddingBlob, err := tableHasColumnTx(ctx, tx, "intel_embeddings", "embedding")
	if err != nil {
		return nil, err
	}
	if !hasEmbeddingBlob {
		return nil, nil
	}
	hasChunkRowID, err := tableHasColumnTx(ctx, tx, "intel_chunks", "id")
	if err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT dimensions
		FROM intel_embeddings
		WHERE dimensions > 0
		ORDER BY dimensions
	`)
	if err != nil {
		return nil, err
	}

	var dimsList []int
	for rows.Next() {
		var dims int
		if err := rows.Scan(&dims); err != nil {
			_ = rows.Close()
			return nil, err
		}
		dimsList = append(dimsList, dims)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for _, dims := range dimsList {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+intelVecTableName(dims)); err != nil {
			return nil, err
		}
		if err := createIntelVecTable(ctx, tx, dims); err != nil {
			return nil, err
		}
		table := intelVecTableName(dims)
		if hasChunkRowID {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
				DELETE FROM %s
				WHERE chunk_id IN (
					SELECT c.id
					FROM intel_embeddings e
					JOIN intel_chunks c ON c.chunk_id = e.chunk_id
					WHERE e.dimensions = ? AND length(e.embedding) = ?
				)
			`, table), dims, dims*4); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
				INSERT INTO %s(chunk_id, owner_type, embedding)
				SELECT c.id, c.owner_type, e.embedding
				FROM intel_embeddings e
				JOIN intel_chunks c ON c.chunk_id = e.chunk_id
				WHERE e.dimensions = ? AND length(e.embedding) = ?
			`, table), dims, dims*4); err != nil {
				return nil, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
				DELETE FROM %s
				WHERE chunk_id IN (
					SELECT chunk_id
					FROM intel_embeddings
					WHERE dimensions = ? AND length(embedding) = ?
				)
			`, table), dims, dims*4); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
				INSERT INTO %s(chunk_id, owner_type, embedding)
				SELECT e.chunk_id, COALESCE(c.owner_type, ''), e.embedding
				FROM intel_embeddings e
				LEFT JOIN intel_chunks c ON c.chunk_id = e.chunk_id
				WHERE dimensions = ? AND length(embedding) = ?
			`, table), dims, dims*4); err != nil {
				return nil, err
			}
		}
	}

	if err := dropTriggersByPrefixTx(ctx, tx, "trg_"+intelVecTablePrefix); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `ALTER TABLE intel_embeddings RENAME TO intel_embeddings_legacy`); err != nil {
		return nil, err
	}
	if hasChunkRowID {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE intel_embeddings (
			chunk_row_id INTEGER PRIMARY KEY,
			chunk_id TEXT NOT NULL UNIQUE CHECK (chunk_id != ''),
			norm REAL NOT NULL DEFAULT 0,
			dimensions INTEGER NOT NULL CHECK (dimensions > 0),
			created_at INTEGER NOT NULL CHECK (created_at >= 0),
			FOREIGN KEY(chunk_row_id) REFERENCES intel_chunks(id) ON DELETE CASCADE
		) STRICT;`); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO intel_embeddings (chunk_row_id, chunk_id, norm, dimensions, created_at)
			SELECT c.id, e.chunk_id, e.norm, e.dimensions, e.created_at
			FROM intel_embeddings_legacy e
			JOIN intel_chunks c ON c.chunk_id = e.chunk_id
		`); err != nil {
			return nil, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE intel_embeddings (
			chunk_id TEXT PRIMARY KEY CHECK (chunk_id != ''),
			norm REAL NOT NULL DEFAULT 0,
			dimensions INTEGER NOT NULL CHECK (dimensions > 0),
			created_at INTEGER NOT NULL CHECK (created_at >= 0),
			FOREIGN KEY(chunk_id) REFERENCES intel_chunks(chunk_id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO intel_embeddings (chunk_id, norm, dimensions, created_at)
			SELECT chunk_id, norm, dimensions, created_at
			FROM intel_embeddings_legacy
		`); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE intel_embeddings_legacy`); err != nil {
		return nil, err
	}
	return dimsList, nil
}

// EnsureEmbeddingsVecPrimary migrates intel_embeddings to vec-primary storage when needed.
// Safe to call repeatedly; no-op after migration is complete.
func (s *Store) EnsureEmbeddingsVecPrimary(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "intel.ensure_vec_primary")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		dimsList, err := migrateIntelEmbeddingsToVecPrimaryTx(ctx, tx)
		if err != nil {
			return err
		}
		if len(dimsList) > 0 {
			s.vecReady = sync.Map{}
			for _, dims := range dimsList {
				s.vecReady.Store(dims, struct{}{})
			}
		}
		return nil
	})
}

func (s *Store) requireIntelVecPrimary(ctx context.Context) error {
	hasEmbeddingBlob, err := s.tableHasColumn(ctx, "intel_embeddings", "embedding")
	if err != nil {
		return err
	}
	if hasEmbeddingBlob {
		return errors.New("legacy intel_embeddings schema detected; run `rzm index` to migrate embeddings to sqlite-vec")
	}
	return nil
}

func (s *Store) ensureIntelVecMirror(ctx context.Context, dims int) error {
	if dims <= 0 {
		return fmt.Errorf("invalid vec dimensions: %d", dims)
	}
	if _, ok := s.vecReady.Load(dims); ok {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.ensure_vec_mirror")
	// Mark vector schema work observable even when the existing table makes the
	// correct query path a zero-statement read.
	indexingperf.MarkCountAvailable(ctx, indexingperf.AgentStartOpSchemaStatements)
	table := intelVecTableName(dims)
	exists, err := s.tableExists(ctx, table)
	if err != nil {
		return err
	}
	if exists {
		s.vecReady.Store(dims, struct{}{})
		return nil
	}
	if s.readOnly {
		return fmt.Errorf("indexed vector table for %d dimensions is missing; run `rzm index`", dims)
	}

	if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return createIntelVecTable(ctx, db, dims)
	}); err != nil {
		return err
	}
	s.vecReady.Store(dims, struct{}{})
	return nil
}

func dropVecTablesByPrefix(ctx context.Context, db *sql.DB, prefix string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name LIKE ?
	`, prefix+"%")
	if err != nil {
		return err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range tables {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
			return err
		}
	}
	return nil
}

func isSchemaError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no such column"):
		return true
	case strings.Contains(msg, "no such table"):
		return true
	case strings.Contains(msg, "malformed database schema"):
		return true
	case strings.Contains(msg, "views may not be indexed"):
		return true
	case strings.Contains(msg, "cannot add a column"):
		return true
	case strings.Contains(msg, "cannot alter table"):
		return true
	case strings.Contains(msg, "is a view"):
		return true
	default:
		return false
	}
}

// IsSchemaIncompatibleError reports schema-version mismatches that require
// rebuild/replacement rather than forward migration.
func IsSchemaIncompatibleError(err error) bool {
	if err == nil {
		return false
	}
	var future *migration.ErrFutureSchema
	if errors.As(err, &future) {
		return true
	}
	var missing *migration.ErrMissingStep
	if errors.As(err, &missing) {
		return true
	}
	var drift *migration.ErrSchemaDrift
	if errors.As(err, &drift) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "schema version downgrade") ||
		strings.Contains(msg, "missing migration for version")
}

func dropViewIfExists(ctx context.Context, db *sql.DB, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `DROP VIEW IF EXISTS `+name)
	if err == nil {
		return nil
	}
	// SQLite returns "use DROP TABLE to delete table X" if X is a table; treat that as a no-op.
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "use drop table") {
		return nil
	}
	return err
}

func (s *Store) ensureSchemaVersion(ctx context.Context, target int) error {
	plan, err := s.intelMigrationPlan(ctx, target)
	if err != nil {
		return err
	}
	return migration.EnsureDomain(ctx, s.db, plan, migration.EnsureOptions{})
}

func (s *Store) intelMigrationPlan(_ context.Context, target int) (migration.DomainPlan, error) {
	stepCount := target - intelBaselineVersion
	if stepCount < 0 || stepCount > len(forwardSchemaMigrations) {
		return migration.DomainPlan{}, &migration.ErrMissingStep{
			Domain: migration.DomainIntel,
			From:   intelBaselineVersion + len(forwardSchemaMigrations),
			To:     intelBaselineVersion + len(forwardSchemaMigrations) + 1,
		}
	}
	steps := make([]domains.TxStep, 0, stepCount)
	for i, migrate := range forwardSchemaMigrations[:stepCount] {
		toVersion := intelBaselineVersion + i + 1
		migrateFn := migrate
		steps = append(steps, func(ctx context.Context, tx *sql.Tx) error {
			if err := migrateFn(ctx, tx); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE schema_version SET version = ?`, toVersion)
			return err
		})
	}
	validate := s.validateIntelSchemaTx
	if target < currentSchemaVersion {
		validate = nil
	}
	plan := domains.IntelPlanFrom(intelBaselineVersion, target, steps, validate)
	if target == currentSchemaVersion {
		plan.SchemaFingerprint = currentIntelSchemaFingerprint
	}
	return plan, nil
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func tableExistsQuery(ctx context.Context, q queryRower, table string) (bool, error) {
	var name string
	err := q.QueryRowContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type='table' AND name=?
		LIMIT 1
	`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return name == table, nil
}

func tableHasColumnQuery(ctx context.Context, q queryRower, table, column string) (bool, error) {
	var found int
	err := q.QueryRowContext(ctx, `
		SELECT 1
		FROM pragma_table_info(?)
		WHERE name = ?
		LIMIT 1
	`, table, column).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found == 1, nil
}

func indexExistsQuery(ctx context.Context, q queryRower, name string) (bool, error) {
	var indexName string
	err := q.QueryRowContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type='index' AND name=?
		LIMIT 1
	`, name).Scan(&indexName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return indexName == name, nil
}

func recreateIntelChunksWithOntologyNode(ctx context.Context, db execer) error {
	stmts := []string{
		`DROP TABLE IF EXISTS intel_chunks_new;`,
		`DROP TABLE IF EXISTS intel_embeddings_v44_backup;`,
		`CREATE TEMP TABLE intel_embeddings_v44_backup AS
			SELECT chunk_row_id, chunk_id, norm, dimensions, created_at
			FROM intel_embeddings;`,
		`CREATE TABLE intel_chunks_new (
			id INTEGER PRIMARY KEY,
			chunk_id TEXT NOT NULL UNIQUE CHECK (chunk_id != ''),
			owner_id TEXT NOT NULL CHECK (owner_id != ''),
			owner_row_id INTEGER,
			owner_type TEXT NOT NULL CHECK (owner_type IN ('anchor', 'doc_section', 'ontology_node')),
			chunk_family TEXT NOT NULL DEFAULT 'default' CHECK (chunk_family != ''),
			ord INTEGER NOT NULL CHECK (ord >= 0),
			granularity TEXT NOT NULL CHECK (granularity != ''),
			breadcrumb TEXT,
			heading TEXT,
			content_hash TEXT NOT NULL CHECK (content_hash != ''),
			start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
			end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
			updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
			UNIQUE(owner_type, owner_id, ord, granularity)
		) STRICT;`,
		`INSERT OR IGNORE INTO intel_chunks_new (
			id, chunk_id, owner_id, owner_row_id, owner_type, chunk_family, ord, granularity,
			breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
		)
		SELECT id, chunk_id, owner_id, owner_row_id, owner_type, chunk_family, ord, granularity,
			breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
		FROM intel_chunks;`,
		`DROP TABLE intel_chunks;`,
		`ALTER TABLE intel_chunks_new RENAME TO intel_chunks;`,
		`INSERT OR IGNORE INTO intel_embeddings (chunk_row_id, chunk_id, norm, dimensions, created_at)
		SELECT b.chunk_row_id, b.chunk_id, b.norm, b.dimensions, b.created_at
		FROM intel_embeddings_v44_backup b
		JOIN intel_chunks c ON c.id = b.chunk_row_id AND c.chunk_id = b.chunk_id;`,
		`DROP TABLE IF EXISTS intel_embeddings_v44_backup;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner ON intel_chunks(owner_type, owner_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner_id ON intel_chunks(owner_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_family_owner ON intel_chunks(chunk_family, owner_type, owner_id, ord);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) validateIntelSchemaTx(ctx context.Context, tx *sql.Tx) error {
	required := map[string][]string{
		"derived_work":                           {"kind", "path", "generation", "attempt", "retry_at", "ready"},
		"derived_work_epoch":                     {"kind", "generation", "revision"},
		"derived_work_sequence":                  {"kind", "path", "generation"},
		"schema_version":                         {"version"},
		"files":                                  {"path", "lang", "hash", "indexer_version", "parse_status", "call_edges_stale", "mtime"},
		"symbols":                                {"id", "lang", "kind", "file", "pkg", "name", "fqn", "fqn_reversed"},
		"anchors":                                {"id", "label", "kind", "path_prefix"},
		"anchor_globs":                           {"anchor_id", "pattern"},
		"notes":                                  {"id", "path", "title", "content_hash", "indexer_version", "mtime", "size", "indexed_at", "note_key_full", "note_key_base", "path_len", "first_segment_norm", "format_id"},
		"property_keys":                          {"property_id", "property_name", "updated_at"},
		"note_property_values":                   {"note_id", "property_id", "source", "value_text", "value_norm", "value_kind", "is_list", "list_ordinal"},
		"note_tags":                              {"note_id", "tag_norm"},
		"note_search_terms":                      {"note_id", "scope", "segment_ordinal", "token_ordinal", "token_text"},
		"note_metadata_state":                    {"notes_hash", "raw_notes_hash", "loaded_at", "ready"},
		"note_projection_state":                  {"note_id", "provider_version", "projection_version", "source_content_hash", "status", "diagnostic_code", "diagnostic_detail", "updated_at"},
		"note_fragment_targets":                  {"note_id", "target_kind", "target_text", "target_norm", "ordinal"},
		"note_projection_diagnostics":            {"note_id", "ordinal", "code", "category", "message", "range_present", "start_byte", "end_byte", "blocking", "affected_operation"},
		"note_search_regions":                    {"note_id", "ordinal", "origin", "region_kind", "region_text", "media_type", "range_present", "start_byte", "end_byte"},
		"intel_code_anchors":                     {"id", "anchor_id", "path", "kind", "fqn"},
		"intel_doc_sections":                     {"id", "section_id", "path", "level", "content", "updated_at"},
		"ontology_nodes":                         {"id", "node_id", "note_path", "node_ref_json", "node_kind", "type_name", "parent_node_id", "source_locator", "fragment", "block_id", "display_label", "locator_status", "structural_fingerprint", "schema_hash", "updated_at"},
		"ontology_node_field_values":             {"id", "node_id", "note_path", "type_name", "field_name", "field_kind", "source_kind", "value_kind", "value_text", "value_norm", "value_bool", "value_int", "value_real", "value_date", "value_datetime", "target_node_id", "target_ref_json", "target_note_path", "target_type_name", "target_source_locator", "list_ordinal", "schema_hash", "updated_at"},
		"ontology_node_field_value_dependencies": {"id", "source_note_path", "node_id", "type_name", "field_name", "target_input", "target_input_norm", "resolved_target_note_path", "resolved_target_type_name", "schema_hash", "updated_at"},
		"intel_edges":                            {"src_type", "src_row_id", "dst_type", "dst_row_id", "kind"},
		"intel_chunks":                           {"id", "chunk_id", "owner_id", "owner_row_id", "owner_type", "chunk_family", "ord", "content_hash", "updated_at"},
		"intel_embeddings":                       {"chunk_row_id", "chunk_id", "norm", "dimensions", "created_at"},
		"ontology_node_embedding_state":          {"chunk_id", "node_id", "note_path", "type_name", "node_kind", "embedding_schema_signature", "chunk_text_hash", "chunk_granularity", "updated_at"},
		"index_metadata":                         {"key", "value"},
		"intel_symbol_ref_files":                 {"file_id", "path"},
		"intel_symbol_ref_targets":               {"target_id", "dst_lang", "dst_pkg", "dst_name", "dst_fqn", "dst_member"},
		"intel_symbol_refs":                      {"src_file_id", "owner_symbol_id", "owner_fqn", "ref_kind", "dst_target_id"},
		"intel_external_targets":                 {"external_id", "handle", "ecosystem", "module", "symbol_path", "target_kind"},
		"intel_external_target_map":              {"raw_target_id", "external_id"},
		"intel_external_symbol_evidence":         {"src_file_id", "owner_fqn", "ref_kind", "raw_target_id", "external_id", "evidence_kind", "confidence", "imported_name", "local_name", "manifest_path", "declared_range", "version_scope"},
		"intel_external_import_evidence":         {"src_path", "module", "binding_ordinal", "external_id", "evidence_kind", "confidence", "imported_name", "local_name", "manifest_path", "declared_range", "version_scope"},
		"intel_import_refs":                      {"src_path", "module"},
		"intel_module_defs":                      {"src_path", "lang", "module"},
		"intel_go_package_relationship_state":    {"package_key", "import_path", "directory", "package_name", "build_variant", "membership_digest", "analyzer_version", "complete", "valid", "diagnostics_json", "updated_at"},
		"intel_go_package_files":                 {"source_path", "package_key", "import_path", "directory", "package_name", "build_variant"},
		"intel_go_derived_relationships":         {"package_key", "kind", "source_path", "source_fqn", "target_fqn", "pointer_only"},
		"intel_fts_rowid":                        {"item_type", "item_id", "fts_rowid"},
		"intel_rationale":                        {"rationale_id", "path", "symbol_fqn", "kind", "content", "start_line", "end_line", "fingerprint", "updated_at"},
		"intel_rationale_fts_rowid":              {"rationale_id", "fts_rowid"},
		"graph_doc_scores":                       {"doc_path", "doc_type", "hub", "authority", "community", "inbound", "outbound", "updated_at"},
		"graph_doc_edges":                        {"src_path", "dst_path", "confidence", "confidence_score", "source_location"},
		"graph_anchor_scores":                    {"anchor_id", "pagerank", "updated_at"},
		"graph_web_revision":                     {"id", "incarnation", "revision"},
		"ontology_note_assessments":              {"note_path", "declared_type", "resolved_type", "assessment_json", "has_issues", "type_ambiguous", "schema_hash", "updated_at"},
		"ontology_note_state":                    {"note_path", "input_fingerprint", "schema_hash", "resolved_type", "updated_at"},
		"ontology_note_types":                    {"note_path", "type_name", "schema_hash", "updated_at"},
		"ontology_edges":                         {"src_path", "src_node_id", "relation_name", "dst_path", "dst_node_id", "dst_type", "provenance", "structural", "schema_hash", "updated_at"},
		"ontology_type_policies":                 {"type_name", "policy_json", "schema_hash", "updated_at"},
		"ontology_schema_state":                  {"schema_hash", "notes_hash", "materialization_version", "loaded_at", "ready", "error_json", "broken_links_json"},
		"validation_state":                       {"id", "status", "result_json", "error", "generation", "published_generation", "started_at", "finished_at", "duration_ms"},
		"validation_generations":                 {"generation", "vault_identity", "scope", "selected_checks_json", "schema_identity", "config_identity", "input_revision", "started_at", "finished_at", "duration_ms", "completion", "stale_reason", "issue_count", "error_count", "affected_file_count", "affected_note_count", "repair_action_count", "repair_plan_fingerprint"},
		"validation_checks":                      {"generation", "check_order", "check_name", "outcome", "issue_count", "summary", "notes_json", "error", "duration_ms"},
		"validation_diagnostics":                 {"generation", "diagnostic_order", "issue_key", "check_name", "code", "message", "evidence_json", "primary_path", "type_name", "field_name", "source", "target", "location_unit", "location_start", "location_end", "node_id", "location_field", "location_relation", "variant_key", "variant_label"},
		"validation_diagnostic_paths":            {"generation", "issue_key", "path", "membership_kind"},
		"validation_diagnostic_scopes":           {"generation", "issue_key", "scope_kind", "scope_key"},
		"validation_actions":                     {"generation", "action_id", "check_name", "issue_code", "kind", "safety", "title", "summary", "question", "instance_count", "candidate_paths_json"},
		"validation_action_issues":               {"generation", "action_id", "issue_key"},
		"validation_action_paths":                {"generation", "action_id", "path"},
		"mcp_sessions":                           {"session_id", "created_at", "last_seen_at"},
		"mcp_session_items":                      {"session_id", "item_key", "fingerprint", "sent_at"},
		"mcp_session_item_reservations":          {"session_id", "item_key", "fingerprint", "reservation_id", "reserved_at"},
		"mcp_session_maintenance":                {"maintenance_key", "next_due_at"},
	}
	for table, cols := range required {
		exists, err := tableExistsQuery(ctx, tx, table)
		if err != nil {
			return fmt.Errorf("validate table %s: %w", table, err)
		}
		if !exists {
			return intelSchemaDrift("missing required table %s", table)
		}
		for _, col := range cols {
			has, err := tableHasColumnQuery(ctx, tx, table, col)
			if err != nil {
				return fmt.Errorf("validate column %s.%s: %w", table, col, err)
			}
			if !has {
				return intelSchemaDrift("missing required column %s.%s", table, col)
			}
		}
	}
	if err := validateIntelVecOwnerPartitionsTx(ctx, tx); err != nil {
		return err
	}
	if err := validateGoRelationshipSchema(ctx, tx); err != nil {
		return err
	}
	if err := validateNoteProjectionStateSchemaTx(ctx, tx); err != nil {
		return err
	}
	if err := validateNoteFormatIDSchemaTx(ctx, tx); err != nil {
		return err
	}
	if err := validateGraphRevisionSchema(ctx, tx); err != nil {
		return err
	}
	if err := validateRationaleFTSSchema(ctx, tx); err != nil {
		return err
	}
	requiredIndexes := []string{
		"idx_anchors_path_prefix",
		"idx_anchor_globs_anchor",
		"idx_property_keys_name",
		"idx_note_property_values_lookup",
		"idx_note_property_values_note",
		"idx_note_tags_tag",
		"idx_note_search_terms_lookup",
		"idx_note_search_terms_note",
		"idx_note_metadata_state_loaded",
		"idx_note_projection_state_status",
		"idx_note_fragment_targets_note",
		"idx_note_fragment_targets_lookup",
		"idx_note_projection_diagnostics_operation",
		"idx_note_search_regions_kind",
		"idx_notes_indexed_at",
		"idx_notes_note_key_full",
		"idx_notes_note_key_base",
		"idx_notes_first_segment_norm",
		"idx_intel_edges_mentions_dst",
		"idx_intel_chunks_owner",
		"idx_intel_chunks_owner_id",
		"idx_files_call_edges_stale",
		"idx_intel_symbol_ref_files_path",
		"idx_intel_symbol_ref_targets_name",
		"idx_intel_symbol_ref_targets_fqn",
		"idx_intel_symbol_refs_dst_target",
		"idx_intel_symbol_refs_owner",
		"idx_intel_external_target_map_external",
		"idx_intel_external_symbol_evidence_external",
		"idx_intel_external_import_evidence_external",
		"idx_intel_rationale_fts_rowid",
		"idx_intel_go_package_files_package",
		"idx_intel_go_package_files_directory",
		"idx_intel_go_derived_relationship_target",
		"idx_intel_go_derived_relationship_source",
		"idx_ontology_note_assessments_resolved",
		"idx_ontology_note_state_schema",
		"idx_ontology_note_types_type",
		"idx_ontology_edges_src",
		"idx_ontology_edges_dst",
		"idx_ontology_edges_src_query",
		"idx_ontology_edges_dst_query",
		"idx_ontology_type_policies_schema",
		"idx_ontology_nodes_note_path",
		"idx_ontology_nodes_type",
		"idx_ontology_nodes_source_locator",
		"idx_ontology_nodes_note_fragment",
		"idx_ontology_nodes_note_block",
		"idx_ontology_node_field_values_unique",
		"idx_ontology_node_field_values_node",
		"idx_ontology_node_field_values_norm",
		"idx_ontology_node_field_values_bool",
		"idx_ontology_node_field_values_date",
		"idx_ontology_node_field_values_datetime",
		"idx_ontology_node_field_values_real",
		"idx_ontology_node_field_values_int",
		"idx_ontology_node_field_values_target",
		"idx_ontology_node_field_values_target_note",
		"idx_ontology_node_field_values_note",
		"idx_ontology_node_field_value_dependencies_unique",
		"idx_ontology_node_field_value_dependencies_source",
		"idx_ontology_node_field_value_dependencies_target_input",
		"idx_ontology_node_field_value_dependencies_resolved_target",
		"idx_ontology_node_embedding_state_node",
		"idx_ontology_node_embedding_state_note",
		"idx_ontology_node_embedding_state_type",
		"idx_mcp_session_item_reservations_owner",
		"idx_validation_checks_order",
		"idx_validation_diagnostics_page",
		"idx_validation_diagnostics_check_code",
		"idx_validation_diagnostics_variant",
		"idx_validation_diagnostic_paths_scope",
		"idx_validation_diagnostic_scopes_scope",
		"idx_validation_action_issues_issue",
		"idx_validation_action_paths_scope",
	}
	for _, idx := range requiredIndexes {
		exists, err := indexExistsQuery(ctx, tx, idx)
		if err != nil {
			return fmt.Errorf("validate index %s: %w", idx, err)
		}
		if !exists {
			return intelSchemaDrift("missing required index %s", idx)
		}
	}
	return nil
}

func validateIntelVecOwnerPartitionsTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT dimensions FROM intel_embeddings WHERE dimensions > 0 ORDER BY dimensions`)
	if err != nil {
		return err
	}
	var dimensions []int
	for rows.Next() {
		var dims int
		if err := rows.Scan(&dims); err != nil {
			_ = rows.Close()
			return err
		}
		dimensions = append(dimensions, dims)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, dims := range dimensions {
		table := intelVecTableName(dims)
		exists, err := tableExistsQuery(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			return intelSchemaDrift("missing vector table %s", table)
		}
		hasOwnerPartition, err := vecOwnerPartitionReadyTx(ctx, tx, table)
		if err != nil {
			return err
		}
		if !hasOwnerPartition {
			return intelSchemaDrift("vector table %s is missing owner_type partition", table)
		}
	}
	return nil
}

func vecOwnerPartitionReadyTx(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var ddl string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(ddl), " "))
	return strings.Contains(normalized, "owner_type text partition key"), nil
}

func validateNoteProjectionStateSchemaTx(ctx context.Context, tx *sql.Tx) error {
	var ddl string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(sql, '')
		FROM sqlite_master
		WHERE type = 'table' AND name = 'note_projection_state'
		LIMIT 1
	`).Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return intelSchemaDrift("missing required table note_projection_state")
		}
		return fmt.Errorf("load note projection state schema: %w", err)
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(ddl)), "")
	for _, required := range []string{
		"strict",
		"foreignkey(note_id)referencesnotes(id)ondeletecascade",
		"check(statusin('stale','current','fatal'))",
		"status='current'andlength(trim(provider_version))>0andlength(trim(projection_version))>0andlength(trim(source_content_hash))>0anddiagnostic_code=''anddiagnostic_detail=''",
		"status='fatal'andlength(trim(provider_version))>0andlength(trim(projection_version))>0andlength(trim(diagnostic_code))>0andlength(trim(diagnostic_detail))>0",
		"status='stale'anddiagnostic_code=''anddiagnostic_detail=''",
	} {
		if !strings.Contains(normalized, required) {
			return intelSchemaDrift("invalid note_projection_state schema: missing %q", required)
		}
	}
	return nil
}

func validateNoteFormatIDSchemaTx(ctx context.Context, tx *sql.Tx) error {
	var ddl string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(sql, '')
		FROM sqlite_master
		WHERE type = 'table' AND name = 'notes'
		LIMIT 1
	`).Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return intelSchemaDrift("missing required table notes")
		}
		return fmt.Errorf("load notes schema: %w", err)
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(ddl)), "")
	for _, required := range []string{
		"format_idtextnotnulldefault'markdown'",
		"check(format_id!=''andformat_id=lower(trim(format_id)))",
	} {
		if !strings.Contains(normalized, required) {
			return intelSchemaDrift("invalid notes schema: missing %q", required)
		}
	}
	return nil
}

func (s *Store) tableExists(ctx context.Context, table string) (bool, error) {
	return tableExistsQuery(ctx, s.db, table)
}

func (s *Store) tableHasColumn(ctx context.Context, table, column string) (bool, error) {
	return tableHasColumnQuery(ctx, s.db, table, column)
}

// ReplaceFileSummary replaces all stored data for the file with the provided summary.
func (s *Store) ReplaceFileSummary(ctx context.Context, summary codeanchor.FileSummary) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_file_summary")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		file := summary.FilePath
		status := strings.TrimSpace(string(summary.ParseStatus))
		if status == "" {
			status = string(codeanchor.ParseOK)
		}

		// Delete previous rows.
		if _, err := tx.ExecContext(ctx, `DELETE FROM super_edges WHERE child_fqn IN (SELECT fqn FROM symbols WHERE file = ?) OR parent_fqn IN (SELECT fqn FROM symbols WHERE file = ?)`, file, file); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM annotations WHERE owner_fqn IN (SELECT fqn FROM symbols WHERE file = ?)`, file); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM symbols WHERE file = ?`, file); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path = ?`, file); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime) VALUES (?, ?, ?, ?, ?, ?)`, file, summary.Lang, summary.Hash, codeanchor.IndexerVersion, status, time.Now().Unix()); err != nil {
			return err
		}

		symbolStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO symbols (lang, kind, file, pkg, name, fqn, fqn_reversed)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fqn) DO UPDATE SET
			lang=excluded.lang,
			kind=excluded.kind,
			file=excluded.file,
			pkg=excluded.pkg,
			name=excluded.name,
			fqn_reversed=excluded.fqn_reversed
	`)
		if err != nil {
			return err
		}
		defer symbolStmt.Close()

		superStmt, err := tx.PrepareContext(ctx, `INSERT INTO super_edges(child_fqn, parent_fqn, kind) VALUES (?, ?, ?)`)
		if err != nil {
			return err
		}
		defer superStmt.Close()

		annotationStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO annotations(owner_fqn, ann_lang, ann_pkg, ann_name, args_json, confidence)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
		if err != nil {
			return err
		}
		defer annotationStmt.Close()

		for _, sym := range summary.Symbols {
			fqn := sym.NormalizeFQN()
			fqnReversed := codeanchor.ReverseString(fqn)
			if _, err := symbolStmt.ExecContext(ctx, sym.Lang, sym.Kind, sym.File, sym.Pkg, sym.Name, fqn, fqnReversed); err != nil {
				return err
			}
		}

		for _, edge := range summary.Supers {
			if edge.ChildFQN == "" || edge.ParentFQN == "" {
				continue
			}
			if _, err := superStmt.ExecContext(ctx, edge.ChildFQN, edge.ParentFQN, "extends"); err != nil {
				return err
			}
		}

		for _, ann := range summary.Annotations {
			blob, _ := json.Marshal(ann.Args)
			if _, err := annotationStmt.ExecContext(ctx, ann.OwnerFQN, ann.AnnSymbol.Lang, ann.AnnSymbol.Pkg, ann.AnnSymbol.Name, string(blob), 1.0); err != nil {
				return err
			}
		}

		return replaceAndInvalidateGoPackageMembershipTx(ctx, tx, []codeanchor.FileSummary{summary}, nil)
	})
}

// ReplaceFileSummariesBatch replaces stored data for multiple files in a single transaction.
func (s *Store) ReplaceFileSummariesBatch(ctx context.Context, summaries []codeanchor.FileSummary) error {
	summaries = dedupeFileSummariesByPath(summaries)
	if len(summaries) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_file_summary")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return s.replaceFileSummariesBatchTx(ctx, tx, summaries)
	})
}

func (s *Store) replaceFileSummariesBatchTx(ctx context.Context, tx *sql.Tx, summaries []codeanchor.FileSummary) error {
	summaries = dedupeFileSummariesByPath(summaries)
	if len(summaries) == 0 {
		return nil
	}
	return func() error {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS tmp_batch_files(path TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tmp_batch_files`); err != nil {
			return err
		}
		tmpFileStmt, err := tx.PrepareContext(ctx, `INSERT INTO tmp_batch_files(path) VALUES (?)`)
		if err != nil {
			return err
		}
		for _, summary := range summaries {
			if _, err := tmpFileStmt.ExecContext(ctx, summary.FilePath); err != nil {
				_ = tmpFileStmt.Close()
				return err
			}
		}
		if err := tmpFileStmt.Close(); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			DELETE FROM super_edges
			WHERE child_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM tmp_batch_files))
			   OR parent_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM tmp_batch_files))
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM annotations
			WHERE owner_fqn IN (SELECT fqn FROM symbols WHERE file IN (SELECT path FROM tmp_batch_files))
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM symbols WHERE file IN (SELECT path FROM tmp_batch_files)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path IN (SELECT path FROM tmp_batch_files)`); err != nil {
			return err
		}

		// Prepare statements once for the entire batch.
		symbolStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO symbols (lang, kind, file, pkg, name, fqn, fqn_reversed)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(fqn) DO UPDATE SET
				lang=excluded.lang,
				kind=excluded.kind,
				file=excluded.file,
				pkg=excluded.pkg,
				name=excluded.name,
				fqn_reversed=excluded.fqn_reversed
		`)
		if err != nil {
			return err
		}
		defer symbolStmt.Close()

		superStmt, err := tx.PrepareContext(ctx, `INSERT INTO super_edges(child_fqn, parent_fqn, kind) VALUES (?, ?, ?)`)
		if err != nil {
			return err
		}
		defer superStmt.Close()

		annotationStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO annotations(owner_fqn, ann_lang, ann_pkg, ann_name, args_json, confidence)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer annotationStmt.Close()

		for _, summary := range summaries {
			file := summary.FilePath
			status := strings.TrimSpace(string(summary.ParseStatus))
			if status == "" {
				status = string(codeanchor.ParseOK)
			}

			if _, err := tx.ExecContext(ctx, `INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime) VALUES (?, ?, ?, ?, ?, ?)`, file, summary.Lang, summary.Hash, codeanchor.IndexerVersion, status, time.Now().Unix()); err != nil {
				return err
			}

			for _, sym := range summary.Symbols {
				fqn := sym.NormalizeFQN()
				fqnReversed := codeanchor.ReverseString(fqn)
				if _, err := symbolStmt.ExecContext(ctx, sym.Lang, sym.Kind, sym.File, sym.Pkg, sym.Name, fqn, fqnReversed); err != nil {
					return err
				}
			}

			for _, edge := range summary.Supers {
				if edge.ChildFQN == "" || edge.ParentFQN == "" {
					continue
				}
				if _, err := superStmt.ExecContext(ctx, edge.ChildFQN, edge.ParentFQN, "extends"); err != nil {
					return err
				}
			}

			for _, ann := range summary.Annotations {
				blob, _ := json.Marshal(ann.Args)
				if _, err := annotationStmt.ExecContext(ctx, ann.OwnerFQN, ann.AnnSymbol.Lang, ann.AnnSymbol.Pkg, ann.AnnSymbol.Name, string(blob), 1.0); err != nil {
					return err
				}
			}
		}

		return replaceAndInvalidateGoPackageMembershipTx(ctx, tx, summaries, nil)
	}()
}

func dedupeFileSummariesByPath(summaries []codeanchor.FileSummary) []codeanchor.FileSummary {
	if len(summaries) < 2 {
		return summaries
	}
	indexByPath := make(map[string]int, len(summaries))
	deduped := make([]codeanchor.FileSummary, 0, len(summaries))
	for _, summary := range summaries {
		if idx, ok := indexByPath[summary.FilePath]; ok {
			deduped[idx] = summary
			continue
		}
		indexByPath[summary.FilePath] = len(deduped)
		deduped = append(deduped, summary)
	}
	return deduped
}

// UpsertFileMeta updates per-file index metadata without touching symbol rows.
// Used to avoid wiping previously indexed data on transient parse failures/timeouts.
func (s *Store) UpsertFileMeta(ctx context.Context, meta codeanchor.FileMeta) error {
	path := strings.TrimSpace(meta.Path)
	if path == "" {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_file_meta")
	status := strings.TrimSpace(string(meta.ParseStatus))
	if status == "" {
		status = string(codeanchor.ParseOK)
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET
				lang=excluded.lang,
				hash=excluded.hash,
				indexer_version=excluded.indexer_version,
				parse_status=excluded.parse_status,
				mtime=excluded.mtime
		`, path, meta.Lang, meta.Hash, codeanchor.IndexerVersion, status, time.Now().Unix())
		if err != nil {
			return err
		}
		return replaceAndInvalidateGoPackageMembershipTx(ctx, tx, nil, []codeanchor.FileMeta{meta})
	})
}

// UpsertNote stores note metadata, anchors, and references.
func (s *Store) UpsertNote(ctx context.Context, note codeanchor.Note) error {
	ctx = indexingperf.WithOp(ctx, "intel.upsert_note")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		noteID, err := s.ensureNoteRow(ctx, tx, note)
		if err != nil {
			return err
		}
		return s.upsertAnchorsAndLinks(ctx, tx, noteID, note)
	})
}

// UpsertNoteWithCleanup stores note metadata and anchors and removes anchors that were dropped from the note.
// Returns AnchorUpsertResult with new/changed/unchanged/deleted anchor IDs for incremental scope recomputation.
func (s *Store) UpsertNoteWithCleanup(ctx context.Context, note codeanchor.Note, keepLabels []string) (codeanchor.AnchorUpsertResult, error) {
	var result codeanchor.AnchorUpsertResult
	ctx = indexingperf.WithOp(ctx, "intel.upsert_note")
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		noteID, err := s.ensureNoteRow(ctx, tx, note)
		if err != nil {
			return err
		}
		if err := s.deleteAnchorsNotInLabelsTx(ctx, tx, noteID, keepLabels, &result.DeletedIDs); err != nil {
			return err
		}
		return s.upsertAnchorsAndLinksWithTracking(ctx, tx, noteID, note, &result)
	})
	return result, err
}

// UpsertNotesWithCleanupBatch stores multiple notes in a single transaction.
// Returns AnchorUpsertResult with new/changed/unchanged/deleted anchor IDs for incremental scope recomputation.
func (s *Store) UpsertNotesWithCleanupBatch(ctx context.Context, notes []codeanchor.NoteWithKeepLabels) (codeanchor.AnchorUpsertResult, error) {
	var result codeanchor.AnchorUpsertResult
	if len(notes) == 0 {
		return result, nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_note")
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		// Collect all labels we'll be upserting to pre-fetch existing anchors.
		allLabels := make([]string, 0)
		for _, item := range notes {
			for _, a := range item.Note.DefinedAnchors {
				if a.Label != "" {
					allLabels = append(allLabels, a.Label)
				}
			}
		}

		// Pre-fetch existing anchors for comparison.
		existing, err := existingAnchorsByLabels(ctx, tx, allLabels)
		if err != nil {
			return err
		}

		// Prepare statements for reuse across all notes. See ensureNoteRow for
		// why we intentionally leave the title untouched on conflict.
		noteStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO notes(path, title) VALUES(?, ?)
			ON CONFLICT(path) DO NOTHING
		`)
		if err != nil {
			return err
		}
		defer noteStmt.Close()

		anchorStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO anchors(label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(label) DO UPDATE SET
				kind=excluded.kind,
				lang=excluded.lang,
				base_lang=excluded.base_lang,
				base_pkg=excluded.base_pkg,
				base_name=excluded.base_name,
				base_member=excluded.base_member,
				ann_lang=excluded.ann_lang,
				ann_pkg=excluded.ann_pkg,
				ann_name=excluded.ann_name,
				ann_args_json=excluded.ann_args_json,
				path_prefix=excluded.path_prefix
		`)
		if err != nil {
			return err
		}
		defer anchorStmt.Close()

		globStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO anchor_globs(anchor_id, pattern) VALUES (?, ?)`)
		if err != nil {
			return err
		}
		defer globStmt.Close()

		noteAnchorStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO note_anchors(note_id, anchor_id) VALUES (?, ?)`)
		if err != nil {
			return err
		}
		defer noteAnchorStmt.Close()

		for _, item := range notes {
			note := item.Note
			keepLabels := item.KeepLabels

			// Upsert note row.
			if _, err := noteStmt.ExecContext(ctx, note.Path, note.Title); err != nil {
				return err
			}
			var noteID int64
			if err := tx.QueryRowContext(ctx, `SELECT id FROM notes WHERE path = ?`, note.Path).Scan(&noteID); err != nil {
				return err
			}

			// Delete anchors not in keep labels.
			if err := s.deleteAnchorsNotInLabelsTx(ctx, tx, noteID, keepLabels, &result.DeletedIDs); err != nil {
				return err
			}

			// Upsert anchors and links.
			for _, a := range note.DefinedAnchors {
				var annArgs string
				var annLang, annPkg, annName string
				if a.Ann != nil {
					annLang = string(a.Ann.Symbol.Lang)
					annPkg = a.Ann.Symbol.Pkg
					annName = a.Ann.Symbol.Name
					if len(a.Ann.ArgFilters) > 0 {
						if blob, err := json.Marshal(a.Ann.ArgFilters); err == nil {
							annArgs = string(blob)
						}
					}
				}
				baseLang, basePkg, baseName := "", "", ""
				baseMember := 0
				if a.BaseSym != nil {
					baseLang = string(a.BaseSym.Lang)
					basePkg = a.BaseSym.Pkg
					baseName = a.BaseSym.Name
					if a.BaseSym.Member {
						baseMember = 1
					}
				}
				if _, err := anchorStmt.ExecContext(ctx, a.Label, a.Kind, a.Lang, baseLang, basePkg, baseName, baseMember, annLang, annPkg, annName, annArgs, a.PathPrefix); err != nil {
					return err
				}

				// Get anchor ID: reuse from pre-fetch for existing anchors, only query for new ones.
				var anchorID int64
				if old, exists := existing[a.Label]; exists {
					anchorID = old.ID
				} else {
					if err := tx.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = ?`, a.Label).Scan(&anchorID); err != nil {
						return err
					}
				}

				// Keep glob patterns in sync.
				if _, err := tx.ExecContext(ctx, `DELETE FROM anchor_globs WHERE anchor_id = ?`, anchorID); err != nil {
					return err
				}
				if a.Kind == codeanchor.AnchorGlob && len(a.Globs) > 0 {
					for _, pat := range a.Globs {
						pat = strings.TrimSpace(pat)
						if pat == "" {
							continue
						}
						if _, err := globStmt.ExecContext(ctx, anchorID, pat); err != nil {
							return err
						}
					}
				}

				// Track change status.
				if old, exists := existing[a.Label]; exists {
					if a.ScopeEqual(old) {
						result.UnchangedIDs = append(result.UnchangedIDs, anchorID)
					} else {
						result.ChangedIDs = append(result.ChangedIDs, anchorID)
					}
				} else {
					result.NewIDs = append(result.NewIDs, anchorID)
				}
			}

			// Update note_anchors links.
			if _, err := tx.ExecContext(ctx, `DELETE FROM note_anchors WHERE note_id = ?`, noteID); err != nil {
				return err
			}
			seen := make(map[string]bool)
			for _, a := range note.DefinedAnchors {
				if a.Label == "" || seen[a.Label] {
					continue
				}
				seen[a.Label] = true
				var anchorID int64
				if err := tx.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = ?`, a.Label).Scan(&anchorID); err != nil {
					continue // Unknown anchors are ignored.
				}
				if _, err := noteAnchorStmt.ExecContext(ctx, noteID, anchorID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return result, err
}

func (s *Store) ensureNoteRow(ctx context.Context, tx *sql.Tx, note codeanchor.Note) (int64, error) {
	// Do NOT overwrite title on conflict. The code-anchor indexer derives title
	// from frontmatter or the path basename only, so it would clobber the
	// higher-quality title (H1 extraction etc.) written by the note metadata
	// indexer via upsertMetadataNotes. Let that path own the title field.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notes(path, title) VALUES(?, ?)
		ON CONFLICT(path) DO NOTHING
	`, note.Path, note.Title); err != nil {
		return 0, err
	}

	var noteID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM notes WHERE path = ?`, note.Path).Scan(&noteID); err != nil {
		return 0, err
	}
	return noteID, nil
}

func (s *Store) upsertAnchorsAndLinks(ctx context.Context, tx *sql.Tx, noteID int64, note codeanchor.Note) error {
	for _, a := range note.DefinedAnchors {
		var annArgs string
		var annLang, annPkg, annName string
		if a.Ann != nil {
			annLang = string(a.Ann.Symbol.Lang)
			annPkg = a.Ann.Symbol.Pkg
			annName = a.Ann.Symbol.Name
			if len(a.Ann.ArgFilters) > 0 {
				if blob, err := json.Marshal(a.Ann.ArgFilters); err == nil {
					annArgs = string(blob)
				}
			}
		}
		baseLang, basePkg, baseName := "", "", ""
		baseMember := 0
		if a.BaseSym != nil {
			baseLang = string(a.BaseSym.Lang)
			basePkg = a.BaseSym.Pkg
			baseName = a.BaseSym.Name
			if a.BaseSym.Member {
				baseMember = 1
			}
		}
		pathPrefix := a.PathPrefix
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO anchors(label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(label) DO UPDATE SET
				kind=excluded.kind,
				lang=excluded.lang,
				base_lang=excluded.base_lang,
				base_pkg=excluded.base_pkg,
				base_name=excluded.base_name,
				base_member=excluded.base_member,
				ann_lang=excluded.ann_lang,
				ann_pkg=excluded.ann_pkg,
				ann_name=excluded.ann_name,
				ann_args_json=excluded.ann_args_json,
				path_prefix=excluded.path_prefix
		`, a.Label, a.Kind, a.Lang, baseLang, basePkg, baseName, baseMember, annLang, annPkg, annName, annArgs, pathPrefix); err != nil {
			return err
		}

		// Keep glob patterns in sync (kind changes should clear previous patterns).
		var anchorID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = ?`, a.Label).Scan(&anchorID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM anchor_globs WHERE anchor_id = ?`, anchorID); err != nil {
			return err
		}
		if a.Kind == codeanchor.AnchorGlob && len(a.Globs) > 0 {
			for _, pat := range a.Globs {
				pat = strings.TrimSpace(pat)
				if pat == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO anchor_globs(anchor_id, pattern) VALUES (?, ?)`, anchorID, pat); err != nil {
					return err
				}
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM note_anchors WHERE note_id = ?`, noteID); err != nil {
		return err
	}
	labels := make([]string, 0, len(note.DefinedAnchors))
	for _, a := range note.DefinedAnchors {
		labels = append(labels, a.Label)
	}
	seen := make(map[string]bool)
	for _, label := range labels {
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		var anchorID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = ?`, label).Scan(&anchorID); err != nil {
			// Unknown anchors are ignored for now.
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO note_anchors(note_id, anchor_id) VALUES (?, ?)`, noteID, anchorID); err != nil {
			return err
		}
	}

	return nil
}

// upsertAnchorsAndLinksWithTracking is like upsertAnchorsAndLinks but tracks which anchors are new/changed/unchanged.
func (s *Store) upsertAnchorsAndLinksWithTracking(ctx context.Context, tx *sql.Tx, noteID int64, note codeanchor.Note, result *codeanchor.AnchorUpsertResult) error {
	// Collect labels to query existing anchors.
	labels := make([]string, 0, len(note.DefinedAnchors))
	for _, a := range note.DefinedAnchors {
		if a.Label != "" {
			labels = append(labels, a.Label)
		}
	}

	// Fetch existing anchors by label for comparison.
	existing, err := existingAnchorsByLabels(ctx, tx, labels)
	if err != nil {
		return err
	}

	// Process each anchor, tracking new vs changed vs unchanged.
	for _, a := range note.DefinedAnchors {
		var annArgs string
		var annLang, annPkg, annName string
		if a.Ann != nil {
			annLang = string(a.Ann.Symbol.Lang)
			annPkg = a.Ann.Symbol.Pkg
			annName = a.Ann.Symbol.Name
			if len(a.Ann.ArgFilters) > 0 {
				if blob, err := json.Marshal(a.Ann.ArgFilters); err == nil {
					annArgs = string(blob)
				}
			}
		}
		baseLang, basePkg, baseName := "", "", ""
		baseMember := 0
		if a.BaseSym != nil {
			baseLang = string(a.BaseSym.Lang)
			basePkg = a.BaseSym.Pkg
			baseName = a.BaseSym.Name
			if a.BaseSym.Member {
				baseMember = 1
			}
		}
		pathPrefix := a.PathPrefix

		// Upsert the anchor.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO anchors(label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(label) DO UPDATE SET
				kind=excluded.kind,
				lang=excluded.lang,
				base_lang=excluded.base_lang,
				base_pkg=excluded.base_pkg,
				base_name=excluded.base_name,
				base_member=excluded.base_member,
				ann_lang=excluded.ann_lang,
				ann_pkg=excluded.ann_pkg,
				ann_name=excluded.ann_name,
				ann_args_json=excluded.ann_args_json,
				path_prefix=excluded.path_prefix
		`, a.Label, a.Kind, a.Lang, baseLang, basePkg, baseName, baseMember, annLang, annPkg, annName, annArgs, pathPrefix); err != nil {
			return err
		}

		// Get anchor ID: reuse from pre-fetch for existing anchors, only query for new ones.
		var anchorID int64
		if old, exists := existing[a.Label]; exists {
			anchorID = old.ID
		} else {
			if err := tx.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = ?`, a.Label).Scan(&anchorID); err != nil {
				return err
			}
		}

		// Update globs.
		if _, err := tx.ExecContext(ctx, `DELETE FROM anchor_globs WHERE anchor_id = ?`, anchorID); err != nil {
			return err
		}
		if a.Kind == codeanchor.AnchorGlob && len(a.Globs) > 0 {
			for _, pat := range a.Globs {
				pat = strings.TrimSpace(pat)
				if pat == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO anchor_globs(anchor_id, pattern) VALUES (?, ?)`, anchorID, pat); err != nil {
					return err
				}
			}
		}

		// Track change status.
		if old, exists := existing[a.Label]; exists {
			if a.ScopeEqual(old) {
				result.UnchangedIDs = append(result.UnchangedIDs, anchorID)
			} else {
				result.ChangedIDs = append(result.ChangedIDs, anchorID)
			}
		} else {
			result.NewIDs = append(result.NewIDs, anchorID)
		}
	}

	// Update note-anchor links.
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_anchors WHERE note_id = ?`, noteID); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, a := range note.DefinedAnchors {
		if a.Label == "" || seen[a.Label] {
			continue
		}
		seen[a.Label] = true
		var anchorID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = ?`, a.Label).Scan(&anchorID); err != nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO note_anchors(note_id, anchor_id) VALUES (?, ?)`, noteID, anchorID); err != nil {
			return err
		}
	}

	return nil
}

// Anchors returns all anchors.
func (s *Store) Anchors(ctx context.Context) ([]codeanchor.Anchor, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix
		FROM anchors
	`)
	if err != nil {
		return nil, err
	}
	return s.readAnchors(ctx, rows)
}

// AnchorsByIDs returns anchors filtered by id set.
func (s *Store) AnchorsByIDs(ctx context.Context, ids []int64) ([]codeanchor.Anchor, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	holders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		holders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT id, label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix
		FROM anchors
		WHERE id IN (%s)
	`, strings.Join(holders, ","))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return s.readAnchors(ctx, rows)
}

// AnchorsByNotePaths returns anchors currently defined by the provided note paths.
func (s *Store) AnchorsByNotePaths(ctx context.Context, paths []string) (map[string][]codeanchor.Anchor, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	holders := make([]string, len(paths))
	args := make([]any, len(paths))
	for i, path := range paths {
		holders[i] = "?"
		args[i] = path
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT n.path, a.id, a.label, a.kind, a.lang, a.base_lang, a.base_pkg, a.base_name, a.base_member, a.ann_lang, a.ann_pkg, a.ann_name, a.ann_args_json, a.path_prefix
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchors a ON a.id = na.anchor_id
		WHERE n.path IN (%s)
	`, strings.Join(holders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.Anchor, len(paths))
	globIDs := make(map[int64]struct{})
	for rows.Next() {
		var notePath string
		var a codeanchor.Anchor
		var kind, lang, baseLang, basePkg, baseName, annLang, annPkg, annName, annArgs string
		var baseMember int
		var pathPrefix sql.NullString
		if err := rows.Scan(&notePath, &a.ID, &a.Label, &kind, &lang, &baseLang, &basePkg, &baseName, &baseMember, &annLang, &annPkg, &annName, &annArgs, &pathPrefix); err != nil {
			return nil, err
		}
		a.Kind = codeanchor.AnchorKind(kind)
		a.Lang = codeanchor.Lang(lang)
		if pathPrefix.Valid {
			a.PathPrefix = pathPrefix.String
		}
		if a.Kind == codeanchor.AnchorGlob {
			globIDs[a.ID] = struct{}{}
		}
		if baseName != "" {
			a.BaseSym = &codeanchor.SymbolRef{Lang: codeanchor.Lang(baseLang), Pkg: basePkg, Name: baseName, Member: baseMember != 0}
		}
		if annName != "" {
			selector := codeanchor.AnnotationSelector{
				Symbol: codeanchor.SymbolRef{Lang: codeanchor.Lang(annLang), Pkg: annPkg, Name: annName},
			}
			if annArgs != "" {
				_ = json.Unmarshal([]byte(annArgs), &selector.ArgFilters)
			}
			a.Ann = &selector
		}
		out[notePath] = append(out[notePath], a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(globIDs) > 0 {
		ids := make([]int64, 0, len(globIDs))
		for id := range globIDs {
			ids = append(ids, id)
		}
		byID, err := s.globsByAnchorIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for notePath, anchors := range out {
			for i := range anchors {
				if anchors[i].Kind == codeanchor.AnchorGlob {
					anchors[i].Globs = byID[anchors[i].ID]
				}
			}
			out[notePath] = anchors
		}
	}
	return out, nil
}

// CodeFilesForNote returns the distinct code files linked to the given note
// via its code anchors. It resolves all four anchor flavors:
//
//   - symbol-based (anchor_scopes.symbol_fqn → symbols.file)
//   - call-based (anchor_scopes.call_file — direct path)
//   - path-prefix (anchors.path_prefix → files.path starting with the prefix)
//   - glob (anchor_globs.pattern → files.path matched via doublestar)
//
// Paths are returned in stable sorted order.
func (s *Store) CodeFilesForNote(ctx context.Context, notePath string) ([]string, error) {
	notePath = strings.TrimSpace(notePath)
	if notePath == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		p = filepath.ToSlash(p)
		seen[p] = struct{}{}
	}

	// Symbol-based scopes: resolve symbol_fqn to the file that owns the symbol.
	symRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT sym.file
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchor_scopes sc ON sc.anchor_id = na.anchor_id
		JOIN symbols sym ON sym.fqn = sc.symbol_fqn
		WHERE n.path = ?
		  AND sc.symbol_fqn IS NOT NULL
		  AND sc.symbol_fqn != ''
	`, notePath)
	if err != nil {
		return nil, err
	}
	for symRows.Next() {
		var file string
		if err := symRows.Scan(&file); err != nil {
			symRows.Close()
			return nil, err
		}
		add(file)
	}
	if err := symRows.Close(); err != nil {
		return nil, err
	}

	// Call-based scopes: the file is already stored directly.
	callRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT sc.call_file
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchor_scopes sc ON sc.anchor_id = na.anchor_id
		WHERE n.path = ?
		  AND sc.call_file IS NOT NULL
		  AND sc.call_file != ''
	`, notePath)
	if err != nil {
		return nil, err
	}
	for callRows.Next() {
		var file string
		if err := callRows.Scan(&file); err != nil {
			callRows.Close()
			return nil, err
		}
		add(file)
	}
	if err := callRows.Close(); err != nil {
		return nil, err
	}

	// Path-prefix anchors: match any indexed file under the prefix.
	prefixRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT a.path_prefix
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchors a ON a.id = na.anchor_id
		WHERE n.path = ?
		  AND a.kind = ?
		  AND a.path_prefix IS NOT NULL
		  AND a.path_prefix != ''
	`, notePath, codeanchor.AnchorPath)
	if err != nil {
		return nil, err
	}
	var prefixes []string
	for prefixRows.Next() {
		var pfx string
		if err := prefixRows.Scan(&pfx); err != nil {
			prefixRows.Close()
			return nil, err
		}
		prefixes = append(prefixes, pfx)
	}
	if err := prefixRows.Close(); err != nil {
		return nil, err
	}
	for _, pfx := range prefixes {
		pfx = filepath.ToSlash(strings.TrimSpace(pfx))
		if pfx == "" {
			continue
		}
		fileRows, err := s.db.QueryContext(ctx, `
			SELECT path FROM files
			WHERE path = ? OR path LIKE ? || '/%'
		`, pfx, pfx)
		if err != nil {
			return nil, err
		}
		for fileRows.Next() {
			var p string
			if err := fileRows.Scan(&p); err != nil {
				fileRows.Close()
				return nil, err
			}
			add(p)
		}
		if err := fileRows.Close(); err != nil {
			return nil, err
		}
	}

	// Glob anchors: load patterns, then walk all files once and doublestar-match.
	globRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ag.pattern
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		JOIN anchors a ON a.id = na.anchor_id
		JOIN anchor_globs ag ON ag.anchor_id = a.id
		WHERE n.path = ?
		  AND a.kind = ?
	`, notePath, codeanchor.AnchorGlob)
	if err != nil {
		return nil, err
	}
	var patterns []string
	for globRows.Next() {
		var pat string
		if err := globRows.Scan(&pat); err != nil {
			globRows.Close()
			return nil, err
		}
		patterns = append(patterns, pat)
	}
	if err := globRows.Close(); err != nil {
		return nil, err
	}
	if len(patterns) > 0 {
		allFiles, err := s.db.QueryContext(ctx, `SELECT path FROM files`)
		if err != nil {
			return nil, err
		}
		for allFiles.Next() {
			var p string
			if err := allFiles.Scan(&p); err != nil {
				allFiles.Close()
				return nil, err
			}
			norm := filepath.ToSlash(p)
			for _, pat := range patterns {
				if ok, _ := doublestar.Match(pat, norm); ok {
					add(p)
					break
				}
			}
		}
		if err := allFiles.Close(); err != nil {
			return nil, err
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) globsByAnchorIDs(ctx context.Context, ids []int64) (map[int64][]string, error) {
	if len(ids) == 0 {
		return map[int64][]string{}, nil
	}
	holders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		holders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT anchor_id, pattern
		FROM anchor_globs
		WHERE anchor_id IN (%s)
	`, strings.Join(holders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64][]string)
	for rows.Next() {
		var anchorID int64
		var pat string
		if err := rows.Scan(&anchorID, &pat); err != nil {
			return nil, err
		}
		out[anchorID] = append(out[anchorID], pat)
	}
	return out, rows.Err()
}

// AnchorsBySymbols returns anchors whose scopes reference any of the provided symbols.
func (s *Store) AnchorsBySymbols(ctx context.Context, symbols []string) (map[int64][]string, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	out := make(map[int64][]string)
	const maxBatch = 500 // stay under SQLite variable limit (default 999)
	for batch := range slices.Chunk(symbols, maxBatch) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT anchor_id, symbol_fqn
			FROM anchor_scopes
			WHERE symbol_fqn IN (%s)
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID int64
			var sym string
			if err := rows.Scan(&anchorID, &sym); err != nil {
				rows.Close()
				return nil, err
			}
			out[anchorID] = append(out[anchorID], sym)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AnchorsByCallFiles returns anchors whose call_file scope matches provided files.
func (s *Store) AnchorsByCallFiles(ctx context.Context, files []string) ([]int64, error) {
	if len(files) == 0 {
		return nil, nil
	}
	holders, args := placeholders(files)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT anchor_id
		FROM anchor_scopes
		WHERE call_file IN (%s)
	`, holders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AnchorsByPathPrefix returns anchors of kind "path" whose path_prefix matches the target.
// A match occurs when target equals path_prefix or is nested under it (directory-boundary aware).
func (s *Store) AnchorsByPathPrefix(ctx context.Context, target string) ([]int64, error) {
	target = string(paths.NormalizeCode(strings.TrimSpace(target)))
	if target == "" {
		return nil, nil
	}
	sep := "/"
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT id
		FROM anchors
		WHERE kind = ?
			AND path_prefix IS NOT NULL
			AND path_prefix != ''
			AND (? = path_prefix OR substr(?, 1, length(path_prefix) + 1) = path_prefix || ?)
	`, codeanchor.AnchorPath, target, target, sep)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AnchorsByGlobMatch returns anchors of kind "glob" whose patterns match the target.
// Patterns use doublestar semantics and should be stored in normalized (typically absolute) form.
func (s *Store) AnchorsByGlobMatch(ctx context.Context, target string) ([]int64, error) {
	target = filepath.Clean(strings.TrimSpace(target))
	if target == "" {
		return nil, nil
	}
	t := filepath.ToSlash(target)

	rows, err := s.db.QueryContext(ctx, `
		SELECT ag.anchor_id, ag.pattern
		FROM anchor_globs ag
		JOIN anchors a ON a.id = ag.anchor_id
		WHERE a.kind = ?
	`, codeanchor.AnchorGlob)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[int64]bool)
	var ids []int64
	for rows.Next() {
		var id int64
		var pat string
		if err := rows.Scan(&id, &pat); err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		ok, _ := doublestar.Match(pat, t)
		if ok {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// NotesForAnchor returns notes linked to the anchor.
func (s *Store) NotesForAnchor(ctx context.Context, anchorID int64) ([]codeanchor.Note, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id, n.path, n.title
		FROM notes n
		JOIN note_anchors na ON na.note_id = n.id
		WHERE na.anchor_id = ?
	`, anchorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []codeanchor.Note
	for rows.Next() {
		var note codeanchor.Note
		if err := rows.Scan(&note.ID, &note.Path, &note.Title); err != nil {
			return nil, err
		}
		res = append(res, note)
	}
	return res, rows.Err()
}

// NotePaths returns all stored note paths.
func (s *Store) NotePaths(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM notes ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		res = append(res, p)
	}
	return res, rows.Err()
}

// NoteIndexMeta returns stored content hash, indexer version, and mtime for a note path.
func (s *Store) NoteIndexMeta(ctx context.Context, path string) (string, string, int64, bool, error) {
	if s == nil || strings.TrimSpace(path) == "" {
		return "", "", 0, false, nil
	}
	var hash, version string
	var mtime sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT content_hash, indexer_version, mtime
		FROM notes
		WHERE path = ?
	`, path).Scan(&hash, &version, &mtime)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", 0, false, nil
		}
		return "", "", 0, false, err
	}
	mt := int64(0)
	if mtime.Valid {
		mt = mtime.Int64
	}
	return hash, version, mt, true, nil
}

// UpsertNoteMetadataBatch updates incremental note metadata in a single transaction.
func (s *Store) UpsertNoteMetadataBatch(ctx context.Context, metadata map[string]codeanchor.NoteIndexMeta) error {
	if len(metadata) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_note_metadata_batch")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO notes(path, content_hash, indexer_version, mtime)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET
				content_hash=excluded.content_hash,
				indexer_version=excluded.indexer_version,
				mtime=excluded.mtime
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for path, meta := range metadata {
			if strings.TrimSpace(path) == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, path, meta.ContentHash, meta.IndexerVersion, meta.Mtime); err != nil {
				return err
			}
		}
		return nil
	})
}

// TouchIntelNotePaths refreshes stored note mtimes for the given paths.
func (s *Store) TouchIntelNotePaths(ctx context.Context, pathMtimes map[string]int64) error {
	if len(pathMtimes) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.touch_note_paths")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `UPDATE notes SET mtime = ? WHERE path = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for path, mtime := range pathMtimes {
			if strings.TrimSpace(path) == "" || mtime <= 0 {
				continue
			}
			if _, err := stmt.ExecContext(ctx, mtime, path); err != nil {
				return err
			}
		}
		return nil
	})
}

// SymbolsByFile returns FQNs of symbols defined in the file.
func (s *Store) SymbolsByFile(ctx context.Context, file string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT fqn FROM symbols WHERE file = ? ORDER BY fqn`, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []string
	for rows.Next() {
		var fqn string
		if err := rows.Scan(&fqn); err != nil {
			return nil, err
		}
		res = append(res, fqn)
	}
	return res, rows.Err()
}

// Ancestors returns all ancestors (direct and transitive) for the symbol.
func (s *Store) Ancestors(ctx context.Context, fqn string) ([]string, error) {
	seen := map[string]bool{}
	var out []string

	// NOTE: This uses an explicit stack rather than recursion so we never attempt
	// nested queries while a rows cursor is still open. This matters when the store
	// limits MaxOpenConns to 1 (to reduce SQLite lock contention).
	stack := []string{fqn}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		rows, err := s.db.QueryContext(ctx, `SELECT parent_fqn FROM super_edges WHERE child_fqn = ?`, cur)
		if err != nil {
			return nil, err
		}

		var parents []string
		for rows.Next() {
			var parent string
			if err := rows.Scan(&parent); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if parent == "" || seen[parent] {
				continue
			}
			seen[parent] = true
			out = append(out, parent)
			parents = append(parents, parent)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()

		// Preserve the prior depth-first-ish behavior: process the first-seen parent next.
		for i := len(parents) - 1; i >= 0; i-- {
			stack = append(stack, parents[i])
		}
	}
	return out, nil
}

// AnnotationsOnSymbols returns annotations for the provided owners.
func (s *Store) AnnotationsOnSymbols(ctx context.Context, owners []string) ([]codeanchor.AnnotationUse, error) {
	if len(owners) == 0 {
		return nil, nil
	}
	holders, args := placeholders(owners)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT owner_fqn, ann_lang, ann_pkg, ann_name, args_json
		FROM annotations
		WHERE owner_fqn IN (%s)
	`, holders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []codeanchor.AnnotationUse
	for rows.Next() {
		var owner, lang, pkg, name, argsJSON string
		if err := rows.Scan(&owner, &lang, &pkg, &name, &argsJSON); err != nil {
			return nil, err
		}
		ann := codeanchor.AnnotationUse{
			OwnerFQN: owner,
			AnnSymbol: codeanchor.SymbolRef{
				Lang: codeanchor.Lang(lang),
				Pkg:  pkg,
				Name: name,
			},
		}
		if argsJSON != "" {
			_ = json.Unmarshal([]byte(argsJSON), &ann.Args)
		}
		res = append(res, ann)
	}
	return res, rows.Err()
}

// CallsFromFile returns all call sites for a file.
func (s *Store) CallsFromFile(ctx context.Context, file string) ([]codeanchor.CallSite, error) {
	// Callsites are indexed using canonical file paths; normalize for cross-platform callers.
	file = normalizeCodeLookupPath(file)
	rows, err := s.db.QueryContext(ctx, `
		SELECT caller.fqn, callee.lang, callee.fqn, callee.symbol
		FROM intel_edges e
		JOIN intel_code_anchors caller ON caller.id = e.src_row_id
		JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
		WHERE e.kind = 'calls' AND e.src_type = 'anchor' AND e.dst_type = 'anchor' AND caller.path = ?
		ORDER BY callee.fqn, callee.symbol, callee.anchor_id
	`, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []codeanchor.CallSite
	for rows.Next() {
		var ownerFQN, lang, fqn, symbol string
		if err := rows.Scan(&ownerFQN, &lang, &fqn, &symbol); err != nil {
			return nil, err
		}
		pkg, name := splitFQN(fqn)
		if name == "" {
			name = strings.TrimSpace(symbol)
			pkg = ""
		}
		if name == "" {
			continue
		}
		res = append(res, codeanchor.CallSite{
			File:     file,
			OwnerFQN: strings.TrimSpace(ownerFQN),
			CalleeSymbol: codeanchor.SymbolRef{
				Lang: codeanchor.Lang(lang),
				Pkg:  pkg,
				Name: name,
			},
		})
	}
	return res, rows.Err()
}

// Children returns symbols that extend or implement the parent.
func (s *Store) Children(ctx context.Context, parent string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT child_fqn FROM super_edges WHERE parent_fqn = ?`, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []string
	for rows.Next() {
		var child string
		if err := rows.Scan(&child); err != nil {
			return nil, err
		}
		res = append(res, child)
	}
	return res, rows.Err()
}

// ChildrenBatch returns children for multiple parent FQNs in one query.
func (s *Store) ChildrenBatch(ctx context.Context, parents []string) (map[string][]string, error) {
	if len(parents) == 0 {
		return map[string][]string{}, nil
	}
	returns := make(map[string][]string, len(parents))
	return returns, s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_super_parents`)
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_super_parents (parent_fqn TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_super_parents(parent_fqn) VALUES (?)`)
		if err != nil {
			return err
		}
		for _, p := range parents {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, p); err != nil {
				_ = stmt.Close()
				return err
			}
		}
		_ = stmt.Close()

		rows, err := tx.QueryContext(ctx, `
			SELECT p.parent_fqn, s.child_fqn
			FROM super_edges s
			JOIN temp_super_parents p ON p.parent_fqn = s.parent_fqn
		`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var parent, child string
			if err := rows.Scan(&parent, &child); err != nil {
				_ = rows.Close()
				return err
			}
			returns[parent] = append(returns[parent], child)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_super_parents`)
		return nil
	})
}

func symbolRefKey(ref codeanchor.SymbolRef) string {
	key := fmt.Sprintf("%s|%s|%s", strings.ToLower(string(ref.Lang)), ref.Pkg, ref.Name)
	if ref.Lang == codeanchor.LangPhp {
		return fmt.Sprintf("%s|%t", key, ref.Member)
	}
	return key
}

func splitFQN(fqn string) (pkg string, name string) {
	fqn = strings.TrimSpace(fqn)
	if fqn == "" {
		return "", ""
	}
	if dot := strings.LastIndex(fqn, "."); dot >= 0 {
		return fqn[:dot], fqn[dot+1:]
	}
	return "", fqn
}

// AnnotationUsesByType returns annotation uses filtered by type.
// If ref.Pkg is empty, it matches any package (wildcard).
func (s *Store) AnnotationUsesByType(ctx context.Context, ref codeanchor.SymbolRef) ([]codeanchor.AnnotationUse, error) {
	var rows *sql.Rows
	var err error
	if ref.Pkg == "" {
		// Empty pkg acts as wildcard - match any package
		rows, err = s.db.QueryContext(ctx, `
			SELECT owner_fqn, ann_pkg, args_json
			FROM annotations
			WHERE ann_lang = ? AND ann_name = ?
		`, ref.Lang, ref.Name)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT owner_fqn, ann_pkg, args_json
			FROM annotations
			WHERE ann_lang = ? AND ann_pkg = ? AND ann_name = ?
		`, ref.Lang, ref.Pkg, ref.Name)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []codeanchor.AnnotationUse
	for rows.Next() {
		var owner, pkg, argsJSON string
		if err := rows.Scan(&owner, &pkg, &argsJSON); err != nil {
			return nil, err
		}
		ann := codeanchor.AnnotationUse{
			OwnerFQN: owner,
			AnnSymbol: codeanchor.SymbolRef{
				Lang: ref.Lang,
				Pkg:  pkg,
				Name: ref.Name,
			},
		}
		if argsJSON != "" {
			_ = json.Unmarshal([]byte(argsJSON), &ann.Args)
		}
		res = append(res, ann)
	}
	return res, rows.Err()
}

// AnnotationUsesByTypes returns annotation uses for multiple types in one query.
// Wildcard pkg refs (empty pkg) are supported and keyed by lang|""|name.
func (s *Store) AnnotationUsesByTypes(ctx context.Context, refs []codeanchor.SymbolRef) (map[string][]codeanchor.AnnotationUse, error) {
	if len(refs) == 0 {
		return map[string][]codeanchor.AnnotationUse{}, nil
	}
	result := make(map[string][]codeanchor.AnnotationUse, len(refs))

	return result, s.withWriteTx(ctx, func(tx *sql.Tx) error {
		exact := make(map[string]codeanchor.SymbolRef)
		wild := make(map[string]codeanchor.SymbolRef)
		for _, ref := range refs {
			key := symbolRefKey(ref)
			if ref.Pkg == "" {
				wild[key] = ref
			} else {
				exact[key] = ref
			}
		}

		if len(exact) > 0 {
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_ann_exact`)
			if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_ann_exact (lang TEXT, pkg TEXT, name TEXT, key TEXT PRIMARY KEY)`); err != nil {
				return err
			}
			stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_ann_exact(lang, pkg, name, key) VALUES (?, ?, ?, ?)`)
			if err != nil {
				return err
			}
			for key, ref := range exact {
				if _, err := stmt.ExecContext(ctx, ref.Lang, ref.Pkg, ref.Name, key); err != nil {
					_ = stmt.Close()
					return err
				}
			}
			_ = stmt.Close()

			rows, err := tx.QueryContext(ctx, `
				SELECT t.key, a.owner_fqn, a.ann_pkg, a.args_json
				FROM annotations a
				JOIN temp_ann_exact t
				  ON a.ann_lang = t.lang AND a.ann_pkg = t.pkg AND a.ann_name = t.name
			`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var key, owner, pkg, argsJSON string
				if err := rows.Scan(&key, &owner, &pkg, &argsJSON); err != nil {
					_ = rows.Close()
					return err
				}
				ref := exact[key]
				ann := codeanchor.AnnotationUse{
					OwnerFQN: owner,
					AnnSymbol: codeanchor.SymbolRef{
						Lang: ref.Lang,
						Pkg:  pkg,
						Name: ref.Name,
					},
				}
				if argsJSON != "" {
					_ = json.Unmarshal([]byte(argsJSON), &ann.Args)
				}
				result[key] = append(result[key], ann)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			_ = rows.Close()
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_ann_exact`)
		}

		if len(wild) > 0 {
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_ann_wild`)
			if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_ann_wild (lang TEXT, name TEXT, key TEXT PRIMARY KEY)`); err != nil {
				return err
			}
			stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_ann_wild(lang, name, key) VALUES (?, ?, ?)`)
			if err != nil {
				return err
			}
			for key, ref := range wild {
				if _, err := stmt.ExecContext(ctx, ref.Lang, ref.Name, key); err != nil {
					_ = stmt.Close()
					return err
				}
			}
			_ = stmt.Close()

			rows, err := tx.QueryContext(ctx, `
				SELECT t.key, a.owner_fqn, a.ann_pkg, a.args_json
				FROM annotations a
				JOIN temp_ann_wild t
				  ON a.ann_lang = t.lang AND a.ann_name = t.name
			`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var key, owner, pkg, argsJSON string
				if err := rows.Scan(&key, &owner, &pkg, &argsJSON); err != nil {
					_ = rows.Close()
					return err
				}
				ref := wild[key]
				ann := codeanchor.AnnotationUse{
					OwnerFQN: owner,
					AnnSymbol: codeanchor.SymbolRef{
						Lang: ref.Lang,
						Pkg:  pkg,
						Name: ref.Name,
					},
				}
				if argsJSON != "" {
					_ = json.Unmarshal([]byte(argsJSON), &ann.Args)
				}
				result[key] = append(result[key], ann)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			_ = rows.Close()
			_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_ann_wild`)
		}
		return nil
	})
}

// CallFilesByCallee returns files containing calls to the callee symbol.
func (s *Store) CallFilesByCallee(ctx context.Context, ref *codeanchor.SymbolRef) ([]string, error) {
	if ref == nil {
		return nil, nil
	}
	refs := []codeanchor.SymbolRef{*ref}
	out, err := s.CallFilesByCallees(ctx, refs)
	if err != nil {
		return nil, err
	}
	return out[symbolRefKey(*ref)], nil
}

// CallFilesByCallees returns files containing calls for multiple callee symbols.
func (s *Store) CallFilesByCallees(ctx context.Context, refs []codeanchor.SymbolRef) (map[string][]string, error) {
	if len(refs) == 0 {
		return map[string][]string{}, nil
	}
	result := make(map[string][]string, len(refs))
	const refChunkSize = 200
	fqnToKeys := make(map[string][]string, len(refs))
	symbolToKeys := make(map[string][]string, len(refs))
	for _, ref := range refs {
		key := symbolRefKey(ref)
		if key == "" || strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(string(ref.Lang)) == "" {
			continue
		}
		if ref.Pkg != "" {
			fqn := callLookupFQN(ref)
			fqnToKeys[string(ref.Lang)+"\x00"+fqn] = append(fqnToKeys[string(ref.Lang)+"\x00"+fqn], key)
			continue
		}
		symbolToKeys[string(ref.Lang)+"\x00"+ref.Name] = append(symbolToKeys[string(ref.Lang)+"\x00"+ref.Name], key)
	}
	refs = dedupeSymbolRefs(refs)
	for batch := range slices.Chunk(refs, refChunkSize) {
		var clauses []string
		args := make([]any, 0, len(batch)*2)
		for _, ref := range batch {
			if strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(string(ref.Lang)) == "" {
				continue
			}
			if ref.Pkg != "" {
				clauses = append(clauses, `(callee.lang = ? AND callee.fqn = ?)`)
				args = append(args, string(ref.Lang), callLookupFQN(ref))
				continue
			}
			clauses = append(clauses, `(callee.lang = ? AND callee.symbol = ?)`)
			args = append(args, string(ref.Lang), ref.Name)
		}
		if len(clauses) == 0 {
			continue
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT caller.path, callee.lang, callee.symbol, callee.fqn
			FROM intel_edges e
			JOIN intel_code_anchors caller ON caller.id = e.src_row_id
			JOIN intel_code_anchors callee ON callee.id = e.dst_row_id
			WHERE e.kind = 'calls'
			  AND e.src_type = 'anchor'
			  AND e.dst_type = 'anchor'
			  AND (`+strings.Join(clauses, ` OR `)+`)
			GROUP BY caller.path, callee.lang, callee.symbol, callee.fqn
		`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var file, lang, symbol, fqn string
			if err := rows.Scan(&file, &lang, &symbol, &fqn); err != nil {
				_ = rows.Close()
				return nil, err
			}
			for _, key := range fqnToKeys[lang+"\x00"+fqn] {
				result[key] = append(result[key], file)
			}
			for _, key := range symbolToKeys[lang+"\x00"+symbol] {
				result[key] = append(result[key], file)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	for key, paths := range result {
		result[key] = dedupeNonEmptyStrings(paths)
	}
	return result, nil
}

func callLookupFQN(ref codeanchor.SymbolRef) string {
	if ref.Lang == codeanchor.LangPhp {
		return ref.Pkg + "::" + ref.Name
	}
	return ref.Pkg + "." + ref.Name
}

// HasAnyCallEdges returns true if any call edges exist in the store.
// Used to avoid unnecessary RebuildAllCallEdges on boot when edges already exist.
func (s *Store) HasAnyCallEdges(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges WHERE kind = 'calls' LIMIT 1`).Scan(&count)
	return count > 0, err
}

// SetAnchorScope replaces scope rows for an anchor.
func (s *Store) SetAnchorScope(ctx context.Context, anchorID int64, symbols []string, callFiles []string) error {
	ctx = indexingperf.WithOp(ctx, "intel.set_anchor_scope")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return s.setAnchorScopesTx(ctx, tx, []codeanchor.AnchorScopeUpdate{{
			ID:        anchorID,
			Symbols:   symbols,
			CallFiles: callFiles,
		}})
	})
}

// SetAnchorScopesBatch updates multiple anchor scopes in a single transaction.
func (s *Store) SetAnchorScopesBatch(ctx context.Context, updates []codeanchor.AnchorScopeUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.set_anchor_scopes_batch")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return s.setAnchorScopesTx(ctx, tx, updates)
	})
}

func (s *Store) setAnchorScopesTx(ctx context.Context, tx *sql.Tx, updates []codeanchor.AnchorScopeUpdate) error {
	for _, upd := range updates {
		if _, err := tx.ExecContext(ctx, `DELETE FROM anchor_scopes WHERE anchor_id = ?`, upd.ID); err != nil {
			return err
		}
		for _, fqn := range upd.Symbols {
			if fqn == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO anchor_scopes(anchor_id, symbol_fqn) VALUES (?, ?)`, upd.ID, fqn); err != nil {
				return err
			}
		}
		for _, file := range upd.CallFiles {
			if file == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO anchor_scopes(anchor_id, call_file) VALUES (?, ?)`, upd.ID, file); err != nil {
				return err
			}
		}
	}
	return nil
}

// AnchorScope returns symbols and call files for the anchor.
func (s *Store) AnchorScope(ctx context.Context, anchorID int64) ([]string, []string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol_fqn, call_file FROM anchor_scopes WHERE anchor_id = ?
	`, anchorID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var symbols, calls []string
	for rows.Next() {
		var sym, callFile sql.NullString
		if err := rows.Scan(&sym, &callFile); err != nil {
			return nil, nil, err
		}
		if sym.Valid && sym.String != "" {
			symbols = append(symbols, sym.String)
		}
		if callFile.Valid && callFile.String != "" {
			calls = append(calls, callFile.String)
		}
	}
	return symbols, calls, rows.Err()
}

func (s *Store) HasAnyAnchorScopes(ctx context.Context) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM anchor_scopes LIMIT 1`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// AllAnchorScopes returns scopes for all anchors keyed by anchor_id.
func (s *Store) AllAnchorScopes(ctx context.Context) (map[int64]codeanchor.AnchorScope, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT anchor_id, symbol_fqn, call_file
		FROM anchor_scopes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]codeanchor.AnchorScope)
	for rows.Next() {
		var anchorID int64
		var sym, call sql.NullString
		if err := rows.Scan(&anchorID, &sym, &call); err != nil {
			return nil, err
		}
		entry := out[anchorID]
		if sym.Valid && sym.String != "" {
			entry.Symbols = append(entry.Symbols, sym.String)
		}
		if call.Valid && call.String != "" {
			entry.Calls = append(entry.Calls, call.String)
		}
		out[anchorID] = entry
	}
	return out, rows.Err()
}

// FileHash returns stored hash, per-file indexer version, and last parse status (if present).
func (s *Store) FileHash(ctx context.Context, path string) (string, string, codeanchor.ParseStatus, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT hash, indexer_version, parse_status FROM files WHERE path = ?`, path)
	var hash sql.NullString
	var version sql.NullString
	var status sql.NullString
	if err := row.Scan(&hash, &version, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", codeanchor.ParseOK, false, nil
		}
		return "", "", codeanchor.ParseOK, false, err
	}
	if !hash.Valid || hash.String == "" {
		return "", "", codeanchor.ParseOK, false, nil
	}
	parseStatus := codeanchor.ParseOK
	if status.Valid && strings.TrimSpace(status.String) != "" {
		parseStatus = codeanchor.ParseStatus(status.String)
	}
	return hash.String, version.String, parseStatus, true, nil
}

// MarkCallEdgesStale marks code files as requiring a second-pass call-edge rebuild.
func (s *Store) MarkCallEdgesStale(ctx context.Context, paths []string) error {
	paths = normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(paths) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.mark_call_edges_stale")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		holders, args := placeholders(paths)
		_, err := tx.ExecContext(ctx, fmt.Sprintf(`
			UPDATE files
			SET call_edges_stale = 1
			WHERE path IN (%s)
		`, holders), args...)
		return err
	})
}

// ClearCallEdgesStale clears the call-edge-stale flag for the provided paths.
func (s *Store) ClearCallEdgesStale(ctx context.Context, paths []string) error {
	paths = normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(paths) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.clear_call_edges_stale")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		holders, args := placeholders(paths)
		_, err := tx.ExecContext(ctx, fmt.Sprintf(`
			UPDATE files
			SET call_edges_stale = 0
			WHERE path IN (%s)
		`, holders), args...)
		return err
	})
}

// StaleCallEdgePaths returns code paths marked as needing call-edge rebuild.
func (s *Store) StaleCallEdgePaths(ctx context.Context, limit int) ([]string, error) {
	query := `SELECT path FROM files WHERE call_edges_stale = 1 ORDER BY path`
	var args []any
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
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

// IndexedFilePaths returns all indexed file paths in this code index.
func (s *Store) IndexedFilePaths(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM files ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// IndexedFilePathsByPrefix returns indexed file paths that match the prefix or live under it.
func (s *Store) IndexedFilePathsByPrefix(ctx context.Context, prefix string) ([]string, error) {
	prefix = normalizeCodeLookupPath(prefix)
	if prefix == "" {
		return nil, nil
	}

	escapeGlob := func(in string) string {
		out := strings.ReplaceAll(in, "[", "[[]")
		out = strings.ReplaceAll(out, "*", "[*]")
		out = strings.ReplaceAll(out, "?", "[?]")
		return out
	}

	escaped := escapeGlob(prefix)
	glob := escaped + "/*"
	rows, err := s.db.QueryContext(ctx, `
		SELECT path
		FROM files
		WHERE path = ? OR path GLOB ?
		ORDER BY path
	`, prefix, glob)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// FilesDefiningSymbolFQN returns code file paths that define the given symbol FQN.
func (s *Store) FilesDefiningSymbolFQN(ctx context.Context, fqn string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT file FROM symbols WHERE fqn = ? ORDER BY file`, fqn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var file string
		if err := rows.Scan(&file); err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertIntelCallEdgesForPath replaces call/import edges for anchors in the given path.
// It deletes existing code-ref edges whose source anchor belongs to this path.
// then inserts the new edges. This is used during the second pass of batch indexing when
// callee/imported anchors are guaranteed to exist.
func (s *Store) UpsertIntelCallEdgesForPath(ctx context.Context, path string, edges []codeanchor.IntelEdge) error {
	path = normalizeCodeLookupPath(path)
	if path == "" {
		return nil
	}
	return s.UpsertIntelCallEdgesForPathsBatch(ctx, []codeanchor.CallEdgesBatch{{Path: path, Edges: edges}})
}

// UpsertIntelCallEdgesForPathsBatch upserts call/import/type_ref/member_ref edges for multiple paths in a single transaction.
func (s *Store) UpsertIntelCallEdgesForPathsBatch(ctx context.Context, batches []codeanchor.CallEdgesBatch) error {
	if len(batches) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_call_edges")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		deleteStmt, err := tx.PrepareContext(ctx, `
			DELETE FROM intel_edges
			WHERE kind IN ('calls', 'tests', 'imports', 'type_ref', 'member_ref')
			  AND src_type = 'anchor'
			  AND src_row_id IN (SELECT id FROM intel_code_anchors WHERE path = ?)
		`)
		if err != nil {
			return err
		}
		defer deleteStmt.Close()

		insertStmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO intel_edges (src_type, src_row_id, dst_type, dst_row_id, kind, meta_json)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer insertStmt.Close()

		anchorRowIDs := map[string]int64{}
		resolveRowID := func(anchorID string) (int64, error) {
			if rowID, ok := anchorRowIDs[anchorID]; ok {
				return rowID, nil
			}
			rowID, err := lookupIntelAnchorRowIDTx(ctx, tx, anchorID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return 0, err
			}
			anchorRowIDs[anchorID] = rowID
			return rowID, nil
		}
		clearPaths := make([]string, 0, len(batches))
		for _, batch := range batches {
			path := normalizeCodeLookupPath(batch.Path)
			if path == "" {
				continue
			}
			if _, err := deleteStmt.ExecContext(ctx, path); err != nil {
				return err
			}
			for _, e := range batch.Edges {
				if !codeanchor.IsCodeRefEdge(e.Kind) {
					continue
				}
				srcRowID, err := resolveRowID(e.SrcID)
				if err != nil {
					return err
				}
				dstRowID, err := resolveRowID(e.DstID)
				if err != nil {
					return err
				}
				if srcRowID <= 0 || dstRowID <= 0 {
					continue
				}
				if _, err := insertStmt.ExecContext(ctx, "anchor", srcRowID, "anchor", dstRowID, e.Kind, e.MetaJSON); err != nil {
					return err
				}
			}
			clearPaths = append(clearPaths, path)
		}
		if len(clearPaths) > 0 {
			holders, args := placeholders(clearPaths)
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE files SET call_edges_stale = 0 WHERE path IN (%s)`, holders), args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceIntelSymbolRefsForPathsBatch replaces symbol-ref rows for multiple paths in a single transaction.
func (s *Store) ReplaceIntelSymbolRefsForPathsBatch(ctx context.Context, batches map[string][]codeanchor.SymbolRefRow) error {
	if len(batches) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_symbol_refs")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmts := []string{
			`DROP TABLE IF EXISTS temp_symbol_ref_batch;`,
			`DROP TABLE IF EXISTS temp_symbol_ref_paths;`,
			`CREATE TEMP TABLE temp_symbol_ref_batch (
				src_path TEXT NOT NULL,
				owner_fqn TEXT NOT NULL,
				ref_kind TEXT NOT NULL,
				dst_lang TEXT NOT NULL,
				dst_pkg TEXT NOT NULL,
				dst_name TEXT NOT NULL,
				dst_fqn TEXT NOT NULL,
				dst_member INTEGER NOT NULL
			);`,
			`CREATE TEMP TABLE temp_symbol_ref_paths (
				src_path TEXT PRIMARY KEY
			) WITHOUT ROWID;`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		defer func() {
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_symbol_ref_batch`)
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_symbol_ref_paths`)
		}()

		pathStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_symbol_ref_paths(src_path) VALUES (?)`)
		if err != nil {
			return err
		}
		defer pathStmt.Close()

		batchStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO temp_symbol_ref_batch (
				src_path, owner_fqn, ref_kind, dst_lang, dst_pkg, dst_name, dst_fqn, dst_member
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer batchStmt.Close()

		for rawPath, rows := range batches {
			path := normalizeCodeLookupPath(rawPath)
			if path == "" {
				continue
			}
			if _, err := pathStmt.ExecContext(ctx, path); err != nil {
				return err
			}
			for _, row := range rows {
				if _, err := batchStmt.ExecContext(ctx,
					path,
					row.OwnerFQN,
					string(row.RefKind),
					string(row.DstLang),
					row.DstPkg,
					row.DstName,
					row.DstFQN,
					row.DstMember,
				); err != nil {
					return err
				}
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_symbol_ref_files(path)
			SELECT src_path
			FROM temp_symbol_ref_paths
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
			SELECT DISTINCT dst_lang, dst_pkg, dst_name, dst_fqn, dst_member
			FROM temp_symbol_ref_batch
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM intel_symbol_refs
			WHERE src_file_id IN (
				SELECT sf.file_id
				FROM intel_symbol_ref_files sf
				JOIN temp_symbol_ref_paths p ON p.src_path = sf.path
			)
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_symbol_refs (
				src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id
			)
			SELECT sf.file_id,
			       s.id,
			       b.owner_fqn,
			       b.ref_kind,
			       t.target_id
			FROM temp_symbol_ref_batch b
			JOIN intel_symbol_ref_files sf ON sf.path = b.src_path
			JOIN intel_symbol_ref_targets t
			  ON t.dst_lang = b.dst_lang
			 AND t.dst_pkg = b.dst_pkg
			 AND t.dst_name = b.dst_name
			 AND t.dst_fqn = b.dst_fqn
			 AND t.dst_member = b.dst_member
			LEFT JOIN symbols s ON s.fqn = b.owner_fqn
		`); err != nil {
			return err
		}
		return pruneOrphanReverseIndexTargetsTx(ctx, tx)
	})
}

// ReplaceIntelImportRefsForPathsBatch replaces import-ref rows for multiple paths in a single transaction.
func (s *Store) ReplaceIntelImportRefsForPathsBatch(ctx context.Context, batches map[string][]codeanchor.ImportRefRow) error {
	if len(batches) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_import_refs")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmts := []string{
			`DROP TABLE IF EXISTS temp_intel_import_ref_paths;`,
			`DROP TABLE IF EXISTS temp_intel_import_ref_batch;`,
			`CREATE TEMP TABLE temp_intel_import_ref_paths (
				src_path TEXT PRIMARY KEY
			) WITHOUT ROWID;`,
			`CREATE TEMP TABLE temp_intel_import_ref_batch (
				src_path TEXT NOT NULL,
				module TEXT NOT NULL,
				PRIMARY KEY (src_path, module)
			) WITHOUT ROWID;`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		defer func() {
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_import_ref_paths`)
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_import_ref_batch`)
		}()

		pathStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_intel_import_ref_paths(src_path) VALUES (?)`)
		if err != nil {
			return err
		}
		defer pathStmt.Close()

		batchStmt, err := tx.PrepareContext(ctx, `
			INSERT OR IGNORE INTO temp_intel_import_ref_batch (src_path, module)
			VALUES (?, ?)
		`)
		if err != nil {
			return err
		}
		defer batchStmt.Close()

		for rawPath, rows := range batches {
			path := normalizeCodeLookupPath(rawPath)
			if path == "" {
				continue
			}
			if _, err := pathStmt.ExecContext(ctx, path); err != nil {
				return err
			}
			for _, row := range rows {
				if strings.TrimSpace(row.Module) == "" {
					continue
				}
				if _, err := batchStmt.ExecContext(ctx, path, row.Module); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM intel_import_refs
			WHERE src_path IN (SELECT src_path FROM temp_intel_import_ref_paths)
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_import_refs (src_path, module)
			SELECT src_path, module
			FROM temp_intel_import_ref_batch
		`); err != nil {
			return err
		}
		return nil
	})
}

// ReplaceIntelModuleDefsForPathsBatch replaces module-def rows for multiple paths in a single transaction.
func (s *Store) ReplaceIntelModuleDefsForPathsBatch(ctx context.Context, batches map[string][]codeanchor.ModuleDefRow) error {
	if len(batches) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.replace_module_defs")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmts := []string{
			`DROP TABLE IF EXISTS temp_intel_module_def_paths;`,
			`DROP TABLE IF EXISTS temp_intel_module_def_batch;`,
			`CREATE TEMP TABLE temp_intel_module_def_paths (
				src_path TEXT PRIMARY KEY
			) WITHOUT ROWID;`,
			`CREATE TEMP TABLE temp_intel_module_def_batch (
				src_path TEXT NOT NULL,
				lang TEXT NOT NULL,
				module TEXT NOT NULL,
				PRIMARY KEY (src_path, lang, module)
			) WITHOUT ROWID;`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		defer func() {
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_module_def_paths`)
			_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_module_def_batch`)
		}()

		pathStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_intel_module_def_paths(src_path) VALUES (?)`)
		if err != nil {
			return err
		}
		defer pathStmt.Close()

		batchStmt, err := tx.PrepareContext(ctx, `
			INSERT OR IGNORE INTO temp_intel_module_def_batch (src_path, lang, module)
			VALUES (?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer batchStmt.Close()

		for rawPath, rows := range batches {
			path := normalizeCodeLookupPath(rawPath)
			if path == "" {
				continue
			}
			if _, err := pathStmt.ExecContext(ctx, path); err != nil {
				return err
			}
			for _, row := range rows {
				if strings.TrimSpace(row.Module) == "" {
					continue
				}
				if _, err := batchStmt.ExecContext(ctx, path, string(row.Lang), row.Module); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM intel_module_defs
			WHERE src_path IN (SELECT src_path FROM temp_intel_module_def_paths)
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_module_defs (src_path, lang, module)
			SELECT src_path, lang, module
			FROM temp_intel_module_def_batch
		`); err != nil {
			return err
		}
		return nil
	})
}

// SymbolRefsByPaths returns stored symbol-ref rows keyed by source path.
func (s *Store) SymbolRefsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.SymbolRefRow, error) {
	normalized := normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(normalized) == 0 {
		return map[string][]codeanchor.SymbolRefRow{}, nil
	}
	holders, args := placeholders(normalized)
	query := fmt.Sprintf(`
		SELECT sf.path, r.owner_fqn, r.ref_kind, t.dst_lang, t.dst_pkg, t.dst_name, t.dst_fqn, t.dst_member
		FROM intel_symbol_refs r
		JOIN intel_symbol_ref_files sf ON sf.file_id = r.src_file_id
		JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
		WHERE sf.path IN (%s)
		ORDER BY sf.path, r.owner_fqn, r.ref_kind, t.dst_lang, t.dst_pkg, t.dst_name
	`, holders)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.SymbolRefRow, len(normalized))
	for rows.Next() {
		var row codeanchor.SymbolRefRow
		var refKind string
		var dstLang string
		var dstMember int
		if err := rows.Scan(&row.SrcPath, &row.OwnerFQN, &refKind, &dstLang, &row.DstPkg, &row.DstName, &row.DstFQN, &dstMember); err != nil {
			return nil, err
		}
		row.RefKind = codeanchor.RefKind(refKind)
		row.DstLang = codeanchor.Lang(dstLang)
		row.DstMember = dstMember != 0
		out[row.SrcPath] = append(out[row.SrcPath], row)
	}
	return out, rows.Err()
}

// ImportRefsByPaths returns stored import-ref rows keyed by source path.
func (s *Store) ImportRefsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.ImportRefRow, error) {
	normalized := normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(normalized) == 0 {
		return map[string][]codeanchor.ImportRefRow{}, nil
	}
	holders, args := placeholders(normalized)
	query := fmt.Sprintf(`
		SELECT src_path, module
		FROM intel_import_refs
		WHERE src_path IN (%s)
		ORDER BY src_path, module
	`, holders)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.ImportRefRow, len(normalized))
	for rows.Next() {
		var row codeanchor.ImportRefRow
		if err := rows.Scan(&row.SrcPath, &row.Module); err != nil {
			return nil, err
		}
		out[row.SrcPath] = append(out[row.SrcPath], row)
	}
	return out, rows.Err()
}

// ModuleDefsByPaths returns stored module-def rows keyed by source path.
func (s *Store) ModuleDefsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.ModuleDefRow, error) {
	normalized := normalizeNonEmptyStrings(normalizeCodePathInputs(paths))
	if len(normalized) == 0 {
		return map[string][]codeanchor.ModuleDefRow{}, nil
	}
	holders, args := placeholders(normalized)
	query := fmt.Sprintf(`
		SELECT src_path, lang, module
		FROM intel_module_defs
		WHERE src_path IN (%s)
		ORDER BY src_path, lang, module
	`, holders)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]codeanchor.ModuleDefRow, len(normalized))
	for rows.Next() {
		var row codeanchor.ModuleDefRow
		var lang string
		if err := rows.Scan(&row.SrcPath, &lang, &row.Module); err != nil {
			return nil, err
		}
		row.Lang = codeanchor.Lang(lang)
		out[row.SrcPath] = append(out[row.SrcPath], row)
	}
	return out, rows.Err()
}

// ReferrerPathsBySymbolRefs returns caller paths keyed by symbolRefKey.
func (s *Store) ReferrerPathsBySymbolRefs(ctx context.Context, refs []codeanchor.SymbolRef) (map[string][]string, error) {
	if len(refs) == 0 {
		return map[string][]string{}, nil
	}
	result := make(map[string][]string, len(refs))

	return result, s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_symbol_refs`)
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_symbol_ref_matches`)
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_symbol_refs (lang TEXT, pkg TEXT, name TEXT, member INTEGER NOT NULL, fqn TEXT, key TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_symbol_ref_matches (key TEXT NOT NULL, target_id INTEGER NOT NULL, PRIMARY KEY (key, target_id)) WITHOUT ROWID`); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_symbol_refs(lang, pkg, name, member, fqn, key) VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			key := symbolRefKey(ref)
			fqn := codeanchor.NormalizeSymbolRef(ref)
			member := ref.Lang == codeanchor.LangPhp && ref.Member
			if _, err := stmt.ExecContext(ctx, ref.Lang, ref.Pkg, ref.Name, member, fqn, key); err != nil {
				_ = stmt.Close()
				return err
			}
		}
		_ = stmt.Close()

		for _, query := range []string{
			`
				INSERT OR IGNORE INTO temp_symbol_ref_matches(key, target_id)
				SELECT t.key, dst.target_id
				FROM temp_symbol_refs t
				JOIN intel_symbol_ref_targets dst
				  ON t.pkg = ''
				 AND dst.dst_lang = t.lang
				 AND dst.dst_name = t.name
				 AND dst.dst_member = t.member
			`,
			`
				INSERT OR IGNORE INTO temp_symbol_ref_matches(key, target_id)
				SELECT t.key, dst.target_id
				FROM temp_symbol_refs t
				JOIN intel_symbol_ref_targets dst
				  ON t.pkg != ''
				 AND dst.dst_lang = t.lang
				 AND dst.dst_fqn = t.fqn
				 AND dst.dst_member = t.member
			`,
		} {
			if _, err := tx.ExecContext(ctx, query); err != nil {
				return err
			}
		}

		rows, err := tx.QueryContext(ctx, `
			SELECT m.key, sf.path
			FROM temp_symbol_ref_matches m
			JOIN intel_symbol_refs r ON r.dst_target_id = m.target_id
			JOIN intel_symbol_ref_files sf ON sf.file_id = r.src_file_id
			GROUP BY m.key, sf.path
			ORDER BY m.key, sf.path
		`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var key, path string
			if err := rows.Scan(&key, &path); err != nil {
				_ = rows.Close()
				return err
			}
			result[key] = append(result[key], path)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_symbol_refs`)
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_symbol_ref_matches`)
		return nil
	})
}

// ReferrerPathsByModules returns caller paths keyed by module string.
func (s *Store) ReferrerPathsByModules(ctx context.Context, modules []string) (map[string][]string, error) {
	unique := normalizeNonEmptyStrings(modules)
	if len(unique) == 0 {
		return map[string][]string{}, nil
	}
	result := make(map[string][]string, len(unique))

	return result, s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_modules`)
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_modules (module TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_modules(module) VALUES (?)`)
		if err != nil {
			return err
		}
		for _, mod := range unique {
			if _, err := stmt.ExecContext(ctx, mod); err != nil {
				_ = stmt.Close()
				return err
			}
		}
		_ = stmt.Close()

		rows, err := tx.QueryContext(ctx, `
			SELECT t.module, r.src_path
			FROM intel_import_refs r
			JOIN temp_modules t ON r.module = t.module
			GROUP BY t.module, r.src_path
			ORDER BY t.module, r.src_path
		`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var mod, path string
			if err := rows.Scan(&mod, &path); err != nil {
				_ = rows.Close()
				return err
			}
			result[mod] = append(result[mod], path)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_modules`)
		return nil
	})
}

type intelCodeInsertStmts struct {
	anchor *sql.Stmt
	edge   *sql.Stmt
}

func (s *Store) prepareIntelCodeInsertStmts(ctx context.Context, tx *sql.Tx) (*intelCodeInsertStmts, error) {
	anchorStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO intel_code_anchors (
			anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
			start_byte, end_byte, start_line, end_line, fingerprint, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return nil, err
	}
	edgeStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO intel_edges (src_type, src_row_id, dst_type, dst_row_id, kind, meta_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = anchorStmt.Close()
		return nil, err
	}
	return &intelCodeInsertStmts{anchor: anchorStmt, edge: edgeStmt}, nil
}

func (s *Store) replaceIntelCodeFileTx(ctx context.Context, tx *sql.Tx, stmts *intelCodeInsertStmts, r codeanchor.IntelCodeFileReplace, now int64) error {
	path := normalizeCodeLookupPath(r.Path)
	if path == "" {
		return nil
	}
	indexingperf.ObserveSample(ctx, "codepersist.batch.anchors", int64(len(r.Anchors)))
	indexingperf.ObserveSample(ctx, "codepersist.batch.edges", int64(len(r.Edges)))
	indexingperf.ObserveSample(ctx, "codepersist.batch.fts_rows", int64(len(r.FTSRows)))
	if err := s.deleteIntelCodeByPathTx(ctx, tx, path); err != nil {
		return err
	}
	anchorIDs := make([]string, 0, len(r.Anchors))
	anchorRowIDs := make(map[string]int64, len(r.Anchors))
	anchorInsertStarted := time.Now()
	for _, a := range r.Anchors {
		if a.UpdatedAt == 0 {
			a.UpdatedAt = now
		}
		if _, err := stmts.anchor.ExecContext(ctx,
			a.AnchorID, a.Lang, a.Kind, a.Path, a.Symbol, a.FQN, a.Signature, a.DocComment,
			a.StartByte, a.EndByte, a.StartLine, a.EndLine, a.Fingerprint, a.UpdatedAt,
		); err != nil {
			return err
		}
		anchorIDs = append(anchorIDs, a.AnchorID)
	}
	indexingperf.ObserveLatency(ctx, "codepersist.intel.anchor_insert", time.Since(anchorInsertStarted))
	rowLookupStarted := time.Now()
	if len(anchorIDs) > 0 {
		rowIDs, err := selectIntelAnchorRowIDsByAnchorIDsTx(ctx, tx, anchorIDs)
		if err != nil {
			return err
		}
		for id, rowID := range rowIDs {
			anchorRowIDs[id] = rowID
		}
		indexingperf.AddCount(ctx, "codepersist.intel.anchor_rowid_lookup.count", int64(len(anchorIDs)))
	}
	indexingperf.ObserveLatency(ctx, "codepersist.intel.anchor_rowid_lookup", time.Since(rowLookupStarted))
	if len(anchorIDs) > 0 {
		ftsDeleteStarted := time.Now()
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, "anchor", anchorIDs); err != nil {
			return err
		}
		indexingperf.ObserveLatency(ctx, "codepersist.intel.fts_delete", time.Since(ftsDeleteStarted))
	}
	missingIDs := make([]string, 0)
	for _, e := range r.Edges {
		if _, ok := anchorRowIDs[e.SrcID]; !ok && strings.TrimSpace(e.SrcID) != "" {
			missingIDs = append(missingIDs, e.SrcID)
		}
		if _, ok := anchorRowIDs[e.DstID]; !ok && strings.TrimSpace(e.DstID) != "" {
			missingIDs = append(missingIDs, e.DstID)
		}
	}
	if len(missingIDs) > 0 {
		rowLookupStarted = time.Now()
		uniqueMissing := dedupeNonEmptyStrings(missingIDs)
		resolved, err := selectIntelAnchorRowIDsByAnchorIDsTx(ctx, tx, uniqueMissing)
		if err != nil {
			return err
		}
		for id, rowID := range resolved {
			anchorRowIDs[id] = rowID
		}
		indexingperf.AddCount(ctx, "codepersist.intel.anchor_rowid_lookup.count", int64(len(uniqueMissing)))
		indexingperf.ObserveLatency(ctx, "codepersist.intel.anchor_rowid_lookup", time.Since(rowLookupStarted))
	}
	edgeInsertStarted := time.Now()
	for _, e := range r.Edges {
		srcRowID := anchorRowIDs[e.SrcID]
		dstRowID := anchorRowIDs[e.DstID]
		if srcRowID <= 0 || dstRowID <= 0 {
			continue
		}
		if _, err := stmts.edge.ExecContext(ctx, "anchor", srcRowID, "anchor", dstRowID, e.Kind, e.MetaJSON); err != nil {
			return err
		}
	}
	indexingperf.ObserveLatency(ctx, "codepersist.intel.edge_insert", time.Since(edgeInsertStarted))
	return nil
}

type intelDocInsertStmts struct {
	section *sql.Stmt
	edge    *sql.Stmt
}

func (s *Store) prepareIntelDocInsertStmts(ctx context.Context, tx *sql.Tx) (*intelDocInsertStmts, error) {
	sectionStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO intel_doc_sections (
			section_id, path, title, level, start_byte, end_byte, content, fingerprint, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return nil, err
	}
	edgeStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO intel_edges (src_type, src_row_id, dst_type, dst_row_id, kind, meta_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = sectionStmt.Close()
		return nil, err
	}
	return &intelDocInsertStmts{section: sectionStmt, edge: edgeStmt}, nil
}

func (s *Store) replaceIntelDocSectionsTx(ctx context.Context, tx *sql.Tx, stmts *intelDocInsertStmts, r codeanchor.IntelDocReplace, now int64) error {
	path := strings.TrimSpace(r.Path)
	if path == "" {
		return nil
	}
	if err := s.deleteIntelDocSectionsByPathTx(ctx, tx, path); err != nil {
		return err
	}
	sectionIDs := make([]string, 0, len(r.Sections))
	sectionRowIDs := make(map[string]int64, len(r.Sections))
	for _, sec := range r.Sections {
		if sec.UpdatedAt == 0 {
			sec.UpdatedAt = now
		}
		if _, err := stmts.section.ExecContext(ctx,
			sec.SectionID, sec.Path, sec.Title, sec.Level, sec.StartByte, sec.EndByte,
			sec.Content, sec.Fingerprint, sec.UpdatedAt,
		); err != nil {
			return err
		}
		sectionIDs = append(sectionIDs, sec.SectionID)
		rowID, err := lookupIntelSectionRowIDTx(ctx, tx, sec.SectionID)
		if err != nil {
			return err
		}
		sectionRowIDs[sec.SectionID] = rowID
	}
	if len(sectionIDs) > 0 {
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, "doc_section", sectionIDs); err != nil {
			return err
		}
	}
	for _, e := range r.Mentions {
		srcRowID, ok := sectionRowIDs[e.SrcID]
		if !ok {
			var err error
			srcRowID, err = lookupIntelSectionRowIDTx(ctx, tx, e.SrcID)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			sectionRowIDs[e.SrcID] = srcRowID
		}
		dstRef, err := lookupIntelOwnerRefTx(ctx, tx, e.DstID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		if srcRowID <= 0 || dstRef.rowID <= 0 {
			continue
		}
		if _, err := stmts.edge.ExecContext(ctx, "doc_section", srcRowID, dstRef.ownerType, dstRef.rowID, e.Kind, e.MetaJSON); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReplaceIntelCodeFile(ctx context.Context, path string, anchors []codeanchor.IntelAnchor, edges []codeanchor.IntelEdge, ftsRows []codeanchor.IntelFTSRow) error {
	return s.ReplaceIntelCodeFilesBatch(ctx, []codeanchor.IntelCodeFileReplace{{
		Path:    path,
		Anchors: anchors,
		Edges:   edges,
		FTSRows: ftsRows,
	}})
}

// ReplaceIntelCodeFilesBatch replaces intel data for multiple code files in a single transaction.
func (s *Store) ReplaceIntelCodeFilesBatch(ctx context.Context, reps []codeanchor.IntelCodeFileReplace) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_code_file")
	if len(reps) == 0 {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmts, err := s.prepareIntelCodeInsertStmts(ctx, tx)
		if err != nil {
			return err
		}
		defer stmts.anchor.Close()
		defer stmts.edge.Close()
		now := time.Now().Unix()
		var allFTS []codeanchor.IntelFTSRow
		for _, r := range reps {
			r.Path = normalizeCodeLookupPath(r.Path)
			if err := s.replaceIntelCodeFileTx(ctx, tx, stmts, r, now); err != nil {
				return err
			}
			if len(r.FTSRows) > 0 {
				allFTS = append(allFTS, r.FTSRows...)
			}
		}
		if err := s.upsertIntelFTSRowsTx(ctx, tx, allFTS); err != nil {
			return err
		}
		return nil
	})
}

// ReplaceIntelDocSections replaces all intel doc sections + mention edges for a note path.
func (s *Store) ReplaceIntelDocSections(ctx context.Context, path string, sections []codeanchor.IntelDocSection, mentions []codeanchor.IntelEdge, ftsRows []codeanchor.IntelFTSRow) error {
	return s.ReplaceIntelDocSectionsBatch(ctx, []codeanchor.IntelDocReplace{{
		Path:     path,
		Sections: sections,
		Mentions: mentions,
		FTSRows:  ftsRows,
	}})
}

// ReplaceIntelDocSectionsBatch replaces intel doc sections for multiple notes in a single transaction.
func (s *Store) ReplaceIntelDocSectionsBatch(ctx context.Context, reps []codeanchor.IntelDocReplace) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_doc_sections")
	if len(reps) == 0 {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmts, err := s.prepareIntelDocInsertStmts(ctx, tx)
		if err != nil {
			return err
		}
		defer stmts.section.Close()
		defer stmts.edge.Close()
		now := time.Now().Unix()
		var allFTS []codeanchor.IntelFTSRow
		for _, r := range reps {
			if err := s.replaceIntelDocSectionsTx(ctx, tx, stmts, r, now); err != nil {
				return err
			}
			if len(r.FTSRows) > 0 {
				allFTS = append(allFTS, r.FTSRows...)
			}
		}
		if err := s.upsertIntelFTSRowsTx(ctx, tx, allFTS); err != nil {
			return err
		}
		return nil
	})
}

// DeleteIntelByPath removes all intel data (anchors, sections, edges, FTS, rationale) for a given path.
func (s *Store) DeleteIntelByPath(ctx context.Context, path string) error {
	ctx = indexingperf.WithOp(ctx, "intel.delete_by_path")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if err := s.deleteIntelCodeByPathTx(ctx, tx, path); err != nil {
			return err
		}
		if err := s.deleteIntelDocSectionsByPathTx(ctx, tx, path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = ?`, path); err != nil {
			return err
		}
		if err := s.deleteRationaleByPathTx(ctx, tx, path); err != nil {
			return err
		}
		return deleteIntelReverseIndexByPathsTx(ctx, tx, `SELECT ?`, normalizeCodeLookupPath(path))
	})
}

// ReplaceIntelChunks atomically replaces all chunks for the given owner IDs.
// This is typically called after generating chunks for a set of anchors/sections.
func (s *Store) ReplaceIntelChunks(ctx context.Context, ownerIDs []string, chunks []codeanchor.IntelChunk) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_chunks")
	return s.replaceIntelChunks(ctx, ownerIDs, "", chunks)
}

// ReplaceIntelChunksByFamily atomically replaces one chunk family without
// deleting other producers that share the same owner IDs.
func (s *Store) ReplaceIntelChunksByFamily(ctx context.Context, ownerIDs []string, family string, chunks []codeanchor.IntelChunk) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_chunks_by_family")
	family = normalizeIntelChunkFamily(family)
	for i := range chunks {
		if strings.TrimSpace(chunks[i].ChunkFamily) == "" {
			chunks[i].ChunkFamily = family
		}
	}
	return s.replaceIntelChunks(ctx, ownerIDs, family, chunks)
}

func (s *Store) replaceIntelChunks(ctx context.Context, ownerIDs []string, family string, chunks []codeanchor.IntelChunk) error {
	if len(ownerIDs) == 0 {
		return nil
	}
	ownerIDs = dedupeStringsStable(ownerIDs)
	chunks = dedupeIntelChunksStable(chunks)
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		ownerRefs := make(map[string]intelOwnerRef, len(ownerIDs))
		for _, ownerID := range ownerIDs {
			ref, err := lookupIntelOwnerRefTx(ctx, tx, ownerID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			ownerRefs[ownerID] = ref
		}

		keepChunkIDs := make(map[string]struct{}, len(chunks))
		for _, c := range chunks {
			if strings.TrimSpace(c.ChunkID) != "" {
				keepChunkIDs[c.ChunkID] = struct{}{}
			}
		}
		useKeepTable := len(keepChunkIDs) > 200
		if useKeepTable {
			if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_intel_keep_chunks`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_intel_keep_chunks (chunk_id TEXT PRIMARY KEY)`); err != nil {
				return err
			}
			stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_intel_keep_chunks(chunk_id) VALUES (?)`)
			if err != nil {
				return err
			}
			for id := range keepChunkIDs {
				if _, err := stmt.ExecContext(ctx, id); err != nil {
					_ = stmt.Close()
					return err
				}
			}
			if err := stmt.Close(); err != nil {
				return err
			}
			defer tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_intel_keep_chunks`) //nolint:errcheck
		}

		// Delete stale chunks for the owner IDs while preserving unchanged chunk
		// rows so their embeddings survive hash-stable incremental indexing.
		// Family-scoped replacement lets primary chunks and authored
		// section chunks share owners without erasing each other.
		for batch := range slices.Chunk(ownerIDs, 100) {
			holders, args := placeholders(batch)
			deleteSQL := fmt.Sprintf(`DELETE FROM intel_chunks WHERE owner_id IN (%s)`, holders)
			if useKeepTable {
				deleteSQL += ` AND chunk_id NOT IN (SELECT chunk_id FROM temp_intel_keep_chunks)`
			} else if len(keepChunkIDs) > 0 {
				chunkHolders := make([]string, 0, len(keepChunkIDs))
				for id := range keepChunkIDs {
					chunkHolders = append(chunkHolders, "?")
					args = append(args, id)
				}
				deleteSQL += fmt.Sprintf(` AND chunk_id NOT IN (%s)`, strings.Join(chunkHolders, ","))
			}
			if family != "" {
				deleteSQL += ` AND chunk_family = ?`
				args = append(args, family)
			}
			if _, err := tx.ExecContext(ctx, deleteSQL, args...); err != nil {
				return err
			}
		}

		// Insert or update current chunks.
		if len(chunks) == 0 {
			return nil
		}
		if err := retireChangedChunkEmbeddingEvidenceTx(ctx, tx, chunks); err != nil {
			return err
		}
		const insertSQL = `
			INSERT INTO intel_chunks (
				chunk_id, owner_id, owner_row_id, owner_type, chunk_family, ord, granularity,
				breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(chunk_id) DO UPDATE SET
				owner_id = excluded.owner_id,
				owner_row_id = excluded.owner_row_id,
				owner_type = excluded.owner_type,
				chunk_family = excluded.chunk_family,
				ord = excluded.ord,
				granularity = excluded.granularity,
				breadcrumb = excluded.breadcrumb,
				heading = excluded.heading,
				content_hash = excluded.content_hash,
				start_byte = excluded.start_byte,
				end_byte = excluded.end_byte,
				updated_at = excluded.updated_at`
		stmt, err := tx.PrepareContext(ctx, insertSQL)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, c := range chunks {
			ref, ok := ownerRefs[c.OwnerID]
			if !ok {
				var err error
				ref, err = lookupIntelOwnerRefTx(ctx, tx, c.OwnerID)
				if err != nil {
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
					ref = intelOwnerRef{ownerType: string(c.OwnerType)}
				}
				ownerRefs[c.OwnerID] = ref
			}
			var ownerRowID any
			if ref.rowID > 0 {
				ownerRowID = ref.rowID
			}
			if _, err := stmt.ExecContext(ctx, c.ChunkID, c.OwnerID, ownerRowID, ref.ownerType, normalizeIntelChunkFamily(c.ChunkFamily), c.Ord, c.Granularity, c.Breadcrumb, c.Heading, c.ContentHash, c.StartByte, c.EndByte, c.UpdatedAt); err != nil {
				return err
			}
		}
		return repartitionIntelEmbeddingOwnersTx(ctx, tx, keepChunkIDs, useKeepTable)
	})
}

func repartitionIntelEmbeddingOwnersTx(ctx context.Context, tx *sql.Tx, chunkIDs map[string]struct{}, useKeepTable bool) error {
	if len(chunkIDs) == 0 {
		return nil
	}
	var scopeSQL string
	var scopeArgs []any
	if useKeepTable {
		scopeSQL = `SELECT chunk_id FROM temp_intel_keep_chunks`
	} else {
		placeholders := make([]string, 0, len(chunkIDs))
		ids := make([]string, 0, len(chunkIDs))
		for id := range chunkIDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			placeholders = append(placeholders, "?")
			scopeArgs = append(scopeArgs, id)
		}
		scopeSQL = `SELECT chunk_id FROM intel_chunks WHERE chunk_id IN (` + strings.Join(placeholders, ",") + `)`
	}

	dimensionArgs := append([]any(nil), scopeArgs...)
	dimensionRows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT e.dimensions
		FROM intel_embeddings e
		WHERE e.chunk_id IN (`+scopeSQL+`)
		ORDER BY e.dimensions
	`, dimensionArgs...)
	if err != nil {
		return err
	}
	var dimensions []int
	for dimensionRows.Next() {
		var dims int
		if err := dimensionRows.Scan(&dims); err != nil {
			_ = dimensionRows.Close()
			return err
		}
		dimensions = append(dimensions, dims)
	}
	if err := dimensionRows.Err(); err != nil {
		_ = dimensionRows.Close()
		return err
	}
	_ = dimensionRows.Close()
	if len(dimensions) == 0 {
		return nil
	}

	const staging = `temp_intel_vec_owner_repartition`
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+staging); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+staging+` (chunk_id INTEGER PRIMARY KEY, owner_type TEXT NOT NULL, embedding BLOB NOT NULL) STRICT`); err != nil {
		return err
	}
	defer tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+staging) //nolint:errcheck

	for _, dims := range dimensions {
		table := intelVecTableName(dims)
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+staging); err != nil {
			return err
		}
		args := []any{dims}
		args = append(args, scopeArgs...)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s(chunk_id, owner_type, embedding)
			SELECT c.id, c.owner_type, v.embedding
			FROM intel_chunks c
			JOIN intel_embeddings e ON e.chunk_id = c.chunk_id AND e.dimensions = ?
			JOIN %s v ON v.chunk_id = c.id
			WHERE c.chunk_id IN (%s) AND v.owner_type <> c.owner_type
		`, staging, table, scopeSQL), args...); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE chunk_id IN (SELECT chunk_id FROM `+staging+`)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+table+`(chunk_id, owner_type, embedding) SELECT chunk_id, owner_type, embedding FROM `+staging); err != nil {
			return err
		}
	}
	return nil
}

// IntelChunksByOwners returns chunks for the given owner IDs, ordered by owner_id then ord.
func (s *Store) IntelChunksByOwners(ctx context.Context, ownerIDs []string) ([]codeanchor.IntelChunk, error) {
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	var out []codeanchor.IntelChunk
	for batch := range slices.Chunk(ownerIDs, 100) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT chunk_id, owner_id, owner_type, chunk_family, ord, granularity, breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
			FROM intel_chunks
			WHERE owner_id IN (%s)
			ORDER BY owner_id, ord
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c codeanchor.IntelChunk
			if err := rows.Scan(&c.ChunkID, &c.OwnerID, &c.OwnerType, &c.ChunkFamily, &c.Ord, &c.Granularity, &c.Breadcrumb, &c.Heading, &c.ContentHash, &c.StartByte, &c.EndByte, &c.UpdatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// IntelChunks returns all chunks in the store, ordered by owner_id then ord.
func (s *Store) IntelChunks(ctx context.Context) ([]codeanchor.IntelChunk, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_id, owner_id, owner_type, chunk_family, ord, granularity, breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
		FROM intel_chunks
		ORDER BY owner_id, ord
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.IntelChunk
	for rows.Next() {
		var c codeanchor.IntelChunk
		if err := rows.Scan(&c.ChunkID, &c.OwnerID, &c.OwnerType, &c.ChunkFamily, &c.Ord, &c.Granularity, &c.Breadcrumb, &c.Heading, &c.ContentHash, &c.StartByte, &c.EndByte, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReplaceOntologyNodes atomically syncs ontology-node owners for the given note
// paths. Existing rows for unchanged node IDs are updated in place so their
// chunk rows and embeddings can survive hash-stable incremental indexing.
func (s *Store) ReplaceOntologyNodes(ctx context.Context, notePaths []string, nodes []codeanchor.IntelOntologyNode) error {
	return s.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: notePaths,
		Nodes:     nodes,
	})
}

// ReplaceOntologyNodeReadModel atomically syncs ontology-node identity rows and
// their queryable field-value sidecar rows for the given note paths.
func (s *Store) ReplaceOntologyNodeReadModel(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel) error {
	return s.replaceOntologyNodeReadModel(ctx, model, false)
}

// ReplaceOntologyNodeReadModelPreservingSemantic refreshes ontology catalog
// rows without advancing or pruning ontology-owned chunks, embeddings, or their
// invalidation sidecar. Validation projection uses this mode because semantic
// freshness is an independent prerequisite that only full indexing may advance.
func (s *Store) ReplaceOntologyNodeReadModelPreservingSemantic(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel) error {
	return s.replaceOntologyNodeReadModel(ctx, model, true)
}

func (s *Store) replaceOntologyNodeReadModel(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel, preserveSemantic bool) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_ontology_node_read_model")
	var fieldRowsWritten int
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var err error
		fieldRowsWritten, err = replaceOntologyNodeReadModelTx(ctx, tx, model, preserveSemantic)
		return err
	})
	if err == nil {
		indexingperf.AddCount(ctx, "ontology.field_rows_written", int64(fieldRowsWritten))
	}
	return err
}

func replaceOntologyNodeReadModelTx(ctx context.Context, tx *sql.Tx, model codeanchor.IntelOntologyNodeReadModel, preserveSemantic bool) (int, error) {
	notePaths := normalizeNonEmptyStrings(model.NotePaths)
	nodes, fieldValues, linkDependencies := model.Nodes, model.FieldValues, model.LinkDependencies
	if len(notePaths) == 0 && !model.FullReplace {
		return 0, nil
	}
	var fieldRowsWritten int
	if model.FullReplace {
		var statements []string
		if !preserveSemantic {
			statements = append(statements, `DELETE FROM ontology_node_embedding_state`)
		}
		statements = append(statements,
			`DELETE FROM ontology_node_field_values`,
			`DELETE FROM ontology_node_field_value_dependencies`,
		)
		if !preserveSemantic {
			statements = append(statements,
				`DELETE FROM intel_chunks WHERE owner_type = 'ontology_node'`,
			)
		}
		statements = append(statements, `DELETE FROM ontology_nodes`)
		for _, stmt := range statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return 0, err
			}
		}
	} else {
		if err := deleteOntologyNodeFieldValuesForPathsTx(ctx, tx, notePaths); err != nil {
			return 0, err
		}
		if err := deleteOntologyNodeLinkDependenciesForPathsTx(ctx, tx, notePaths); err != nil {
			return 0, err
		}
		if err := deleteStaleOntologyNodesForPathsTx(ctx, tx, notePaths, nodes, preserveSemantic); err != nil {
			return 0, err
		}
	}
	if len(nodes) > 0 {
		if err := insertOntologyNodesTx(ctx, tx, nodes); err != nil {
			return 0, err
		}
	}
	if len(fieldValues) > 0 {
		written, err := insertOntologyNodeFieldValuesTx(ctx, tx, fieldValues)
		if err != nil {
			return 0, err
		}
		fieldRowsWritten = written
	}
	if len(linkDependencies) > 0 {
		return fieldRowsWritten, insertOntologyNodeLinkDependenciesTx(ctx, tx, linkDependencies)
	}
	return fieldRowsWritten, nil
}

func insertOntologyNodesTx(ctx context.Context, tx *sql.Tx, nodes []codeanchor.IntelOntologyNode) error {
	stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO ontology_nodes (
				node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
				parent_type_name, title, source_locator, fragment, block_id, display_label,
				locator_status, start_byte, end_byte, structural_fingerprint,
				schema_hash, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(node_id) DO UPDATE SET
				note_path = excluded.note_path,
				node_ref_json = excluded.node_ref_json,
				node_kind = excluded.node_kind,
				type_name = excluded.type_name,
				parent_node_id = excluded.parent_node_id,
				parent_type_name = excluded.parent_type_name,
				title = excluded.title,
				source_locator = excluded.source_locator,
				fragment = excluded.fragment,
				block_id = excluded.block_id,
				display_label = excluded.display_label,
				locator_status = excluded.locator_status,
				start_byte = excluded.start_byte,
				end_byte = excluded.end_byte,
				structural_fingerprint = excluded.structural_fingerprint,
				schema_hash = excluded.schema_hash,
				updated_at = excluded.updated_at
		`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, node := range nodes {
		if strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.NotePath) == "" || strings.TrimSpace(node.NodeRefJSON) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			node.NodeID,
			node.NotePath,
			node.NodeRefJSON,
			node.NodeKind,
			node.TypeName,
			node.ParentNodeID,
			node.ParentTypeName,
			node.Title,
			node.SourceLocator,
			node.Fragment,
			node.BlockID,
			node.DisplayLabel,
			node.LocatorStatus,
			node.StartByte,
			node.EndByte,
			node.StructuralFingerprint,
			node.SchemaHash,
			node.UpdatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func insertOntologyNodeFieldValuesTx(ctx context.Context, tx *sql.Tx, fieldValues []codeanchor.IntelOntologyNodeFieldValue) (int, error) {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO ontology_node_field_values (
			node_id, note_path, type_name, field_name, field_kind, source_kind,
			value_kind, value_text, value_norm, value_bool, value_int, value_real,
			value_date, value_datetime, target_node_id, target_ref_json, target_note_path,
			target_type_name, target_source_locator, list_ordinal, schema_hash, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, field_name, list_ordinal, value_norm, target_node_id) DO UPDATE SET
			note_path = excluded.note_path,
			type_name = excluded.type_name,
			field_kind = excluded.field_kind,
			source_kind = excluded.source_kind,
			value_kind = excluded.value_kind,
			value_text = excluded.value_text,
			value_bool = excluded.value_bool,
			value_int = excluded.value_int,
			value_real = excluded.value_real,
			value_date = excluded.value_date,
			value_datetime = excluded.value_datetime,
			target_ref_json = excluded.target_ref_json,
			target_note_path = excluded.target_note_path,
			target_type_name = excluded.target_type_name,
			target_source_locator = excluded.target_source_locator,
			schema_hash = excluded.schema_hash,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	written := 0
	for _, row := range fieldValues {
		if strings.TrimSpace(row.NodeID) == "" || strings.TrimSpace(row.NotePath) == "" || strings.TrimSpace(row.FieldName) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			row.NodeID,
			row.NotePath,
			row.TypeName,
			row.FieldName,
			row.FieldKind,
			row.SourceKind,
			row.ValueKind,
			row.ValueText,
			row.ValueNorm,
			nullableBool(row.ValueBool),
			nullableInt64(row.ValueInt),
			nullableFloat64(row.ValueReal),
			nullableString(row.ValueDate),
			nullableString(row.ValueDateTime),
			row.TargetNodeID,
			row.TargetRefJSON,
			row.TargetNotePath,
			row.TargetTypeName,
			row.TargetSourceLocator,
			row.ListOrdinal,
			row.SchemaHash,
			row.UpdatedAt,
		); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func insertOntologyNodeLinkDependenciesTx(ctx context.Context, tx *sql.Tx, dependencies []codeanchor.IntelOntologyNodeLinkDependency) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO ontology_node_field_value_dependencies (
			source_note_path, node_id, type_name, field_name, target_input, target_input_norm,
			resolved_target_note_path, resolved_target_type_name, schema_hash, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_note_path, node_id, field_name, target_input_norm) DO UPDATE SET
			type_name = excluded.type_name,
			target_input = excluded.target_input,
			resolved_target_note_path = excluded.resolved_target_note_path,
			resolved_target_type_name = excluded.resolved_target_type_name,
			schema_hash = excluded.schema_hash,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range dependencies {
		if strings.TrimSpace(row.SourceNotePath) == "" || strings.TrimSpace(row.FieldName) == "" || strings.TrimSpace(row.TargetInputNorm) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			row.SourceNotePath,
			row.NodeID,
			row.TypeName,
			row.FieldName,
			row.TargetInput,
			row.TargetInputNorm,
			row.ResolvedTargetNotePath,
			row.ResolvedTargetTypeName,
			row.SchemaHash,
			row.UpdatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func nullableBool(v *bool) any {
	if v == nil {
		return nil
	}
	if *v {
		return 1
	}
	return 0
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableFloat64(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func deleteOntologyNodeFieldValuesForPathsTx(ctx context.Context, tx *sql.Tx, notePaths []string) error {
	notePaths = normalizeNonEmptyStrings(notePaths)
	for batch := range slices.Chunk(notePaths, 400) {
		holders, args := placeholders(batch)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM ontology_node_field_values WHERE note_path IN (%s)`,
			holders,
		), args...); err != nil {
			return err
		}
	}
	return nil
}

func deleteOntologyNodeLinkDependenciesForPathsTx(ctx context.Context, tx *sql.Tx, notePaths []string) error {
	notePaths = normalizeNonEmptyStrings(notePaths)
	for batch := range slices.Chunk(notePaths, 400) {
		holders, args := placeholders(batch)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM ontology_node_field_value_dependencies WHERE source_note_path IN (%s)`,
			holders,
		), args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) OntologyNodeLinkDependencySources(ctx context.Context, resolvedTargetNotePaths, targetInputNorms []string) ([]string, error) {
	resolvedTargetNotePaths = normalizeNonEmptyStrings(resolvedTargetNotePaths)
	targetInputNorms = normalizeNonEmptyStrings(targetInputNorms)
	if len(resolvedTargetNotePaths) == 0 && len(targetInputNorms) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{})
	querySources := func(column string, values []string) error {
		for batch := range slices.Chunk(values, 400) {
			holders, args := placeholders(batch)
			rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
				`SELECT DISTINCT source_note_path FROM ontology_node_field_value_dependencies WHERE %s IN (%s)`,
				column,
				holders,
			), args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var source string
				if err := rows.Scan(&source); err != nil {
					rows.Close()
					return err
				}
				if strings.TrimSpace(source) != "" {
					seen[source] = struct{}{}
				}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
		}
		return nil
	}
	if err := querySources("resolved_target_note_path", resolvedTargetNotePaths); err != nil {
		return nil, err
	}
	if err := querySources("target_input_norm", targetInputNorms); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for source := range seen {
		out = append(out, source)
	}
	sort.Strings(out)
	return out, nil
}

func deleteStaleOntologyNodesForPathsTx(ctx context.Context, tx *sql.Tx, notePaths []string, nodes []codeanchor.IntelOntologyNode, preserveSemantic bool) error {
	notePaths = normalizeNonEmptyStrings(notePaths)
	if len(notePaths) == 0 {
		return nil
	}
	keep := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if id := strings.TrimSpace(node.NodeID); id != "" {
			keep[id] = struct{}{}
		}
	}
	for batch := range slices.Chunk(notePaths, 400) {
		holders, args := placeholders(batch)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(
			`SELECT node_id FROM ontology_nodes WHERE note_path IN (%s)`,
			holders,
		), args...)
		if err != nil {
			return err
		}
		var staleIDs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if _, ok := keep[id]; !ok {
				staleIDs = append(staleIDs, id)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(staleIDs) == 0 {
			continue
		}
		idHolders := make([]string, len(staleIDs))
		idArgs := make([]any, len(staleIDs))
		for i, id := range staleIDs {
			idHolders[i] = "?"
			idArgs[i] = id
		}
		if !preserveSemantic {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(
				`DELETE FROM ontology_node_embedding_state
				 WHERE node_id IN (%s)`,
				strings.Join(idHolders, ","),
			), idArgs...); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(
				`DELETE FROM intel_chunks
				 WHERE owner_type = 'ontology_node'
				   AND owner_id IN (%s)`,
				strings.Join(idHolders, ","),
			), idArgs...); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM ontology_nodes WHERE node_id IN (%s)`,
			strings.Join(idHolders, ","),
		), idArgs...); err != nil {
			return err
		}
	}
	return nil
}

// OntologyNodesByIDs returns ontology-node owners keyed by node_id.
func (s *Store) OntologyNodesByIDs(ctx context.Context, nodeIDs []string) (map[string]codeanchor.IntelOntologyNode, error) {
	nodeIDs = normalizeNonEmptyStrings(nodeIDs)
	out := make(map[string]codeanchor.IntelOntologyNode, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return out, nil
	}
	for batch := range slices.Chunk(nodeIDs, 400) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT node_id, note_path, node_ref_json, node_kind, COALESCE(type_name, ''),
				COALESCE(parent_node_id, ''), COALESCE(parent_type_name, ''), COALESCE(title, ''),
				COALESCE(source_locator, ''), COALESCE(fragment, ''), COALESCE(block_id, ''),
				COALESCE(display_label, ''), COALESCE(locator_status, ''),
				start_byte, end_byte, COALESCE(structural_fingerprint, ''), COALESCE(schema_hash, ''), updated_at
			FROM ontology_nodes
			WHERE node_id IN (%s)
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var node codeanchor.IntelOntologyNode
			if err := scanOntologyNode(rows, &node); err != nil {
				rows.Close()
				return nil, err
			}
			out[node.NodeID] = node
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// AllOntologyNodes returns every persisted ontology-node owner.
func (s *Store) AllOntologyNodes(ctx context.Context) ([]codeanchor.IntelOntologyNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
			parent_type_name, title, source_locator, fragment, block_id, display_label,
			locator_status, start_byte, end_byte, structural_fingerprint, schema_hash, updated_at
		FROM ontology_nodes
		ORDER BY note_path, node_kind, node_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.IntelOntologyNode
	for rows.Next() {
		var node codeanchor.IntelOntologyNode
		if err := scanOntologyNode(rows, &node); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

// OntologyNodesByType returns ontology-node owners for the supplied type.
func (s *Store) OntologyNodesByType(ctx context.Context, typeName string) ([]codeanchor.IntelOntologyNode, error) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
			parent_type_name, title, source_locator, fragment, block_id, display_label,
			locator_status, start_byte, end_byte, structural_fingerprint, schema_hash, updated_at
		FROM ontology_nodes
		WHERE type_name = ?
		ORDER BY note_path, node_kind, node_id
	`, typeName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.IntelOntologyNode
	for rows.Next() {
		var node codeanchor.IntelOntologyNode
		if err := scanOntologyNode(rows, &node); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func (s *Store) OntologyNodeFieldValuesByNodeIDs(ctx context.Context, nodeIDs []string, fieldNames []string) ([]codeanchor.IntelOntologyNodeFieldValue, error) {
	nodeIDs = normalizeNonEmptyStrings(nodeIDs)
	if len(nodeIDs) == 0 {
		return []codeanchor.IntelOntologyNodeFieldValue{}, nil
	}
	var out []codeanchor.IntelOntologyNodeFieldValue
	for batch := range slices.Chunk(nodeIDs, 400) {
		rows, err := s.ontologyNodeFieldValuesByNodeIDsBatch(ctx, batch, fieldNames)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func (s *Store) ontologyNodeFieldValuesByNodeIDsBatch(ctx context.Context, nodeIDs []string, fieldNames []string) ([]codeanchor.IntelOntologyNodeFieldValue, error) {
	conditions := []string{fmt.Sprintf("node_id IN (%s)", strings.TrimSuffix(strings.Repeat("?,", len(nodeIDs)), ","))}
	args := sliceAny(nodeIDs)
	normalized := normalizeNonEmptyStrings(fieldNames)
	if len(normalized) > 0 {
		// Copy + lowercase so we never mutate the caller's slice.
		lowered := make([]string, len(normalized))
		for i, name := range normalized {
			lowered[i] = strings.ToLower(strings.TrimSpace(name))
		}
		conditions = append(conditions, fmt.Sprintf("field_name IN (%s)", strings.TrimSuffix(strings.Repeat("?,", len(lowered)), ",")))
		args = append(args, sliceAny(lowered)...)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT node_id, note_path, type_name, field_name, field_kind, source_kind,
			value_kind, value_text, value_norm, value_bool, value_int, value_real,
			value_date, value_datetime, target_node_id, target_ref_json, target_note_path,
			target_type_name, target_source_locator, list_ordinal, schema_hash, updated_at
		FROM ontology_node_field_values
		WHERE %s
		ORDER BY node_id, field_name, list_ordinal, value_norm, target_node_id
	`, strings.Join(conditions, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOntologyNodeFieldValueRows(rows)
}

func ontologyNodePredicateSQL(idx int, typeNames []string, predicate codeanchor.OntologyFieldPredicate) (string, []any, error) {
	fieldName := strings.ToLower(strings.TrimSpace(predicate.FieldName))
	switch fieldName {
	case "notepath", "path":
		return ontologyNodePathPredicateSQL(predicate)
	default:
		return ontologyFieldPredicateSQL(idx, typeNames, predicate)
	}
}

func ontologyNodePathPredicateSQL(predicate codeanchor.OntologyFieldPredicate) (string, []any, error) {
	op := predicate.Op
	if op == "" {
		op = codeanchor.OntologyFieldOpEq
	}
	switch op {
	case codeanchor.OntologyFieldOpExists:
		return "n.note_path != ''", nil, nil
	case codeanchor.OntologyFieldOpEq:
		if len(predicate.Values) == 0 {
			return "", nil, fmt.Errorf("notePath predicate requires value")
		}
		return "n.note_path = ?", []any{strings.TrimSpace(predicate.Values[0].ValueNorm)}, nil
	case codeanchor.OntologyFieldOpIn:
		if len(predicate.Values) == 0 {
			return "", nil, fmt.Errorf("notePath in predicate requires values")
		}
		holders := strings.TrimSuffix(strings.Repeat("?,", len(predicate.Values)), ",")
		args := make([]any, 0, len(predicate.Values))
		for _, value := range predicate.Values {
			args = append(args, strings.TrimSpace(value.ValueNorm))
		}
		return fmt.Sprintf("n.note_path IN (%s)", holders), args, nil
	default:
		return "", nil, fmt.Errorf("unsupported notePath predicate op %q", op)
	}
}

func ontologyNodeBuiltinFieldSort(fieldName string, sortSpec codeanchor.OntologyFieldSort, orderParts *[]string) bool {
	dir := "ASC"
	if sortSpec.Desc {
		dir = "DESC"
	}
	switch fieldName {
	case "notepath", "path":
		*orderParts = append(*orderParts, "n.note_path "+dir)
	case "updatedat":
		// The updatedAt record fact: the note's indexed modification time,
		// notes without one last in either direction (notes.path is unique).
		mtime := "(SELECT nm.mtime FROM notes nm WHERE nm.path = n.note_path AND nm.indexed_at > 0 AND nm.mtime > 0)"
		*orderParts = append(*orderParts, mtime+" IS NULL", mtime+" "+dir)
	default:
		return false
	}
	return true
}

func ontologyFieldPredicateSQL(idx int, typeNames []string, predicate codeanchor.OntologyFieldPredicate) (string, []any, error) {
	fieldName := strings.ToLower(strings.TrimSpace(predicate.FieldName))
	if fieldName == "" {
		return "", nil, fmt.Errorf("ontology field predicate requires field name")
	}
	alias := fmt.Sprintf("pred_f%d", idx)
	conditions := []string{}
	args := []any{}
	if len(typeNames) == 1 {
		conditions = append(conditions, fmt.Sprintf("%s.type_name = ?", alias))
		args = append(args, typeNames[0])
	} else {
		placeholders := make([]string, len(typeNames))
		for i, name := range typeNames {
			placeholders[i] = "?"
			args = append(args, name)
		}
		conditions = append(conditions, fmt.Sprintf("%s.type_name IN (%s)", alias, strings.Join(placeholders, ",")))
	}
	conditions = append(conditions, fmt.Sprintf("%s.field_name = ?", alias))
	args = append(args, fieldName)
	op := predicate.Op
	if op == "" {
		op = codeanchor.OntologyFieldOpEq
	}
	switch op {
	case codeanchor.OntologyFieldOpExists:
		// "exists" means "field row present with non-empty normalized value".
		// The value_norm != '' filter lets SQLite use the partial index
		// idx_ontology_node_field_values_norm. Empty-value rows are deliberately
		// excluded so a Person with `assignee:` blank does not match
		// `assignee_exists: true`.
		conditions = append(conditions, fmt.Sprintf("%s.value_norm != ''", alias))
	case codeanchor.OntologyFieldOpEq:
		if len(predicate.Values) == 0 {
			return "", nil, fmt.Errorf("eq predicate for %s requires value", fieldName)
		}
		condition, valueArgs := ontologyFieldValuePredicateSQL(alias, "=", predicate.Values[0])
		conditions = append(conditions, condition)
		args = append(args, valueArgs...)
	case codeanchor.OntologyFieldOpIn:
		values := predicate.Values
		if len(values) == 0 {
			return "", nil, fmt.Errorf("in predicate for %s requires values", fieldName)
		}
		condition, valueArgs := ontologyFieldValueInPredicateSQL(alias, values)
		conditions = append(conditions, condition)
		args = append(args, valueArgs...)
	case codeanchor.OntologyFieldOpGT, codeanchor.OntologyFieldOpGTE, codeanchor.OntologyFieldOpLT, codeanchor.OntologyFieldOpLTE:
		if len(predicate.Values) == 0 {
			return "", nil, fmt.Errorf("%s predicate for %s requires value", op, fieldName)
		}
		sqlOp := map[codeanchor.OntologyFieldOperator]string{
			codeanchor.OntologyFieldOpGT:  ">",
			codeanchor.OntologyFieldOpGTE: ">=",
			codeanchor.OntologyFieldOpLT:  "<",
			codeanchor.OntologyFieldOpLTE: "<=",
		}[op]
		condition, valueArgs := ontologyFieldValuePredicateSQL(alias, sqlOp, predicate.Values[0])
		conditions = append(conditions, condition)
		args = append(args, valueArgs...)
	default:
		return "", nil, fmt.Errorf("unsupported ontology field predicate op %q", op)
	}
	return fmt.Sprintf("n.node_id IN (SELECT %s.node_id FROM ontology_node_field_values %s WHERE %s)", alias, alias, strings.Join(conditions, " AND ")), args, nil
}

func ontologyFieldValueInPredicateSQL(alias string, values []codeanchor.IntelOntologyNodeFieldValue) (string, []any) {
	parts := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for _, value := range values {
		condition, valueArgs := ontologyFieldValuePredicateSQL(alias, "=", value)
		parts = append(parts, "("+condition+")")
		args = append(args, valueArgs...)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func ontologyFieldValuePredicateSQL(alias, op string, value codeanchor.IntelOntologyNodeFieldValue) (string, []any) {
	if value.ValueBool != nil {
		return fmt.Sprintf("%s.value_bool %s ?", alias, op), []any{nullableBool(value.ValueBool)}
	}
	if value.ValueInt != nil {
		return fmt.Sprintf("%s.value_int %s ?", alias, op), []any{*value.ValueInt}
	}
	if value.ValueReal != nil {
		return fmt.Sprintf("%s.value_real %s ?", alias, op), []any{*value.ValueReal}
	}
	if value.ValueDate != nil {
		return fmt.Sprintf("%s.value_date %s ?", alias, op), []any{*value.ValueDate}
	}
	if value.ValueDateTime != nil {
		return fmt.Sprintf("%s.value_datetime %s ?", alias, op), []any{*value.ValueDateTime}
	}
	if strings.TrimSpace(value.TargetNodeID) != "" {
		// Explicit target_node_id != '' lets the partial index
		// idx_ontology_node_field_values_target serve the lookup.
		return fmt.Sprintf("%s.target_node_id != '' AND %s.target_node_id %s ?", alias, alias, op), []any{strings.TrimSpace(value.TargetNodeID)}
	}
	if strings.TrimSpace(value.TargetNotePath) != "" {
		targets := ontologyTargetNotePathVariants(value.TargetNotePath)
		norm := strings.TrimSpace(value.ValueNorm)
		var normCondition string
		var normArgs []any
		if op == "=" && norm != "" {
			normCondition = fmt.Sprintf("%s.value_norm != '' AND %s.value_norm = ?", alias, alias)
			normArgs = []any{norm}
		}
		// Explicit target_note_path != '' lets the partial index
		// idx_ontology_node_field_values_target_note serve the lookup.
		var targetCondition string
		var targetArgs []any
		if op == "=" && len(targets) > 1 {
			holders := strings.TrimSuffix(strings.Repeat("?,", len(targets)), ",")
			args := make([]any, 0, len(targets))
			for _, target := range targets {
				args = append(args, target)
			}
			targetCondition = fmt.Sprintf("%s.target_note_path != '' AND %s.target_note_path IN (%s)", alias, alias, holders)
			targetArgs = args
		} else {
			targetCondition = fmt.Sprintf("%s.target_note_path != '' AND %s.target_note_path %s ?", alias, alias, op)
			targetArgs = []any{targets[0]}
		}
		if normCondition != "" {
			args := append(targetArgs, normArgs...)
			return fmt.Sprintf("((%s) OR (%s))", targetCondition, normCondition), args
		}
		return targetCondition, targetArgs
	}
	if strings.TrimSpace(value.ValueNorm) != "" {
		return fmt.Sprintf("%s.value_norm != '' AND %s.value_norm %s ?", alias, alias, op), []any{strings.TrimSpace(value.ValueNorm)}
	}
	return fmt.Sprintf("%s.value_norm %s ?", alias, op), []any{strings.TrimSpace(value.ValueNorm)}
}

func ontologyTargetNotePathVariants(path string) []string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return []string{""}
	}
	normalized := string(paths.Normalize(trimmed))
	withSuffix := string(paths.NormalizeNote(trimmed))
	if normalized == withSuffix {
		return []string{normalized}
	}
	return []string{normalized, withSuffix}
}

func ontologyFieldValueColumn(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "bool", "boolean":
		return "value_bool"
	case "int", "integer":
		return "value_int"
	case "float", "real", "number":
		return "value_real"
	case "date":
		return "value_date"
	case "datetime":
		return "value_datetime"
	default:
		return "value_norm"
	}
}

func scanOntologyNodeFieldValueRows(rows *sql.Rows) ([]codeanchor.IntelOntologyNodeFieldValue, error) {
	var out []codeanchor.IntelOntologyNodeFieldValue
	for rows.Next() {
		var row codeanchor.IntelOntologyNodeFieldValue
		var valueBool sql.NullBool
		var valueInt sql.NullInt64
		var valueReal sql.NullFloat64
		var valueDate sql.NullString
		var valueDateTime sql.NullString
		if err := rows.Scan(
			&row.NodeID,
			&row.NotePath,
			&row.TypeName,
			&row.FieldName,
			&row.FieldKind,
			&row.SourceKind,
			&row.ValueKind,
			&row.ValueText,
			&row.ValueNorm,
			&valueBool,
			&valueInt,
			&valueReal,
			&valueDate,
			&valueDateTime,
			&row.TargetNodeID,
			&row.TargetRefJSON,
			&row.TargetNotePath,
			&row.TargetTypeName,
			&row.TargetSourceLocator,
			&row.ListOrdinal,
			&row.SchemaHash,
			&row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if valueBool.Valid {
			row.ValueBool = &valueBool.Bool
		}
		if valueInt.Valid {
			row.ValueInt = &valueInt.Int64
		}
		if valueReal.Valid {
			row.ValueReal = &valueReal.Float64
		}
		if valueDate.Valid {
			row.ValueDate = &valueDate.String
		}
		if valueDateTime.Valid {
			row.ValueDateTime = &valueDateTime.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// OntologyNodesByPaths returns ontology-node owners for the supplied note paths.
func (s *Store) OntologyNodesByPaths(ctx context.Context, notePaths []string) ([]codeanchor.IntelOntologyNode, error) {
	notePaths = normalizeNonEmptyStrings(notePaths)
	if len(notePaths) == 0 {
		return nil, nil
	}
	var out []codeanchor.IntelOntologyNode
	for batch := range slices.Chunk(notePaths, 400) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
				parent_type_name, title, source_locator, fragment, block_id, display_label,
				locator_status, start_byte, end_byte, structural_fingerprint, schema_hash, updated_at
			FROM ontology_nodes
			WHERE note_path IN (%s)
			ORDER BY note_path, node_id
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var node codeanchor.IntelOntologyNode
			if err := scanOntologyNode(rows, &node); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, node)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// OntologyNodesBySourceLocators returns ontology-node owners keyed by source locator.
func (s *Store) OntologyNodesBySourceLocators(ctx context.Context, locators []string) (map[string]codeanchor.IntelOntologyNode, error) {
	locators = normalizeNonEmptyStrings(locators)
	out := make(map[string]codeanchor.IntelOntologyNode, len(locators))
	if len(locators) == 0 {
		return out, nil
	}
	for batch := range slices.Chunk(locators, 400) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT node_id, note_path, node_ref_json, node_kind, COALESCE(type_name, ''),
				COALESCE(parent_node_id, ''), COALESCE(parent_type_name, ''), COALESCE(title, ''),
				COALESCE(source_locator, ''), COALESCE(fragment, ''), COALESCE(block_id, ''),
				COALESCE(display_label, ''), COALESCE(locator_status, ''),
				start_byte, end_byte, COALESCE(structural_fingerprint, ''), COALESCE(schema_hash, ''), updated_at
			FROM ontology_nodes
			WHERE source_locator IN (%s)
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var node codeanchor.IntelOntologyNode
			if err := scanOntologyNode(rows, &node); err != nil {
				rows.Close()
				return nil, err
			}
			out[node.SourceLocator] = node
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// OntologyNodesByNoteFragments returns ontology-node owners keyed by note_path#fragment.
func (s *Store) OntologyNodesByNoteFragments(ctx context.Context, locators []string) (map[string]codeanchor.IntelOntologyNode, error) {
	locators = normalizeNonEmptyStrings(locators)
	out := make(map[string]codeanchor.IntelOntologyNode, len(locators))
	if len(locators) == 0 {
		return out, nil
	}
	for batch := range slices.Chunk(locators, 400) {
		parts := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*2)
		for _, locator := range batch {
			notePath, fragment := splitOntologyNodeLocator(locator)
			if notePath == "" || fragment == "" {
				continue
			}
			parts = append(parts, "(note_path = ? AND (fragment = ? OR block_id = ?))")
			args = append(args, notePath, fragment, strings.TrimPrefix(fragment, "^"))
		}
		if len(parts) == 0 {
			continue
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT node_id, note_path, node_ref_json, node_kind, COALESCE(type_name, ''),
				COALESCE(parent_node_id, ''), COALESCE(parent_type_name, ''), COALESCE(title, ''),
				COALESCE(source_locator, ''), COALESCE(fragment, ''), COALESCE(block_id, ''),
				COALESCE(display_label, ''), COALESCE(locator_status, ''),
				start_byte, end_byte, COALESCE(structural_fingerprint, ''), COALESCE(schema_hash, ''), updated_at
			FROM ontology_nodes
			WHERE `+strings.Join(parts, " OR "), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var node codeanchor.IntelOntologyNode
			if err := scanOntologyNode(rows, &node); err != nil {
				rows.Close()
				return nil, err
			}
			if node.SourceLocator != "" {
				out[node.SourceLocator] = node
			}
			if node.NotePath != "" && node.Fragment != "" {
				out[node.NotePath+"#"+node.Fragment] = node
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

type ontologyNodeScanner interface {
	Scan(dest ...any) error
}

func scanOntologyNode(rows ontologyNodeScanner, node *codeanchor.IntelOntologyNode) error {
	return rows.Scan(
		&node.NodeID,
		&node.NotePath,
		&node.NodeRefJSON,
		&node.NodeKind,
		&node.TypeName,
		&node.ParentNodeID,
		&node.ParentTypeName,
		&node.Title,
		&node.SourceLocator,
		&node.Fragment,
		&node.BlockID,
		&node.DisplayLabel,
		&node.LocatorStatus,
		&node.StartByte,
		&node.EndByte,
		&node.StructuralFingerprint,
		&node.SchemaHash,
		&node.UpdatedAt,
	)
}

func splitOntologyNodeLocator(locator string) (string, string) {
	locator = strings.TrimSpace(locator)
	idx := strings.LastIndex(locator, "#")
	if idx <= 0 || idx >= len(locator)-1 {
		return "", ""
	}
	return locator[:idx], locator[idx+1:]
}

// UpsertOntologyNodeEmbeddingStates records ontology-only invalidation metadata
// beside vector rows.
func (s *Store) UpsertOntologyNodeEmbeddingStates(ctx context.Context, states []codeanchor.IntelOntologyNodeEmbeddingState) error {
	if len(states) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_ontology_node_embedding_state")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO ontology_node_embedding_state (
				chunk_id, node_id, note_path, type_name, node_kind,
				embedding_schema_signature, node_structure_fingerprint,
				source_content_hash, chunk_text_hash, chunk_granularity,
				provider, model, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(chunk_id) DO UPDATE SET
				node_id = excluded.node_id,
				note_path = excluded.note_path,
				type_name = excluded.type_name,
				node_kind = excluded.node_kind,
				embedding_schema_signature = excluded.embedding_schema_signature,
				node_structure_fingerprint = excluded.node_structure_fingerprint,
				source_content_hash = excluded.source_content_hash,
				chunk_text_hash = excluded.chunk_text_hash,
				chunk_granularity = excluded.chunk_granularity,
				provider = excluded.provider,
				model = excluded.model,
				updated_at = excluded.updated_at
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, state := range states {
			if strings.TrimSpace(state.ChunkID) == "" || strings.TrimSpace(state.NodeID) == "" || strings.TrimSpace(state.NotePath) == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx,
				state.ChunkID,
				state.NodeID,
				state.NotePath,
				state.TypeName,
				state.NodeKind,
				state.EmbeddingSchemaSignature,
				state.NodeStructureFingerprint,
				state.SourceContentHash,
				state.ChunkTextHash,
				state.ChunkGranularity,
				state.Provider,
				state.Model,
				state.UpdatedAt,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// OntologyNodeEmbeddingStatesByChunkIDs returns sidecar invalidation metadata keyed by chunk_id.
func (s *Store) OntologyNodeEmbeddingStatesByChunkIDs(ctx context.Context, chunkIDs []string) (map[string]codeanchor.IntelOntologyNodeEmbeddingState, error) {
	chunkIDs = normalizeNonEmptyStrings(chunkIDs)
	out := make(map[string]codeanchor.IntelOntologyNodeEmbeddingState, len(chunkIDs))
	if len(chunkIDs) == 0 {
		return out, nil
	}
	for batch := range slices.Chunk(chunkIDs, 400) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT chunk_id, node_id, note_path, COALESCE(type_name, ''), node_kind,
				embedding_schema_signature, node_structure_fingerprint, source_content_hash,
				chunk_text_hash, chunk_granularity, COALESCE(provider, ''), COALESCE(model, ''), updated_at
			FROM ontology_node_embedding_state
			WHERE chunk_id IN (%s)
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var state codeanchor.IntelOntologyNodeEmbeddingState
			if err := rows.Scan(
				&state.ChunkID,
				&state.NodeID,
				&state.NotePath,
				&state.TypeName,
				&state.NodeKind,
				&state.EmbeddingSchemaSignature,
				&state.NodeStructureFingerprint,
				&state.SourceContentHash,
				&state.ChunkTextHash,
				&state.ChunkGranularity,
				&state.Provider,
				&state.Model,
				&state.UpdatedAt,
			); err != nil {
				rows.Close()
				return nil, err
			}
			out[state.ChunkID] = state
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// EmbeddingSearchFilters specifies filters for embedding search.
type EmbeddingSearchFilters struct {
	PathPrefixes       []string // Filter by path prefix (directory-aware)
	Kinds              []string // Filter by anchor kind (e.g., "function", "type")
	Granularity        []string // Filter by chunk granularity (e.g., "symbol", "section")
	ExcludeGranularity []string // Exclude granularities before the nearest-neighbor limit.
	OwnerTypes         []string // Filter by owner type ("anchor", "doc_section", or "ontology_node")
	OntologyTypeNames  []string // Filter ontology_node chunks by ontology type name.
	// NoteTypes filter the resolved type of the note owning a chunk. This
	// includes doc sections and embedded ontology nodes from that note.
	NoteTypes    []string
	ExactSymbols []string
	TestsOnly    bool
	ExcludeTests bool
}

// ScoredChunk represents a chunk with its similarity score from embedding search.
type ScoredChunk struct {
	ChunkID     string
	OwnerID     string
	OwnerType   string
	Ord         int
	Path        string
	Breadcrumb  string
	Heading     string
	Granularity string
	Score       float64
}

// UpsertEmbeddings atomically upserts embeddings for the given chunk IDs.
// The embeddings map keys are chunk_id values from intel_chunks.
func (s *Store) UpsertEmbeddings(ctx context.Context, embeddings map[string]embeddings.Embedding) error {
	ctx = indexingperf.WithOp(ctx, "intel.upsert_embeddings")
	if len(embeddings) == 0 {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		const batchSize = 50
		chunkIDs := make([]string, 0, len(embeddings))
		for chunkID := range embeddings {
			chunkIDs = append(chunkIDs, chunkID)
		}
		sort.Strings(chunkIDs)
		dimSet := make(map[int]struct{})
		for _, chunkID := range chunkIDs {
			dims := len(embeddings[chunkID])
			if dims <= 0 {
				continue
			}
			dimSet[dims] = struct{}{}
		}
		dimsList := make([]int, 0, len(dimSet))
		for dims := range dimSet {
			dimsList = append(dimsList, dims)
		}
		sort.Ints(dimsList)
		for _, dims := range dimsList {
			if err := createIntelVecTable(ctx, tx, dims); err != nil {
				return err
			}
		}

		for batch := range slices.Chunk(chunkIDs, batchSize) {
			chunkRowIDs, err := lookupIntelChunkRowIDsTx(ctx, tx, batch)
			if err != nil {
				return fmt.Errorf("lookup intel chunk row ids: %w", err)
			}
			type embedRow struct {
				chunkID    string
				chunkRowID int64
				blob       []byte
				norm       float64
				dims       int
			}
			byDims := make(map[int][]embedRow)
			for _, chunkID := range batch {
				emb := embeddings[chunkID]
				if len(emb) == 0 {
					continue
				}
				chunkRowID, exists := chunkRowIDs[chunkID]
				if !exists {
					continue
				}
				dims := len(emb)
				byDims[dims] = append(byDims[dims], embedRow{
					chunkID:    chunkID,
					chunkRowID: chunkRowID,
					blob:       embedToBytes(emb),
					norm:       math.Sqrt(dotFloat64(emb, emb)),
					dims:       dims,
				})
			}

			for dims, rows := range byDims {
				metaValueSQL := make([]string, 0, len(rows))
				metaArgs := make([]any, 0, len(rows)*5)
				vecValueSQL := make([]string, 0, len(rows))
				vecArgs := make([]any, 0, len(rows)*2)
				vecIDValueSQL := make([]string, 0, len(rows))
				vecIDArgs := make([]any, 0, len(rows))
				for _, row := range rows {
					metaValueSQL = append(metaValueSQL, "(?, ?, ?, ?, ?)")
					metaArgs = append(metaArgs, row.chunkRowID, row.chunkID, row.norm, row.dims, now)
					vecValueSQL = append(vecValueSQL, "(?, ?)")
					vecArgs = append(vecArgs, row.chunkRowID, row.blob)
					vecIDValueSQL = append(vecIDValueSQL, "(?)")
					vecIDArgs = append(vecIDArgs, row.chunkRowID)
				}
				if len(metaValueSQL) == 0 {
					continue
				}

				metaStmt := fmt.Sprintf(`
					WITH input(chunk_row_id, chunk_id, norm, dimensions, created_at) AS (VALUES %s)
					INSERT INTO intel_embeddings (chunk_row_id, chunk_id, norm, dimensions, created_at)
					SELECT i.chunk_row_id, i.chunk_id, i.norm, i.dimensions, i.created_at
					FROM input i
					JOIN intel_chunks c ON c.id = i.chunk_row_id
					ON CONFLICT(chunk_row_id) DO UPDATE SET
						chunk_id = excluded.chunk_id,
						norm = excluded.norm,
						dimensions = excluded.dimensions,
						created_at = excluded.created_at
				`, strings.Join(metaValueSQL, ","))
				if _, err := tx.ExecContext(ctx, metaStmt, metaArgs...); err != nil {
					return fmt.Errorf("upsert intel embedding meta dims=%d rows=%d vars=%d: %w", dims, len(rows), len(metaArgs), err)
				}

				vecDeleteStmt := fmt.Sprintf(`
					WITH input(chunk_row_id) AS (VALUES %s)
					DELETE FROM %s
					WHERE chunk_id IN (
						SELECT i.chunk_row_id
						FROM input i
						JOIN intel_chunks c ON c.id = i.chunk_row_id
					)
				`, strings.Join(vecIDValueSQL, ","), intelVecTableName(dims))
				if _, err := tx.ExecContext(ctx, vecDeleteStmt, vecIDArgs...); err != nil {
					return fmt.Errorf("delete intel vec dims=%d rows=%d vars=%d: %w", dims, len(rows), len(vecIDArgs), err)
				}

				vecStmt := fmt.Sprintf(`
					WITH input(chunk_row_id, embedding) AS (VALUES %s)
					INSERT INTO %s(chunk_id, owner_type, embedding)
					SELECT i.chunk_row_id, c.owner_type, i.embedding
					FROM input i
					JOIN intel_chunks c ON c.id = i.chunk_row_id
				`, strings.Join(vecValueSQL, ","), intelVecTableName(dims))
				if _, err := tx.ExecContext(ctx, vecStmt, vecArgs...); err != nil {
					return fmt.Errorf("insert intel vec dims=%d rows=%d vars=%d: %w", dims, len(rows), len(vecArgs), err)
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO index_metadata(key, value) VALUES (?, '1')
			ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1
		`, searchEmbeddingGenerationKey); err != nil {
			return fmt.Errorf("advance search embedding generation: %w", err)
		}
		return nil
	})
}

// searchEmbeddingsScalar is the exact fallback for requests outside sqlite-vec's
// bounded KNN contract and unresolved equal-score boundaries.
func (s *Store) searchEmbeddingsScalar(ctx context.Context, query embeddings.Embedding, k int, filters EmbeddingSearchFilters) ([]ScoredChunk, int, error) {
	if len(query) == 0 {
		return nil, 0, errors.New("query embedding is empty")
	}
	if k <= 0 {
		k = 25
	}

	if math.Sqrt(dotFloat64(query, query)) == 0 {
		return nil, 0, errors.New("query embedding has zero norm")
	}
	queryDims := len(query)
	if err := s.requireIntelVecPrimary(ctx); err != nil {
		return nil, 0, err
	}
	if err := s.ensureIntelVecMirror(ctx, queryDims); err != nil {
		return nil, 0, err
	}
	queryBlob := embedToBytes(query)
	vecTable := intelVecTableName(queryDims)

	filterSQL, filterArgs := buildEmbeddingFilterSQL(filters, "c", "a", "s", "n")
	args := append([]any{queryBlob, queryDims}, filterArgs...)

	// Join vec-scored chunk ids with intel_chunks and owner tables for path/kind.
	baseQuery := `
		SELECT c.chunk_id, c.owner_id, c.owner_type, c.ord, c.granularity, c.breadcrumb, c.heading,
		       COALESCE(a.path, s.path, n.note_path, '') as path,
		       1.0 - vec_distance_cosine(v.embedding, ?) AS score
		FROM ` + vecTable + ` v
		JOIN intel_chunks c ON c.id = v.chunk_id
		JOIN (` + currentEmbeddingRowIDsSQL + `) e ON e.chunk_row_id = c.id
		LEFT JOIN intel_code_anchors a ON a.id = c.owner_row_id AND c.owner_type = 'anchor'
		LEFT JOIN intel_doc_sections s ON s.id = c.owner_row_id AND c.owner_type = 'doc_section'
		LEFT JOIN ontology_nodes n ON n.node_id = c.owner_id AND c.owner_type = 'ontology_node'
		WHERE 1 = 1
	`
	baseQuery += filterSQL
	baseQuery += " ORDER BY score DESC, c.chunk_id ASC"
	if k > 0 {
		baseQuery += " LIMIT ?"
		args = append(args, k)
	}

	rows, err := s.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]ScoredChunk, 0, k)
	for rows.Next() {
		var sc ScoredChunk
		if err := rows.Scan(&sc.ChunkID, &sc.OwnerID, &sc.OwnerType, &sc.Ord, &sc.Granularity, &sc.Breadcrumb, &sc.Heading, &sc.Path, &sc.Score); err != nil {
			return nil, 0, err
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) > k {
		out = out[:k]
	}
	return out, 0, nil
}

func embedToBytes(emb embeddings.Embedding) []byte {
	if len(emb) == 0 {
		return nil
	}
	b := make([]byte, 4*len(emb))
	for i, f := range emb {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func bytesToEmbed(b []byte) embeddings.Embedding {
	if len(b)%4 != 0 {
		return nil
	}
	out := make(embeddings.Embedding, len(b)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		out[i] = math.Float32frombits(bits)
	}
	return out
}

func dotFloat64(a, b embeddings.Embedding) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

// AnchorIDByLabel returns anchor ID for a label.
func (s *Store) AnchorIDByLabel(ctx context.Context, label string) (int64, bool) {
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM anchors WHERE label = ?`, label).Scan(&id); err != nil {
		return 0, false
	}
	return id, true
}

// AnchorsMatchingSymbols finds anchors whose base matches provided symbols (lang+pkg+name).
func (s *Store) AnchorsMatchingSymbols(ctx context.Context, names []string, pkgs []string, lang codeanchor.Lang) ([]int64, error) {
	if len(names) == 0 || len(pkgs) == 0 {
		return nil, nil
	}
	limit := len(names)
	if len(pkgs) < limit {
		limit = len(pkgs)
	}

	seenPairs := make(map[string]bool)
	values := make([]string, 0, limit)
	args := make([]any, 0, 1+limit*2)
	args = append(args, lang)

	for i := 0; i < limit; i++ {
		if names[i] == "" {
			continue
		}
		key := pkgs[i] + "\x00" + names[i]
		if seenPairs[key] {
			continue
		}
		seenPairs[key] = true
		values = append(values, "(?, ?)")
		args = append(args, pkgs[i], names[i])
	}

	if len(values) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT id FROM anchors
		WHERE base_lang = ? AND (base_pkg, base_name) IN (%s)
	`, strings.Join(values, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SymbolsBySuffix finds symbols whose FQN ends with the given suffix.
// Uses the fqn_reversed index for efficient prefix matching on the reversed string.
// Results are ordered by FQN length ascending (shortest = most specific match first).
func (s *Store) SymbolsBySuffix(ctx context.Context, suffix string, lang codeanchor.Lang, limit int) ([]codeanchor.Symbol, error) {
	if suffix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}

	reversed := codeanchor.ReverseString(suffix)

	query := `
		SELECT id, lang, kind, file, pkg, name, fqn
		FROM symbols
		WHERE fqn_reversed LIKE ? || '%'
	`
	args := []any{reversed}

	if lang != "" {
		query += ` AND lang = ?`
		args = append(args, lang)
	}

	query += ` ORDER BY LENGTH(fqn) ASC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var symbols []codeanchor.Symbol
	for rows.Next() {
		var sym codeanchor.Symbol
		var id int64
		var kind string
		if err := rows.Scan(&id, &sym.Lang, &kind, &sym.File, &sym.Pkg, &sym.Name, &sym.FQN); err != nil {
			return nil, err
		}
		sym.Kind = codeanchor.SymbolKind(kind)
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// SymbolExistsByFQN checks if a symbol with the exact FQN exists.
func (s *Store) SymbolExistsByFQN(ctx context.Context, fqn string, lang codeanchor.Lang) (bool, error) {
	if fqn == "" {
		return false, nil
	}
	query := `SELECT 1 FROM symbols WHERE fqn = ?`
	args := []any{fqn}
	if lang != "" {
		query += ` AND lang = ?`
		args = append(args, lang)
	}
	query += ` LIMIT 1`

	var exists int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// AnchorsMatchingAnnotations finds annotation anchors by annotation type (lang+pkg+name).
func (s *Store) AnchorsMatchingAnnotations(ctx context.Context, annTypes []codeanchor.SymbolRef) ([]int64, error) {
	if len(annTypes) == 0 {
		return nil, nil
	}
	seen := make(map[int64]bool)
	var ids []int64
	for _, ref := range annTypes {
		// Match by lang+pkg+name for precision; also match where anchor has empty pkg (wildcard)
		rows, err := s.db.QueryContext(ctx, `
			SELECT id FROM anchors
			WHERE ann_lang = ? AND ann_name = ? AND (ann_pkg = ? OR ann_pkg = '')
		`, ref.Lang, ref.Name, ref.Pkg)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		rows.Close()
	}
	return ids, nil
}

// DeleteFile removes all data associated with a code file.
func (s *Store) DeleteFile(ctx context.Context, path string) error {
	path = normalizeCodeLookupPath(path)
	ctx = indexingperf.WithOp(ctx, "intel.delete_file")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM super_edges WHERE child_fqn IN (SELECT fqn FROM symbols WHERE file = ?) OR parent_fqn IN (SELECT fqn FROM symbols WHERE file = ?)`, path, path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM annotations WHERE owner_fqn IN (SELECT fqn FROM symbols WHERE file = ?)`, path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM anchor_scopes
			WHERE symbol_fqn IN (SELECT fqn FROM symbols WHERE file = ?)
				OR call_file = ?
		`, path, path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM symbols WHERE file = ?`, path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path = ?`, path); err != nil {
			return err
		}
		if err := s.deleteIntelCodeByPathTx(ctx, tx, path); err != nil {
			return err
		}
		if err := s.deleteRationaleByPathTx(ctx, tx, path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM doc_links WHERE src_path = ?`, path); err != nil {
			return err
		}
		return deleteIntelReverseIndexByPathsTx(ctx, tx, `SELECT ?`, path)
	})
}

// DeleteNote removes a note and its anchor associations.
// Returns the IDs of anchors that were defined by this note (for cache invalidation).
func (s *Store) DeleteNote(ctx context.Context, path string) ([]int64, error) {
	var affectedAnchors []int64
	ctx = indexingperf.WithOp(ctx, "intel.delete_note")
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		// Get note ID
		var noteID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM notes WHERE path = ?`, path).Scan(&noteID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // Note doesn't exist, nothing to delete
			}
			return err
		}

		// Get anchors linked to this note (for dirty marking)
		rows, err := tx.QueryContext(ctx, `SELECT anchor_id FROM note_anchors WHERE note_id = ?`, noteID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var anchorID int64
			if err := rows.Scan(&anchorID); err != nil {
				rows.Close()
				return err
			}
			affectedAnchors = append(affectedAnchors, anchorID)
		}
		rows.Close()

		// Delete note_anchors links
		if _, err := tx.ExecContext(ctx, `DELETE FROM note_anchors WHERE note_id = ?`, noteID); err != nil {
			return err
		}

		// Delete the note
		if _, err := tx.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, noteID); err != nil {
			return err
		}

		return nil
	})
	return affectedAnchors, err
}

// DeleteAnchorsNotInLabels removes anchors whose labels are not in the provided set.
// Used to clean up anchors that were removed from a note's frontmatter.
func (s *Store) DeleteAnchorsNotInLabels(ctx context.Context, noteID int64, keepLabels []string) ([]int64, error) {
	var deletedIDs []int64
	ctx = indexingperf.WithOp(ctx, "intel.delete_anchors_not_in_labels")
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return s.deleteAnchorsNotInLabelsTx(ctx, tx, noteID, keepLabels, &deletedIDs)
	})
	return deletedIDs, err
}

func (s *Store) deleteAnchorsNotInLabelsTx(ctx context.Context, tx *sql.Tx, noteID int64, keepLabels []string, deletedIDs *[]int64) error {
	// Find anchors defined by this note that are no longer in keepLabels
	// We identify "defined by this note" as anchors linked to this note
	// that aren't referenced by any other note
	rows, err := tx.QueryContext(ctx, `
		SELECT a.id, a.label FROM anchors a
		JOIN note_anchors na ON na.anchor_id = a.id
		WHERE na.note_id = ?
	`, noteID)
	if err != nil {
		return err
	}

	keepSet := make(map[string]bool)
	for _, l := range keepLabels {
		keepSet[l] = true
	}

	var toCheck []struct {
		id    int64
		label string
	}
	for rows.Next() {
		var id int64
		var label string
		if err := rows.Scan(&id, &label); err != nil {
			rows.Close()
			return err
		}
		if !keepSet[label] {
			toCheck = append(toCheck, struct {
				id    int64
				label string
			}{id, label})
		}
	}
	rows.Close()

	// For each anchor not in keepLabels, check if it's only linked to this note
	for _, anchor := range toCheck {
		var count int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM note_anchors WHERE anchor_id = ? AND note_id != ?
		`, anchor.id, noteID).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			// This anchor is only linked to this note; delete it
			if _, err := tx.ExecContext(ctx, `DELETE FROM anchor_scopes WHERE anchor_id = ?`, anchor.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM note_anchors WHERE anchor_id = ?`, anchor.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM anchors WHERE id = ?`, anchor.id); err != nil {
				return err
			}
			*deletedIDs = append(*deletedIDs, anchor.id)
		}
	}
	return nil
}

// GarbageCollectOrphanedAnchors removes anchors with no note links.
func (s *Store) GarbageCollectOrphanedAnchors(ctx context.Context) (int, error) {
	var count int
	ctx = indexingperf.WithOp(ctx, "intel.gc_orphaned_anchors")
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		// Find orphaned anchors
		rows, err := tx.QueryContext(ctx, `
			SELECT a.id FROM anchors a
			LEFT JOIN note_anchors na ON na.anchor_id = a.id
			WHERE na.anchor_id IS NULL
		`)
		if err != nil {
			return err
		}
		var orphanIDs []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			orphanIDs = append(orphanIDs, id)
		}
		rows.Close()

		for _, id := range orphanIDs {
			if _, err := tx.ExecContext(ctx, `DELETE FROM anchor_scopes WHERE anchor_id = ?`, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM anchors WHERE id = ?`, id); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

// NoteIDByPath returns the note ID for a path.
func (s *Store) NoteIDByPath(ctx context.Context, path string) (int64, bool) {
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM notes WHERE path = ?`, path).Scan(&id); err != nil {
		return 0, false
	}
	return id, true
}

func placeholders(in []string) (string, []any) {
	args := make([]any, len(in))
	for i, v := range in {
		args[i] = v
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(in)), ","), args
}

// MetadataKey constants for index metadata.
const (
	MetaKeyIndexerVersion  = "indexer_version"
	MetaKeyConfigHash      = "config_hash"
	MetaKeyModelHash       = "model_hash"
	MetaKeyAlgoVersion     = "algo_version"
	MetaKeyScopeConfigHash = "scope_config_hash"
	MetaKeyReverseIndexVer = "reverse_index_version"
	MetaKeyReverseIdxReady = "reverse_index_backfill_complete"
)

// GetMetadata retrieves a metadata value by key.
func (s *Store) GetMetadata(ctx context.Context, key string) (string, bool, error) {
	var value sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT value FROM index_metadata WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return value.String, value.Valid, nil
}

// SetMetadata stores a metadata key-value pair.
func (s *Store) SetMetadata(ctx context.Context, key, value string) error {
	ctx = indexingperf.WithOp(ctx, "intel.set_metadata")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO index_metadata(key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, key, value)
		return err
	})
}

// IndexerVersion returns the stored indexer version, if any.
func (s *Store) IndexerVersion(ctx context.Context) (string, bool, error) {
	return s.GetMetadata(ctx, MetaKeyIndexerVersion)
}

// SetIndexerVersion stores the indexer version.
func (s *Store) SetIndexerVersion(ctx context.Context, version string) error {
	return s.SetMetadata(ctx, MetaKeyIndexerVersion, version)
}

// GetScopeConfigHash returns the stored scope config hash, if any.
func (s *Store) GetScopeConfigHash(ctx context.Context) (string, bool, error) {
	return s.GetMetadata(ctx, MetaKeyScopeConfigHash)
}

// SetScopeConfigHash stores the scope config hash.
func (s *Store) SetScopeConfigHash(ctx context.Context, hash string) error {
	return s.SetMetadata(ctx, MetaKeyScopeConfigHash, hash)
}

// ReverseIndexVersion returns the stored reverse-index version, if any.
func (s *Store) ReverseIndexVersion(ctx context.Context) (string, bool, error) {
	return s.GetMetadata(ctx, MetaKeyReverseIndexVer)
}

// SetReverseIndexVersion stores the reverse-index version.
func (s *Store) SetReverseIndexVersion(ctx context.Context, version string) error {
	return s.SetMetadata(ctx, MetaKeyReverseIndexVer, version)
}

// ReverseIndexBackfillComplete returns whether backfill is marked complete.
func (s *Store) ReverseIndexBackfillComplete(ctx context.Context) (bool, bool, error) {
	value, ok, err := s.GetMetadata(ctx, MetaKeyReverseIdxReady)
	if err != nil || !ok {
		return false, ok, err
	}
	return strings.EqualFold(strings.TrimSpace(value), "true"), true, nil
}

// SetReverseIndexBackfillComplete stores the backfill completeness flag.
func (s *Store) SetReverseIndexBackfillComplete(ctx context.Context, complete bool) error {
	value := "false"
	if complete {
		value = "true"
	}
	return s.SetMetadata(ctx, MetaKeyReverseIdxReady, value)
}

// UpsertNoteMeta updates stored note hash/version/mtime for change detection.
func (s *Store) UpsertNoteMeta(ctx context.Context, path, contentHash, indexerVersion string, mtime int64) error {
	if s == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_note_meta")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE notes
			SET content_hash = ?, indexer_version = ?, mtime = ?
			WHERE path = ?
		`, contentHash, indexerVersion, mtime, path)
		return err
	})
}

// TouchNoteMtimes refreshes stored mtimes for the given note paths.
func (s *Store) TouchNoteMtimes(ctx context.Context, updates map[string]int64) error {
	if s == nil || len(updates) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.touch_note_mtimes")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `UPDATE notes SET mtime = ? WHERE path = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for path, mtime := range updates {
			if strings.TrimSpace(path) == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, mtime, path); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetPackMetadata retrieves all pack-related metadata.
func (s *Store) GetPackMetadata(ctx context.Context) (codeanchor.PackMetadata, error) {
	var pm codeanchor.PackMetadata
	pm.ConfigHash, _, _ = s.GetMetadata(ctx, MetaKeyConfigHash)
	pm.ModelHash, _, _ = s.GetMetadata(ctx, MetaKeyModelHash)
	pm.AlgoVersion, _, _ = s.GetMetadata(ctx, MetaKeyAlgoVersion)
	pm.IndexerVersion, _, _ = s.GetMetadata(ctx, MetaKeyIndexerVersion)
	return pm, nil
}

// SetPackMetadata stores pack-related metadata. Empty fields are skipped.
func (s *Store) SetPackMetadata(ctx context.Context, pm codeanchor.PackMetadata) error {
	ctx = indexingperf.WithOp(ctx, "intel.set_pack_metadata")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		set := func(key, val string) error {
			if val == "" {
				return nil
			}
			_, err := db.ExecContext(ctx, `
				INSERT INTO index_metadata(key, value) VALUES (?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value
			`, key, val)
			return err
		}
		if err := set(MetaKeyConfigHash, pm.ConfigHash); err != nil {
			return err
		}
		if err := set(MetaKeyModelHash, pm.ModelHash); err != nil {
			return err
		}
		if err := set(MetaKeyAlgoVersion, pm.AlgoVersion); err != nil {
			return err
		}
		if err := set(MetaKeyIndexerVersion, pm.IndexerVersion); err != nil {
			return err
		}
		return nil
	})
}

// Internal helpers for intel persistence.
func (s *Store) deleteIntelCodeByPathTx(ctx context.Context, tx *sql.Tx, path string) error {
	anchorIDs, err := s.selectIntelAnchorIDsByPathTx(ctx, tx, path)
	if err != nil {
		return err
	}
	anchorRowIDs, err := s.selectIntelAnchorRowIDsByPathTx(ctx, tx, path)
	if err != nil {
		return err
	}
	if len(anchorIDs) > 0 {
		if err := s.deleteIntelChunksByOwnerRowIDsTx(ctx, tx, "anchor", anchorRowIDs); err != nil {
			return err
		}
		if err := s.deleteIntelEdgesByRowIDsTx(ctx, tx, "anchor", anchorRowIDs); err != nil {
			return err
		}
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, "anchor", anchorIDs); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intel_code_anchors WHERE path = ?`, path); err != nil {
		return err
	}
	return nil
}

func (s *Store) deleteIntelDocSectionsByPathTx(ctx context.Context, tx *sql.Tx, path string) error {
	sectionIDs, err := s.selectIntelSectionIDsByPathTx(ctx, tx, path)
	if err != nil {
		return err
	}
	sectionRowIDs, err := s.selectIntelSectionRowIDsByPathTx(ctx, tx, path)
	if err != nil {
		return err
	}
	if len(sectionIDs) > 0 {
		if err := s.deleteIntelChunksByOwnerRowIDsTx(ctx, tx, "doc_section", sectionRowIDs); err != nil {
			return err
		}
		if err := s.deleteIntelEdgesByRowIDsTx(ctx, tx, "doc_section", sectionRowIDs); err != nil {
			return err
		}
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, "doc_section", sectionIDs); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intel_doc_sections WHERE path = ?`, path); err != nil {
		return err
	}
	return nil
}

func (s *Store) deleteIntelChunksByOwnerRowIDsTx(ctx context.Context, tx *sql.Tx, ownerType string, ownerRowIDs []int64) error {
	if len(ownerRowIDs) == 0 {
		return nil
	}
	for batch := range slices.Chunk(ownerRowIDs, 400) {
		holders := make([]string, len(batch))
		args := make([]any, 0, len(batch)+1)
		args = append(args, ownerType)
		for i, id := range batch {
			holders[i] = "?"
			args = append(args, id)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM intel_chunks WHERE owner_type = ? AND owner_row_id IN (%s)`,
			strings.Join(holders, ","),
		), args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) deleteIntelEdgesByRowIDsTx(ctx context.Context, tx *sql.Tx, nodeType string, nodeRowIDs []int64) error {
	if len(nodeRowIDs) == 0 {
		return nil
	}

	if len(nodeRowIDs) <= 900 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(nodeRowIDs)), ",")
		args := make([]any, 0, len(nodeRowIDs)*2+2)
		args = append(args, nodeType)
		for _, id := range nodeRowIDs {
			args = append(args, id)
		}
		args = append(args, nodeType)
		for _, id := range nodeRowIDs {
			args = append(args, id)
		}
		stmt := fmt.Sprintf(`DELETE FROM intel_edges WHERE (src_type = ? AND src_row_id IN (%s)) OR (dst_type = ? AND dst_row_id IN (%s))`, placeholders, placeholders)
		_, err := tx.ExecContext(ctx, stmt, args...)
		return err
	}

	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_intel_edge_ids`)
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_delete_intel_edge_ids (id INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_delete_intel_edge_ids(id) VALUES (?)`)
	if err != nil {
		return err
	}
	for _, id := range nodeRowIDs {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()

	_, err = tx.ExecContext(ctx, `
		DELETE FROM intel_edges
		WHERE (src_type = ? AND src_row_id IN (SELECT id FROM temp_delete_intel_edge_ids))
		   OR (dst_type = ? AND dst_row_id IN (SELECT id FROM temp_delete_intel_edge_ids))
	`, nodeType, nodeType)
	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_intel_edge_ids`)
	return err
}

func (s *Store) deleteIntelFTSByIDsTx(ctx context.Context, tx *sql.Tx, itemType string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// Avoid SQLite bound-parameter limits for large batch deletes.
	if len(ids) <= 900 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, 0, len(ids)+1)
		args = append(args, itemType)
		for _, id := range ids {
			args = append(args, id)
		}

		stmt := fmt.Sprintf(`SELECT fts_rowid FROM intel_fts_rowid WHERE item_type = ? AND item_id IN (%s)`, placeholders)
		rows, err := tx.QueryContext(ctx, stmt, args...)
		if err != nil {
			return err
		}
		var rowIDs []int64
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
			delPH := strings.TrimRight(strings.Repeat("?,", len(rowIDs)), ",")
			delArgs := make([]any, len(rowIDs))
			for i, id := range rowIDs {
				delArgs[i] = id
			}
			delStmt := fmt.Sprintf(`DELETE FROM intel_fts WHERE rowid IN (%s)`, delPH)
			if _, err := tx.ExecContext(ctx, delStmt, delArgs...); err != nil {
				return err
			}
		}
		delMap := fmt.Sprintf(`DELETE FROM intel_fts_rowid WHERE item_type = ? AND item_id IN (%s)`, placeholders)
		if _, err := tx.ExecContext(ctx, delMap, args...); err != nil {
			return err
		}
		return nil
	}

	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_intel_fts_ids`)
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_delete_intel_fts_ids (id TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_delete_intel_fts_ids(id) VALUES (?)`)
	if err != nil {
		return err
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM intel_fts
		WHERE rowid IN (
			SELECT r.fts_rowid
			FROM intel_fts_rowid r
			JOIN temp_delete_intel_fts_ids d ON d.id = r.item_id
			WHERE r.item_type = ?
		)
	`, itemType); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM intel_fts_rowid
		WHERE item_type = ?
		  AND item_id IN (SELECT id FROM temp_delete_intel_fts_ids)
	`, itemType); err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_intel_fts_ids`)
	return nil
}

func (s *Store) upsertIntelFTSRowsTx(ctx context.Context, tx *sql.Tx, rows []codeanchor.IntelFTSRow) error {
	if len(rows) == 0 {
		return nil
	}

	idsByType := make(map[string][]string)
	for _, r := range rows {
		if r.ItemType == "" || r.ItemID == "" {
			continue
		}
		idsByType[r.ItemType] = append(idsByType[r.ItemType], r.ItemID)
	}
	ftsDeleteStarted := time.Now()
	for itemType, ids := range idsByType {
		if err := s.deleteIntelFTSByIDsTx(ctx, tx, itemType, ids); err != nil {
			return err
		}
	}
	indexingperf.ObserveLatency(ctx, "codepersist.intel.fts_delete", time.Since(ftsDeleteStarted))
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_intel_fts_rows`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE temp_intel_fts_rows (
			item_type TEXT NOT NULL,
			item_id TEXT NOT NULL,
			path TEXT,
			title TEXT,
			body TEXT,
			PRIMARY KEY (item_type, item_id)
		)
	`); err != nil {
		return err
	}
	defer func() {
		_, _ = tx.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp_intel_fts_rows`)
	}()
	tempStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO temp_intel_fts_rows (item_type, item_id, path, title, body)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer tempStmt.Close()
	for _, r := range rows {
		if r.ItemType == "" || r.ItemID == "" {
			continue
		}
		if _, err := tempStmt.ExecContext(ctx, r.ItemType, r.ItemID, r.Path, r.Title, r.Body); err != nil {
			return err
		}
	}
	ftsInsertStarted := time.Now()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO intel_fts (item_type, item_id, path, title, body)
		SELECT item_type, item_id, path, title, body
		FROM temp_intel_fts_rows
	`); err != nil {
		return err
	}
	indexingperf.ObserveLatency(ctx, "codepersist.intel.fts_insert", time.Since(ftsInsertStarted))
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO intel_fts_rowid (item_type, item_id, fts_rowid)
		SELECT t.item_type, t.item_id, f.rowid
		FROM temp_intel_fts_rows t
		JOIN intel_fts f
		  ON f.item_type = t.item_type
		 AND f.item_id = t.item_id
	`); err != nil {
		return err
	}
	return nil
}

func (s *Store) selectIntelAnchorIDsByPathTx(ctx context.Context, tx *sql.Tx, path string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT anchor_id FROM intel_code_anchors WHERE path = ?`, path)
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

func (s *Store) selectIntelAnchorRowIDsByPathTx(ctx context.Context, tx *sql.Tx, path string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM intel_code_anchors WHERE path = ?`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func lookupIntelAnchorRowIDTx(ctx context.Context, tx *sql.Tx, anchorID string) (int64, error) {
	var rowID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM intel_code_anchors WHERE anchor_id = ?`, anchorID).Scan(&rowID)
	return rowID, err
}

func selectIntelAnchorRowIDsByAnchorIDsTx(ctx context.Context, tx *sql.Tx, anchorIDs []string) (map[string]int64, error) {
	anchorIDs = dedupeNonEmptyStrings(anchorIDs)
	if len(anchorIDs) == 0 {
		return map[string]int64{}, nil
	}
	out := make(map[string]int64, len(anchorIDs))
	const batchSize = 400
	for batch := range slices.Chunk(anchorIDs, batchSize) {
		holders, args := placeholders(batch)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(
			`SELECT anchor_id, id FROM intel_code_anchors WHERE anchor_id IN (%s)`,
			holders,
		), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID string
			var rowID int64
			if err := rows.Scan(&anchorID, &rowID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[anchorID] = rowID
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

func lookupIntelSectionRowIDTx(ctx context.Context, tx *sql.Tx, sectionID string) (int64, error) {
	var rowID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM intel_doc_sections WHERE section_id = ?`, sectionID).Scan(&rowID)
	return rowID, err
}

func lookupOntologyNodeRowIDTx(ctx context.Context, tx *sql.Tx, nodeID string) (int64, error) {
	var rowID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM ontology_nodes WHERE node_id = ?`, nodeID).Scan(&rowID)
	return rowID, err
}

type intelOwnerRef struct {
	rowID     int64
	ownerType string
}

func lookupIntelOwnerRefTx(ctx context.Context, tx *sql.Tx, ownerID string) (intelOwnerRef, error) {
	if rowID, err := lookupIntelAnchorRowIDTx(ctx, tx, ownerID); err == nil {
		return intelOwnerRef{rowID: rowID, ownerType: "anchor"}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return intelOwnerRef{}, err
	}
	if rowID, err := lookupIntelSectionRowIDTx(ctx, tx, ownerID); err == nil {
		return intelOwnerRef{rowID: rowID, ownerType: "doc_section"}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return intelOwnerRef{}, err
	}
	rowID, err := lookupOntologyNodeRowIDTx(ctx, tx, ownerID)
	if err != nil {
		return intelOwnerRef{}, err
	}
	return intelOwnerRef{rowID: rowID, ownerType: "ontology_node"}, nil
}

// Resolve one bounded embedding publication batch inside its write transaction.
func lookupIntelChunkRowIDsTx(ctx context.Context, tx *sql.Tx, chunkIDs []string) (map[string]int64, error) {
	holders, args := placeholders(chunkIDs)
	rows, err := tx.QueryContext(ctx, `SELECT chunk_id, id FROM intel_chunks WHERE chunk_id IN (`+holders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rowIDs := make(map[string]int64, len(chunkIDs))
	for rows.Next() {
		var chunkID string
		var rowID int64
		if err := rows.Scan(&chunkID, &rowID); err != nil {
			return nil, err
		}
		rowIDs[chunkID] = rowID
	}
	return rowIDs, rows.Err()
}

func normalizeIntelChunkFamily(family string) string {
	family = strings.TrimSpace(family)
	if family == "" {
		return codeanchor.IntelChunkFamilyDefault
	}
	return family
}

func dedupeNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func dedupeStringsStable(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func dedupeIntelChunksStable(chunks []codeanchor.IntelChunk) []codeanchor.IntelChunk {
	if len(chunks) == 0 {
		return nil
	}
	indexByKey := make(map[string]int, len(chunks))
	out := make([]codeanchor.IntelChunk, 0, len(chunks))
	for _, chunk := range chunks {
		key := strings.Join([]string{
			string(chunk.OwnerType),
			chunk.OwnerID,
			strconv.Itoa(chunk.Ord),
			chunk.Granularity,
		}, "\x00")
		if idx, ok := indexByKey[key]; ok {
			out[idx] = chunk
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, chunk)
	}
	return out
}

func dedupeSymbolRefs(refs []codeanchor.SymbolRef) []codeanchor.SymbolRef {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	out := make([]codeanchor.SymbolRef, 0, len(refs))
	for _, ref := range refs {
		key := symbolRefKey(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func (s *Store) selectIntelSectionIDsByPathTx(ctx context.Context, tx *sql.Tx, path string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT section_id FROM intel_doc_sections WHERE path = ?`, path)
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

func (s *Store) selectIntelSectionRowIDsByPathTx(ctx context.Context, tx *sql.Tx, path string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM intel_doc_sections WHERE path = ?`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

var _ codeanchor.Store = (*Store)(nil)
