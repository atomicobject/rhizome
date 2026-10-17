package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// ValidateExistingMetadata checks a previously indexed code-embedding domain
// without creating, healing, resetting, or otherwise mutating it.
func validateCodeExisting(ctx context.Context, db *sql.DB, expected embeddings.IndexMetadata) error {
	if db == nil {
		return errors.New("sqlite db is required")
	}
	var provider, model string
	var dimensions int
	err := db.QueryRowContext(ctx, `SELECT provider, model, dimensions FROM `+codeTableIndexMeta+` WHERE id = 1`).Scan(&provider, &model, &dimensions)
	if errors.Is(err, sql.ErrNoRows) {
		return embeddings.MetadataError{Err: errors.New("code embedding metadata is missing")}
	}
	if err != nil {
		return embeddings.MetadataError{Err: err}
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

type codeFTSRow struct {
	anchorID    string
	chunkIndex  int
	path        string
	symbol      string
	fqn         string
	kind        string
	granularity string
	breadcrumb  string
	heading     string
	body        string
}

func itemVecTableName(dims int) string {
	return fmt.Sprintf("%s%d", vecItemTablePrefix, dims)
}

type tableColumnQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type statementPreparer interface {
	PrepareContext(context.Context, string) (*sql.Stmt, error)
}

type anchorUpsertPreparer interface {
	tableColumnQueryer
	statementPreparer
}

type anchorRowIDQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// SetFingerprintVersion updates the stored fingerprint version metadata.
func (s *CodeStore) SetFingerprintVersion(ctx context.Context, version int) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.set_fingerprint_version")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `UPDATE `+codeTableIndexMeta+` SET fingerprint_version = ? WHERE id = 1`, version)
		return err
	})
}

// PruneStaleItems removes items that weren't seen in the current sync generation.
// If threshold > 0, only prunes when stale items exceed threshold fraction of total.
// Returns the number of items pruned.
func (s *CodeStore) PruneStaleItems(ctx context.Context, threshold float64) (int, error) {
	if err := s.requireWrite(); err != nil {
		return 0, err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.prune_stale_items")
	gen, err := s.SyncGeneration(ctx)
	if err != nil {
		return 0, err
	}

	var total, stale int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableItems).Scan(&total); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableItems+` WHERE sync_generation < ?`, gen).Scan(&stale); err != nil {
		return 0, err
	}
	if stale == 0 {
		return 0, nil
	}

	// If stale fraction is below threshold, keep items around for potential reuse.
	staleFraction := float64(stale) / float64(total)
	if threshold > 0 && staleFraction < threshold {
		return 0, nil // Items preserved for branch-switching reuse
	}

	// Prune stale items and their embeddings.
	pruned := 0
	err = s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		result, err := execWithRetry(ctx, db, `DELETE FROM `+tableItems+` WHERE sync_generation < ?`, gen)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		pruned = int(rows)

		// Also clean up orphaned FTS entries.
		_, err = execWithRetry(ctx, db, `
			DELETE FROM `+tableChunkFTS+`
			WHERE anchor_id NOT IN (
				SELECT a.anchor_id
				FROM `+tableItems+` i
				JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			)
		`)
		return err
	})
	return pruned, err
}

// UpsertItemMeta inserts or updates code item metadata.
// The item is automatically marked with the current sync generation.
func (s *CodeStore) UpsertItemMeta(ctx context.Context, item codeindex.Item) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_meta")
	now := time.Now().Unix()
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		// Get current sync generation to mark this item as active.
		var gen int64
		_ = db.QueryRowContext(ctx, `SELECT IFNULL(sync_generation, 0) FROM `+codeTableIndexMeta+` WHERE id = 1`).Scan(&gen)

		anchorStmt, err := prepareIntelAnchorUpsertStmt(ctx, db)
		if err != nil {
			return err
		}
		defer anchorStmt.Close()
		if _, err := anchorStmt.ExecContext(ctx, string(item.AnchorID), item.Lang, item.Kind, item.Path, item.Symbol, item.FQN); err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `
                INSERT INTO `+tableItems+` (anchor_row_id, fingerprint, updated_at, sync_generation)
                SELECT id, ?, ?, ?
                FROM intel_code_anchors
                WHERE anchor_id = ?
                ON CONFLICT(anchor_row_id) DO UPDATE SET
                        fingerprint = excluded.fingerprint,
                        updated_at = excluded.updated_at,
                        sync_generation = excluded.sync_generation
        `, item.Fingerprint, now, gen, string(item.AnchorID))
		return err
	})
}

