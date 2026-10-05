package codeanchor

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

type stubStore struct {
	mu                sync.RWMutex
	fileSymbols       map[string][]string
	ancestors         map[string][]string
	annotations       map[string][]AnnotationUse
	calls             map[string][]CallSite
	callFilesByCallee map[string][]string
	staleCallPaths    []string
	staleCallCalls    int
	anchors           []Anchor
	notesByAnchor     map[int64][]Note
	scopeByAnchor     map[int64]struct {
		syms  []string
		files []string
	}
	suffixMatches  map[string][]Symbol // suffix -> matching symbols
	existingSymFQN map[string]bool     // fqn -> exists (for SymbolExistsByFQN)
	suffixError    error
	symbolFQNError error
	replaceCount   int
	metaCount      int
	lastMeta       FileMeta
	lastSummary    FileSummary
}

func (s *stubStore) ReplaceFileSummary(ctx context.Context, summary FileSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceCount++
	s.lastSummary = summary
	return nil
}
func (s *stubStore) UpsertFileMeta(ctx context.Context, meta FileMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metaCount++
	s.lastMeta = meta
	return nil
}
func (s *stubStore) UpsertNote(ctx context.Context, note Note) error { return nil }
func (s *stubStore) UpsertNoteWithCleanup(ctx context.Context, note Note, keepLabels []string) (AnchorUpsertResult, error) {
	// Stub: treat all anchors as new for test purposes.
	var result AnchorUpsertResult
	for _, a := range note.DefinedAnchors {
		result.NewIDs = append(result.NewIDs, a.ID)
	}
	return result, s.UpsertNote(ctx, note)
}
func (s *stubStore) Anchors(ctx context.Context) ([]Anchor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Anchor, len(s.anchors))
	copy(out, s.anchors)
	return out, nil
}
func (s *stubStore) AnchorsByIDs(ctx context.Context, ids []int64) ([]Anchor, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Anchor
	for _, a := range s.anchors {
		if want[a.ID] {
			out = append(out, a)
		}
	}
	return out, nil
}
func (s *stubStore) AnchorsByNotePaths(ctx context.Context, paths []string) (map[string][]Anchor, error) {
	return map[string][]Anchor{}, nil
}
func (s *stubStore) AnchorsBySymbols(ctx context.Context, symbols []string) (map[int64][]string, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	symSet := make(map[string]bool, len(symbols))
	for _, sym := range symbols {
		symSet[sym] = true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int64][]string)
	for id, scope := range s.scopeByAnchor {
		for _, sym := range scope.syms {
			if symSet[sym] {
				out[id] = append(out[id], sym)
			}
		}
	}
	return out, nil
}
func (s *stubStore) AnchorsByCallFiles(ctx context.Context, files []string) ([]int64, error) {
	if len(files) == 0 {
		return nil, nil
	}
	fileSet := make(map[string]bool, len(files))
	for _, f := range files {
		fileSet[f] = true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []int64
	for id, scope := range s.scopeByAnchor {
		for _, f := range scope.files {
			if fileSet[f] {
				ids = append(ids, id)
				break
			}
		}
	}
	return ids, nil
}
func (s *stubStore) AnchorsByPathPrefix(ctx context.Context, target string) ([]int64, error) {
	// Existing tests use a pure scope-based stub; directory anchors are tested via sqlite integration.
	return nil, nil
}
func (s *stubStore) AnchorsByGlobMatch(ctx context.Context, target string) ([]int64, error) {
	// Glob anchors are exercised in sqlite integration tests.
	return nil, nil
}
func (s *stubStore) AllAnchorScopes(ctx context.Context) (map[int64]AnchorScope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int64]AnchorScope)
	for id, scope := range s.scopeByAnchor {
		out[id] = AnchorScope{Symbols: scope.syms, Calls: scope.files}
	}
	return out, nil
}
func (s *stubStore) NotePaths(ctx context.Context) ([]string, error) { return nil, nil }
func (s *stubStore) NotesForAnchor(ctx context.Context, anchorID int64) ([]Note, error) {
	return s.notesByAnchor[anchorID], nil
}
func (s *stubStore) SymbolsByFile(ctx context.Context, file string) ([]string, error) {
	return s.fileSymbols[file], nil
}
func (s *stubStore) Ancestors(ctx context.Context, fqn string) ([]string, error) {
	return s.ancestors[fqn], nil
}
func (s *stubStore) AnnotationsOnSymbols(ctx context.Context, owners []string) ([]AnnotationUse, error) {
	var res []AnnotationUse
	for _, o := range owners {
		res = append(res, s.annotations[o]...)
	}
	return res, nil
}
func (s *stubStore) CallsFromFile(ctx context.Context, file string) ([]CallSite, error) {
	return s.calls[file], nil
}
func (s *stubStore) Children(ctx context.Context, parent string) ([]string, error) { return nil, nil }
func (s *stubStore) AnnotationUsesByType(ctx context.Context, ref SymbolRef) ([]AnnotationUse, error) {
	return nil, nil
}
func (s *stubStore) CallFilesByCallee(ctx context.Context, ref *SymbolRef) ([]string, error) {
	if ref == nil {
		return nil, nil
	}
	if s.callFilesByCallee == nil {
		return nil, nil
	}
	key := symbolRefKey(*ref)
	return s.callFilesByCallee[key], nil
}
func (s *stubStore) CallFilesByCallees(ctx context.Context, refs []SymbolRef) (map[string][]string, error) {
	out := make(map[string][]string)
	for _, ref := range refs {
		key := symbolRefKey(ref)
		if s.callFilesByCallee == nil {
			continue
		}
		if files := s.callFilesByCallee[key]; len(files) > 0 {
			out[key] = append([]string(nil), files...)
		}
	}
	return out, nil
}
func (s *stubStore) StaleCallEdgePaths(ctx context.Context, limit int) ([]string, error) {
	s.mu.Lock()
	s.staleCallCalls++
	s.mu.Unlock()
	if len(s.staleCallPaths) == 0 {
		return nil, nil
	}
	if limit > 0 && len(s.staleCallPaths) > limit {
		return append([]string(nil), s.staleCallPaths[:limit]...), nil
	}
	return append([]string(nil), s.staleCallPaths...), nil
}
func (s *stubStore) SetAnchorScope(ctx context.Context, anchorID int64, symbols []string, callFiles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scopeByAnchor[anchorID] = struct {
		syms  []string
		files []string
	}{symbols, callFiles}
	return nil
}
func (s *stubStore) AnchorScope(ctx context.Context, anchorID int64) ([]string, []string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	scope := s.scopeByAnchor[anchorID]
	return scope.syms, scope.files, nil
}
func (s *stubStore) FileHash(ctx context.Context, path string) (string, string, ParseStatus, bool, error) {
	return "", "", ParseOK, false, nil
}
func (s *stubStore) AnchorIDByLabel(ctx context.Context, label string) (int64, bool) {
	for _, a := range s.anchors {
		if a.Label == label {
			return a.ID, true
		}
	}
	return 0, false
}
func (s *stubStore) AnchorsMatchingSymbols(ctx context.Context, names []string, pkgs []string, lang Lang) ([]int64, error) {
	var ids []int64
	for _, a := range s.anchors {
		if a.BaseSym != nil && a.BaseSym.Name != "" && slices.Contains(names, a.BaseSym.Name) {
			ids = append(ids, a.ID)
		}
	}
	return ids, nil
}
func (s *stubStore) AnchorsMatchingAnnotations(ctx context.Context, annTypes []SymbolRef) ([]int64, error) {
	var ids []int64
	for _, a := range s.anchors {
		if a.Ann != nil {
			for _, at := range annTypes {
				if a.Ann.Symbol.Name == at.Name && a.Ann.Symbol.Pkg == at.Pkg && a.Ann.Symbol.Lang == at.Lang {
					ids = append(ids, a.ID)
					break
				}
			}
		}
	}
	return ids, nil
}
func (s *stubStore) DeleteFile(ctx context.Context, path string) error {
	return nil
}
func (s *stubStore) DeleteNote(ctx context.Context, path string) ([]int64, error) {
	return nil, nil
}
func (s *stubStore) DeleteAnchorsNotInLabels(ctx context.Context, noteID int64, keepLabels []string) ([]int64, error) {
	return nil, nil
}
func (s *stubStore) NoteIDByPath(ctx context.Context, path string) (int64, bool) {
	return 0, false
}
func (s *stubStore) GarbageCollectOrphanedAnchors(ctx context.Context) (int, error) {
	return 0, nil
}
func (s *stubStore) SymbolsBySuffix(ctx context.Context, suffix string, lang Lang, limit int) ([]Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.suffixError != nil {
		return nil, s.suffixError
	}
	if s.suffixMatches == nil {
		return nil, nil
	}
	syms := s.suffixMatches[suffix]
	if lang != "" {
		var filtered []Symbol
		for _, sym := range syms {
			if sym.Lang == lang {
				filtered = append(filtered, sym)
			}
		}
		syms = filtered
	}
	if limit > 0 && len(syms) > limit {
		syms = syms[:limit]
	}
	return syms, nil
}
func (s *stubStore) SymbolExistsByFQN(ctx context.Context, fqn string, lang Lang) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.symbolFQNError != nil {
		return false, s.symbolFQNError
	}
	if s.existingSymFQN == nil {
		return false, nil
	}
	return s.existingSymFQN[fqn], nil
}
func (s *stubStore) ReplaceDocLinksForPath(ctx context.Context, srcPath string, links []DocLink) error {
	return nil
}
func (s *stubStore) DocLinksForAnchor(ctx context.Context, anchorID string, limit int) ([]DocLink, error) {
	return nil, nil
}
func (s *stubStore) DocLinksForNote(ctx context.Context, notePath string, limit int) ([]DocLink, error) {
	return nil, nil
}
func (s *stubStore) DocLinksFromCodePath(ctx context.Context, srcPath string, limit int) ([]DocLink, error) {
	return nil, nil
}
func (s *stubStore) DeleteDocLinksByPath(ctx context.Context, srcPath string) error { return nil }

