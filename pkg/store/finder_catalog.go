package store

import (
	"context"
	"database/sql"
)

// FinderNoteRow is one indexed note as the ⌘P finder lists it.
type FinderNoteRow struct {
	Path  string
	Title string
	// Type is the lexically first resolved ontology type, empty when untyped.
	Type  string
	Mtime int64
}

// FinderCodeRow is one indexed code file.
type FinderCodeRow struct {
	Path string
	Lang string
}

// FinderCatalogRows reads every indexed note and code file in one read
// snapshot. Both lists come from the index, so they already honor its ignore
// and ownership rules.
func (s *Store) FinderCatalogRows(ctx context.Context) ([]FinderNoteRow, []FinderCodeRow, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT n.path, COALESCE(n.title, ''), n.mtime,
		COALESCE((SELECT MIN(t.type_name) FROM ontology_note_types t WHERE t.note_path = n.path AND t.type_name NOT LIKE '\_Fallback%' ESCAPE '\'), '')
		FROM notes n WHERE n.indexed_at > 0`)
	if err != nil {
		return nil, nil, err
	}
	var notes []FinderNoteRow
	for rows.Next() {
		var row FinderNoteRow
		if err := rows.Scan(&row.Path, &row.Title, &row.Mtime, &row.Type); err != nil {
			rows.Close()
			return nil, nil, err
		}
		notes = append(notes, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT path, lang FROM files`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var code []FinderCodeRow
	for rows.Next() {
		var row FinderCodeRow
		if err := rows.Scan(&row.Path, &row.Lang); err != nil {
			return nil, nil, err
		}
		code = append(code, row)
	}
	return notes, code, rows.Err()
}
