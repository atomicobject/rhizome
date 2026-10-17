package store

import (
	"context"
	"database/sql"
)

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
