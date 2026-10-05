//go:build cgo

package codeanchor_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
)

type countingRefStore struct {
	*sqlite.Store
	symbolCalls  atomic.Int64
	moduleCalls  atomic.Int64
	lookupError  error
	failLookupAt int64
}

type countingAnchorLookupStore struct {
	*sqlite.Store
	pathCalls  atomic.Int64
	pathsCalls atomic.Int64
}

type countingIntelResolverStore struct {
	*sqlite.Store
	fqnCalls          atomic.Int64
	seedCalls         atomic.Int64
	moduleSuffixCalls atomic.Int64
}

type failingCallEdgeBatchStore struct {
	*sqlite.Store
	err   error
	calls atomic.Int64
}

func (s *failingCallEdgeBatchStore) UpsertIntelCallEdgesForPathsBatch(ctx context.Context, batches []codeanchor.CallEdgesBatch) error {
	s.calls.Add(1)
	return s.err
}

func (s *countingRefStore) ReferrerPathsBySymbolRefs(ctx context.Context, refs []codeanchor.SymbolRef) (map[string][]string, error) {
	if s.symbolCalls.Add(1) == s.failLookupAt {
		return nil, s.lookupError
	}
	return s.Store.ReferrerPathsBySymbolRefs(ctx, refs)
}

func (s *countingRefStore) ReferrerPathsByModules(ctx context.Context, modules []string) (map[string][]string, error) {
	if s.moduleCalls.Add(1) == s.failLookupAt {
		return nil, s.lookupError
	}
	return s.Store.ReferrerPathsByModules(ctx, modules)
}

func (s *countingIntelResolverStore) IntelAnchorIDsByExactFQNsAndLang(ctx context.Context, lang string, fqns []string) (map[string][]string, error) {
	s.fqnCalls.Add(1)
	return s.Store.IntelAnchorIDsByExactFQNsAndLang(ctx, lang, fqns)
}

func (s *countingIntelResolverStore) IntelAnchorIDsByFQNsAndLang(ctx context.Context, lang string, fqns []string) (map[string][]string, error) {
	s.fqnCalls.Add(1)
	return s.Store.IntelAnchorIDsByFQNsAndLang(ctx, lang, fqns)
}

func (s *countingIntelResolverStore) CallEdgeSuffixSeedsByLangs(ctx context.Context, langs []codeanchor.Lang) ([]codeanchor.CallEdgeSuffixSeed, error) {
	s.seedCalls.Add(1)
	return s.Store.CallEdgeSuffixSeedsByLangs(ctx, langs)
}

func (s *countingIntelResolverStore) ModuleAnchorIDsByModuleSuffix(ctx context.Context, modules []string) (map[string]string, error) {
	s.moduleSuffixCalls.Add(1)
	return s.Store.ModuleAnchorIDsByModuleSuffix(ctx, modules)
}

func (s *countingAnchorLookupStore) IntelAnchorsByPath(ctx context.Context, path string) ([]codeanchor.IntelAnchor, error) {
	s.pathCalls.Add(1)
	return s.Store.IntelAnchorsByPath(ctx, path)
}

func (s *countingAnchorLookupStore) IntelAnchorsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.IntelAnchor, error) {
	s.pathsCalls.Add(1)
	return s.Store.IntelAnchorsByPaths(ctx, paths)
}

func newIntelStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "intel-svc.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

func newCountingIntelResolverStore(t *testing.T) *countingIntelResolverStore {
	t.Helper()
	baseStore, err := sqlite.Open(filepath.Join(t.TempDir(), "intel-resolver.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, baseStore.Close())
	})
	return &countingIntelResolverStore{Store: baseStore}
}

func newCountingAnchorLookupStore(t *testing.T) *countingAnchorLookupStore {
	t.Helper()
	baseStore, err := sqlite.Open(filepath.Join(t.TempDir(), "intel-anchor-lookup.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, baseStore.Close())
	})
	return &countingAnchorLookupStore{Store: baseStore}
}

// minimalIndexer is a no-op indexer that returns an empty FileSummary for tests.
type minimalIndexer struct {
	lang codeanchor.Lang
}

func (m *minimalIndexer) IndexFile(content []byte, path paths.CodePathRef) (codeanchor.FileSummary, error) {
	return codeanchor.FileSummary{FilePath: path.Rel.String(), Lang: m.lang}, nil
}

func (m *minimalIndexer) Lang() codeanchor.Lang { return m.lang }

type summaryIndexer struct {
	lang      codeanchor.Lang
	summaries map[string]codeanchor.FileSummary
}

func (s *summaryIndexer) IndexFile(_ []byte, ref paths.CodePathRef) (codeanchor.FileSummary, error) {
	if summary, ok := s.summaries[ref.Rel.String()]; ok {
		return summary, nil
	}
	return codeanchor.FileSummary{FilePath: ref.Rel.String(), Lang: s.lang}, nil
}

func (s *summaryIndexer) Lang() codeanchor.Lang { return s.lang }

func TestService_IntelHooks_CodeFile_StoresRationaleWithoutSymbolDefinitions(t *testing.T) {
	ctx := context.Background()
	store := newIntelStore(t)
	svc := codeanchor.NewService(store, &minimalIndexer{lang: codeanchor.LangGo})

	err := svc.IndexCodeFile(ctx, codeanchor.LangGo, "a.go", []byte("// NOTE: keep this invariant\npackage main\n"))
	require.NoError(t, err)

	rationale, err := store.RationaleForPath(ctx, "a.go")
	require.NoError(t, err)
	require.Len(t, rationale, 1)
	require.Equal(t, codeanchor.RationaleNote, rationale[0].Kind)
	anchors, err := store.IntelAnchorsByPath(ctx, "a.go")
	require.NoError(t, err)
	require.NotEmpty(t, anchors)
	for _, anchor := range anchors {
		require.Equal(t, "module", anchor.Kind, "a file without definitions should persist only its module anchor")
	}
}

func TestService_IntelHooks_CodeFile_StoresRationaleOnParseError(t *testing.T) {
	ctx := context.Background()
	store := newIntelStore(t)
	svc := codeanchor.NewService(store, &summaryIndexer{
		lang: codeanchor.LangGo,
		summaries: map[string]codeanchor.FileSummary{
			"a.go": {
				FilePath:    "a.go",
				Lang:        codeanchor.LangGo,
				ParseStatus: codeanchor.ParseErrored,
			},
		},
	})

	err := svc.IndexCodeFile(ctx, codeanchor.LangGo, "a.go", []byte("// HACK: parser fallback still needs rationale\npackage main\n"))
	require.NoError(t, err)

	rationale, err := store.RationaleForPath(ctx, "a.go")
	require.NoError(t, err)
	require.Len(t, rationale, 1)
	require.Equal(t, codeanchor.RationaleHack, rationale[0].Kind)
}

