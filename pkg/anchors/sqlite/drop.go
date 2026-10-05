package sqlite

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// DropSchema drops all code-index (codeanchors + intel) tables from the SQLite database at path.
//
// This is used when semantic and code indexes share a single unified DB file and the user
// requests a code-only rebuild.
func DropSchema(path string) error {
	if path == "" {
		return fmt.Errorf("empty sqlite path")
	}
	db, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{})
	if err != nil {
		return err
	}
	defer db.Close()

	_, _ = db.Exec(`PRAGMA foreign_keys = OFF`)

	for _, stmt := range []string{
		`DROP TABLE IF EXISTS graph_doc_scores;`,
		`DROP TABLE IF EXISTS graph_doc_edges;`,
		`DROP TABLE IF EXISTS graph_anchor_scores;`,

		// Intel tables
		`DROP TABLE IF EXISTS intel_rationale_fts;`,
		`DROP TABLE IF EXISTS intel_rationale_fts_rowid;`,
		`DROP TABLE IF EXISTS intel_rationale;`,
		`DROP TABLE IF EXISTS intel_fts;`,
		`DROP TABLE IF EXISTS intel_fts_rowid;`,
		`DROP TABLE IF EXISTS intel_edges;`,
		`DROP TABLE IF EXISTS intel_doc_sections;`,
		`DROP TABLE IF EXISTS intel_code_anchors;`,

		// Codeanchor tables
		`DROP TABLE IF EXISTS anchor_scopes;`,
		`DROP TABLE IF EXISTS file_symbols;`,
		`DROP TABLE IF EXISTS note_anchors;`,
		`DROP TABLE IF EXISTS notes;`,
		`DROP TABLE IF EXISTS anchor_globs;`,
		`DROP TABLE IF EXISTS anchors;`,
		`DROP TABLE IF EXISTS calls;`,
		`DROP TABLE IF EXISTS annotations;`,
		`DROP TABLE IF EXISTS super_edges;`,
		`DROP TABLE IF EXISTS symbols;`,
		`DROP TABLE IF EXISTS files;`,
		`DROP TABLE IF EXISTS index_metadata;`,
		`DROP TABLE IF EXISTS schema_version;`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
