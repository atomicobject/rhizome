package cmd

// WHY: locks the JSON contract for `rzm code stats`. Per SPEC-0070 US1, the
// output shape (top-level keys + nested struct fields) is the agent-facing
// contract consumed by legacy-codebase-assessor (SPEC-0069) and future audit
// skills. If you change a json tag in code_stats.go, this test must change
// with it — that's the lock.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// seedTestDB builds a minimal sqlite schema mirroring the relevant tables and
// inserts a small fixture covering two languages, resolved + unresolved calls,
// and one entry that should be filtered by the exclude catalog.
func seedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema := []string{
		`CREATE TABLE files (path TEXT PRIMARY KEY, lang TEXT NOT NULL, hash TEXT, indexer_version TEXT, parse_status TEXT NOT NULL DEFAULT 'ok', call_edges_stale INTEGER NOT NULL DEFAULT 0, mtime INTEGER)`,
		`CREATE TABLE symbols (id INTEGER PRIMARY KEY, lang TEXT NOT NULL, kind TEXT NOT NULL, file TEXT NOT NULL, pkg TEXT, name TEXT NOT NULL, fqn TEXT NOT NULL UNIQUE, fqn_reversed TEXT)`,
		`CREATE TABLE intel_symbol_ref_targets (target_id INTEGER PRIMARY KEY, dst_lang TEXT NOT NULL, dst_pkg TEXT NOT NULL, dst_name TEXT NOT NULL, dst_fqn TEXT NOT NULL, UNIQUE(dst_lang, dst_pkg, dst_name, dst_fqn))`,
		`CREATE TABLE intel_symbol_refs (src_file_id INTEGER NOT NULL, owner_symbol_id INTEGER, owner_fqn TEXT NOT NULL, ref_kind TEXT NOT NULL, dst_target_id INTEGER NOT NULL, PRIMARY KEY (src_file_id, owner_fqn, ref_kind, dst_target_id))`,
		`CREATE TABLE intel_import_refs (src_path TEXT NOT NULL, module TEXT NOT NULL, PRIMARY KEY (src_path, module))`,
		`CREATE TABLE intel_module_defs (src_path TEXT NOT NULL, lang TEXT NOT NULL, module TEXT NOT NULL)`,
	}
	for _, q := range schema {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	mustExec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed insert: %v\nQUERY: %s", err, q)
		}
	}

	mustExec(`INSERT INTO files (path, lang) VALUES
		('src/a.php', 'php'),
		('src/b.php', 'php'),
		('web/c.ts', 'ts')`)

	mustExec(`INSERT INTO symbols (id, lang, kind, file, pkg, name, fqn) VALUES
		(1, 'php', 'function', 'src/a.php', '', 'realFunc',  'realFunc'),
		(2, 'php', 'function', 'src/b.php', '', 'otherFunc', 'otherFunc'),
		(3, 'ts',  'function', 'web/c.ts',  '', 'tsFn',      'tsFn')`)

	mustExec(`INSERT INTO intel_module_defs VALUES
		('src/a.php', 'php', 'src/a.php'),
		('src/b.php', 'php', 'src/b.php'),
		('web/c.ts',  'ts',  'web')`)

	mustExec(`INSERT INTO intel_symbol_ref_targets (target_id, dst_lang, dst_pkg, dst_name, dst_fqn) VALUES
		(10, 'php', '', 'realFunc',    'realFunc'),
		(11, 'php', '', 'empty',       'empty'),
		(12, 'php', '', 'missingFunc', 'missingFunc'),
		(13, 'ts',  '', 'jsRuntime',   'jsRuntime')`)

	// Per-row inserts avoid duplicate-PK silent drops. PK is
	// (src_file_id, owner_fqn, ref_kind, dst_target_id) so each row needs a
	// unique combo. We give each call a unique synthetic owner_fqn suffix.
	//
	// Plan:
	//   - realFunc resolves: target 10 (4 distinct call sites, all resolved)
	//   - empty unresolved: target 11 (3 distinct call sites, php built-in)
	//   - missingFunc unresolved: target 12 (5 distinct call sites, real-gap candidate)
	//   - 1 member_ref to realFunc (target 10)
	//   - 1 ts call to jsRuntime (target 13, unresolved)
	callPlan := []struct {
		srcFile int
		owner   string
		kind    string
		target  int
	}{
		{1, "callerA1", "calls", 10},
		{1, "callerA2", "calls", 10},
		{2, "callerB1", "calls", 10},
		{2, "callerB2", "calls", 10},
		{1, "emptyA1", "calls", 11},
		{1, "emptyA2", "calls", 11},
		{2, "emptyB1", "calls", 11},
		{1, "missA1", "calls", 12},
		{1, "missA2", "calls", 12},
		{2, "missB1", "calls", 12},
		{2, "missB2", "calls", 12},
		{2, "missB3", "calls", 12},
		{1, "memberA1", "member_ref", 10},
		{3, "tsCaller1", "calls", 13},
	}
	for _, c := range callPlan {
		if _, err := db.Exec(`INSERT INTO intel_symbol_refs (src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id) VALUES (?, NULL, ?, ?, ?)`,
			c.srcFile, c.owner, c.kind, c.target); err != nil {
			t.Fatalf("seed call: %v", err)
		}
	}

	mustExec(`INSERT INTO intel_import_refs (src_path, module) VALUES
		('src/a.php', 'shared.php'),
		('src/b.php', 'config.php')`)

	return db
}