func TestService_IntelHooks_CSharpWritesImportTypeRefAndCallEdges(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "intel-csharp.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{&summaryIndexer{
		lang: codeanchor.LangCs,
		summaries: map[string]codeanchor.FileSummary{
			"csharp/User.cs": {
				FilePath:    "csharp/User.cs",
				Lang:        codeanchor.LangCs,
				ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{
					{Lang: codeanchor.LangCs, Kind: codeanchor.SymClass, File: "csharp/User.cs", Pkg: "Polyglot.Todo.Models", Name: "User"},
					{Lang: codeanchor.LangCs, Kind: codeanchor.SymField, File: "csharp/User.cs", Pkg: "Polyglot.Todo.Models.User", Name: "Primary"},
				},
			},
			"csharp/Worker.cs": {
				FilePath:    "csharp/Worker.cs",
				Lang:        codeanchor.LangCs,
				ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{
					{Lang: codeanchor.LangCs, Kind: codeanchor.SymClass, File: "csharp/Worker.cs", Pkg: "Polyglot.Todo.Services", Name: "Worker"},
					{Lang: codeanchor.LangCs, Kind: codeanchor.SymMethod, File: "csharp/Worker.cs", Pkg: "Polyglot.Todo.Services.Worker", Name: "Run"},
				},
				Imports: []codeanchor.ImportEdge{
					{Module: "Polyglot.Todo.Models"},
				},
				TypeRefs: []codeanchor.TypeRef{
					{File: "csharp/Worker.cs", OwnerFQN: "Polyglot.Todo.Services.Worker.Run", TypeSym: codeanchor.SymbolRef{Lang: codeanchor.LangCs, Pkg: "Polyglot.Todo.Models", Name: "User"}},
				},
				MemberRefs: []codeanchor.MemberRef{
					{File: "csharp/Worker.cs", OwnerFQN: "Polyglot.Todo.Services.Worker.Run", Sym: codeanchor.SymbolRef{Lang: codeanchor.LangCs, Pkg: "Polyglot.Todo.Models.User", Name: "Primary"}},
				},
				Calls: []codeanchor.CallSite{
					{File: "csharp/Worker.cs", OwnerFQN: "Polyglot.Todo.Services.Worker.Run", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangCs, Pkg: "Polyglot.Todo.Models", Name: "User"}},
				},
			},
		},
	}}, codeanchor.WithBasePath(tmp))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangCs, filepath.Join(tmp, "csharp", "User.cs"), []byte("class User {}")))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangCs, filepath.Join(tmp, "csharp", "Worker.cs"), []byte("class Worker {}")))

	sqlDB, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	rows, err := sqlDB.QueryContext(ctx, `SELECT kind, COUNT(*) FROM intel_edges GROUP BY kind ORDER BY kind`)
	require.NoError(t, err)
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var kind string
		var count int
		require.NoError(t, rows.Scan(&kind, &count))
		counts[kind] = count
	}
	require.NoError(t, rows.Err())
	require.GreaterOrEqual(t, counts["calls"], 1)
	require.GreaterOrEqual(t, counts["imports"], 1)
	require.GreaterOrEqual(t, counts["member_ref"], 1)
	require.GreaterOrEqual(t, counts["type_ref"], 1)
}

func TestService_ApplyCodeIndexBatch_StoresRationaleOnParseError(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-batch-rationale.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{&summaryIndexer{
		lang: codeanchor.LangGo,
		summaries: map[string]codeanchor.FileSummary{
			"batch.go": {
				FilePath:    "batch.go",
				Lang:        codeanchor.LangGo,
				ParseStatus: codeanchor.ParseErrored,
			},
		},
	}}, codeanchor.WithBasePath(tmp))

	path := filepath.Join(tmp, "batch.go")
	work, err := svc.BuildCodeIndexWork(ctx, codeanchor.LangGo, path, []byte("// TODO: keep batch rationale\npackage main\n"))
	require.NoError(t, err)
	require.NotNil(t, work)
	require.False(t, work.ReplaceIndex)

	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*work}))

	rationale, err := store.RationaleForPath(ctx, "batch.go")
	require.NoError(t, err)
	require.Len(t, rationale, 1)
	require.Equal(t, codeanchor.RationaleTodo, rationale[0].Kind)
}

func TestService_BuildCodeIndexWork_BatchIndexingDefersResolvedIntelEdges(t *testing.T) {
	ctx := context.Background()
	store := newCountingIntelResolverStore(t)
	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{&summaryIndexer{
		lang: codeanchor.LangPy,
		summaries: map[string]codeanchor.FileSummary{
			"caller.py": {
				FilePath:    "caller.py",
				Lang:        codeanchor.LangPy,
				ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{
					{Name: "Caller", Kind: codeanchor.SymFunc, Pkg: "caller"},
				},
				Calls: []codeanchor.CallSite{
					{OwnerFQN: "caller.Caller", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "pkg.service", Name: "run"}},
				},
				TypeRefs: []codeanchor.TypeRef{
					{OwnerFQN: "caller.Caller", TypeSym: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "pkg.models", Name: "User"}},
				},
				MemberRefs: []codeanchor.MemberRef{
					{OwnerFQN: "caller.Caller", Sym: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "pkg.constants.Statuses", Name: "Active"}},
				},
				Imports: []codeanchor.ImportEdge{
					{Module: "pkg.models"},
				},
			},
		},
	}}, codeanchor.WithBasePath(t.TempDir()))

	normalWork, err := svc.BuildCodeIndexWork(ctx, codeanchor.LangPy, "caller.py", []byte("def caller(): pass\n"))
	require.NoError(t, err)
	require.NotNil(t, normalWork)
	require.NotZero(t, store.fqnCalls.Load(), "non-batch indexing should resolve eager intel edges")
	require.NotZero(t, store.moduleSuffixCalls.Load(), "non-batch indexing should resolve eager import edges")

	store.fqnCalls.Store(0)
	store.moduleSuffixCalls.Store(0)

	batchWork, err := svc.BuildCodeIndexWork(codeanchor.WithBatchIndexing(ctx), codeanchor.LangPy, "caller.py", []byte("def caller(): pass\n"))
	require.NoError(t, err)
	require.NotNil(t, batchWork)
	require.Zero(t, store.fqnCalls.Load(), "batch indexing should defer resolved call/type/member edges to rebuild")
	require.Zero(t, store.moduleSuffixCalls.Load(), "batch indexing should defer resolved import edges to rebuild")

	for _, edge := range batchWork.IntelEdges {
		require.Equal(t, "defines", edge.Kind)
	}
}

func TestService_IntelNoteExtraction_StoresSections(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "intel-note.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(tmp))

	_, err = svc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot("notes/foo.md", "# Foo\nbody\n## Bar\nmore", 456))
	require.NoError(t, err)

	sqlDB, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_doc_sections`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts`).Scan(&count))
	require.Equal(t, 2, count)
	var minUpdatedAt, maxUpdatedAt int64
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT MIN(updated_at), MAX(updated_at) FROM intel_doc_sections`).Scan(&minUpdatedAt, &maxUpdatedAt))
	require.Equal(t, int64(456), minUpdatedAt)
	require.Equal(t, minUpdatedAt, maxUpdatedAt)
}

func TestService_IntelNoteExtraction_RemovesStaleSectionsOnReingest(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "intel-note-stale.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(tmp))

	_, err = svc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot("notes/foo.md", "# Foo\nbody\n## Bar\nmore", 0))
	require.NoError(t, err)

	_, err = svc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot("notes/foo.md", "# Foo\nbody", 0))
	require.NoError(t, err)

	sqlDB, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_doc_sections`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestService_DeleteNote_RemovesIntelDocSections(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "intel-note-delete.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(tmp))
	path := filepath.Join(tmp, "notes/foo.md")

	_, err = svc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot("notes/foo.md", "# Foo\nbody\n## Bar\nmore", 0))
	require.NoError(t, err)

	require.NoError(t, svc.DeleteNote(ctx, path))

	sqlDB, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_doc_sections`).Scan(&count))
	require.Equal(t, 0, count)
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts`).Scan(&count))
	require.Equal(t, 0, count)
}

