package codeanchor

import (
	"context"
)

// intelStore is an optional capability implemented by the underlying store for intel tables.
type intelStore interface {
	ReplaceIntelCodeFile(ctx context.Context, path string, anchors []IntelAnchor, edges []IntelEdge, ftsRows []IntelFTSRow) error
	ReplaceIntelDocSections(ctx context.Context, path string, sections []IntelDocSection, mentions []IntelEdge, ftsRows []IntelFTSRow) error
	DeleteIntelByPath(ctx context.Context, path string) error
	UpsertIntelCallEdgesForPath(ctx context.Context, path string, edges []IntelEdge) error
}

// intelRefStore persists and queries reverse-index reference rows.
type intelRefStore interface {
	ReplaceIntelSymbolRefsForPathsBatch(ctx context.Context, batches map[string][]SymbolRefRow) error
	ReplaceIntelImportRefsForPathsBatch(ctx context.Context, batches map[string][]ImportRefRow) error
	ReplaceIntelModuleDefsForPathsBatch(ctx context.Context, batches map[string][]ModuleDefRow) error

	SymbolRefsByPaths(ctx context.Context, paths []string) (map[string][]SymbolRefRow, error)
	ImportRefsByPaths(ctx context.Context, paths []string) (map[string][]ImportRefRow, error)
	ModuleDefsByPaths(ctx context.Context, paths []string) (map[string][]ModuleDefRow, error)

	ReferrerPathsBySymbolRefs(ctx context.Context, refs []SymbolRef) (map[string][]string, error)
	ReferrerPathsByModules(ctx context.Context, modules []string) (map[string][]string, error)
}

// CallEdgesBatch groups call/import/type_ref edges by source path for batch operations.
type CallEdgesBatch struct {
	Path  string
	Edges []IntelEdge
}

type intelCallEdgesBatchStore interface {
	UpsertIntelCallEdgesForPathsBatch(ctx context.Context, batches []CallEdgesBatch) error
}

type intelCallResolver interface {
	IntelAnchorIDsByFQNsAndLang(ctx context.Context, lang string, fqns []string) (map[string][]string, error)
}

type intelExactCallResolver interface {
	IntelAnchorIDsByExactFQNsAndLang(ctx context.Context, lang string, fqns []string) (map[string][]string, error)
}

type intelCallEdgeSuffixSeedStore interface {
	CallEdgeSuffixSeedsByLangs(ctx context.Context, langs []Lang) ([]CallEdgeSuffixSeed, error)
}

// intelGoMethodResolver supports best-effort resolution of Go method calls when receiver type is unknown.
// This is used to improve file-to-file call edges for Go-heavy repos without go/types.
type intelGoMethodResolver interface {
	IntelAnchorIDsByGoMethodNameInPkg(ctx context.Context, pkgImportPath string, methodName string, limit int) ([]string, error)
}

// intelModuleSuffixResolver resolves Python module names to module anchor IDs using path suffix matching.
// Given module names like "charm.misc.colors", it finds anchors whose paths end with "charm/misc/colors.py".
type intelModuleSuffixResolver interface {
	ModuleAnchorIDsByModuleSuffix(ctx context.Context, modules []string) (map[string]string, error)
}

// intelModulePathResolver resolves file paths to module anchor IDs using exact path matching.
// Used for TypeScript/JavaScript import edges where resolved file paths are available.
type intelModulePathResolver interface {
	ModuleAnchorIDsByPaths(ctx context.Context, paths []string) (map[string]string, error)
}

type intelMentionLookup interface {
	IntelAnchorsByPath(ctx context.Context, path string) ([]IntelAnchor, error)
	IntelAnchorsByPaths(ctx context.Context, paths []string) (map[string][]IntelAnchor, error)
	IntelAnchorsBySymbol(ctx context.Context, symbol string, limit int) ([]IntelAnchor, error)
}

type callFilesByCalleeBatch interface {
	CallFilesByCallees(ctx context.Context, refs []SymbolRef) (map[string][]string, error)
}

// callEdgeChecker checks if any call edges exist in the store.
type callEdgeChecker interface {
	HasAnyCallEdges(ctx context.Context) (bool, error)
}

type callEdgeStaleMarker interface {
	MarkCallEdgesStale(ctx context.Context, paths []string) error
}

type callEdgeStaleLister interface {
	StaleCallEdgePaths(ctx context.Context, limit int) ([]string, error)
}

