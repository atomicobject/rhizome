package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

const (
	noteRegionFTSVisible      = "note_region_visible"
	noteRegionFTSSupplemental = "note_region_supplemental"
)

func (s *Store) replaceAllNoteRegionFTSTx(ctx context.Context, tx *sql.Tx, notes []NoteMetadataRow, regions []NoteSearchRegionRow) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM intel_fts
		WHERE item_type IN ('note_region_visible', 'note_region_supplemental')
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM intel_fts_rowid
		WHERE item_type IN ('note_region_visible', 'note_region_supplemental')
	`); err != nil {
		return err
	}
	return s.insertNoteRegionFTSTx(ctx, tx, notes, regions)
}

func (s *Store) replaceNoteRegionFTSPathsTx(ctx context.Context, tx *sql.Tx, changedPaths, deletedPaths []string, notes []NoteMetadataRow, regions []NoteSearchRegionRow) error {
	paths := normalizeNonEmptyStrings(append(append([]string(nil), changedPaths...), deletedPaths...))
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := min(start+sqliteValuesBatchMaxParams, len(paths))
		batch := paths[start:end]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
			SELECT item_type, item_id
			FROM intel_fts
			WHERE item_type IN ('note_region_visible', 'note_region_supplemental')
			  AND path IN (%s)
		`, holders), sliceAny(batch)...)
		if err != nil {
			return err
		}
		ids := map[string][]string{}
		for rows.Next() {
			var itemType, itemID string
			if err := rows.Scan(&itemType, &itemID); err != nil {
				rows.Close()
				return err
			}
			ids[itemType] = append(ids[itemType], itemID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for itemType, itemIDs := range ids {
			if err := s.deleteIntelFTSByIDsTx(ctx, tx, itemType, itemIDs); err != nil {
				return err
			}
		}
	}
	return s.insertNoteRegionFTSTx(ctx, tx, notes, regions)
}

func (s *Store) insertNoteRegionFTSTx(ctx context.Context, tx *sql.Tx, notes []NoteMetadataRow, regions []NoteSearchRegionRow) error {
	titles := make(map[string]string, len(notes))
	for _, note := range notes {
		path := strings.TrimSpace(note.Path)
		titles[path] = note.Title
	}
	rows := make([]codeanchor.IntelFTSRow, 0, len(regions))
	for _, region := range regions {
		path := strings.TrimSpace(region.NotePath)
		// Authored source keeps its established section-level lexical surface.
		// Provider-derived regions are the structural-free lexical lane for
		// root-only providers.
		if region.Origin == "authored" {
			continue
		}
		itemType := noteRegionFTSVisible
		if region.Kind == "supplemental" {
			itemType = noteRegionFTSSupplemental
		}
		rows = append(rows, codeanchor.IntelFTSRow{
			ItemType: itemType,
			ItemID:   fmt.Sprintf("note-region:%s:%d", path, region.Ordinal),
			Path:     path,
			Title:    titles[path],
			Body:     region.Text,
		})
	}
	return s.upsertIntelFTSRowsTx(ctx, tx, rows)
}