// Note: call-edge fallbacks are covered by indexer-level tests (parsing + symbol extraction).

func TestService_IntelNoteExtraction_StoresMentionsEdges(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "intel-note-mentions.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(tmp))

	sqlDB, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	// Seed a code anchor to resolve against (path is repo-relative under basePath).
	_, err = sqlDB.ExecContext(ctx, `
		INSERT INTO intel_code_anchors (
			anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment,
			start_byte, end_byte, start_line, end_line, fingerprint, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"a1",
		"go",
		"type",
		"pkg/embeddings/codeindex/types.go",
		"Item",
		"embeddings.codeindex.types.Item",
		"",
		"",
		0, 0, 0, 0,
		"fp",
		1,
	)
	require.NoError(t, err)

	_, err = svc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot("notes/foo.md", "# Foo\nSee @embeddings.codeindex.types.Item and [Item](pkg/embeddings/codeindex/types.go#Item)", 0))
	require.NoError(t, err)

	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges WHERE kind = 'mentions'`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestService_RebuildCallEdgesFromRefs(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-rebuild-refs.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "from app import callee\n\n\ndef run():\n    return callee.new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte(caller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerPath, []byte(caller)))

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, string(paths.NormalizeCode(callerRel)), nil))

	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "app.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, callerRel)
}

func TestService_RebuildCallEdgesForPaths_UsesRuntimeRefsForTouchedPaths(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-rebuild-runtime-refs.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "from app import callee\n\n\ndef run():\n    return callee.new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte(caller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	work, err := svc.BuildCodeIndexWork(codeanchor.WithBatchIndexing(ctx), codeanchor.LangPy, callerPath, []byte(caller))
	require.NoError(t, err)
	require.NotNil(t, work)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*work}))

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		callerRel: {},
	}))
	require.NoError(t, store.ReplaceIntelImportRefsForPathsBatch(ctx, map[string][]codeanchor.ImportRefRow{
		callerRel: {},
	}))
	require.NoError(t, store.ReplaceIntelModuleDefsForPathsBatch(ctx, map[string][]codeanchor.ModuleDefRow{
		callerRel: {},
	}))
	require.NoError(t, os.Remove(callerPath))

	svc.NoteRuntimeCodeIndexWorks([]codeanchor.CodeIndexWork{*work})
	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, string(paths.NormalizeCode(callerRel)), nil))
	var logOutput bytes.Buffer
	prevWriter := log.Writer()
	log.SetOutput(&logOutput)
	defer log.SetOutput(prevWriter)
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))
	require.NotContains(t, logOutput.String(), "RebuildCallEdgesForPaths:")

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "app.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, callerRel)
}

func TestService_RebuildCallEdgesForPaths_UsesInMemorySuffixIndex(t *testing.T) {
	rebuildCollector := indexingperf.New()
	rebuildCtx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), rebuildCollector), "rebuild_call_edges")
	tmp := t.TempDir()
	store := newCountingIntelResolverStore(t)

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{&summaryIndexer{
			lang: codeanchor.LangPy,
			summaries: map[string]codeanchor.FileSummary{
				"src/app/callee.py": {
					FilePath:    "src/app/callee.py",
					Lang:        codeanchor.LangPy,
					ParseStatus: codeanchor.ParseOK,
					Symbols: []codeanchor.Symbol{
						{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: "src/app/callee.py", Pkg: "root.pkg.callee", Name: "new_func"},
					},
				},
				"src/app/caller.py": {
					FilePath:    "src/app/caller.py",
					Lang:        codeanchor.LangPy,
					ParseStatus: codeanchor.ParseOK,
					Symbols: []codeanchor.Symbol{
						{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: "src/app/caller.py", Pkg: "app.caller", Name: "run"},
					},
					Calls: []codeanchor.CallSite{
						{File: "src/app/caller.py", OwnerFQN: "app.caller.run", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "pkg.callee", Name: "new_func"}},
					},
				},
			},
		}},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))
	require.NoError(t, os.WriteFile(calleePath, []byte("def new_func():\n    return 1\n"), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte("def run():\n    return new_func()\n"), 0o644))

	require.NoError(t, svc.IndexCodeFile(context.Background(), codeanchor.LangPy, calleePath, []byte("def new_func():\n    return 1\n")))
	require.NoError(t, svc.IndexCodeFile(context.Background(), codeanchor.LangPy, callerPath, []byte("def run():\n    return new_func()\n")))

	store.fqnCalls.Store(0)
	store.seedCalls.Store(0)
	require.NoError(t, store.UpsertIntelCallEdgesForPath(rebuildCtx, callerRel, nil))
	require.NoError(t, svc.RebuildCallEdgesForPaths(rebuildCtx, []string{callerRel}))

	pathsByCallee, err := store.CallFilesByCallee(rebuildCtx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "root.pkg.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Equal(t, []string{callerRel}, pathsByCallee)
	require.EqualValues(t, 1, store.fqnCalls.Load())
	require.EqualValues(t, 1, store.seedCalls.Load())

	window := rebuildCollector.RenderWindow(time.Second)
	require.Contains(t, window, "calledge_cache_build_suffix_seed_count=2")
	require.Contains(t, window, "calledge_cache_build_suffix_index_entries=1")
	require.Contains(t, window, "calledge_cache_build_suffix_call_candidates=1")
	require.Contains(t, window, "calledge_cache_build_suffix_candidates_py=1")
	require.Contains(t, window, "calledge_cache_build_fqn_lookup_suffix_requested=1")
	require.Contains(t, window, "calledge_cache_build_fqn_lookup_suffix_resolved=1")
}

func TestService_RebuildCallEdgesForPaths_BatchesAnchorLookupForStoredRefs(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store := newCountingAnchorLookupStore(t)

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "from app import callee\n\n\ndef run():\n    return callee.new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte(caller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerPath, []byte(caller)))

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, string(paths.NormalizeCode(callerRel)), nil))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))

	require.Equal(t, int64(1), store.pathsCalls.Load())
	require.Equal(t, int64(0), store.pathCalls.Load())
}

