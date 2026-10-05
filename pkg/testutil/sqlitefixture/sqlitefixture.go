package sqlitefixture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

var migratedStoreSnapshot struct {
	once sync.Once
	data []byte
	err  error
}

// Open creates a writable store from a process-wide snapshot of the current
// empty schema. Existing databases are opened without modification.
func Open(path string) (*semdb.Store, error) {
	if _, err := os.Stat(path); err == nil {
		return semdb.Open(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	migratedStoreSnapshot.once.Do(buildMigratedStoreSnapshot)
	if migratedStoreSnapshot.err != nil {
		return nil, migratedStoreSnapshot.err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, migratedStoreSnapshot.data, 0o600); err != nil {
		return nil, err
	}
	return semdb.Open(path)
}

func buildMigratedStoreSnapshot() {
	file, err := os.CreateTemp("", "rhizome-migrated-store-*.sqlite")
	if err != nil {
		migratedStoreSnapshot.err = err
		return
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		migratedStoreSnapshot.err = err
		return
	}
	defer func() {
		_ = os.Remove(path)
		_ = os.Remove(path + "-wal")
		_ = os.Remove(path + "-shm")
		_ = os.Remove(path + ".init.lock")
	}()

	store, err := semdb.Open(path)
	if err != nil {
		migratedStoreSnapshot.err = fmt.Errorf("create migrated store snapshot: %w", err)
		return
	}
	if _, err := store.DB().Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		_ = store.Close()
		migratedStoreSnapshot.err = fmt.Errorf("checkpoint migrated store snapshot: %w", err)
		return
	}
	if err := store.Close(); err != nil {
		migratedStoreSnapshot.err = fmt.Errorf("close migrated store snapshot: %w", err)
		return
	}
	migratedStoreSnapshot.data, migratedStoreSnapshot.err = os.ReadFile(path)
}