func TestCodeStatsContract(t *testing.T) {
	db := seedTestDB(t)
	stats := &CodeStats{
		Vault:                    "/tmp/fixture",
		IndexPath:                "/tmp/fixture/db",
		Languages:                map[string]LanguageCounts{},
		Edges:                    map[string]EdgeCounts{},
		ReferenceClassifications: map[string]ReferenceClassificationStats{},
		UnresolvedCallees:        map[string][]UnresolvedCallee{},
		ByDirectory:              map[string][]DirectoryDistribution{},
	}

	if err := queryLanguageCounts(db, stats); err != nil {
		t.Fatalf("languages: %v", err)
	}
	if err := queryEdgeCategories(db, stats); err != nil {
		t.Fatalf("edges: %v", err)
	}
	if err := queryTopUnresolvedCallees(db, stats, 20, nil); err != nil {
		t.Fatalf("unresolved: %v", err)
	}
	if err := queryByDirectory(db, stats); err != nil {
		t.Fatalf("byDirectory: %v", err)
	}

	php := stats.Languages["php"]
	if php.Symbols != 2 || php.Files != 2 {
		t.Errorf("php counts: got symbols=%d files=%d, want 2/2", php.Symbols, php.Files)
	}
	if got := stats.Languages["ts"].Symbols; got != 1 {
		t.Errorf("ts symbols: got %d, want 1", got)
	}

	phpEdges := stats.Edges["php"]
	// Fixture: 4 realFunc calls (resolved) + 3 empty + 5 missingFunc = 12 extracted
	if phpEdges.CallsExtracted != 12 {
		t.Errorf("php extracted: got %d, want 12", phpEdges.CallsExtracted)
	}
	if phpEdges.CallsResolved != 4 {
		t.Errorf("php resolved: got %d, want 4 (only realFunc target resolves)", phpEdges.CallsResolved)
	}
	if phpEdges.MemberRefs != 1 {
		t.Errorf("php member_refs: got %d, want 1", phpEdges.MemberRefs)
	}
	if phpEdges.Imports != 2 {
		t.Errorf("php imports: got %d, want 2", phpEdges.Imports)
	}

	// Unresolved should include `empty` and `missingFunc`
	phpUnresolved := stats.UnresolvedCallees["php"]
	names := map[string]int{}
	for _, u := range phpUnresolved {
		names[u.Name] = u.Calls
	}
	if names["empty"] == 0 {
		t.Errorf("expected `empty` in unresolved, got %v", names)
	}
	if names["missingFunc"] == 0 {
		t.Errorf("expected `missingFunc` in unresolved, got %v", names)
	}

	// JSON shape lock: encode and re-decode into a map; check top-level keys.
	buf, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(buf, &generic); err != nil {
		t.Fatal(err)
	}
	requiredTopLevel := []string{"vault", "indexPath", "languages", "edges", "referenceClassifications", "unresolvedCallees", "byDirectory"}
	for _, k := range requiredTopLevel {
		if _, ok := generic[k]; !ok {
			t.Errorf("JSON missing required top-level key %q", k)
		}
	}
}

func TestCodeStatsExcludeCatalogFilter(t *testing.T) {
	db := seedTestDB(t)

	// Write a minimal catalog: `empty` is a language-builtin, should be excluded;
	// `missingFunc` lives under real-gap, should NOT be excluded.
	catalog := `# Test catalog
## language-builtin
- empty — language construct, dominates the unresolved list

## real-gap
- ` + "`missingFunc`" + ` — should still show in unresolved
`
	catalogPath := filepath.Join(t.TempDir(), "catalog.md")
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
		t.Fatal(err)
	}

	excludeSet, err := loadExcludeCatalog(catalogPath)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if !excludeSet["empty"] {
		t.Errorf("expected `empty` in exclude set, got %v", excludeSet)
	}
	if excludeSet["missingFunc"] {
		t.Errorf("missingFunc was under real-gap; should NOT be excluded")
	}

	stats := &CodeStats{
		Languages:         map[string]LanguageCounts{},
		Edges:             map[string]EdgeCounts{},
		UnresolvedCallees: map[string][]UnresolvedCallee{},
		ByDirectory:       map[string][]DirectoryDistribution{},
	}
	if err := queryTopUnresolvedCallees(db, stats, 20, excludeSet); err != nil {
		t.Fatal(err)
	}

	for _, u := range stats.UnresolvedCallees["php"] {
		if u.Name == "empty" {
			t.Errorf("`empty` should have been filtered out by exclude catalog, got %+v", stats.UnresolvedCallees["php"])
		}
	}

	// missingFunc should still be present (it's a real-gap entry, not excluded).
	var foundMissing bool
	for _, u := range stats.UnresolvedCallees["php"] {
		if u.Name == "missingFunc" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("expected `missingFunc` to remain in unresolved (it's real-gap, not excluded), got %+v", stats.UnresolvedCallees["php"])
	}
}

func TestLoadExcludeCatalogEmpty(t *testing.T) {
	set, err := loadExcludeCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	if set != nil {
		t.Errorf("expected nil exclude set for empty path, got %v", set)
	}
}
