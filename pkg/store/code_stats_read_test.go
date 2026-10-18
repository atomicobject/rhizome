package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadCodeStatsPreservesLegacyIndex(t *testing.T) {
	db := seedTestDB(t)
	var sequence int
	var name, path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	opts := CodeStatsOptions{UnresolvedTop: 20, ExcludeNames: map[string]bool{"empty": true}}
	expected, err := (&Store{db: db}).CodeStats(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := ReadCodeStats(context.Background(), path, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("diagnostic result = %+v, want %+v", actual, expected)
	}
	if actual.Languages["php"].Symbols != 2 || actual.Edges["php"].CallsResolved != 4 {
		t.Fatalf("coverage counts changed: %+v", actual)
	}
	if got := actual.ReferenceClassifications["php"].Associations; got != (ClassificationCountSet{LocalResolved: 5, Unknown: 10}) {
		t.Fatalf("legacy reference counts = %+v", got)
	}
	if len(actual.UnresolvedCallees["php"]) != 1 || actual.UnresolvedCallees["php"][0].Name != "missingFunc" {
		t.Fatalf("exclusions changed: %+v", actual.UnresolvedCallees)
	}
	var schemas int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('rzm_migration_state', 'intel_external_targets')`).Scan(&schemas); err != nil {
		t.Fatal(err)
	}
	if schemas != 0 {
		t.Fatalf("diagnostic read migrated legacy schema: %d tables", schemas)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadCodeStats(ctx, path, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled diagnostic error = %v", err)
	}
}

func TestReadCodeStatsDoesNotCreateMissingIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	if _, err := ReadCodeStats(context.Background(), path, CodeStatsOptions{}); err == nil {
		t.Fatal("missing index was accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing diagnostic index was created: %v", err)
	}
}
