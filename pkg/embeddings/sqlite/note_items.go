package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// ValidateExistingMetadata checks a previously indexed note-embedding domain
// without creating, healing, resetting, or otherwise mutating it.
func validateNoteExisting(ctx context.Context, db *sql.DB, expected embeddings.IndexMetadata) error {
	if db == nil {
		return errors.New("sqlite db is required")
	}
	var provider, model string
	var dimensions int
	err := db.QueryRowContext(ctx, `SELECT provider, model, dimensions FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&provider, &model, &dimensions)
	if errors.Is(err, sql.ErrNoRows) {
		return validateLegacyMetadataVectors(ctx, db, expected)
	}
	if err != nil {
		return embeddings.MetadataError{Err: err}
	}
	if provider == "" && model == "" && dimensions == 0 {
		return validateLegacyMetadataVectors(ctx, db, expected)
	}
	if provider == "" || model == "" || dimensions <= 0 {
		return embeddings.MetadataError{Err: errors.New("note embedding metadata is partially populated")}
	}
	if expected.Dimensions > 0 && dimensions > 0 && dimensions != expected.Dimensions {
		return embeddings.MetadataError{Err: fmt.Errorf("dimensions mismatch: have %d, expected %d", dimensions, expected.Dimensions)}
	}
	if expected.Provider != "" && provider != expected.Provider {
		return embeddings.MetadataError{Err: fmt.Errorf("provider mismatch: have %s, expected %s", provider, expected.Provider)}
	}
	if expected.Model != "" && model != expected.Model {
		return embeddings.MetadataError{Err: fmt.Errorf("model mismatch: have %s, expected %s", model, expected.Model)}
	}
	return nil
}

const noteSchemaVersion = 7

func chunkVecTableName(dims int) string {
	return fmt.Sprintf("%s%d", vecChunkTablePrefix, dims)
}

// PruneStaleNotes removes notes that weren't seen in the current sync generation.
// If threshold > 0, only prunes when stale notes exceed threshold fraction of total.
// Returns the number of notes pruned.
func (s *NoteStore) PruneStaleNotes(ctx context.Context, threshold float64) (int, error) {
	if err := s.requireWrite(); err != nil {
		return 0, err
	}

	ctx = indexingperf.WithOp(ctx, "noteemb.prune_stale_notes")
	gen, err := s.SyncGeneration(ctx)
	if err != nil {
		return 0, err
	}

	var total, stale int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableNotes).Scan(&total); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableNotes+` WHERE sync_generation < ?`, gen).Scan(&stale); err != nil {
		return 0, err
	}
	if stale == 0 {
		return 0, nil
	}

	// If stale fraction is below threshold, keep notes around for potential reuse.
	staleFraction := float64(stale) / float64(total)
	if threshold > 0 && staleFraction < threshold {
		return 0, nil // Notes preserved for branch-switching reuse
	}

	// Prune stale notes and their embeddings (via cascade).
	var rows int64
	err = s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		result, err := execWithRetry(ctx, db, `DELETE FROM `+tableNotes+` WHERE sync_generation < ?`, gen)
		if err != nil {
			return err
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(rows), nil
}

func (s *NoteStore) Stats(ctx context.Context) (int, int, error) {
	var notes, chunks int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableNotes).Scan(&notes); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableChunkEmbeddings).Scan(&chunks); err != nil {
		return 0, 0, err
	}
	return notes, chunks, nil
}

// UpsertNoteMeta inserts or updates metadata for a note.
// The note is automatically marked with the current sync generation.
func (s *NoteStore) UpsertNoteMeta(ctx context.Context, info embeddings.NoteFileInfo) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "noteemb.upsert_note_meta")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		// Get current sync generation to mark this note as active.
		var gen int64
		_ = db.QueryRowContext(ctx, `SELECT IFNULL(sync_generation, 0) FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&gen)

		_, err := execWithRetry(ctx, db, `
			INSERT INTO `+tableNotes+` (note_id, title, path, last_seen_mtime, last_seen_size, sync_generation)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(note_id) DO UPDATE SET
				title = excluded.title,
				path = excluded.path,
				last_seen_mtime = excluded.last_seen_mtime,
				last_seen_size = excluded.last_seen_size,
				sync_generation = excluded.sync_generation
		`, string(info.ID), info.Title, info.Path, info.Mtime.Unix(), info.Size, gen)
		return err
	})
}

