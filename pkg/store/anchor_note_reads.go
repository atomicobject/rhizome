package store

import (
	"context"
	"database/sql"
	"errors"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
)

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