// UpsertItemMetaBatch inserts or updates metadata for multiple code items in one transaction.
func (s *CodeStore) UpsertItemMetaBatch(ctx context.Context, items []codeindex.Item) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_meta_batch")
	if len(items) == 0 {
		return nil
	}
	now := time.Now().Unix()
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		// Get current sync generation to mark items as active.
		var gen int64
		_ = tx.QueryRowContext(ctx, `SELECT IFNULL(sync_generation, 0) FROM `+codeTableIndexMeta+` WHERE id = 1`).Scan(&gen)

		anchorUpsertStarted := time.Now()
		if err := bulkUpsertIntelAnchorsTx(ctx, tx, items); err != nil {
			return err
		}
		indexingperf.ObserveLatency(ctx, indexingperf.CodeembMetaAnchorUpsert, time.Since(anchorUpsertStarted))

		itemUpsertStarted := time.Now()
		if err := bulkUpsertCodeItemsTx(ctx, tx, items, now, gen); err != nil {
			return err
		}
		indexingperf.AddCount(ctx, indexingperf.CodeembMetaItemRowsCount, int64(len(items)))
		indexingperf.ObserveLatency(ctx, indexingperf.CodeembMetaItemUpsert, time.Since(itemUpsertStarted))
		return nil
	})
}