// UpsertNoteMetaBatch inserts or updates metadata for multiple notes in a single transaction.
func (s *NoteStore) UpsertNoteMetaBatch(ctx context.Context, infos []embeddings.NoteFileInfo) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "noteemb.upsert_note_meta")
	if len(infos) == 0 {
		return nil
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			var gen int64
			_ = tx.QueryRowContext(ctx, `SELECT IFNULL(sync_generation, 0) FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&gen)

			stmt, err := tx.PrepareContext(ctx, `
				INSERT INTO `+tableNotes+` (note_id, title, path, last_seen_mtime, last_seen_size, sync_generation)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT(note_id) DO UPDATE SET
					title = excluded.title,
					path = excluded.path,
					last_seen_mtime = excluded.last_seen_mtime,
					last_seen_size = excluded.last_seen_size,
					sync_generation = excluded.sync_generation
			`)
			if err != nil {
				return err
			}
			defer stmt.Close()

			for _, info := range infos {
				if _, err := stmt.ExecContext(ctx, string(info.ID), info.Title, info.Path, info.Mtime.Unix(), info.Size, gen); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// DeleteNotesNotIn removes notes (and embeddings via cascade) not present in existingIDs.
func (s *NoteStore) DeleteNotesNotIn(ctx context.Context, existingIDs []embeddings.NoteID) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "noteemb.delete_notes_not_in")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if len(existingIDs) == 0 {
			_, err := execWithRetry(ctx, db, `DELETE FROM `+tableNotes)
			return err
		}

		if len(existingIDs) <= 900 {
			args := make([]any, len(existingIDs))
			holders := make([]string, len(existingIDs))
			for i, id := range existingIDs {
				args[i] = string(id)
				holders[i] = "?"
			}
			query := fmt.Sprintf(`DELETE FROM %s WHERE note_id NOT IN (%s)`, tableNotes, strings.Join(holders, ","))
			_, err := execWithRetry(ctx, db, query, args...)
			return err
		}

		existingSet := make(map[string]struct{}, len(existingIDs))
		for _, id := range existingIDs {
			existingSet[string(id)] = struct{}{}
		}

		rows, err := db.QueryContext(ctx, `SELECT note_id FROM `+tableNotes)
		if err != nil {
			return err
		}
		defer rows.Close()

		var toDelete []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			if _, ok := existingSet[id]; !ok {
				toDelete = append(toDelete, id)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(toDelete) == 0 {
			return nil
		}

		const batch = 500
		for chunk := range slices.Chunk(toDelete, batch) {
			args := make([]any, len(chunk))
			holders := make([]string, len(chunk))
			for i, id := range chunk {
				args[i] = id
				holders[i] = "?"
			}
			query := fmt.Sprintf(`DELETE FROM %s WHERE note_id IN (%s)`, tableNotes, strings.Join(holders, ","))
			if _, err := execWithRetry(ctx, db, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListNotes returns metadata for all tracked notes.
func (s *NoteStore) ListNotes(ctx context.Context) ([]embeddings.NoteFileInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT note_id, title, path, last_seen_mtime, last_seen_size
		FROM `+tableNotes+`
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []embeddings.NoteFileInfo
	for rows.Next() {
		var id, title, path string
		var mtimeUnix int64
		var size int64
		if err := rows.Scan(&id, &title, &path, &mtimeUnix, &size); err != nil {
			return nil, err
		}
		res = append(res, embeddings.NoteFileInfo{
			ID:    embeddings.NoteID(id),
			Title: title,
			Path:  path,
			Mtime: time.Unix(mtimeUnix, 0),
			Size:  size,
		})
	}
	return res, rows.Err()
}

// DeleteNote removes a note and cascades embeddings.
func (s *NoteStore) DeleteNote(ctx context.Context, id embeddings.NoteID) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "noteemb.delete_note")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `DELETE FROM `+tableNotes+` WHERE note_id = ?`, string(id))
		return err
	})
}

// NoteCentroid returns the average chunk embedding for a note, when no note-level
// embedding is stored.
func (s *NoteStore) NoteCentroid(ctx context.Context, id embeddings.NoteID) (embeddings.Embedding, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cache.embedding
		FROM `+tableChunkEmbeddings+` c
		JOIN `+tableNotes+` n ON c.note_row_id = n.id
		JOIN `+tableEmbeddingCache+` cache ON c.content_hash = cache.content_hash
		WHERE n.note_id = ?
	`, string(id))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var sum embeddings.Embedding
	count := 0
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return nil, false, err
		}
		emb := bytesToEmbed(blob)
		if len(emb) == 0 {
			continue
		}
		if sum == nil {
			sum = make(embeddings.Embedding, len(emb))
		}
		if len(sum) != len(emb) {
			continue
		}
		for i := range sum {
			sum[i] += emb[i]
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if count == 0 || sum == nil {
		return nil, false, nil
	}
	for i := range sum {
		sum[i] /= float32(count)
	}
	return sum, true, nil
}

func makePlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	holders := make([]string, n)
	for i := range holders {
		holders[i] = "?"
	}
	return strings.Join(holders, ",")
}

var _ embeddings.Index = (*NoteStore)(nil)