func TestService_NotesForFile_MatchesBaseClassAndAnnotationAndCalls(t *testing.T) {
	// Use normalized path to match what the service will use internally
	testFile := string(paths.NormalizeCode("svc/invoice.py"))

	store := &stubStore{
		fileSymbols: map[string][]string{
			testFile: {"svc.invoice.InvoiceService"},
		},
		ancestors: map[string][]string{
			"svc.invoice.InvoiceService": {"svc.invoice.BaseService"},
		},
		annotations: map[string][]AnnotationUse{
			"svc.invoice.InvoiceService": {{
				OwnerFQN:  "svc.invoice.InvoiceService",
				AnnSymbol: SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "decorator"},
				Args:      map[string]string{"tag": "billing"},
			}},
		},
		calls: map[string][]CallSite{
			testFile: {{
				File:         testFile,
				CalleeSymbol: SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "charge"},
			}},
		},
		anchors: []Anchor{
			{ID: 1, Label: "BaseSvc", Kind: AnchorBaseClass, BaseSym: &SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "BaseService"}},
			{ID: 2, Label: "DecoratorBilling", Kind: AnchorAnnotation, Ann: &AnnotationSelector{
				Symbol:     SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "decorator"},
				ArgFilters: map[string]string{"tag": "billing"},
			}},
			{ID: 3, Label: "ChargeUse", Kind: AnchorFunc, BaseSym: &SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "charge"}},
		},
		notesByAnchor: map[int64][]Note{
			1: {{ID: 10, Path: "notes/base.md", Title: "Base rules"}},
			2: {{ID: 11, Path: "notes/ann.md", Title: "Decorator rules"}},
			3: {{ID: 12, Path: "notes/charge.md", Title: "Charge rules"}},
		},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{
			1: {syms: []string{"svc.invoice.BaseService"}, files: nil},
			2: {syms: []string{"svc.invoice.InvoiceService"}, files: nil},
			3: {files: []string{testFile}},
		},
	}
	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())

	fc, err := svc.NotesForFile(context.Background(), "svc/invoice.py")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"BaseSvc", "DecoratorBilling", "ChargeUse"}, fc.Anchors)

	paths := []string{}
	for _, n := range fc.Notes {
		paths = append(paths, n.Path)
	}
	require.ElementsMatch(t, []string{"notes/base.md", "notes/ann.md", "notes/charge.md"}, paths)

	// Trace sanity
	require.Len(t, fc.Trace, 3)
}

