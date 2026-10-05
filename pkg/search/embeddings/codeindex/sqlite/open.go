package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// ValidateExistingMetadata checks a previously indexed code-embedding domain
// without creating, healing, resetting, or otherwise mutating it.
func ValidateExistingMetadata(ctx context.Context, db *sql.DB, expected embeddings.IndexMetadata) error {
	if db == nil {
		return errors.New("sqlite db is required")
	}
	var provider, model string
	var dimensions int
	err := db.QueryRowContext(ctx, `SELECT provider, model, dimensions FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&provider, &model, &dimensions)
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

// OpenWithMetadata opens a code embeddings store and validates metadata.
func OpenWithMetadata(ctx context.Context, path string, provider embeddings.Provider, meta embeddings.IndexMetadata) (*Store, error) {
	return OpenWithMetadataWithOptions(ctx, path, provider, meta, OpenOptions{})
}

// OpenWithMetadataWithOptions opens a code embeddings store and validates metadata.
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

// OpenWithMetadataWithDB opens a code embeddings store on an existing SQLite handle.
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