func TestService_RebuildCallEdgesForPaths_CachesResolverLookupsAcrossMixedSources(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "rebuild_call_edges")
	tmp := t.TempDir()
	store := newCountingIntelResolverStore(t)

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerOneRel := "src/app/caller_one.py"
	callerTwoRel := "src/app/caller_two.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerOnePath := filepath.Join(tmp, callerOneRel)
	callerTwoPath := filepath.Join(tmp, callerTwoRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerOnePath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "from app import callee\n\n\ndef run():\n    return callee.new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerOnePath, []byte(caller), 0o644))
	require.NoError(t, os.WriteFile(callerTwoPath, []byte(caller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerOnePath, []byte(caller)))
	work, err := svc.BuildCodeIndexWork(codeanchor.WithBatchIndexing(ctx), codeanchor.LangPy, callerTwoPath, []byte(caller))
	require.NoError(t, err)
	require.NotNil(t, work)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*work}))
	svc.NoteRuntimeCodeIndexWorks([]codeanchor.CodeIndexWork{*work})

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, callerOneRel, nil))
	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, callerTwoRel, nil))
	store.fqnCalls.Store(0)
	store.moduleSuffixCalls.Store(0)

	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerOneRel, callerTwoRel}))

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "app.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{callerOneRel, callerTwoRel}, pathsByCallee)
	require.EqualValues(t, 1, store.fqnCalls.Load())
	require.EqualValues(t, 1, store.moduleSuffixCalls.Load())

	window := collector.RenderWindow(time.Second)
	require.Contains(t, window, "calledge_direct_paths=1")
	require.Contains(t, window, "calledge_planned_paths=1")
	require.Contains(t, window, "calledge_runtime_paths=1")
	require.Contains(t, window, "calledge_stored_paths=1")
	require.Contains(t, window, "calledge_cache_fqn_entries=1")
	require.Contains(t, window, "calledge_cache_fqn_hits=2")
	require.Contains(t, window, "calledge_cache_import_misses=2")
	require.Contains(t, window, "calledge_cache_py_fallback_entries=1")
}

func TestService_RebuildCallEdgesForPaths_CachesRuntimeOnlyRebuildSources(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "rebuild_call_edges")
	tmp := t.TempDir()
	store := newCountingIntelResolverStore(t)

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "from app import callee\n\n\ndef run():\n    return callee.new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte(caller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	work, err := svc.BuildCodeIndexWork(codeanchor.WithBatchIndexing(ctx), codeanchor.LangPy, callerPath, []byte(caller))
	require.NoError(t, err)
	require.NotNil(t, work)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*work}))
	svc.NoteRuntimeCodeIndexWorks([]codeanchor.CodeIndexWork{*work})

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, callerRel, nil))
	store.fqnCalls.Store(0)
	store.moduleSuffixCalls.Store(0)

	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "app.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Equal(t, []string{callerRel}, pathsByCallee)
	require.EqualValues(t, 1, store.fqnCalls.Load())
	require.EqualValues(t, 1, store.moduleSuffixCalls.Load())

	window := collector.RenderWindow(time.Second)
	require.Contains(t, window, "calledge_runtime_paths=1")
	require.NotContains(t, window, "calledge_stored_paths=")
	require.Contains(t, window, "calledge_cache_fqn_entries=1")
	require.Contains(t, window, "calledge_cache_fqn_hits=1")
	require.Contains(t, window, "calledge_cache_import_misses=1")
	require.Contains(t, window, "calledge_cache_py_fallback_entries=1")
}

func TestService_RebuildCallEdgesForPaths_RuntimeRecoveredSummaryUsesRuntimeRefs(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "rebuild_call_edges")
	tmp := t.TempDir()
	store := newCountingIntelResolverStore(t)

	indexer := &summaryIndexer{
		lang: codeanchor.LangPy,
		summaries: map[string]codeanchor.FileSummary{
			"src/app/callee.py": {
				FilePath:    "src/app/callee.py",
				Lang:        codeanchor.LangPy,
				ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{
					{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: "src/app/callee.py", Pkg: "app.callee", Name: "new_func"},
				},
			},
			"src/app/caller.py": {
				FilePath:    "src/app/caller.py",
				Lang:        codeanchor.LangPy,
				ParseStatus: codeanchor.ParseRecovered,
				Calls: []codeanchor.CallSite{
					{File: "src/app/caller.py", OwnerFQN: "app.caller.run", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "app.callee", Name: "new_func"}},
				},
				Imports: []codeanchor.ImportEdge{{Module: "app.callee"}},
			},
		},
	}
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))
	require.NoError(t, os.WriteFile(calleePath, []byte("def new_func():\n    return 1\n"), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte("def run():\n    return new_func()\n"), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte("def new_func():\n    return 1\n")))
	work, err := svc.BuildCodeIndexWork(codeanchor.WithBatchIndexing(ctx), codeanchor.LangPy, callerPath, []byte("def run():\n    return new_func()\n"))
	require.NoError(t, err)
	require.NotNil(t, work)
	require.Equal(t, codeanchor.ParseRecovered, work.Summary.ParseStatus)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*work}))
	svc.NoteRuntimeCodeIndexWorks([]codeanchor.CodeIndexWork{*work})

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, callerRel, nil))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "app.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Equal(t, []string{callerRel}, pathsByCallee)

	window := collector.RenderWindow(time.Second)
	require.Contains(t, window, "calledge_runtime_paths=1")
	require.NotContains(t, window, "calledge_source_fallback_runtime_parse_untrusted=")
}

func TestService_RebuildCallEdgesForPaths_SkipsPythonUnqualifiedFQNResolverLookups(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "rebuild_call_edges")
	tmp := t.TempDir()
	store := newCountingIntelResolverStore(t)

	indexer := &summaryIndexer{
		lang: codeanchor.LangPy,
		summaries: map[string]codeanchor.FileSummary{
			"src/app/callee.py": {
				FilePath:    "src/app/callee.py",
				Lang:        codeanchor.LangPy,
				ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{
					{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: "src/app/callee.py", Pkg: "app.callee", Name: "new_func"},
				},
			},
			"src/app/caller.py": {
				FilePath:    "src/app/caller.py",
				Lang:        codeanchor.LangPy,
				ParseStatus: codeanchor.ParseOK,
				Calls: []codeanchor.CallSite{
					{File: "src/app/caller.py", OwnerFQN: "app.caller.run", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Name: "new_func"}},
				},
			},
		},
	}
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))
	require.NoError(t, os.WriteFile(calleePath, []byte("def new_func():\n    return 1\n"), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte("def run():\n    return new_func()\n"), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte("def new_func():\n    return 1\n")))
	work, err := svc.BuildCodeIndexWork(codeanchor.WithBatchIndexing(ctx), codeanchor.LangPy, callerPath, []byte("def run():\n    return new_func()\n"))
	require.NoError(t, err)
	require.NotNil(t, work)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*work}))
	svc.NoteRuntimeCodeIndexWorks([]codeanchor.CodeIndexWork{*work})
	store.fqnCalls.Store(0)

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, callerRel, nil))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "app.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Equal(t, []string{callerRel}, pathsByCallee)
	require.Zero(t, store.fqnCalls.Load())

	window := collector.RenderWindow(time.Second)
	require.Contains(t, window, "calledge_cache_build_py_fallback_candidates=1")
	require.Contains(t, window, "calledge_cache_build_fqn_skipped_py_unqualified=1")
}