type countingStore struct {
	*stubStore

	replaceFileSummaryCalls      int
	callsFromFileCalls           int
	symbolsByFileCalls           int
	ancestorsCalls               int
	annotationsOnSymbolsCalls    int
	anchorsMatchingSymbolsCalls  int
	anchorsMatchingAnnTypesCalls int
}

func (s *countingStore) ReplaceFileSummary(ctx context.Context, summary FileSummary) error {
	s.replaceFileSummaryCalls++
	return nil
}

func (s *countingStore) CallsFromFile(ctx context.Context, file string) ([]CallSite, error) {
	s.callsFromFileCalls++
	return s.stubStore.CallsFromFile(ctx, file)
}

func (s *countingStore) SymbolsByFile(ctx context.Context, file string) ([]string, error) {
	s.symbolsByFileCalls++
	return s.stubStore.SymbolsByFile(ctx, file)
}

func (s *countingStore) Ancestors(ctx context.Context, fqn string) ([]string, error) {
	s.ancestorsCalls++
	return s.stubStore.Ancestors(ctx, fqn)
}

func (s *countingStore) AnnotationsOnSymbols(ctx context.Context, owners []string) ([]AnnotationUse, error) {
	s.annotationsOnSymbolsCalls++
	return s.stubStore.AnnotationsOnSymbols(ctx, owners)
}

func (s *countingStore) AnchorsMatchingSymbols(ctx context.Context, names []string, pkgs []string, lang Lang) ([]int64, error) {
	s.anchorsMatchingSymbolsCalls++
	return s.stubStore.AnchorsMatchingSymbols(ctx, names, pkgs, lang)
}

func (s *countingStore) AnchorsMatchingAnnotations(ctx context.Context, annTypes []SymbolRef) ([]int64, error) {
	s.anchorsMatchingAnnTypesCalls++
	return s.stubStore.AnchorsMatchingAnnotations(ctx, annTypes)
}

type staticIndexer struct {
	lang    Lang
	summary FileSummary
}

