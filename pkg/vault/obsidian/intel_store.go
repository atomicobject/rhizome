package obsidian

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	embsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// IntelStoreOpenOptions controls SQLite open behavior for unified intel store paths.
type IntelStoreOpenOptions struct {
	// Context carries cancellation and startup diagnostics through schema open.
	Context context.Context
	// TxLockMode sets sqlite _txlock mode (supported: immediate, exclusive).
	TxLockMode string
	// Pool controls SQLite connection pooling.
	Pool sqliteutil.Options
	// SkipIntegrityCheck is reserved for explicitly latency-sensitive opens.
	SkipIntegrityCheck bool
}

func existingVaultBasePath(vaultPath string) string {
	basePath := strings.TrimSpace(vaultPath)
	if basePath == "" {
		return ""
	}
	info, err := os.Stat(basePath)
	if err != nil || !info.IsDir() {
		return ""
	}
	return basePath
}

// OpenIntelStoreFromConfig opens the unified intel store using a preloaded code config.
// When requireEnabled is true, disabled configs return (nil, nil, nil).
// Missing index paths return (nil, nil, nil); other stat/open errors are returned.
func OpenIntelStoreFromConfig(vaultPath string, cfg codeanchor.Config, requireEnabled bool) (*semdb.Store, func(), error) {
	return OpenIntelStoreFromConfigWithOptions(vaultPath, cfg, requireEnabled, IntelStoreOpenOptions{})
}