type annotationUsesByTypeBatch interface {
	AnnotationUsesByTypes(ctx context.Context, refs []SymbolRef) (map[string][]AnnotationUse, error)
}

type childrenBatch interface {
	ChildrenBatch(ctx context.Context, parents []string) (map[string][]string, error)
}

// scopeBatchSetter allows stores to batch scope updates in one transaction.
type scopeBatchSetter interface {
	SetAnchorScopesBatch(ctx context.Context, updates []AnchorScopeUpdate) error
}

// AnchorScopeUpdate groups scope data for an anchor.
type AnchorScopeUpdate struct {
	ID        int64
	Symbols   []string
	CallFiles []string
}

type serviceOptions struct {
	warmCache   bool
	warmCtx     context.Context
	basePath    string
	tailIdx     *PathTailIndex
	cacheLimit  int
	linker      CodeDocLinker
	writeAccess bool
}

// ServiceOption customizes Service construction.
type ServiceOption func(*serviceOptions)

func defaultServiceOptions() serviceOptions {
	return serviceOptions{
		warmCache:  true,
		warmCtx:    context.Background(),
		cacheLimit: 128,
		linker:     nil,
	}
}

// WithoutWarmCache disables background cache warming (useful for short-lived commands).
func WithoutWarmCache() ServiceOption {
	return func(o *serviceOptions) {
		o.warmCache = false
	}
}

// WithWarmCacheContext sets the context for cache warmup; cancellation stops warming early.
func WithWarmCacheContext(ctx context.Context) ServiceOption {
	return func(o *serviceOptions) {
		if ctx == nil {
			ctx = context.Background()
		}
		o.warmCtx = ctx
	}
}

// WithBasePath sets the base path used to resolve relative directory anchors (dir:).
// For vault-backed usage, this should typically be the vault root.
func WithBasePath(basePath string) ServiceOption {
	return func(o *serviceOptions) {
		o.basePath = basePath
	}
}

// WithPathTailIndex configures an optional in-memory path tail index used for
// best-effort resolution when language-specific module resolution fails.
func WithPathTailIndex(idx *PathTailIndex) ServiceOption {
	return func(o *serviceOptions) {
		o.tailIdx = idx
	}
}

// WithCodeDocLinker attaches a doc-linker used to populate doc_links from code/note ingest.
func WithCodeDocLinker(linker CodeDocLinker) ServiceOption {
	return func(o *serviceOptions) {
		o.linker = linker
	}
}

// WithWriteAccess enables index updates (RecomputeAnchorScopes, RebuildAllCallEdges,
// background warming). By default, services are read-only; use this for indexing
// commands and the live watcher runtime that need to modify the index.
func WithWriteAccess() ServiceOption {
	return func(o *serviceOptions) {
		o.writeAccess = true
	}
}