func (s *staticIndexer) Lang() Lang { return s.lang }

func (s *staticIndexer) IndexFile(content []byte, path paths.CodePathRef) (FileSummary, error) {
	out := s.summary
	out.Lang = s.lang
	out.FilePath = path.Rel.String()
	return out, nil
}

func TestIndexCodeFile_BatchIndexingSkipsDirtySignalQueries(t *testing.T) {
	file := string(paths.NormalizeCode("svc/invoice.py"))
	base := &stubStore{
		fileSymbols: map[string][]string{
			file: {"svc.invoice.OldSymbol"},
		},
		ancestors: map[string][]string{
			"svc.invoice.OldSymbol": {"svc.invoice.BaseService"},
		},
		anchors: []Anchor{
			{
				ID:   1,
				Kind: AnchorBaseClass,
				BaseSym: &SymbolRef{
					Lang: LangPy,
					Pkg:  "svc.invoice",
					Name: "BaseService",
				},
			},
			{
				ID:   2,
				Kind: AnchorAnnotation,
				Ann: &AnnotationSelector{
					Symbol: SymbolRef{Lang: LangPy, Pkg: "svc.decorators", Name: "audit"},
				},
			},
		},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{},
	}

	store := &countingStore{stubStore: base}
	idx := &staticIndexer{
		lang: LangPy,
		summary: FileSummary{
			Symbols: []Symbol{{
				Lang: LangPy,
				Kind: SymClass,
				Pkg:  "svc.invoice",
				Name: "InvoiceService",
				FQN:  "svc.invoice.InvoiceService",
			}},
			Annotations: []AnnotationUse{{
				OwnerFQN:  "svc.invoice.InvoiceService",
				AnnSymbol: SymbolRef{Lang: LangPy, Pkg: "svc.decorators", Name: "audit"},
			}},
		},
	}

	svc := NewServiceWithOptions(store, []LanguageIndexer{idx}, WithoutWarmCache())
	require.NoError(t, svc.IndexCodeFile(context.Background(), LangPy, file, []byte("class InvoiceService: pass")))
	require.Greater(t, store.callsFromFileCalls+store.symbolsByFileCalls+store.anchorsMatchingSymbolsCalls, 0, "non-batch indexing should touch store to compute dirty signals")

	store.callsFromFileCalls = 0
	store.symbolsByFileCalls = 0
	store.ancestorsCalls = 0
	store.annotationsOnSymbolsCalls = 0
	store.anchorsMatchingSymbolsCalls = 0
	store.anchorsMatchingAnnTypesCalls = 0

	batchCtx := WithBatchIndexing(context.Background())
	require.NoError(t, svc.IndexCodeFile(batchCtx, LangPy, file, []byte("class InvoiceService: pass")))
	require.Equal(t, 0, store.callsFromFileCalls)
	require.Equal(t, 0, store.symbolsByFileCalls)
	require.Equal(t, 0, store.ancestorsCalls)
	require.Equal(t, 0, store.annotationsOnSymbolsCalls)
	require.Equal(t, 0, store.anchorsMatchingSymbolsCalls)
	require.Equal(t, 0, store.anchorsMatchingAnnTypesCalls)
}

func TestRecomputeAnchorScopes_OverflowClearsDirtySet(t *testing.T) {
	store := &stubStore{scopeByAnchor: map[int64]struct {
		syms  []string
		files []string
	}{}}
	svc := NewServiceWithOptions(store, nil, WithoutWarmCache(), WithWriteAccess())

	svc.dirtyMu.Lock()
	svc.dirtyAnchors[123] = true
	svc.dirtyOverflow = true
	svc.dirtyMu.Unlock()

	require.True(t, svc.HasDirtyAnchors())
	require.NoError(t, svc.RecomputeAnchorScopes(context.Background()))
	require.False(t, svc.HasDirtyAnchors())
}

func TestRecomputeAnchorScopes_RebuildsStaleCallEdgesWithCallFiles(t *testing.T) {
	caller := string(paths.NormalizeCode("svc/caller.py"))
	ref := SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "charge"}

	store := &stubStore{
		callFilesByCallee: map[string][]string{
			symbolRefKey(ref): {caller},
		},
		staleCallPaths: []string{caller},
		anchors: []Anchor{
			{ID: 1, Label: "charge", Kind: AnchorFunc, Lang: LangPy, BaseSym: &ref},
		},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{},
	}
	svc := NewServiceWithOptions(store, nil, WithoutWarmCache(), WithWriteAccess())

	require.NoError(t, svc.RecomputeAnchorScopes(context.Background()))
	require.Equal(t, 1, store.staleCallCalls)
	require.True(t, svc.callEdgesRebuilt)

	_, files, err := store.AnchorScope(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, []string{caller}, files)
}

