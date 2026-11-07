package store

import (
	"context"
	"database/sql"
	"encoding/json"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

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

// claimAnchorLabelsTx drops the anchors whose label another note already
// defines with a different selector. Labels are vault-global (anchors.label is
// UNIQUE), so an upsert would otherwise rewrite the other note's anchor. The
// note with the smallest path keeps a contested label whatever the indexing
// order; the other notes lose their link, and the conflict is reported.
func claimAnchorLabelsTx(ctx context.Context, tx *sql.Tx, noteID int64, note codeanchor.Note, result *codeanchor.AnchorUpsertResult) (codeanchor.Note, error) {
	labels := make([]string, 0, len(note.DefinedAnchors))
	for _, a := range note.DefinedAnchors {
		if a.Label != "" {
			labels = append(labels, a.Label)
		}
	}
	existing, err := existingAnchorsByLabels(ctx, tx, labels)
	if err != nil {
		return note, err
	}
	kept := make([]codeanchor.Anchor, 0, len(note.DefinedAnchors))
	for _, a := range note.DefinedAnchors {
		old, ok := existing[a.Label]
		if !ok || a.ScopeEqual(old) {
			kept = append(kept, a)
			continue
		}
		others, err := otherNotePathsForAnchorTx(ctx, tx, old.ID, noteID)
		if err != nil {
			return note, err
		}
		if len(others) == 0 {
			kept = append(kept, a)
			continue
		}
		conflict := codeanchor.AnchorLabelConflict{Label: a.Label, Kept: others[0], Dropped: []string{note.Path}}
		// A dropped note may agree with a note indexed later, so its indexer
		// version is cleared and the next index retries it.
		if note.Path < others[0] {
			if _, err := tx.ExecContext(ctx, `UPDATE notes SET indexer_version = '' WHERE id IN (SELECT note_id FROM note_anchors WHERE anchor_id = ? AND note_id <> ?)`, old.ID, noteID); err != nil {
				return note, err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM note_anchors WHERE anchor_id = ? AND note_id <> ?`, old.ID, noteID); err != nil {
				return note, err
			}
			conflict.Kept, conflict.Dropped = note.Path, others
			kept = append(kept, a)
		} else if _, err := tx.ExecContext(ctx, `UPDATE notes SET indexer_version = '' WHERE id = ?`, noteID); err != nil {
			return note, err
		}
		if result != nil {
			result.LabelConflicts = append(result.LabelConflicts, conflict)
		}
	}
	note.DefinedAnchors = kept
	return note, nil
}

func otherNotePathsForAnchorTx(ctx context.Context, tx *sql.Tx, anchorID, noteID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT n.path FROM note_anchors na JOIN notes n ON n.id = na.note_id
		WHERE na.anchor_id = ? AND na.note_id <> ?
		ORDER BY n.path
	`, anchorID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notePaths []string
	for rows.Next() {
		var notePath string
		if err := rows.Scan(&notePath); err != nil {
			return nil, err
		}
		notePaths = append(notePaths, notePath)
	}
	return notePaths, rows.Err()
}

func (s *Store) upsertAnchorsAndLinks(ctx context.Context, tx *sql.Tx, noteID int64, note codeanchor.Note) error {
	note, err := claimAnchorLabelsTx(ctx, tx, noteID, note, nil)
	if err != nil {
		return err
	}
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
	note, err := claimAnchorLabelsTx(ctx, tx, noteID, note, result)
	if err != nil {
		return err
	}
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