func TestService_RebuildCallEdgesForPaths_SkipsPythonBuiltinsFallback(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-builtins-rebuild.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{&summaryIndexer{
		lang: codeanchor.LangPy,
		summaries: map[string]codeanchor.FileSummary{
			"src/app/helpers.py": {
				FilePath:    "src/app/helpers.py",
				Lang:        codeanchor.LangPy,
				ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{
					{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: "src/app/helpers.py", Pkg: "app.helpers", Name: "print"},
				},
			},
			"src/app/caller.py": {
				FilePath:    "src/app/caller.py",
				Lang:        codeanchor.LangPy,
				ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{
					{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: "src/app/caller.py", Pkg: "app.caller", Name: "run"},
				},
				Calls: []codeanchor.CallSite{
					{
						File:     "src/app/caller.py",
						OwnerFQN: "app.caller.run",
						CalleeSymbol: codeanchor.SymbolRef{
							Lang: codeanchor.LangPy,
							Pkg:  "builtins",
							Name: "print",
						},
					},
				},
			},
		},
	}}, codeanchor.WithBasePath(tmp), codeanchor.WithWriteAccess())

	helperRel := "src/app/helpers.py"
	callerRel := "src/app/caller.py"
	helperPath := filepath.Join(tmp, helperRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))
	require.NoError(t, os.WriteFile(helperPath, []byte("def print():\n    return 1\n"), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte("def run():\n    return print('x')\n"), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, helperPath, []byte("def print():\n    return 1\n")))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerPath, []byte("def run():\n    return print('x')\n")))

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, string(paths.NormalizeCode(callerRel)), nil))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))

	sqlDB, err := sql.Open("sqlite3", filepath.Join(tmp, "intel-builtins-rebuild.db"))
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	var count int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_edges e
		JOIN intel_code_anchors dst ON dst.id = e.dst_row_id
		WHERE e.kind = 'calls'
		  AND e.dst_type = 'anchor'
		  AND dst.fqn = 'app.helpers.print'
	`).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count, "builtins-qualified calls should not fall back to same-dir symbols during rebuild")
}

func TestService_RebuildCallEdgesForPaths_SourceFallbackScenariosDoNotInventEdges(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-rebuild-fallback-reasons.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	runtimeParseRel := "src/app/runtime_parse_untrusted.py"
	runtimeMissingRel := "src/app/runtime_missing_refs.py"
	sourceOnlyRel := "src/other/source_only.py"
	validRel := "src/app/valid.py"
	targetRel := "src/app/helpers.py"
	indexer := &summaryIndexer{
		lang: codeanchor.LangPy,
		summaries: map[string]codeanchor.FileSummary{
			runtimeParseRel: {
				FilePath: runtimeParseRel, Lang: codeanchor.LangPy, ParseStatus: codeanchor.ParseErrored,
				Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: runtimeParseRel, Pkg: "app.runtime_parse_untrusted", Name: "run"}},
				Calls:   []codeanchor.CallSite{{File: runtimeParseRel, OwnerFQN: "app.runtime_parse_untrusted.run", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "app.helpers", Name: "broken"}}},
			},
			runtimeMissingRel: {
				FilePath: runtimeMissingRel, Lang: codeanchor.LangPy, ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: runtimeMissingRel, Pkg: "app.runtime_missing_refs", Name: "run"}},
				Calls:   []codeanchor.CallSite{{File: runtimeMissingRel, OwnerFQN: "app.runtime_missing_refs.run", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangCs, Pkg: "app.helpers", Name: "missing_ref"}}},
			},
			sourceOnlyRel: {
				FilePath: sourceOnlyRel, Lang: codeanchor.LangPy, ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: sourceOnlyRel, Pkg: "app.source_only", Name: "run"}},
				Calls:   []codeanchor.CallSite{{File: sourceOnlyRel, OwnerFQN: "app.source_only.run", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "app.wrong", Name: "source_only"}}},
			},
			targetRel: {
				FilePath: targetRel, Lang: codeanchor.LangPy, ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: targetRel, Pkg: "app.helpers", Name: "valid"}, {Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: targetRel, Pkg: "app.helpers", Name: "broken"}, {Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: targetRel, Pkg: "app.helpers", Name: "missing_ref"}, {Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: targetRel, Pkg: "app.helpers", Name: "source_only"}},
			},
			validRel: {
				FilePath: validRel, Lang: codeanchor.LangPy, ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: validRel, Pkg: "app.valid", Name: "run"}},
				Calls:   []codeanchor.CallSite{{File: validRel, OwnerFQN: "app.valid.run", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "app.helpers", Name: "valid"}}},
			},
		},
	}
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	files := map[string]string{
		runtimeParseRel:   "def run():\n    return broken(\n",
		runtimeMissingRel: "def run():\n    return missing_ref()\n",
		sourceOnlyRel:     "def run():\n    return source_only()\n",
		validRel:          "def run():\n    return valid()\n",
		targetRel:         "def valid(): pass\ndef broken(): pass\ndef missing_ref(): pass\ndef source_only(): pass\n",
	}
	for rel, body := range files {
		abs := filepath.Join(tmp, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(body), 0o644))
		require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, abs, []byte(body)))
		anchors, err := store.IntelAnchorsByPath(ctx, rel)
		require.NoError(t, err)
		require.NotEmpty(t, anchors, "source or target must exist in durable graph: %s", rel)
	}

	runtimeWorks := map[string]codeanchor.CodeIndexWork{
		runtimeParseRel: {
			Path: runtimeParseRel,
			Summary: codeanchor.FileSummary{
				FilePath:    runtimeParseRel,
				Lang:        codeanchor.LangPy,
				ParseStatus: codeanchor.ParseErrored,
				Symbols:     []codeanchor.Symbol{{Lang: codeanchor.LangPy, Kind: codeanchor.SymFunc, File: runtimeParseRel, Pkg: "app.runtime_parse_untrusted", Name: "run"}},
				Calls:       []codeanchor.CallSite{{File: runtimeParseRel, OwnerFQN: "app.runtime_parse_untrusted.run", CalleeSymbol: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "app.helpers", Name: "broken"}}},
			},
		},
		runtimeMissingRel: {
			Path: runtimeMissingRel,
			Summary: codeanchor.FileSummary{
				FilePath:    runtimeMissingRel,
				Lang:        codeanchor.LangPy,
				ParseStatus: codeanchor.ParseOK,
			},
		},
	}

	sqlDB, err := sql.Open("sqlite3", filepath.Join(tmp, "intel-rebuild-fallback-reasons.db"))
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	runRebuild := func(path string, runtimeWork codeanchor.CodeIndexWork, hasRuntimeWork bool) {
		if hasRuntimeWork {
			svc.NoteRuntimeCodeIndexWorks([]codeanchor.CodeIndexWork{runtimeWork})
		}
		require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, path, nil))
		require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{path}))

		var count int
		err := sqlDB.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM intel_edges e
			JOIN intel_code_anchors src ON src.id = e.src_row_id
			WHERE e.kind = 'calls'
			  AND src.path = ?
		`, string(paths.NormalizeCode(path))).Scan(&count)
		require.NoError(t, err)
		require.Zero(t, count, "fallback rebuild should not invent call edges for unresolved source")
	}

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, validRel, nil))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{validRel}))
	var positive int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM intel_edges e
		JOIN intel_code_anchors src ON src.id = e.src_row_id
		JOIN intel_code_anchors dst ON dst.id = e.dst_row_id
		WHERE e.kind = 'calls' AND src.path = ? AND dst.fqn = 'app.helpers.valid'
	`, validRel).Scan(&positive))
	require.Positive(t, positive, "same rebuild must resolve an eligible target")

	runRebuild(runtimeParseRel, runtimeWorks[runtimeParseRel], true)
	runRebuild(runtimeMissingRel, runtimeWorks[runtimeMissingRel], true)
	runRebuild(sourceOnlyRel, codeanchor.CodeIndexWork{}, false)
}

func TestService_RebuildCallEdgesForPaths_RestoresCSharpMemberRefEdges(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "intel-rebuild-csharp-member-refs.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{&summaryIndexer{
		lang: codeanchor.LangCs,
		summaries: map[string]codeanchor.FileSummary{
			"csharp/EventCategoryTypes.cs": {
				FilePath:    "csharp/EventCategoryTypes.cs",
				Lang:        codeanchor.LangCs,
				ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{
					{Lang: codeanchor.LangCs, Kind: codeanchor.SymClass, File: "csharp/EventCategoryTypes.cs", Pkg: "Polyglot.Todo.Constants", Name: "EventCategoryTypes"},
					{Lang: codeanchor.LangCs, Kind: codeanchor.SymField, File: "csharp/EventCategoryTypes.cs", Pkg: "Polyglot.Todo.Constants.EventCategoryTypes", Name: "OTJ"},
				},
			},
			"csharp/Worker.cs": {
				FilePath:    "csharp/Worker.cs",
				Lang:        codeanchor.LangCs,
				ParseStatus: codeanchor.ParseOK,
				Symbols: []codeanchor.Symbol{
					{Lang: codeanchor.LangCs, Kind: codeanchor.SymClass, File: "csharp/Worker.cs", Pkg: "Polyglot.Todo.Services", Name: "Worker"},
					{Lang: codeanchor.LangCs, Kind: codeanchor.SymMethod, File: "csharp/Worker.cs", Pkg: "Polyglot.Todo.Services.Worker", Name: "Run"},
				},
				MemberRefs: []codeanchor.MemberRef{
					{
						File:     "csharp/Worker.cs",
						OwnerFQN: "Polyglot.Todo.Services.Worker.Run",
						Sym: codeanchor.SymbolRef{
							Lang: codeanchor.LangCs,
							Pkg:  "Todo.Constants.EventCategoryTypes",
							Name: "OTJ",
						},
					},
				},
			},
		},
	}}, codeanchor.WithBasePath(tmp), codeanchor.WithWriteAccess())

	constsRel := "csharp/EventCategoryTypes.cs"
	callerRel := "csharp/Worker.cs"
	constsPath := filepath.Join(tmp, constsRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(constsPath), 0o755))
	require.NoError(t, os.WriteFile(constsPath, []byte("class EventCategoryTypes {}"), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte("class Worker {}"), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangCs, constsPath, []byte("class EventCategoryTypes {}")))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangCs, callerPath, []byte("class Worker {}")))

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, string(paths.NormalizeCode(callerRel)), nil))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))

	sqlDB, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	var count int
	err = sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_edges e
		JOIN intel_code_anchors dst ON dst.id = e.dst_row_id
		WHERE e.kind = 'member_ref'
		  AND e.dst_type = 'anchor'
		  AND dst.fqn = 'Polyglot.Todo.Constants.EventCategoryTypes.OTJ'
	`).Scan(&count)
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, 1)

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, string(paths.NormalizeCode(callerRel)), nil))
	require.NoError(t, svc.RebuildAllCallEdges(ctx))

	count = 0
	err = sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_edges e
		JOIN intel_code_anchors dst ON dst.id = e.dst_row_id
		WHERE e.kind = 'member_ref'
		  AND e.dst_type = 'anchor'
		  AND dst.fqn = 'Polyglot.Todo.Constants.EventCategoryTypes.OTJ'
	`).Scan(&count)
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, 1)
}

func TestService_RebuildAllCallEdgesReturnsWriteErrorWithoutDeadlock(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	baseStore, err := sqlite.Open(filepath.Join(tmp, "intel-rebuild-write-error.db"))
	require.NoError(t, err)
	defer func() { _ = baseStore.Close() }()

	writeErr := errors.New("forced call-edge batch write failure")
	store := &failingCallEdgeBatchStore{Store: baseStore, err: writeErr}
	summaries := map[string]codeanchor.FileSummary{
		"pkg/target.go": {
			FilePath:    "pkg/target.go",
			Lang:        codeanchor.LangGo,
			ParseStatus: codeanchor.ParseOK,
			Symbols: []codeanchor.Symbol{
				{Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: "pkg/target.go", Pkg: "pkg", Name: "Target"},
			},
		},
	}
	const callerCount = 520
	for i := 0; i < callerCount; i++ {
		rel := fmt.Sprintf("pkg/caller_%03d.go", i)
		name := fmt.Sprintf("Caller%d", i)
		summaries[rel] = codeanchor.FileSummary{
			FilePath:    rel,
			Lang:        codeanchor.LangGo,
			ParseStatus: codeanchor.ParseOK,
			Symbols: []codeanchor.Symbol{
				{Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: rel, Pkg: "pkg", Name: name},
			},
			Calls: []codeanchor.CallSite{
				{
					File:     rel,
					OwnerFQN: "pkg." + name,
					CalleeSymbol: codeanchor.SymbolRef{
						Lang: codeanchor.LangGo,
						Pkg:  "pkg",
						Name: "Target",
					},
				},
			},
		}
	}
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{&summaryIndexer{lang: codeanchor.LangGo, summaries: summaries}},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	works := make([]codeanchor.CodeIndexWork, 0, len(summaries))
	for rel, summary := range summaries {
		absPath := filepath.Join(tmp, rel)
		work, buildErr := svc.BuildCodeIndexWork(codeanchor.WithBatchIndexing(ctx), codeanchor.LangGo, absPath, []byte("package pkg\n"))
		require.NoError(t, buildErr, summary.FilePath)
		require.NotNil(t, work, summary.FilePath)
		works = append(works, *work)
	}
	require.NoError(t, svc.ApplyCodeIndexBatch(codeanchor.WithBatchIndexing(ctx), works))
	svc.NoteRuntimeCodeIndexWorks(works)

	rebuildCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err = svc.RebuildAllCallEdges(rebuildCtx)
	require.ErrorIs(t, err, writeErr)
	require.GreaterOrEqual(t, store.calls.Load(), int64(1))
	require.NoError(t, rebuildCtx.Err())
}

func TestService_RebuildCallEdgesForPathsReturnsWriteErrorWithoutDeadlock(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	baseStore, err := sqlite.Open(filepath.Join(tmp, "intel-rebuild-paths-write-error.db"))
	require.NoError(t, err)
	defer func() { _ = baseStore.Close() }()

	writeErr := errors.New("forced targeted call-edge batch write failure")
	store := &failingCallEdgeBatchStore{Store: baseStore, err: writeErr}
	summaries := map[string]codeanchor.FileSummary{
		"pkg/target.go": {
			FilePath:    "pkg/target.go",
			Lang:        codeanchor.LangGo,
			ParseStatus: codeanchor.ParseOK,
			Symbols: []codeanchor.Symbol{
				{Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: "pkg/target.go", Pkg: "pkg", Name: "Target"},
			},
		},
	}
	pathsToRebuild := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		rel := fmt.Sprintf("pkg/targeted_caller_%03d.go", i)
		name := fmt.Sprintf("TargetedCaller%d", i)
		pathsToRebuild = append(pathsToRebuild, rel)
		summaries[rel] = codeanchor.FileSummary{
			FilePath:    rel,
			Lang:        codeanchor.LangGo,
			ParseStatus: codeanchor.ParseOK,
			Symbols: []codeanchor.Symbol{
				{Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: rel, Pkg: "pkg", Name: name},
			},
			Calls: []codeanchor.CallSite{
				{
					File:     rel,
					OwnerFQN: "pkg." + name,
					CalleeSymbol: codeanchor.SymbolRef{
						Lang: codeanchor.LangGo,
						Pkg:  "pkg",
						Name: "Target",
					},
				},
			},
		}
	}
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{&summaryIndexer{lang: codeanchor.LangGo, summaries: summaries}},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "pkg"), 0o755))
	for rel, summary := range summaries {
		absPath := filepath.Join(tmp, rel)
		require.NoError(t, os.WriteFile(absPath, []byte("package pkg\n"), 0o644))
		require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, absPath, []byte("package pkg\n")), summary.FilePath)
	}

	rebuildCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err = svc.RebuildCallEdgesForPaths(rebuildCtx, pathsToRebuild)
	require.ErrorIs(t, err, writeErr)
	require.GreaterOrEqual(t, store.calls.Load(), int64(1))
	require.NoError(t, rebuildCtx.Err())
}

func TestService_RebuildCallEdgesPreservesExistingOnParseFallbackWithoutSignals(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-rebuild-preserve.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "from app import callee\n\n\ndef run():\n    return callee.new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte(caller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerPath, []byte(caller)))

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		callerRel: {},
	}))
	require.NoError(t, store.ReplaceIntelImportRefsForPathsBatch(ctx, map[string][]codeanchor.ImportRefRow{
		callerRel: {},
	}))
	require.NoError(t, store.ReplaceIntelModuleDefsForPathsBatch(ctx, map[string][]codeanchor.ModuleDefRow{
		callerRel: {},
	}))

	badCaller := "from app import (\n"
	require.NoError(t, os.WriteFile(callerPath, []byte(badCaller), 0o644))

	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "app.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, callerRel)
}

func TestService_RebuildAllCallEdgesFromRefsWithoutReparse(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-rebuild-all-refs.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "from app import callee\n\n\ndef run():\n    return callee.new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte(caller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerPath, []byte(caller)))

	require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, string(paths.NormalizeCode(callerRel)), nil))
	require.NoError(t, os.Remove(calleePath))
	require.NoError(t, os.Remove(callerPath))

	var logOutput bytes.Buffer
	prevWriter := log.Writer()
	log.SetOutput(&logOutput)
	defer log.SetOutput(prevWriter)
	require.NoError(t, svc.RebuildAllCallEdges(ctx))
	require.NotContains(t, logOutput.String(), "RebuildAllCallEdges:")

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "app.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, callerRel)
}

func TestService_TargetedDeferredRebuildPreservesPHPMemberCalls(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "php-member-rebuild.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPHPIndexer()},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)
	files := map[string]string{
		"php/SyncClient.php": `<?php