func TestRebuildAnchorScopesForIDs_DoesNotTriggerHiddenCallEdgeRebuild(t *testing.T) {
	caller := string(paths.NormalizeCode("svc/caller.py"))
	ref := SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "charge"}

	store := &stubStore{
		callFilesByCallee: map[string][]string{
			symbolRefKey(ref): {caller},
		},
		staleCallPaths: []string{caller},
		anchors: []Anchor{
			{ID: 1, Label: "charge", Kind: AnchorFunc, Lang: LangPy, BaseSym: &ref},
		},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{},
	}
	svc := NewServiceWithOptions(store, nil, WithoutWarmCache(), WithWriteAccess())

	require.NoError(t, svc.RebuildAnchorScopesForIDs(context.Background(), []int64{1}))
	require.Equal(t, 0, store.staleCallCalls)
	require.False(t, svc.callEdgesRebuilt)

	_, files, err := store.AnchorScope(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, []string{caller}, files)
}

func TestRebuildAnchorScopesForIDs_UsesRuntimeCallFilesBeforeStoreReadback(t *testing.T) {
	caller := string(paths.NormalizeCode("svc/caller.py"))
	ref := SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "charge"}

	store := &stubStore{
		anchors: []Anchor{
			{ID: 1, Label: "charge", Kind: AnchorFunc, Lang: LangPy, BaseSym: &ref},
		},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{},
	}
	svc := NewServiceWithOptions(store, nil, WithoutWarmCache(), WithWriteAccess())
	svc.NoteRuntimeCodeIndexWorks([]CodeIndexWork{{
		Path: "svc/caller.py",
		SymbolRefs: []SymbolRefRow{{
			OwnerFQN: "svc.worker.run",
			RefKind:  RefKindCalls,
			DstLang:  LangPy,
			DstPkg:   ref.Pkg,
			DstName:  ref.Name,
			DstFQN:   ref.Pkg + "." + ref.Name,
		}},
		HasRefSignals: true,
	}})

	require.NoError(t, svc.RebuildAnchorScopesForIDs(context.Background(), []int64{1}))

	_, files, err := store.AnchorScope(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, []string{caller}, files)
}

func TestRecomputeAnchorScopes_WildcardBypassesDirtyAndClearsCache(t *testing.T) {
	store := &stubStore{
		anchors: []Anchor{
			{ID: 1, Kind: AnchorBaseClass, BaseSym: &SymbolRef{Lang: LangPy, Name: "Any"}},
			{ID: 2, Kind: AnchorFunc, BaseSym: &SymbolRef{Lang: LangPy, Pkg: "pkg", Name: "Func"}},
		},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{},
	}
	svc := NewServiceWithOptions(store, nil, WithoutWarmCache(), WithWriteAccess())

	// Seed file-context cache and mark only one anchor dirty.
	svc.cacheFileContext("svc/file.py", FileContext{File: "svc/file.py"}, 0)
	svc.markAnchorsDirty([]int64{2})

	require.NoError(t, svc.RecomputeAnchorScopes(context.Background()))

	// File cache should be cleared after scope recompute.
	_, _, ok := svc.cachedFileCtx("svc/file.py")
	require.False(t, ok)

	// Both anchors should be recomputed even though only one was marked dirty.
	require.Equal(t, []string{"Any"}, store.scopeByAnchor[1].syms)
	require.Equal(t, []string{"pkg.Func"}, store.scopeByAnchor[2].syms)
}

func TestRecomputeAnchorScopes_WildcardDecoratorDoesNotBypassDirty(t *testing.T) {
	store := &stubStore{
		anchors: []Anchor{
			{ID: 1, Kind: AnchorAnnotation, Ann: &AnnotationSelector{Symbol: SymbolRef{Lang: LangPy, Name: "decorator"}}}, // wildcard pkg (allowed)
			{ID: 2, Kind: AnchorFunc, BaseSym: &SymbolRef{Lang: LangPy, Pkg: "pkg", Name: "Func"}},
		},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{},
	}
	svc := NewServiceWithOptions(store, nil, WithoutWarmCache(), WithWriteAccess())

	// Mark only the function anchor dirty.
	svc.markAnchorsDirty([]int64{2})

	require.NoError(t, svc.RecomputeAnchorScopes(context.Background()))

	// Only the dirty function anchor should be recomputed. Wildcard decorators are still safe to
	// recompute incrementally (they can be dirtied via AnchorsMatchingAnnotations).
	_, ok := store.scopeByAnchor[1]
	require.False(t, ok, "wildcard decorator anchor should not force full recompute")
	require.Equal(t, []string{"pkg.Func"}, store.scopeByAnchor[2].syms)
}

func TestService_HasAnyIndexer(t *testing.T) {
	store := &stubStore{}

	// No indexers
	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())
	require.False(t, svc.HasAnyIndexer())
	require.Empty(t, svc.SupportedLangs())

	// With indexers (use nil-safe filtering)
	svc2 := NewServiceWithOptions(store, []LanguageIndexer{nil}, WithoutWarmCache())
	require.False(t, svc2.HasAnyIndexer())
}

