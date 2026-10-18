package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
)

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
