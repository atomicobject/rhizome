package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

// NoteLinkInputRow preserves unresolved provider lookup identity so namespace
// changes can find dependent sources without rereading every authored note.
type NoteLinkInputRow struct{ NotePath, LookupKey string }

func createNoteLinkInputsSchema(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS note_link_inputs (
   note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
   lookup_key TEXT NOT NULL CHECK (lookup_key != ''),
   PRIMARY KEY(note_id, lookup_key)
  ) WITHOUT ROWID, STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_note_link_inputs_lookup ON note_link_inputs(lookup_key,note_id)`,
		`CREATE TABLE IF NOT EXISTS note_projection_payloads (
 note_id INTEGER PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json))
 ) STRICT`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func insertNoteLinkInputs(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, rows []NoteLinkInputRow) error {
	values := make([][]any, 0, len(rows))
	for _, row := range rows {
		if id, ok := noteIDs[row.NotePath]; ok && row.LookupKey != "" {
			values = append(values, []any{id, row.LookupKey})
		}
	}
	return execValuesBatch(ctx, tx, `INSERT OR IGNORE INTO note_link_inputs(note_id,lookup_key) VALUES`, values, 2, "")
}

// NoteLinkInputSources returns persisted sources affected by any supplied
// normalized target name, including unresolved and ambiguous references.
func (s *Store) NoteLinkInputSources(ctx context.Context, keys []string) ([]string, error) {
	keys = normalizeNonEmptyStrings(keys)
	result := map[string]struct{}{}
	for start := 0; start < len(keys); start += sqliteValuesBatchMaxParams {
		batch := keys[start:min(start+sqliteValuesBatchMaxParams, len(keys))]
		holders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT n.path FROM note_link_inputs i JOIN notes n ON n.id=i.note_id WHERE i.lookup_key IN (`+holders+`)`, sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				rows.Close()
				return nil, err
			}
			result[path] = struct{}{}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(result))
	for path := range result {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}
