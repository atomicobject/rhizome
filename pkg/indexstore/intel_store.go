// Package indexstore owns configured opening and repair of the unified vault index.
package indexstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// AccessMode is the authority granted to a configured index handle.
type AccessMode uint8

const (
	ReadOnly AccessMode = iota + 1
	ReadWrite
)

// Options supplies operation policy and an already resolved configuration.
// Config nil loads the repository configuration at this boundary.
type Options struct {
	Access         AccessMode
	Config         *codeanchor.Config
	Create         bool
	Repair         bool
	IfVaultPresent bool
	RequireEnabled bool
	Context        context.Context
	TxLockMode     string
	Pool           sqliteutil.Options
}

// Open opens one configured index and returns its owned cleanup function.
// Missing optional vaults, disabled code, and absent existing indexes return nil.
// Configuration, access, schema, and failed repair errors are always returned.
func Open(vaultPath string, opts Options) (*semdb.Store, func(), error) {
	if opts.Access != ReadOnly && opts.Access != ReadWrite {
		return nil, nil, errors.New("index access must be ReadOnly or ReadWrite")
	}
	if opts.Access == ReadOnly && (opts.Create || opts.Repair || opts.TxLockMode != "") {
		return nil, nil, errors.New("read-only index access cannot create, repair, or select a write transaction lock")
	}

	vaultPath = strings.TrimSpace(vaultPath)
	if vaultPath == "" {
		if opts.Create && !opts.IfVaultPresent {
			return nil, nil, errors.New("vault path is required")
		}
		return nil, nil, nil
	}
	if opts.IfVaultPresent {
		info, err := os.Stat(vaultPath)
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if !info.IsDir() {
			return nil, nil, fmt.Errorf("vault path %s is not a directory", vaultPath)
		}
	}
	var cfg codeanchor.Config
	if opts.Config != nil {
		cfg = *opts.Config
	} else {
		resolved, err := repoconfig.Load(vaultPath)
		cfg = resolved.Code
		if err != nil {
			return nil, nil, err
		}
	}
	if opts.RequireEnabled && !cfg.Enabled {
		return nil, nil, nil
	}
	indexPath := repoconfig.UnifiedIndexPath(vaultPath, cfg.IndexPath)
	if !opts.Create {
		if _, err := os.Stat(indexPath); err != nil {
			if os.IsNotExist(err) {
				return nil, nil, nil
			}
			return nil, nil, err
		}
	}
	open := func() (*semdb.Store, func(), error) {
		var store *semdb.Store
		var err error
		if opts.Access == ReadOnly {
			store, err = semdb.OpenReadOnlyExisting(indexPath, opts.Context, opts.Pool)
			if err != nil {
				return nil, nil, fmt.Errorf("open read-only index (run `rzm index` to update it): %w", err)
			}
		} else {
			store, err = semdb.OpenWithOptions(indexPath, semdb.OpenOptions{Context: opts.Context, TxLockMode: opts.TxLockMode, Pool: opts.Pool})
		}
		if err != nil {
			return nil, nil, err
		}
		return store, func() { _ = store.Close() }, nil
	}
	store, cleanup, err := open()
	if err == nil || !opts.Repair || semdb.IsSchemaIncompatibleError(err) || !sqliteutil.IsCorruptError(err) {
		return store, cleanup, err
	}
	if isEmbeddingsCorruption(err) {
		if repairErr := tryRepairEmbeddingsDomainWithOptions(indexPath, opts); repairErr != nil {
			return nil, nil, fmt.Errorf("repair index %s: %w (original open error: %v)", indexPath, repairErr, err)
		}
		store, cleanup, err = open()
		if err == nil {
			return store, cleanup, nil
		}
	}
	return nil, nil, fmt.Errorf("automatic removal of corrupt index %s is disabled for safety; stop every Rhizome process, move the database and its -wal/-shm sidecars aside, then rebuild: %w", indexPath, err)
}

// isEmbeddingsCorruption checks if a corruption error mentions embeddings tables.
func isEmbeddingsCorruption(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Check for embeddings table names in the error
	return strings.Contains(msg, "emb_") ||
		strings.Contains(msg, "emb_index_meta") ||
		strings.Contains(msg, "emb_notes") ||
		strings.Contains(msg, "emb_chunk_embeddings") ||
		strings.Contains(msg, "emb_embedding_cache")
}

func tryRepairEmbeddingsDomainWithOptions(indexPath string, opts Options) error {
	// Open the embeddings store directly - it has its own schema recovery logic
	// that will try rebuildDomainPreservingEmbeddings internally
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(indexPath), embsqlite.Options{TxLockMode: opts.TxLockMode,
		Pool: opts.Pool, Access: embsqlite.WriteIndex, Dimensions: 0},
	)
	if err != nil {
		return fmt.Errorf("open embeddings store for repair: %w", err)
	}
	defer store.Close()

	// The Open call already ran ensureSchemaWithRecovery which attempts
	// rebuildDomainPreservingEmbeddings. If we got here without error,
	// the repair succeeded.
	return nil
}