func TestService_SupportedLangs(t *testing.T) {
	store := &stubStore{}
	idx := &stubIndexer{lang: LangPy}
	svc := NewServiceWithOptions(store, []LanguageIndexer{idx}, WithoutWarmCache())
	require.True(t, svc.HasAnyIndexer())
	require.True(t, svc.HasIndexer(LangPy))
	require.Equal(t, []Lang{LangPy}, svc.SupportedLangs())
}

func TestIngestNoteSource_InvalidatesAllFilesWhenAnchorsChange(t *testing.T) {
	store := &stubStore{
		fileSymbols: map[string][]string{},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{},
		anchors: []Anchor{},
	}
	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())

	// Pre-populate file context cache
	testPath := string(paths.NormalizeCode("test/file.py"))
	svc.cacheFileContext(testPath, FileContext{File: testPath}, 0)

	// Verify cache is populated
	_, _, ok := svc.cachedFileCtx(testPath)
	require.True(t, ok, "expected file context to be cached initially")

	// Ingest a note with anchor definitions
	noteContent := `---
anchors:
  - define:
      label: "TestAnchor"
      kind: "baseClass"
      lang: "py"
      baseClass:
        pkg: "test"
        name: "Base"
---
`
	notePath := string(paths.NormalizeNote("notes/test.md"))
	_, err := ingestTestNoteFile(context.Background(), svc, notePath, noteContent)
	require.NoError(t, err)

	// File context cache should be invalidated because anchors were defined
	_, _, ok = svc.cachedFileCtx(testPath)
	require.False(t, ok, "expected file context cache to be invalidated after anchor definition")
}

type stubIndexer struct {
	lang Lang
}

func (s *stubIndexer) Lang() Lang { return s.lang }
func (s *stubIndexer) IndexFile(content []byte, path paths.CodePathRef) (FileSummary, error) {
	return FileSummary{}, nil
}

type panicIndexer struct {
	lang Lang
}

func (p *panicIndexer) Lang() Lang { return p.lang }
func (p *panicIndexer) IndexFile(_ []byte, _ paths.CodePathRef) (FileSummary, error) {
	panic("boom")
}

type timeoutIndexer struct {
	lang Lang
}

func (t *timeoutIndexer) Lang() Lang { return t.lang }
func (t *timeoutIndexer) IndexFile(_ []byte, path paths.CodePathRef) (FileSummary, error) {
	return FileSummary{
		FilePath:    path.Rel.String(),
		Lang:        t.lang,
		ParseStatus: ParseTimeout,
	}, nil
}

type fixedSummaryIndexer struct {
	lang    Lang
	summary FileSummary
}

func (f *fixedSummaryIndexer) Lang() Lang { return f.lang }

func (f *fixedSummaryIndexer) IndexFile(_ []byte, path paths.CodePathRef) (FileSummary, error) {
	summary := f.summary
	if summary.FilePath == "" {
		summary.FilePath = path.Rel.String()
	}
	if summary.Lang == "" {
		summary.Lang = f.lang
	}
	return summary, nil
}

type refErrorStore struct {
	*stubStore
	symbolErr    error
	importErr    error
	moduleErr    error
	symbolCalled bool
	importCalled bool
	moduleCalled bool
}

func (s *refErrorStore) ReplaceIntelSymbolRefsForPathsBatch(ctx context.Context, batches map[string][]SymbolRefRow) error {
	s.symbolCalled = true
	return s.symbolErr
}

func (s *refErrorStore) ReplaceIntelImportRefsForPathsBatch(ctx context.Context, batches map[string][]ImportRefRow) error {
	s.importCalled = true
	return s.importErr
}

func (s *refErrorStore) ReplaceIntelModuleDefsForPathsBatch(ctx context.Context, batches map[string][]ModuleDefRow) error {
	s.moduleCalled = true
	return s.moduleErr
}

func (s *refErrorStore) SymbolRefsByPaths(ctx context.Context, paths []string) (map[string][]SymbolRefRow, error) {
	return nil, nil
}

func (s *refErrorStore) ImportRefsByPaths(ctx context.Context, paths []string) (map[string][]ImportRefRow, error) {
	return nil, nil
}

func (s *refErrorStore) ModuleDefsByPaths(ctx context.Context, paths []string) (map[string][]ModuleDefRow, error) {
	return nil, nil
}

func (s *refErrorStore) ReferrerPathsBySymbolRefs(ctx context.Context, refs []SymbolRef) (map[string][]string, error) {
	return nil, nil
}

func (s *refErrorStore) ReferrerPathsByModules(ctx context.Context, modules []string) (map[string][]string, error) {
	return nil, nil
}

