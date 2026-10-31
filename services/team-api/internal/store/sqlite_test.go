package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/services/team-api/internal/store"
	"github.com/atomicobject/rhizome/services/team-api/internal/storetest"
)

func TestSQLiteContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		db, err := store.Open(filepath.Join(t.TempDir(), "gateway.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return store.NewSQLite(db)
	})
}

func TestOpenIsPrivateAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "gateway.db")
	for range 2 {
		db, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var mode string
		if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
			t.Fatalf("journal_mode = %q, %v", mode, err)
		}
		var fk int
		if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
			t.Fatalf("foreign_keys = %d, %v", fk, err)
		}
		db.Close()
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %v, want 0600", info.Mode().Perm())
	}
}
