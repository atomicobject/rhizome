package store

import (
	"context"
	"database/sql"
	"encoding/json"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
)

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
