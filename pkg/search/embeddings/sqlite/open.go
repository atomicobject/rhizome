package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// ValidateExistingMetadata checks a previously indexed note-embedding domain
// without creating, healing, resetting, or otherwise mutating it.
func ValidateExistingMetadata(ctx context.Context, db *sql.DB, expected embeddings.IndexMetadata) error {
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

// validateLegacyMetadataVectors accepts metadata that is absent or wholly
// unknown only when compatible persisted vectors prove that the legacy index
// is usable. It is deliberately read-only for query-only runtime callers.
func validateLegacyMetadataVectors(ctx context.Context, db *sql.DB, expected embeddings.IndexMetadata) error {
	if expected.Dimensions > 0 {
		table := fmt.Sprintf("intel_embeddings_vec_d%d", expected.Dimensions)
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM sqlite_master
				WHERE type = 'table' AND name = ?
			)
		`, table).Scan(&exists)
		if err != nil {
			return embeddings.MetadataError{Err: err}
		}
		if exists {
			err = db.QueryRowContext(ctx, fmt.Sprintf(`
				SELECT EXISTS(
					SELECT 1
					FROM intel_embeddings e
					JOIN intel_chunks c ON c.id = e.chunk_row_id
					JOIN %s v ON v.chunk_id = e.chunk_row_id
					WHERE e.dimensions = ?
				)
			`, table), expected.Dimensions).Scan(&exists)
			if err != nil {
				return embeddings.MetadataError{Err: err}
			}
		}
		if exists {
			return nil
		}
	}
	if expected.Provider != "" {
		return embeddings.MetadataError{Err: fmt.Errorf("provider mismatch: have , expected %s", expected.Provider)}
	}
	if expected.Model != "" {
		return embeddings.MetadataError{Err: fmt.Errorf("model mismatch: have , expected %s", expected.Model)}
	}
	return embeddings.MetadataError{Err: fmt.Errorf("dimensions mismatch: no compatible vectors for expected %d", expected.Dimensions)}
}

// OpenWithMetadata opens a note embeddings store and validates metadata.
func OpenWithMetadata(ctx context.Context, path string, provider embeddings.Provider, meta embeddings.IndexMetadata) (*Store, error) {
	return OpenWithMetadataWithOptions(ctx, path, provider, meta, OpenOptions{})
}

// OpenWithMetadataWithOptions opens a note embeddings store and validates metadata.
func OpenWithMetadataWithOptions(ctx context.Context, path string, provider embeddings.Provider, meta embeddings.IndexMetadata, opts OpenOptions) (*Store, error) {
	store, err := OpenWithOptions(path, provider.Dimensions(), opts)
	if err != nil {
		return nil, err
	}
	if err := store.ValidateOrInitMetadata(ctx, meta); err != nil {
		_ = store.Close()
		return nil, embeddings.MetadataError{Err: err}
	}
	return store, nil
}

// OpenWithMetadataWithDB opens a note embeddings store on an existing SQLite handle.
func OpenWithMetadataWithDB(ctx context.Context, db *sql.DB, provider embeddings.Provider, meta embeddings.IndexMetadata) (*Store, error) {
	store, err := OpenWithDB(db, provider.Dimensions())
	if err != nil {
		return nil, err
	}
	if err := store.ValidateOrInitMetadata(ctx, meta); err != nil {
		_ = store.Close()
		return nil, embeddings.MetadataError{Err: err}
	}
	return store, nil
}