// Store abstracts persistence for semantic notes.
type Store interface {
	ReplaceFileSummary(ctx context.Context, summary FileSummary) error
	UpsertFileMeta(ctx context.Context, meta FileMeta) error
	UpsertNote(ctx context.Context, note Note) error
	UpsertNoteWithCleanup(ctx context.Context, note Note, keepLabels []string) (AnchorUpsertResult, error)
	Anchors(ctx context.Context) ([]Anchor, error)
	AnchorsByIDs(ctx context.Context, ids []int64) ([]Anchor, error)
	AnchorsByNotePaths(ctx context.Context, paths []string) (map[string][]Anchor, error)
	AnchorsBySymbols(ctx context.Context, symbols []string) (map[int64][]string, error)
	AnchorsByCallFiles(ctx context.Context, files []string) ([]int64, error)
	AnchorsByPathPrefix(ctx context.Context, target string) ([]int64, error)
	AnchorsByGlobMatch(ctx context.Context, target string) ([]int64, error)
	AllAnchorScopes(ctx context.Context) (map[int64]AnchorScope, error)
	NotePaths(ctx context.Context) ([]string, error)
	NotesForAnchor(ctx context.Context, anchorID int64) ([]Note, error)
	SymbolsByFile(ctx context.Context, file string) ([]string, error)
	Ancestors(ctx context.Context, fqn string) ([]string, error)
	AnnotationsOnSymbols(ctx context.Context, owners []string) ([]AnnotationUse, error)
	CallsFromFile(ctx context.Context, file string) ([]CallSite, error)
	Children(ctx context.Context, parent string) ([]string, error)
	AnnotationUsesByType(ctx context.Context, ref SymbolRef) ([]AnnotationUse, error)
	CallFilesByCallee(ctx context.Context, ref *SymbolRef) ([]string, error)
	SetAnchorScope(ctx context.Context, anchorID int64, symbols []string, callFiles []string) error
	AnchorScope(ctx context.Context, anchorID int64) ([]string, []string, error)
	FileHash(ctx context.Context, path string) (hash string, indexerVersion string, parseStatus ParseStatus, ok bool, err error)
	AnchorIDByLabel(ctx context.Context, label string) (int64, bool)
	AnchorsMatchingSymbols(ctx context.Context, names []string, pkgs []string, lang Lang) ([]int64, error)
	AnchorsMatchingAnnotations(ctx context.Context, annTypes []SymbolRef) ([]int64, error)
	DeleteFile(ctx context.Context, path string) error
	DeleteNote(ctx context.Context, path string) ([]int64, error)
	DeleteAnchorsNotInLabels(ctx context.Context, noteID int64, keepLabels []string) ([]int64, error)
	NoteIDByPath(ctx context.Context, path string) (int64, bool)
	GarbageCollectOrphanedAnchors(ctx context.Context) (int, error)

	// SymbolsBySuffix finds symbols whose FQN ends with the given suffix.
	// Used for fuzzy matching when exact FQN lookup fails.
	SymbolsBySuffix(ctx context.Context, suffix string, lang Lang, limit int) ([]Symbol, error)

	// SymbolExistsByFQN checks if a symbol with the exact FQN exists.
	SymbolExistsByFQN(ctx context.Context, fqn string, lang Lang) (bool, error)

	// Doc links (optional; implementations may return ErrUnsupported).
	ReplaceDocLinksForPath(ctx context.Context, srcPath string, links []DocLink) error
	DocLinksForAnchor(ctx context.Context, anchorID string, limit int) ([]DocLink, error)
	DeleteDocLinksByPath(ctx context.Context, srcPath string) error
}

type notePathLister interface {
	NotePaths(ctx context.Context) ([]string, error)
}

// noteMtimeLister is implemented by stores that can return note mtimes.
type noteMtimeLister interface {
	IntelNoteMtimes(ctx context.Context) (map[string]int64, error)
}

type noteMetaWriter interface {
	UpsertNoteMeta(ctx context.Context, path, contentHash, indexerVersion string, mtime int64) error
}

// noteMetaLister is implemented by stores that can return note index metadata.
type noteMetaLister interface {
	IntelNoteIndexMeta(ctx context.Context) (map[string]NoteIndexMeta, error)
}

// noteMtimeToucher is implemented by stores that can refresh note mtime cache entries.
type noteMtimeToucher interface {
	TouchNoteMtimes(ctx context.Context, updates map[string]int64) error
	TouchIntelNotePaths(ctx context.Context, pathMtimes map[string]int64) error
}

type anchorScopeProber interface {
	HasAnyAnchorScopes(ctx context.Context) (bool, error)
}

// FileWithLang pairs a file path with its language.
type FileWithLang struct {
	Path string
	Lang string
}

// fileWithLangLister is implemented by stores that can list files with their languages.
type fileWithLangLister interface {
	ListFilesWithLang(ctx context.Context, limit int) ([]FileWithLang, error)
}

// indexSignals tracks symbol/annotation signals for dirty tracking.
type indexSignals struct {
	symPairs map[string]struct{} // "pkg\x00name"
	annTypes map[string]SymbolRef
}

func (s *indexSignals) Merge(other indexSignals) {
	if s == nil {
		return
	}
	if s.symPairs == nil {
		s.symPairs = make(map[string]struct{})
	}
	if s.annTypes == nil {
		s.annTypes = make(map[string]SymbolRef)
	}
	for k := range other.symPairs {
		s.symPairs[k] = struct{}{}
	}
	for k, v := range other.annTypes {
		s.annTypes[k] = v
	}
}

func (s *indexSignals) AddPair(pkg, name string) {
	if pkg == "" || name == "" {
		return
	}
	if s.symPairs == nil {
		s.symPairs = make(map[string]struct{})
	}
	s.symPairs[pkg+"\x00"+name] = struct{}{}
}