func TestIndexCodeFile_RecoversFromIndexerPanic(t *testing.T) {
	store := &stubStore{}
	svc := NewServiceWithOptions(store, []LanguageIndexer{&panicIndexer{lang: LangPy}}, WithoutWarmCache())
	require.NoError(t, svc.IndexCodeFile(context.Background(), LangPy, "src/todoapp/services/tasks.py", []byte("x")))
}

func TestIndexCodeFile_SkipsDestructiveWriteOnTimeout(t *testing.T) {
	store := &stubStore{}
	svc := NewServiceWithOptions(store, []LanguageIndexer{&timeoutIndexer{lang: LangPy}}, WithoutWarmCache())
	require.NoError(t, svc.IndexCodeFile(context.Background(), LangPy, "src/todoapp/services/tasks.py", []byte("x")))
	require.Equal(t, 0, store.replaceCount)
	require.Equal(t, 1, store.metaCount)
	require.Equal(t, ParseTimeout, store.lastMeta.ParseStatus)
	require.Equal(t, "src/todoapp/services/tasks.py", store.lastMeta.Path)
}

func TestIndexCodeFile_PropagatesReverseIndexErrors(t *testing.T) {
	errSymbol := errors.New("symbol refs failed")
	errImport := errors.New("import refs failed")
	errModule := errors.New("module defs failed")
	store := &refErrorStore{
		stubStore: &stubStore{},
		symbolErr: errSymbol,
		importErr: errImport,
		moduleErr: errModule,
	}
	idx := &fixedSummaryIndexer{
		lang: LangPy,
		summary: FileSummary{
			FilePath: "src/app.py",
			Lang:     LangPy,
			Symbols: []Symbol{
				{Pkg: "app", Name: "Thing"},
			},
		},
	}
	svc := NewServiceWithOptions(store, []LanguageIndexer{idx}, WithoutWarmCache())

	err := svc.IndexCodeFile(context.Background(), LangPy, "src/app.py", []byte("x"))
	require.Error(t, err)
	require.ErrorIs(t, err, errSymbol)
	require.ErrorIs(t, err, errImport)
	require.ErrorIs(t, err, errModule)
	require.Zero(t, store.replaceCount, "failed publication must not advance freshness")
	store.symbolErr, store.importErr, store.moduleErr = nil, nil, nil
	require.NoError(t, svc.IndexCodeFile(context.Background(), LangPy, "src/app.py", []byte("x")))
	require.Equal(t, 1, store.replaceCount)
	require.True(t, store.symbolCalled)
	require.True(t, store.importCalled)
	require.True(t, store.moduleCalled)
}

func TestIndexCodeFile_ParseRecoveredWritesReverseIndex(t *testing.T) {
	store := &refErrorStore{
		stubStore: &stubStore{},
	}
	idx := &fixedSummaryIndexer{
		lang: LangPy,
		summary: FileSummary{
			FilePath:    "src/app.py",
			Lang:        LangPy,
			ParseStatus: ParseRecovered,
			Symbols: []Symbol{
				{Pkg: "app", Name: "Thing"},
			},
		},
	}
	svc := NewServiceWithOptions(store, []LanguageIndexer{idx}, WithoutWarmCache())

	err := svc.IndexCodeFile(context.Background(), LangPy, "src/app.py", []byte("x"))
	require.NoError(t, err)
	require.True(t, store.symbolCalled)
	require.True(t, store.importCalled)
	require.True(t, store.moduleCalled)
}

func TestValidateAnchors_Valid(t *testing.T) {
	store := &stubStore{
		anchors: []Anchor{
			{
				ID:      1,
				Label:   "user-model",
				Kind:    AnchorFunc,
				Lang:    LangPy,
				BaseSym: &SymbolRef{Lang: LangPy, Pkg: "myapp.models", Name: "User"},
			},
		},
		existingSymFQN: map[string]bool{
			"myapp.models.User": true, // The FQN exists in the index
		},
	}

	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())

	results, err := svc.ValidateAnchors(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ValidationValid, results[0].Status)
	require.Equal(t, 1, results[0].MatchedCount)
}

func TestValidateAnchors_NoMatch_NoSuggestions(t *testing.T) {
	store := &stubStore{
		anchors: []Anchor{
			{
				ID:      1,
				Label:   "nonexistent",
				Kind:    AnchorFunc,
				Lang:    LangPy,
				BaseSym: &SymbolRef{Lang: LangPy, Pkg: "nonexistent.module", Name: "NoClass"},
			},
		},
		existingSymFQN: map[string]bool{},     // No symbols exist
		suffixMatches:  map[string][]Symbol{}, // No suffix matches either
	}

	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())

	results, err := svc.ValidateAnchors(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ValidationNoMatch, results[0].Status)
	require.Empty(t, results[0].SuffixMatches)
	require.Contains(t, results[0].Message, "no symbols found")
}

