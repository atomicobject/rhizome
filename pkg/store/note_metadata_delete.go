package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func deleteMetadataRowsForPaths(ctx context.Context, tx *sql.Tx, paths []string, deleteNotes bool) error {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil
	}
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := min(start+sqliteValuesBatchMaxParams, len(paths))
		batch := paths[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := sliceAny(batch)
		for _, stmt := range []string{
			`DELETE FROM note_property_values WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_tags WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_search_terms WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_fragment_targets WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_projection_diagnostics WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_search_regions WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_link_inputs WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
			`DELETE FROM note_projection_payloads WHERE note_id IN (SELECT id FROM notes WHERE path IN (%s))`,
		} {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(stmt, holders), args...); err != nil {
				return err
			}
		}
		if deleteNotes {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM notes WHERE path IN (%s)`, holders), args...); err != nil {
				return err
			}
		}
	}
	return nil
}

func deleteGraphEdgesForPaths(ctx context.Context, tx *sql.Tx, changedPaths, deletedPaths []string) error {
	changedPaths = normalizeNonEmptyStrings(changedPaths)
	deletedPaths = normalizeNonEmptyStrings(deletedPaths)
	for start := 0; start < len(changedPaths); start += sqliteValuesBatchMaxParams {
		end := min(start+sqliteValuesBatchMaxParams, len(changedPaths))
		batch := changedPaths[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM graph_doc_edges WHERE src_path IN (%s) AND (kind = 'wikilink' OR kind = 'mdlink' OR kind GLOB 'note_link:*')`, holders), sliceAny(batch)...); err != nil {
			return err
		}
	}
	maxDeletedPaths := sqliteValuesBatchMaxParams / 2
	for start := 0; start < len(deletedPaths); start += maxDeletedPaths {
		end := min(start+maxDeletedPaths, len(deletedPaths))
		batch := deletedPaths[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := append(sliceAny(batch), sliceAny(batch)...)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM graph_doc_edges WHERE (src_path IN (%s) OR dst_path IN (%s)) AND (kind = 'wikilink' OR kind = 'mdlink' OR kind GLOB 'note_link:*')`, holders, holders), args...); err != nil {
			return err
		}
	}
	return nil
}
