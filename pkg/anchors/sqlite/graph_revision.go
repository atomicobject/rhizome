package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type graphRevisionTriggerSpec struct {
	table           string
	predicateFormat string
	// updateColumns narrows the UPDATE trigger to columns the web graph reads.
	// SQLite fires an `UPDATE OF` trigger only when a listed column appears in
	// the statement's SET list, so metadata-only writes leave the revision alone.
	updateColumns []string
}

var graphRevisionTriggerSpecs = []graphRevisionTriggerSpec{
	{table: "notes", updateColumns: []string{"path", "title"}},
	{table: "files"},
	{table: "graph_doc_edges", predicateFormat: "%[1]s.kind IN ('wikilink', 'mdlink') OR %[1]s.kind GLOB 'note_link:*'"},
	{table: "doc_links", predicateFormat: "%[1]s.src_type = 'code'"},
	{table: "intel_doc_sections"},
	{table: "intel_code_anchors"},
	{table: "intel_edges", predicateFormat: "%[1]s.kind != 'defines'"},
	{table: "graph_doc_scores"},
	{table: "graph_anchor_scores"},
	{table: "ontology_note_types"},
	{table: "ontology_nodes"},
	{table: "ontology_edges"},
}

func createGraphRevisionSchema(ctx context.Context, db execer) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS graph_web_revision (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			incarnation TEXT NOT NULL CHECK (incarnation != ''),
			revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0)
		) STRICT;
		INSERT OR IGNORE INTO graph_web_revision (id, incarnation, revision)
		VALUES (1, lower(hex(randomblob(16))), 0)
	`); err != nil {
		return err
	}

	for _, spec := range graphRevisionTriggerSpecs {
		for _, op := range []string{"insert", "delete", "update"} {
			stmt := graphRevisionTriggerSQL(spec, op)
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("create graph revision trigger for %s %s: %w", spec.table, op, err)
			}
		}
	}
	return nil
}

func graphRevisionTriggerSQL(spec graphRevisionTriggerSpec, op string) string {
	return fmt.Sprintf(`
				CREATE TRIGGER IF NOT EXISTS graph_web_revision_%s_%s
				AFTER %s ON %s
				%s
				BEGIN
					UPDATE graph_web_revision SET revision = revision + 1 WHERE id = 1;
				END
			`, spec.table, op, graphRevisionEvent(spec, op), spec.table, graphRevisionWhen(op, spec.predicateFormat))
}

func graphRevisionEvent(spec graphRevisionTriggerSpec, op string) string {
	if op != "update" || len(spec.updateColumns) == 0 {
		return op
	}
	return op + " OF " + strings.Join(spec.updateColumns, ", ")
}

func graphRevisionWhen(op, predicateFormat string) string {
	if predicateFormat == "" {
		return ""
	}
	switch op {
	case "insert":
		return "WHEN " + fmt.Sprintf(predicateFormat, "NEW")
	case "delete":
		return "WHEN " + fmt.Sprintf(predicateFormat, "OLD")
	default:
		return "WHEN (" + fmt.Sprintf(predicateFormat, "OLD") + ") OR (" + fmt.Sprintf(predicateFormat, "NEW") + ")"
	}
}

func validateGraphRevisionSchema(ctx context.Context, tx *sql.Tx) error {
	var rows, id int
	var incarnation string
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MIN(id), 0), COALESCE(MIN(incarnation), ''), COALESCE(MIN(revision), -1) FROM graph_web_revision`).Scan(&rows, &id, &incarnation, &revision); err != nil {
		return fmt.Errorf("validate graph revision singleton: %w", err)
	}
	if rows != 1 || id != 1 || incarnation == "" || revision < 0 {
		return intelSchemaDrift("invalid graph revision singleton")
	}
	for _, spec := range graphRevisionTriggerSpecs {
		for _, op := range []string{"insert", "delete", "update"} {
			name := fmt.Sprintf("graph_web_revision_%s_%s", spec.table, op)
			var triggerSQL string
			if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&triggerSQL); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return intelSchemaDrift("missing required trigger %s", name)
				}
				return fmt.Errorf("validate trigger %s: %w", name, err)
			}
			if normalizeGraphRevisionSQL(triggerSQL) != normalizeGraphRevisionSQL(graphRevisionTriggerSQL(spec, op)) {
				return intelSchemaDrift("invalid graph revision trigger %s", name)
			}
		}
	}
	return nil
}

func normalizeGraphRevisionSQL(sqlText string) string {
	normalized := strings.TrimSuffix(strings.Join(strings.Fields(sqlText), " "), ";")
	return strings.Replace(normalized, "CREATE TRIGGER IF NOT EXISTS", "CREATE TRIGGER", 1)
}