func TestValidateAnchors_PropagatesExactLookupStoreFailure(t *testing.T) {
	storeErr := errors.New("exact lookup unavailable")
	store := &stubStore{
		anchors: []Anchor{{
			ID: 1, Label: "lookup-failure", Kind: AnchorFunc, Lang: LangPy,
			BaseSym: &SymbolRef{Lang: LangPy, Pkg: "example", Name: "Thing"},
		}},
		symbolFQNError: storeErr,
	}

	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())
	results, err := svc.ValidateAnchors(context.Background())

	require.ErrorIs(t, err, storeErr)
	require.ErrorContains(t, err, `validate anchor "lookup-failure"`)
	require.Empty(t, results, "store failures must not masquerade as no-match findings")
}

func TestValidateAnchors_PropagatesSuffixLookupStoreFailure(t *testing.T) {
	storeErr := errors.New("suffix lookup unavailable")
	store := &stubStore{
		anchors: []Anchor{{
			ID: 1, Label: "suffix-failure", Kind: AnchorFunc, Lang: LangPy,
			BaseSym: &SymbolRef{Lang: LangPy, Pkg: "example", Name: "Thing"},
		}},
		existingSymFQN: map[string]bool{},
		suffixError:    storeErr,
	}

	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())
	results, err := svc.ValidateAnchors(context.Background())

	require.ErrorIs(t, err, storeErr)
	require.ErrorContains(t, err, `validate anchor "suffix-failure"`)
	require.Empty(t, results, "store failures must not masquerade as no-match findings")
}

func TestValidateAnchors_SuffixMatch_SuggestsCorrection(t *testing.T) {
	store := &stubStore{
		anchors: []Anchor{
			{
				ID:    1,
				Label: "wrong-fqn",
				Kind:  AnchorFunc,
				Lang:  LangPy,
				// User wrote import-style path that doesn't exist
				BaseSym: &SymbolRef{Lang: LangPy, Pkg: "myapp.models", Name: "User"},
			},
		},
		existingSymFQN: map[string]bool{
			// The exact FQN "myapp.models.User" doesn't exist
			// But "backend.src.myapp.models.User" does (simulated via suffixMatches)
		},
		// Suffix matches with the correct indexed FQN
		suffixMatches: map[string][]Symbol{
			"myapp.models.User": {
				{FQN: "backend.src.myapp.models.User", Lang: LangPy},
			},
		},
	}

	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())

	results, err := svc.ValidateAnchors(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ValidationSuffix, results[0].Status)
	require.Len(t, results[0].SuffixMatches, 1)
	require.Equal(t, "backend.src.myapp.models.User", results[0].SuffixMatches[0])
}

func TestValidateAnchors_SkipsNonSymbolAnchors(t *testing.T) {
	store := &stubStore{
		anchors: []Anchor{
			{ID: 1, Label: "glob-anchor", Kind: AnchorGlob, Lang: LangPy, Globs: []string{"pkg/**/*.py"}},
			{ID: 2, Label: "path-anchor", Kind: AnchorPath, Lang: LangPy, PathPrefix: "pkg/utils"},
		},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{},
	}

	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())

	results, err := svc.ValidateAnchors(context.Background())
	require.NoError(t, err)
	// Glob and path anchors should be skipped (no validation results)
	require.Empty(t, results)
}

func TestValidateAnchors_MultipleSuffixMatches(t *testing.T) {
	store := &stubStore{
		anchors: []Anchor{
			{
				ID:      1,
				Label:   "user-model",
				Kind:    AnchorFunc,
				Lang:    LangPy,
				BaseSym: &SymbolRef{Lang: LangPy, Pkg: "models", Name: "User"},
			},
		},
		existingSymFQN: map[string]bool{}, // The exact FQN "models.User" doesn't exist
		// Multiple FQNs end with models.User
		suffixMatches: map[string][]Symbol{
			"models.User": {
				{FQN: "myapp.models.User", Lang: LangPy},
				{FQN: "backend.src.myapp.models.User", Lang: LangPy},
				{FQN: "tests.fixtures.models.User", Lang: LangPy},
			},
		},
	}

	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())

	results, err := svc.ValidateAnchors(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ValidationSuffix, results[0].Status)
	require.Len(t, results[0].SuffixMatches, 3)
}

func TestValidateAnchors_BaseClassAnchor(t *testing.T) {
	store := &stubStore{
		anchors: []Anchor{
			{
				ID:      1,
				Label:   "base-service",
				Kind:    AnchorBaseClass,
				Lang:    LangPy,
				BaseSym: &SymbolRef{Lang: LangPy, Pkg: "svc.base", Name: "BaseService"},
			},
		},
		existingSymFQN: map[string]bool{
			"svc.base.BaseService": true, // The base class exists
		},
	}

	svc := NewServiceWithOptions(store, nil, WithoutWarmCache())

	results, err := svc.ValidateAnchors(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ValidationValid, results[0].Status)
	require.Equal(t, 1, results[0].MatchedCount) // Now just checks if the target FQN exists
}