namespace Polyglot\Todo;
class SyncClient { public function pushUpdates(string $payload): void {} }
`,
		"php/Worker.php": `<?php
namespace Polyglot\Todo;
class Worker { public function run(): void { $client = new SyncClient(); $client->pushUpdates("worker"); } }
`,
		"php/Controller.php": `<?php
namespace Polyglot\Todo;
class Controller {
    public function __construct(private SyncClient $client) {}
    public function store(): void { $this->client->pushUpdates("controller"); }
}

`,
	}
	for rel, content := range files {
		path := filepath.Join(tmp, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPhp, path, []byte(content)))
	}

	for _, rel := range []string{"php/Worker.php", "php/Controller.php"} {
		require.NoError(t, store.UpsertIntelCallEdgesForPath(ctx, rel, nil))
	}
	require.NoError(t, svc.RebuildParserCallEdgesForPaths(ctx, []string{"php/Worker.php", "php/Controller.php"}))

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPhp, Pkg: `Polyglot\Todo\SyncClient`, Name: "pushUpdates", Member: true,
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, "php/Worker.php")
	require.Contains(t, pathsByCallee, "php/Controller.php")
}

func TestService_ReopenAndPlanPHPMemberDefinitionDelta(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "php-member-plan.db")
	indexer := codeanchor.NewPHPIndexer()
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{indexer}, codeanchor.WithBasePath(tmp), codeanchor.WithWriteAccess())

	calleeRel := "php/SyncClient.php"
	callerRel := "php/Worker.php"
	oldCallee := []byte("<?php\nnamespace Polyglot\\Todo;\nclass SyncClient {}\n")
	newCallee := []byte("<?php\nnamespace Polyglot\\Todo;\nclass SyncClient { public function pushUpdates(string $payload): void {} }\n")
	caller := []byte("<?php\nnamespace Polyglot\\Todo;\nclass Worker { public function run(): void { $client = new SyncClient(); $client->pushUpdates(\"worker\"); } }\n")
	for rel, content := range map[string][]byte{calleeRel: oldCallee, callerRel: caller} {
		path := filepath.Join(tmp, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, content, 0o644))
		require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPhp, path, content))
	}
	rows, err := store.SymbolRefsByPaths(ctx, []string{callerRel})
	require.NoError(t, err)
	require.Contains(t, rows[callerRel], codeanchor.SymbolRefRow{
		SrcPath: callerRel, OwnerFQN: `Polyglot\Todo\Worker::run`, RefKind: codeanchor.RefKindCalls,
		DstLang: codeanchor.LangPhp, DstPkg: `Polyglot\Todo\SyncClient`, DstName: "pushUpdates", DstFQN: `Polyglot\Todo\SyncClient::pushUpdates`, DstMember: true,
	})
	require.NoError(t, store.Close())

	store, err = sqlite.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	svc = codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{indexer}, codeanchor.WithBasePath(tmp), codeanchor.WithWriteAccess())
	oldSummary, err := indexer.IndexFile(oldCallee, paths.CodePathRef{Rel: paths.NormalizeCode(calleeRel)})
	require.NoError(t, err)
	newSummary, err := indexer.IndexFile(newCallee, paths.CodePathRef{Rel: paths.NormalizeCode(calleeRel)})
	require.NoError(t, err)
	added, removed := codeanchor.DiffSymbolRefs(codeanchor.SymbolRefsFromSymbols(oldSummary.Symbols), codeanchor.SymbolRefsFromSymbols(newSummary.Symbols))
	require.Empty(t, removed)
	require.Contains(t, added, codeanchor.SymbolRef{Lang: codeanchor.LangPhp, Pkg: `Polyglot\Todo\SyncClient`, Name: "pushUpdates", Member: true})

	plan, err := svc.PlanCallEdgeRebuildForDefDeltas(ctx, []string{calleeRel}, codeanchor.DefDeltas{AddedSymbols: added})
	require.NoError(t, err)
	require.Contains(t, plan.Paths, callerRel)
	require.Equal(t, 1, plan.Summary.ImpactedCallers)
}

func TestService_DefDeltas_PythonDirCollisionGuard(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-def-delta-guard.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/pkg/one/callee.py"
	callerSameRel := "src/pkg/one/caller.py"
	callerOtherRel := "src/pkg/two/caller.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerSamePath := filepath.Join(tmp, callerSameRel)
	callerOtherPath := filepath.Join(tmp, callerOtherRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerSamePath), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(callerOtherPath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "def run():\n    return new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerSamePath, []byte(caller), 0o644))
	require.NoError(t, os.WriteFile(callerOtherPath, []byte(caller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerSamePath, []byte(caller)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerOtherPath, []byte(caller)))

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"src/pkg/two/caller.go": {{
			SrcPath:  "src/pkg/two/caller.go",
			OwnerFQN: "",
			RefKind:  codeanchor.RefKindCalls,
			DstLang:  codeanchor.LangGo,
			DstPkg:   "",
			DstName:  "new_func",
			DstFQN:   "new_func",
		}},
	}))

	deltas := codeanchor.DefDeltas{
		AddedSymbols: []codeanchor.SymbolRef{{
			Lang: codeanchor.LangPy,
			Pkg:  "pkg.one.callee",
			Name: "new_func",
		}},
	}

	summary, err := svc.RebuildCallEdgesForDefDeltas(ctx, nil, deltas)
	require.NoError(t, err)
	require.Equal(t, 1, summary.ImpactedCallers)
}

func TestService_PlanCallEdgeRebuildIncremental_ReturnsReadyPathsAndResidual(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-incremental-plan.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerOneRel := "src/app/caller_one.py"
	callerTwoRel := "src/app/caller_two.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerOnePath := filepath.Join(tmp, callerOneRel)
	callerTwoPath := filepath.Join(tmp, callerTwoRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerOnePath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "from app import callee\n\n\ndef run():\n    return callee.new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerOnePath, []byte(caller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerOnePath, []byte(caller)))

	deltas := codeanchor.DefDeltas{
		AddedSymbols: []codeanchor.SymbolRef{{
			Lang: codeanchor.LangPy,
			Pkg:  "app.callee",
			Name: "new_func",
		}},
	}

	plan, err := svc.PlanCallEdgeRebuildIncremental(ctx, nil, deltas, codeanchor.CallEdgeResidual{}, false)
	require.NoError(t, err)
	require.Empty(t, plan.Paths)
	require.True(t, plan.Residual.HasPending())
	require.Equal(t, []codeanchor.SymbolRef{{
		Lang: codeanchor.LangPy,
		Pkg:  "app.callee",
		Name: "new_func",
	}}, plan.Residual.SymbolRefs)
	require.Len(t, plan.Residual.Fallbacks, 1)

	require.NoError(t, os.WriteFile(callerTwoPath, []byte(caller), 0o644))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerTwoPath, []byte(caller)))

	plan, err = svc.PlanCallEdgeRebuildIncremental(ctx, nil, codeanchor.DefDeltas{}, plan.Residual, false)
	require.NoError(t, err)
	require.Empty(t, plan.Paths)
	require.True(t, plan.Residual.HasPending())
}

func TestService_PlanCallEdgeRebuildIncremental_FinalDrainClearsResidual(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-final-drain-plan.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	calleeRel := "src/app/callee.py"
	callerRel := "src/app/caller.py"
	removedCallerRel := "src/app/caller_removed.py"
	calleePath := filepath.Join(tmp, calleeRel)
	callerPath := filepath.Join(tmp, callerRel)
	removedCallerPath := filepath.Join(tmp, removedCallerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	callee := "def new_func():\n    return 1\n"
	caller := "from app import callee\n\n\ndef run():\n    return callee.new_func()\n"
	require.NoError(t, os.WriteFile(calleePath, []byte(callee), 0o644))
	require.NoError(t, os.WriteFile(callerPath, []byte(caller), 0o644))
	removedCaller := "from app import callee\n\n\ndef run():\n    return callee.old_func()\n"
	require.NoError(t, os.WriteFile(removedCallerPath, []byte(removedCaller), 0o644))

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, calleePath, []byte(callee)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, callerPath, []byte(caller)))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, removedCallerPath, []byte(removedCaller)))

	residual := codeanchor.CallEdgeResidual{
		SymbolRefs: []codeanchor.SymbolRef{{
			Lang: codeanchor.LangPy,
			Pkg:  "app.callee",
			Name: "new_func",
		}},
		Fallbacks: []codeanchor.ReverseIndexFallback{{
			Ref: codeanchor.SymbolRef{Lang: codeanchor.LangPy, Name: "new_func"},
		}},
	}

	plan, err := svc.PlanCallEdgeRebuildIncremental(ctx, nil, codeanchor.DefDeltas{RemovedSymbols: []codeanchor.SymbolRef{{Lang: codeanchor.LangPy, Pkg: "app.callee", Name: "old_func"}}}, residual, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{callerRel, removedCallerRel}, plan.Paths)
	require.False(t, plan.Residual.HasPending())
}

func TestMergeCallEdgeResidual_DedupesFallbacks(t *testing.T) {
	merged := codeanchor.MergeCallEdgeResidual(
		codeanchor.CallEdgeResidual{
			SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
			Modules:    []string{"pkg/a"},
			Fallbacks: []codeanchor.ReverseIndexFallback{{
				Ref: codeanchor.SymbolRef{Lang: codeanchor.LangGo, Name: "Run"},
			}},
		},
		codeanchor.CallEdgeResidual{
			SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
			Modules:    []string{"pkg/a", "pkg/b"},
			Fallbacks: []codeanchor.ReverseIndexFallback{{
				Ref: codeanchor.SymbolRef{Lang: codeanchor.LangGo, Name: "Run"},
			}},
		},
	)

	require.Equal(t, []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}}, merged.SymbolRefs)
	require.Equal(t, []string{"pkg/a", "pkg/b"}, merged.Modules)
	require.Len(t, merged.Fallbacks, 1)
}

func TestService_PlanCallEdgeRebuildIncremental_SkipsReferrerLookupsUntilBackfillComplete(t *testing.T) {
	ctx := context.Background()
	inner, err := sqlite.Open(filepath.Join(t.TempDir(), "intel-skip-midrun-lookups.db"))
	require.NoError(t, err)
	defer func() { _ = inner.Close() }()

	store := &countingRefStore{Store: inner}
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithWriteAccess())

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"src/caller.py": {{
			SrcPath: "src/caller.py",
			RefKind: codeanchor.RefKindCalls,
			DstLang: codeanchor.LangPy,
			DstPkg:  "app.core",
			DstName: "Run",
			DstFQN:  "app.core.Run",
		}},
	}))

	plan, err := svc.PlanCallEdgeRebuildIncremental(ctx, nil, codeanchor.DefDeltas{
		AddedSymbols: []codeanchor.SymbolRef{{
			Lang: codeanchor.LangPy,
			Pkg:  "app.core",
			Name: "Run",
		}},
	}, codeanchor.CallEdgeResidual{}, false)
	require.NoError(t, err)
	require.Empty(t, plan.Paths)
	require.True(t, plan.Residual.HasPending())
	require.Zero(t, store.symbolCalls.Load())
}