// OpenIntelStoreFromConfigWithOptions opens the unified intel store using a preloaded code config.
func OpenIntelStoreFromConfigWithOptions(vaultPath string, cfg codeanchor.Config, requireEnabled bool, opts IntelStoreOpenOptions) (*semdb.Store, func(), error) {
	if vaultPath == "" {
		return nil, nil, nil
	}
	if requireEnabled && !cfg.Enabled {
		return nil, nil, nil
	}
	cfg.IndexPath = UnifiedIndexPath(vaultPath, cfg.IndexPath)
	if cfg.IndexPath == "" {
		return nil, nil, nil
	}
	if _, err := os.Stat(cfg.IndexPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	store, err := semdb.OpenWithOptions(cfg.IndexPath, semdb.OpenOptions{
		Context:            opts.Context,
		TxLockMode:         opts.TxLockMode,
		Pool:               opts.Pool,
		SkipIntegrityCheck: opts.SkipIntegrityCheck,
	})
	if err != nil {
		return nil, nil, err
	}
	return store, func() { _ = store.Close() }, nil
}

// OpenIntelStoreFromConfigIfVaultPresent opens the intel store only when vaultPath exists.
func OpenIntelStoreFromConfigIfVaultPresent(vaultPath string, cfg codeanchor.Config, requireEnabled bool) (*semdb.Store, func(), error) {
	return OpenIntelStoreFromConfigIfVaultPresentWithOptions(vaultPath, cfg, requireEnabled, IntelStoreOpenOptions{})
}

// OpenIntelStoreFromConfigIfVaultPresentWithOptions opens the intel store only when vaultPath exists.
func OpenIntelStoreFromConfigIfVaultPresentWithOptions(vaultPath string, cfg codeanchor.Config, requireEnabled bool, opts IntelStoreOpenOptions) (*semdb.Store, func(), error) {
	basePath := existingVaultBasePath(vaultPath)
	if basePath == "" {
		return nil, nil, nil
	}
	return OpenIntelStoreFromConfigWithOptions(basePath, cfg, requireEnabled, opts)
}

// OpenIntelStore opens the unified intel store using the vault's code config.
// When requireEnabled is true, disabled configs return (nil, nil, nil).
func OpenIntelStore(vaultPath string, requireEnabled bool) (*semdb.Store, func(), error) {
	cfg, err := LoadCodeConfig(vaultPath)
	if err != nil {
		return nil, nil, err
	}
	return OpenIntelStoreFromConfigWithOptions(vaultPath, cfg, requireEnabled, IntelStoreOpenOptions{})
}

// OpenIntelStoreBestEffort mirrors OpenIntelStore but suppresses config/open errors.
func OpenIntelStoreBestEffort(vaultPath string, requireEnabled bool) (*semdb.Store, func(), error) {
	store, cleanup, err := OpenIntelStore(vaultPath, requireEnabled)
	if err != nil {
		return nil, nil, nil
	}
	return store, cleanup, nil
}

// OpenIntelStoreForWriteFromConfig opens the intel store, creating it if needed.
func OpenIntelStoreForWriteFromConfig(vaultPath string, cfg codeanchor.Config) (*semdb.Store, func(), error) {
	return OpenIntelStoreForWriteFromConfigWithOptions(vaultPath, cfg, IntelStoreOpenOptions{})
}

// OpenIntelStoreForWriteFromConfigWithOptions opens the intel store, creating it if needed.
func OpenIntelStoreForWriteFromConfigWithOptions(vaultPath string, cfg codeanchor.Config, opts IntelStoreOpenOptions) (*semdb.Store, func(), error) {
	if vaultPath == "" {
		return nil, nil, errors.New("vault path is required")
	}
	cfg.IndexPath = UnifiedIndexPath(vaultPath, cfg.IndexPath)
	if cfg.IndexPath == "" {
		return nil, nil, errors.New("index path is required")
	}
	store, err := semdb.OpenWithOptions(cfg.IndexPath, semdb.OpenOptions{
		Context:            opts.Context,
		TxLockMode:         opts.TxLockMode,
		Pool:               opts.Pool,
		SkipIntegrityCheck: opts.SkipIntegrityCheck,
	})
	if err != nil {
		return nil, nil, err
	}
	return store, func() { _ = store.Close() }, nil
}

// OpenIntelStoreForWriteFromConfigIfVaultPresent opens the intel store for write only when vaultPath exists.
func OpenIntelStoreForWriteFromConfigIfVaultPresent(vaultPath string, cfg codeanchor.Config) (*semdb.Store, func(), error) {
	return OpenIntelStoreForWriteFromConfigIfVaultPresentWithOptions(vaultPath, cfg, IntelStoreOpenOptions{})
}

// OpenIntelStoreForWriteFromConfigIfVaultPresentWithOptions opens the intel store for write only when vaultPath exists.
func OpenIntelStoreForWriteFromConfigIfVaultPresentWithOptions(vaultPath string, cfg codeanchor.Config, opts IntelStoreOpenOptions) (*semdb.Store, func(), error) {
	basePath := existingVaultBasePath(vaultPath)
	if basePath == "" {
		return nil, nil, nil
	}
	return OpenIntelStoreForWriteFromConfigWithOptions(basePath, cfg, opts)
}

// OpenIntelStoreForWrite opens the intel store using the vault's code config.
func OpenIntelStoreForWrite(vaultPath string) (*semdb.Store, func(), error) {
	cfg, err := LoadCodeConfig(vaultPath)
	if err != nil {
		return nil, nil, err
	}
	return OpenIntelStoreForWriteFromConfigWithOptions(vaultPath, cfg, IntelStoreOpenOptions{})
}

// OpenIntelStoreWithRecovery opens the intel store, automatically recovering from
// database corruption by attempting to preserve embeddings before falling back to
// full database removal.
//
// Recovery strategy:
//  1. If corruption is in embeddings tables (emb_*), try rebuildDomainPreservingEmbeddings
//  2. If that fails or corruption is elsewhere, remove the corrupted database and retry
//
// This provides auto-healing behavior for `rzm index` without requiring --rebuild,
// while preserving expensive embeddings when possible.
//
// lockPath is retained for caller/API parity. Automatic database removal is
// disabled because the index lock is not a lifecycle lock held by every opener.
func OpenIntelStoreWithRecovery(vaultPath string, cfg codeanchor.Config, lockPath string) (*semdb.Store, func(), error) {
	return OpenIntelStoreWithRecoveryWithOptions(vaultPath, cfg, lockPath, IntelStoreOpenOptions{})
}

// OpenIntelStoreWithRecoveryWithOptions opens the intel store with corruption recovery and open options.
func OpenIntelStoreWithRecoveryWithOptions(vaultPath string, cfg codeanchor.Config, lockPath string, opts IntelStoreOpenOptions) (*semdb.Store, func(), error) {
	store, cleanup, err := OpenIntelStoreForWriteFromConfigWithOptions(vaultPath, cfg, opts)
	if err == nil {
		return store, cleanup, nil
	}
	if semdb.IsSchemaIncompatibleError(err) {
		// Future/incompatible schema must be handled by explicit rebuild policy.
		return nil, nil, err
	}

	if !sqliteutil.IsCorruptError(err) {
		return nil, nil, err
	}

	indexPath := UnifiedIndexPath(vaultPath, cfg.IndexPath)
	fmt.Fprintf(os.Stderr, "Index corruption detected at %s\n", indexPath)

	// Check if corruption is in embeddings tables - if so, try to preserve them
	if isEmbeddingsCorruption(err) {
		fmt.Fprintf(os.Stderr, "Auto-recovering: attempting to preserve embeddings...\n")
		if repairErr := tryRepairEmbeddingsDomainWithOptions(indexPath, opts); repairErr == nil {
			// Retry open after repair
			store, cleanup, err = OpenIntelStoreForWriteFromConfigWithOptions(vaultPath, cfg, opts)
			if err == nil {
				fmt.Fprintf(os.Stderr, "Auto-recovery: embeddings preserved successfully\n")
				return store, cleanup, nil
			}
			// Repair succeeded but open still failed - fall through to full removal
			fmt.Fprintf(os.Stderr, "Auto-recovery: repair succeeded but open failed; manual index replacement required\n")
		} else {
			fmt.Fprintf(os.Stderr, "Auto-recovery: could not preserve embeddings (%v); manual index replacement required\n", repairErr)
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

func tryRepairEmbeddingsDomainWithOptions(indexPath string, opts IntelStoreOpenOptions) error {
	// Open the embeddings store directly - it has its own schema recovery logic
	// that will try rebuildDomainPreservingEmbeddings internally
	store, err := embsqlite.OpenWithOptions(indexPath, 0, embsqlite.OpenOptions{
		TxLockMode: opts.TxLockMode,
		Pool:       opts.Pool,
	})
	if err != nil {
		return fmt.Errorf("open embeddings store for repair: %w", err)
	}
	defer store.Close()

	// The Open call already ran ensureSchemaWithRecovery which attempts
	// rebuildDomainPreservingEmbeddings. If we got here without error,
	// the repair succeeded.
	return nil
}
