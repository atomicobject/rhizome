package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology/sectionparse"
)

// addIntelSectionSourceIdentitySchema moves canonical section identity recovery
// from every search into one upgrade. Paid embeddings and FTS rows stay intact.
func addIntelSectionSourceIdentitySchema(ctx context.Context, tx *sql.Tx) error {
	sourceExists, err := tableHasColumnQuery(ctx, tx, "intel_doc_sections", "source_node_id")
	if err != nil {
		return err
	}
	if !sourceExists {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE intel_doc_sections ADD COLUMN source_node_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	ordinalExists, err := tableHasColumnQuery(ctx, tx, "intel_doc_sections", "ord")
	if err != nil {
		return err
	}
	if !ordinalExists {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE intel_doc_sections ADD COLUMN ord INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `WITH positions AS (SELECT id, row_number() OVER (PARTITION BY path ORDER BY start_byte, section_id) - 1 AS ord FROM intel_doc_sections) UPDATE intel_doc_sections SET ord = (SELECT ord FROM positions WHERE positions.id = intel_doc_sections.id)`); err != nil {
			return err
		}
	}
	if sourceExists {
		return nil
	}
	type section struct {
		id                int64
		path, title, body string
		start             int64
	}
	var lastID int64
	for {
		rows, err := tx.QueryContext(ctx, `SELECT id,path,title,content,start_byte FROM intel_doc_sections WHERE id > ? ORDER BY id LIMIT 128`, lastID)
		if err != nil {
			return err
		}
		batch := make([]section, 0, 128)
		for rows.Next() {
			var item section
			if err := rows.Scan(&item.id, &item.path, &item.title, &item.body, &item.start); err != nil {
				_ = rows.Close()
				return err
			}
			batch = append(batch, item)
		}
		scanErr := rows.Err()
		_ = rows.Close()
		if scanErr != nil {
			return scanErr
		}
		if len(batch) == 0 {
			return nil
		}
		for _, item := range batch {
			nodeID := ""
			if strings.HasPrefix(strings.TrimSpace(item.body), "#") {
				nodes := sectionparse.Parse(item.path, item.body)
				if len(nodes) > 0 && nodes[0].StartByte == 0 {
					if nodes[0].BlockID != "" {
						nodeID = nodes[0].ID
					} else {
						nodeID = fmt.Sprintf("%s#%s-%d", item.path, markdownsyntax.HeadingSlug(strings.TrimSpace(item.title)), item.start)
					}
				}
			}
			if _, err := tx.ExecContext(ctx, `UPDATE intel_doc_sections SET source_node_id = ? WHERE id = ?`, nodeID, item.id); err != nil {
				return err
			}
			lastID = item.id
		}
	}
}