// DeleteItemsNotIn removes metadata and embeddings for items not present in anchorIDs.
func (s *CodeStore) DeleteItemsNotIn(ctx context.Context, anchorIDs []codeindex.AnchorID) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.delete_items_not_in")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if len(anchorIDs) == 0 {
			if _, err := execWithRetry(ctx, db, `DELETE FROM `+tableItems); err != nil {
				return err
			}
			_, err := execWithRetry(ctx, db, `DELETE FROM `+tableChunkFTS)
			return err
		}

		// SQLite has a limit on bound parameters; fall back to a two-pass strategy when the list is large.
		if len(anchorIDs) <= 900 {
			holders := make([]string, len(anchorIDs))
			args := make([]any, 0, len(anchorIDs))
			for i, id := range anchorIDs {
				holders[i] = "?"
				args = append(args, string(id))
			}
			stmt := fmt.Sprintf(`DELETE FROM %s WHERE anchor_row_id NOT IN (SELECT id FROM intel_code_anchors WHERE anchor_id IN (%s))`, tableItems, strings.Join(holders, ","))
			if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
				return err
			}
			stmt = fmt.Sprintf(`DELETE FROM %s WHERE anchor_id NOT IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
			_, err := execWithRetry(ctx, db, stmt, args...)
			return err
		}

		existing := make(map[string]struct{})
		rows, err := db.QueryContext(ctx, `SELECT a.anchor_id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr != nil {
				_ = rows.Close()
				return scanErr
			}
			existing[id] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()

		keep := make(map[string]struct{}, len(anchorIDs))
		for _, id := range anchorIDs {
			keep[string(id)] = struct{}{}
		}

		var toDelete []string
		for id := range existing {
			if _, ok := keep[id]; !ok {
				toDelete = append(toDelete, id)
			}
		}
		if len(toDelete) == 0 {
			return nil
		}

		const batch = 500
		for chunk := range slices.Chunk(toDelete, batch) {
			holders := make([]string, len(chunk))
			args := make([]any, len(chunk))
			for i, id := range chunk {
				holders[i] = "?"
				args[i] = id
			}
			stmt := fmt.Sprintf(`DELETE FROM %s WHERE anchor_row_id IN (SELECT id FROM intel_code_anchors WHERE anchor_id IN (%s))`, tableItems, strings.Join(holders, ","))
			if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
				return err
			}
			stmt = fmt.Sprintf(`DELETE FROM %s WHERE anchor_id IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
			if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteItemsForPathNotIn removes metadata and embeddings for items under one path
// that are not present in anchorIDs.
func (s *CodeStore) DeleteItemsForPathNotIn(ctx context.Context, path string, anchorIDs []codeindex.AnchorID) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.delete_items_for_path_not_in")
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return nil
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return deleteItemsForPathNotIn(ctx, db, path, anchorIDs)
	})
}

// DeleteItemsForPathsNotIn removes metadata and embeddings for many paths in one write transaction.
func (s *CodeStore) DeleteItemsForPathsNotIn(ctx context.Context, anchorIDsByPath map[string][]codeindex.AnchorID) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.delete_items_for_paths_not_in_batch")
	if len(anchorIDsByPath) == 0 {
		return nil
	}
	paths := make([]string, 0, len(anchorIDsByPath))
	for path := range anchorIDsByPath {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		for _, path := range paths {
			if err := deleteItemsForPathNotIn(ctx, db, path, anchorIDsByPath[path]); err != nil {
				return err
			}
		}
		return nil
	})
}

func deleteItemsForPathNotIn(ctx context.Context, db *sql.DB, path string, anchorIDs []codeindex.AnchorID) error {
	if len(anchorIDs) == 0 {
		if _, err := execWithRetry(ctx, db, `DELETE FROM `+tableItems+` WHERE anchor_row_id IN (SELECT id FROM intel_code_anchors WHERE path = ?)`, path); err != nil {
			return err
		}
		_, err := execWithRetry(ctx, db, `DELETE FROM `+tableChunkFTS+` WHERE path = ?`, path)
		return err
	}
	holders := make([]string, len(anchorIDs))
	args := make([]any, 0, len(anchorIDs)+1)
	args = append(args, path)
	for i, id := range anchorIDs {
		holders[i] = "?"
		args = append(args, string(id))
	}
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE anchor_row_id IN (SELECT id FROM intel_code_anchors WHERE path = ?) AND anchor_row_id NOT IN (SELECT id FROM intel_code_anchors WHERE anchor_id IN (%s))`, tableItems, strings.Join(holders, ","))
	if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
		return err
	}
	stmt = fmt.Sprintf(`DELETE FROM %s WHERE path = ? AND anchor_id NOT IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
	_, err := execWithRetry(ctx, db, stmt, args...)
	return err
}

// ListItems returns all stored item metadata.
func (s *CodeStore) ListItems(ctx context.Context) ([]codeindex.Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.anchor_id, a.lang, a.kind, a.path, a.symbol, a.fqn,
		       i.fingerprint, i.updated_at
		FROM `+tableItems+` i
		JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []codeindex.Item
	for rows.Next() {
		var item codeindex.Item
		var updated int64
		if err := rows.Scan(&item.AnchorID, &item.Lang, &item.Kind, &item.Path, &item.Symbol, &item.FQN, &item.Fingerprint, &updated); err != nil {
			return nil, err
		}
		item.UpdatedAt = time.Unix(updated, 0)
		out = append(out, item)
	}
	return out, rows.Err()
}

// AnchorIDsByPathPrefix returns up to limit anchor IDs whose stored path equals prefix or is nested under it.
// Prefix matching is directory-boundary aware (prefix "pkg" matches "pkg/foo.go" but not "pkgx/foo.go").
//
// This is used to make directory/file seed expansion efficient without scanning the entire item list.
func (s *CodeStore) AnchorIDsByPathPrefix(ctx context.Context, prefix string, limit int) ([]codeindex.AnchorID, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}

	prefix = filepath.ToSlash(filepath.Clean(prefix))
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "." || prefix == "/" {
		return nil, nil
	}
	pattern := prefix + "/%"

	rows, err := s.db.QueryContext(ctx, `
		SELECT a.anchor_id
		FROM `+tableItems+` i
		JOIN intel_code_anchors a ON a.id = i.anchor_row_id
		WHERE a.path = ? OR a.path LIKE ?
		ORDER BY a.path
		LIMIT ?
	`, prefix, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]codeindex.AnchorID, 0, min(limit, 256))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if strings.TrimSpace(id) == "" {
			continue
		}
		out = append(out, codeindex.AnchorID(id))
	}
	return out, rows.Err()
}
