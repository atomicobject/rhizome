package store

import (
	"context"
	"database/sql"
	"errors"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"strings"
)

// MetadataKey constants for index metadata.
const (
	MetaKeyIndexerVersion  = "indexer_version"
	MetaKeyConfigHash      = "config_hash"
	MetaKeyModelHash       = "model_hash"
	MetaKeyAlgoVersion     = "algo_version"
	MetaKeyScopeConfigHash = "scope_config_hash"
	MetaKeyReverseIndexVer = "reverse_index_version"
	MetaKeyReverseIdxReady = "reverse_index_backfill_complete"
)

// GetMetadata retrieves a metadata value by key.
func (s *Store) GetMetadata(ctx context.Context, key string) (string, bool, error) {
	var value sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT value FROM index_metadata WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return value.String, value.Valid, nil
}

// SetMetadata stores a metadata key-value pair.
func (s *Store) SetMetadata(ctx context.Context, key, value string) error {
	ctx = indexingperf.WithOp(ctx, "intel.set_metadata")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO index_metadata(key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, key, value)
		return err
	})
}

// IndexerVersion returns the stored indexer version, if any.
func (s *Store) IndexerVersion(ctx context.Context) (string, bool, error) {
	return s.GetMetadata(ctx, MetaKeyIndexerVersion)
}

// SetIndexerVersion stores the indexer version.
func (s *Store) SetIndexerVersion(ctx context.Context, version string) error {
	return s.SetMetadata(ctx, MetaKeyIndexerVersion, version)
}

// GetScopeConfigHash returns the stored scope config hash, if any.
func (s *Store) GetScopeConfigHash(ctx context.Context) (string, bool, error) {
	return s.GetMetadata(ctx, MetaKeyScopeConfigHash)
}

// SetScopeConfigHash stores the scope config hash.
func (s *Store) SetScopeConfigHash(ctx context.Context, hash string) error {
	return s.SetMetadata(ctx, MetaKeyScopeConfigHash, hash)
}

// ReverseIndexVersion returns the stored reverse-index version, if any.
func (s *Store) ReverseIndexVersion(ctx context.Context) (string, bool, error) {
	return s.GetMetadata(ctx, MetaKeyReverseIndexVer)
}

// SetReverseIndexVersion stores the reverse-index version.
func (s *Store) SetReverseIndexVersion(ctx context.Context, version string) error {
	return s.SetMetadata(ctx, MetaKeyReverseIndexVer, version)
}

// ReverseIndexBackfillComplete returns whether backfill is marked complete.
func (s *Store) ReverseIndexBackfillComplete(ctx context.Context) (bool, bool, error) {
	value, ok, err := s.GetMetadata(ctx, MetaKeyReverseIdxReady)
	if err != nil || !ok {
		return false, ok, err
	}
	return strings.EqualFold(strings.TrimSpace(value), "true"), true, nil
}

// SetReverseIndexBackfillComplete stores the backfill completeness flag.
func (s *Store) SetReverseIndexBackfillComplete(ctx context.Context, complete bool) error {
	value := "false"
	if complete {
		value = "true"
	}
	return s.SetMetadata(ctx, MetaKeyReverseIdxReady, value)
}

// UpsertNoteMeta updates stored note hash/version/mtime for change detection.
func (s *Store) UpsertNoteMeta(ctx context.Context, path, contentHash, indexerVersion string, mtime int64) error {
	if s == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_note_meta")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE notes
			SET content_hash = ?, indexer_version = ?, mtime = ?
			WHERE path = ?
		`, contentHash, indexerVersion, mtime, path)
		return err
	})
}

// TouchNoteMtimes refreshes stored mtimes for the given note paths.
func (s *Store) TouchNoteMtimes(ctx context.Context, updates map[string]int64) error {
	if s == nil || len(updates) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.touch_note_mtimes")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `UPDATE notes SET mtime = ? WHERE path = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for path, mtime := range updates {
			if strings.TrimSpace(path) == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, mtime, path); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetPackMetadata retrieves all pack-related metadata.
func (s *Store) GetPackMetadata(ctx context.Context) (codeanchor.PackMetadata, error) {
	var pm codeanchor.PackMetadata
	pm.ConfigHash, _, _ = s.GetMetadata(ctx, MetaKeyConfigHash)
	pm.ModelHash, _, _ = s.GetMetadata(ctx, MetaKeyModelHash)
	pm.AlgoVersion, _, _ = s.GetMetadata(ctx, MetaKeyAlgoVersion)
	pm.IndexerVersion, _, _ = s.GetMetadata(ctx, MetaKeyIndexerVersion)
	return pm, nil
}

// SetPackMetadata stores pack-related metadata. Empty fields are skipped.
func (s *Store) SetPackMetadata(ctx context.Context, pm codeanchor.PackMetadata) error {
	ctx = indexingperf.WithOp(ctx, "intel.set_pack_metadata")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		set := func(key, val string) error {
			if val == "" {
				return nil
			}
			_, err := db.ExecContext(ctx, `
				INSERT INTO index_metadata(key, value) VALUES (?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value
			`, key, val)
			return err
		}
		if err := set(MetaKeyConfigHash, pm.ConfigHash); err != nil {
			return err
		}
		if err := set(MetaKeyModelHash, pm.ModelHash); err != nil {
			return err
		}
		if err := set(MetaKeyAlgoVersion, pm.AlgoVersion); err != nil {
			return err
		}
		if err := set(MetaKeyIndexerVersion, pm.IndexerVersion); err != nil {
			return err
		}
		return nil
	})
}
