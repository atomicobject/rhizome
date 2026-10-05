package cmd

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func seedExternalStatsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "external-stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, stmt := range []string{
		`CREATE TABLE files (path TEXT PRIMARY KEY, lang TEXT NOT NULL)`,
		`CREATE TABLE symbols (id INTEGER PRIMARY KEY, lang TEXT NOT NULL, fqn TEXT NOT NULL)`,
		`CREATE TABLE intel_symbol_ref_files (file_id INTEGER PRIMARY KEY, path TEXT NOT NULL UNIQUE)`,
		`CREATE TABLE intel_symbol_ref_targets (target_id INTEGER PRIMARY KEY, dst_lang TEXT NOT NULL, dst_pkg TEXT NOT NULL, dst_name TEXT NOT NULL, dst_fqn TEXT NOT NULL)`,
		`CREATE TABLE intel_symbol_refs (src_file_id INTEGER NOT NULL, owner_fqn TEXT NOT NULL, ref_kind TEXT NOT NULL, dst_target_id INTEGER NOT NULL)`,
		`CREATE TABLE intel_import_refs (src_path TEXT NOT NULL, module TEXT NOT NULL)`,
		`CREATE TABLE intel_module_defs (src_path TEXT NOT NULL, lang TEXT NOT NULL, module TEXT NOT NULL)`,
		`CREATE TABLE intel_external_targets (external_id INTEGER PRIMARY KEY, handle TEXT NOT NULL, target_kind TEXT NOT NULL)`,
		`CREATE TABLE intel_external_symbol_evidence (src_file_id INTEGER NOT NULL, owner_fqn TEXT NOT NULL, ref_kind TEXT NOT NULL, raw_target_id INTEGER NOT NULL, external_id INTEGER NOT NULL, evidence_kind TEXT NOT NULL)`,
		`CREATE TABLE intel_external_import_evidence (src_path TEXT NOT NULL, module TEXT NOT NULL, binding_ordinal INTEGER NOT NULL, external_id INTEGER NOT NULL, evidence_kind TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	must := func(stmt string) {
		t.Helper()
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed external stats: %v\n%s", err, stmt)
		}
	}
	must(`INSERT INTO files VALUES ('src/a.ts','ts'), ('src/b.ts','ts')`)
	must(`INSERT INTO intel_symbol_ref_files VALUES (1,'src/a.ts'), (2,'src/b.ts')`)
	must(`INSERT INTO symbols VALUES (1,'ts','app.Local')`)
	must(`INSERT INTO intel_symbol_ref_targets VALUES
		(10,'ts','app','Local','app.Local'),
		(11,'ts','react','useState','react.useState'),
		(12,'ts','','console.log','console.log'),
		(13,'ts','','Missing','Missing'),
		(14,'ts','react','ReactNode','react.ReactNode'),
		(15,'ts','','UnknownType','UnknownType')`)
	must(`INSERT INTO intel_symbol_refs VALUES
		(1,'app.a','calls',10),
		(1,'app.a','calls',11),
		(2,'app.b','calls',11),
		(2,'app.b','calls',13),
		(1,'app.a','member_ref',12),
		(1,'app.a','type_ref',14),
		(2,'app.b','type_ref',15)`)
	must(`INSERT INTO intel_external_targets VALUES
		(100,'npm:react#useState:symbol','symbol'),
		(101,'npm:node:runtime#console.log:runtime_global','runtime_global'),
		(102,'npm:react#ReactNode:type','type'),
		(103,'npm:react#:module','module')`)
	must(`INSERT INTO intel_external_symbol_evidence VALUES
		(1,'app.a','calls',11,100,'esm_named'),
		(2,'app.b','calls',11,100,'esm_named'),
		(1,'app.a','member_ref',12,101,'runtime_global'),
		(1,'app.a','type_ref',14,102,'esm_type')`)
	must(`INSERT INTO intel_import_refs VALUES
		('src/a.ts','app/local'),
		('src/a.ts','react'),
		('src/b.ts','missing-package')`)
	must(`INSERT INTO intel_module_defs VALUES ('src/local.ts','ts','app/local')`)
	must(`INSERT INTO intel_external_import_evidence VALUES
		('src/a.ts','react',0,103,'esm_named'),
		('src/b.ts','react',0,103,'esm_side_effect')`)
	return db
}

func TestQueryReferenceClassificationsFourWayAndUniqueTargets(t *testing.T) {
	db := seedExternalStatsDB(t)
	stats := &CodeStats{ReferenceClassifications: map[string]ReferenceClassificationStats{}}
	if err := queryReferenceClassifications(db, stats); err != nil {
		t.Fatal(err)
	}

	ts := stats.ReferenceClassifications["ts"]
	wantOverall := ClassificationCountSet{
		LocalResolved: 2, ExternalClassified: 5, RuntimeGlobalBuiltin: 1, Unknown: 3,
	}
	if ts.Associations != wantOverall {
		t.Fatalf("associations = %+v, want %+v", ts.Associations, wantOverall)
	}
	wantTargets := ClassificationCountSet{
		LocalResolved: 2, ExternalClassified: 3, RuntimeGlobalBuiltin: 1, Unknown: 3,
	}
	if ts.UniqueTargets != wantTargets {
		t.Fatalf("unique targets = %+v, want %+v", ts.UniqueTargets, wantTargets)
	}

	calls := ts.ByKind["calls"]
	if calls.Associations != (ClassificationCountSet{LocalResolved: 1, ExternalClassified: 2, Unknown: 1}) {
		t.Errorf("call associations = %+v", calls.Associations)
	}
	if calls.UniqueTargets != (ClassificationCountSet{LocalResolved: 1, ExternalClassified: 1, Unknown: 1}) {
		t.Errorf("call targets = %+v", calls.UniqueTargets)
	}
	if got := ts.ByKind["type_ref"].Associations; got != (ClassificationCountSet{ExternalClassified: 1, Unknown: 1}) {
		t.Errorf("type refs = %+v", got)
	}
	if got := ts.ByKind["member_ref"].Associations.RuntimeGlobalBuiltin; got != 1 {
		t.Errorf("runtime member refs = %d", got)
	}
	if got := ts.ByKind["imports"].Associations; got != (ClassificationCountSet{LocalResolved: 1, ExternalClassified: 2, Unknown: 1}) {
		t.Errorf("imports = %+v", got)
	}
	if got := sumClassificationCounts(ts.ByKind["imports"].Associations); got != 4 {
		t.Errorf("imports double-counted base module rows: total=%d", got)
	}
	for _, kind := range []string{"calls", "type_ref", "member_ref", "imports"} {
		if _, ok := ts.ByKind[kind]; !ok {
			t.Errorf("missing explicit relation kind %q", kind)
		}
	}
}

func TestQueryReferenceClassificationsUsesRuntimeTargetKindForImportSyntaxEvidence(t *testing.T) {
	db := seedExternalStatsDB(t)
	if _, err := db.Exec(`UPDATE intel_external_targets SET target_kind='runtime_builtin' WHERE external_id IN (100,103)`); err != nil {
		t.Fatal(err)
	}
	stats := &CodeStats{ReferenceClassifications: map[string]ReferenceClassificationStats{}}
	if err := queryReferenceClassifications(db, stats); err != nil {
		t.Fatal(err)
	}
	ts := stats.ReferenceClassifications["ts"]
	if got := ts.ByKind["calls"].Associations.RuntimeGlobalBuiltin; got != 2 {
		t.Fatalf("runtime builtin calls = %d, want 2", got)
	}
	if got := ts.ByKind["imports"].Associations.RuntimeGlobalBuiltin; got != 2 {
		t.Fatalf("runtime builtin imports = %d, want 2", got)
	}
}

func TestQueryReferenceClassificationsLocalPrecedence(t *testing.T) {
	db := seedExternalStatsDB(t)
	if _, err := db.Exec(`INSERT INTO symbols VALUES (2,'ts','react.useState')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO intel_module_defs VALUES ('vendor/react.ts','ts','react')`); err != nil {
		t.Fatal(err)
	}

	stats := &CodeStats{ReferenceClassifications: map[string]ReferenceClassificationStats{}}
	if err := queryReferenceClassifications(db, stats); err != nil {
		t.Fatal(err)
	}
	ts := stats.ReferenceClassifications["ts"]
	if ts.Associations.LocalResolved != 6 || ts.Associations.ExternalClassified != 1 {
		t.Fatalf("local precedence not applied: %+v", ts.Associations)
	}
	if got := sumClassificationCounts(ts.ByKind["imports"].Associations); got != 4 {
		t.Fatalf("stale external import evidence was double-counted: total=%d", got)
	}
}

func TestQueryReferenceClassificationsSupportsPreExternalTargetSchema(t *testing.T) {
	db := seedExternalStatsDB(t)
	for _, table := range []string{
		"intel_external_import_evidence",
		"intel_external_symbol_evidence",
		"intel_external_targets",
	} {
		if _, err := db.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}

	stats := &CodeStats{
		Languages: map[string]LanguageCounts{"ts": {Symbols: 1, Files: 2}},
	}
	if err := queryReferenceClassifications(db, stats); err != nil {
		t.Fatalf("pre-external-target schema must remain queryable: %v", err)
	}

	ts := stats.ReferenceClassifications["ts"]
	if got, want := ts.Associations, (ClassificationCountSet{LocalResolved: 2, Unknown: 8}); got != want {
		t.Fatalf("legacy associations = %+v, want %+v", got, want)
	}
	if got, want := ts.UniqueTargets, (ClassificationCountSet{LocalResolved: 2, Unknown: 7}); got != want {
		t.Fatalf("legacy unique targets = %+v, want %+v", got, want)
	}
	if got, want := ts.ByKind["imports"].Associations, (ClassificationCountSet{LocalResolved: 1, Unknown: 2}); got != want {
		t.Fatalf("legacy imports = %+v, want %+v", got, want)
	}
}

func TestReferenceClassificationJSONUsesContractClassNames(t *testing.T) {
	payload, err := json.Marshal(ClassificationCountSet{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"local_resolved", "external_classified", "runtime_global_builtin", "unknown"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing classification key %q in %s", key, payload)
		}
	}
}

func TestReferenceClassificationTextSplitsRelationKinds(t *testing.T) {
	stats := &CodeStats{
		Languages: map[string]LanguageCounts{"ts": {}},
		Edges:     map[string]EdgeCounts{},
		ReferenceClassifications: map[string]ReferenceClassificationStats{
			"ts": {
				ByKind: map[string]ReferenceKindClassificationStats{
					"calls": {}, "type_ref": {}, "member_ref": {}, "imports": {},
				},
			},
		},
		UnresolvedCallees: map[string][]UnresolvedCallee{},
	}
	var out bytes.Buffer
	writeTextOutput(&out, stats)
	for _, kind := range []string{"calls", "type_ref", "member_ref", "imports"} {
		if !strings.Contains(out.String(), kind) {
			t.Errorf("text output missing %q:\n%s", kind, out.String())
		}
	}
}

func TestReferenceClassificationsIncludeIndexedLanguageWithoutRefs(t *testing.T) {
	db := seedExternalStatsDB(t)
	stats := &CodeStats{
		Languages: map[string]LanguageCounts{
			"ts":   {Symbols: 1, Files: 2},
			"rust": {Symbols: 1, Files: 1},
		},
		Edges:                    map[string]EdgeCounts{},
		ReferenceClassifications: map[string]ReferenceClassificationStats{},
		UnresolvedCallees:        map[string][]UnresolvedCallee{},
	}
	if err := queryReferenceClassifications(db, stats); err != nil {
		t.Fatal(err)
	}

	rust, ok := stats.ReferenceClassifications["rust"]
	if !ok {
		t.Fatal("indexed no-ref language missing from reference classifications")
	}
	if rust.Associations != (ClassificationCountSet{}) || rust.UniqueTargets != (ClassificationCountSet{}) {
		t.Fatalf("rust totals must be explicit zeroes: %+v", rust)
	}
	for _, kind := range []string{"calls", "type_ref", "member_ref", "imports"} {
		if _, ok := rust.ByKind[kind]; !ok {
			t.Errorf("rust missing zero-valued relation kind %q", kind)
		}
	}

	payload, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"rust":{"associations":{"local_resolved":0`)) {
		t.Fatalf("JSON omits explicit no-ref language counts: %s", payload)
	}
	var text bytes.Buffer
	writeTextOutput(&text, stats)
	if !strings.Contains(text.String(), "[rust]") ||
		!strings.Contains(text.String(), "classified associations local=0 external=0 runtime=0 unknown=0") {
		t.Fatalf("text omits explicit no-ref language counts:\n%s", text.String())
	}
}

func sumClassificationCounts(counts ClassificationCountSet) int {
	return counts.LocalResolved + counts.ExternalClassified + counts.RuntimeGlobalBuiltin + counts.Unknown
}
