package store

import (
	"context"
	"database/sql"
)

type NoteProjectionPayloadRow struct{ NotePath, JSON string }

func insertNoteProjectionPayloads(ctx context.Context, tx *sql.Tx, noteIDs map[string]int64, rows []NoteProjectionPayloadRow) error {
	values := make([][]any, 0, len(rows))
	for _, row := range rows {
		if id, ok := noteIDs[row.NotePath]; ok {
			values = append(values, []any{id, row.JSON})
		}
	}
	return execValuesBatch(ctx, tx, `INSERT INTO note_projection_payloads(note_id,payload_json) VALUES`, values, 2, "")
}

func noteProjectionPayloads(ctx context.Context, q noteMetadataQueryer, paths []string) (map[string]string, error) {
	out := map[string]string{}
	err := queryNoteFactsByPaths(ctx, q, paths, `SELECT n.path,p.payload_json FROM note_projection_payloads p JOIN notes n ON n.id=p.note_id WHERE n.path IN (%s) ORDER BY n.path`, func(rows *sql.Rows) error {
		var path, payload string
		if err := rows.Scan(&path, &payload); err != nil {
			return err
		}
		out[path] = payload
		return nil
	})
	return out, err
}
