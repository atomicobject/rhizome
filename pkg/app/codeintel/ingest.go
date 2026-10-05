package codeintel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// NoteIngestResult reports note ingest progress/counters.
type NoteIngestResult struct {
	Count        int
	Unchanged    int
	Deleted      int
	HasAnchors   bool
	ChangedPaths []string
	DeletedPaths []string
	BuildErrors  []NoteIngestBuildError
}

// NoteIngestBuildError records a note that could not be parsed into index work.
// Ingest keeps scanning other notes, but callers can surface these warnings.
type NoteIngestBuildError struct {
	Path  string
	Error string
}

// markdownCandidateRun supplies already-selected Markdown candidates to the
// shared Markdown processing core. Discovery callers use ProcessFiles;
// ownership-aware callers use ProcessCandidates. Keeping selection outside the
// core makes the ownership boundary explicit while preserving one
// read/build/write path.
type markdownCandidateRun func(
	context.Context,
	func(indexingpipe.FileCandidate) bool,
	func(indexingpipe.FileCandidate),
	func(indexingpipe.FilePayload) error,
) error

// markdownIngestPolicy keeps legacy discovery cleanup separate from explicit
// ownership-routed candidate intake. Candidate callers report parse failures
// but never retire durable ownership or derived state themselves.
type markdownIngestPolicy struct {
	cleanupStale      bool
	deleteBuildErrors bool
}

type markdownSourceLookup func(indexingpipe.FileCandidate) (notemeta.NoteSourceSnapshot, bool)

// codeCandidateRun supplies already-selected code candidates to the shared
// code processing core. Classification remains an orchestration concern.
type codeCandidateRun func(
	context.Context,
	*IndexResult,
	func(indexingpipe.FileCandidate) bool,
	func(indexingpipe.FileCandidate),
	func(indexingpipe.FilePayload) error,
) error

// codeIngestPolicy separates legacy root-walk tolerance from strict,
// ownership-routed candidate intake. A strict run returns filesystem and work
// build failures so reconciliation cannot acknowledge incomplete code state.
type codeIngestPolicy struct {
	failCandidateErrors bool
}

// CodeCandidateReadError reports a filesystem read failure from explicit
// ownership-routed code intake.
type CodeCandidateReadError struct {
	Path string
	Err  error
}

func (e *CodeCandidateReadError) Error() string {
	if e == nil {
		return "code candidate read error"
	}
	return fmt.Sprintf("read code candidate %q: %v", e.Path, e.Err)
}

func (e *CodeCandidateReadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// CodeCandidateBuildError reports a code-index work build failure from
// explicit ownership-routed code intake.
type CodeCandidateBuildError struct {
	Path string
	Err  error
}

func (e *CodeCandidateBuildError) Error() string {
	if e == nil {
		return "code candidate build error"
	}
	return fmt.Sprintf("build code index work for %q: %v", e.Path, e.Err)
}

func (e *CodeCandidateBuildError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IndexResult reports code index progress/counters for a single root.
type IndexResult struct {
	Indexed            int
	Unchanged          int
	Skipped            map[codeanchor.Lang]int
	Unsupported        map[codeanchor.Lang]int
	SeenPaths          []string
	IndexedPaths       []string
	ChangedCallerPaths []string
	DefDeltas          codeanchor.DefDeltas
}

type codeIndexResultFragment struct {
	indexed           bool
	unsupportedLang   codeanchor.Lang
	indexedPath       string
	changedCallerPath string
	defDeltas         codeanchor.DefDeltas
}

type codeIndexReducedResult struct {
	indexedPaths       []string
	changedCallerPaths []string
	unsupported        map[codeanchor.Lang]int
	defDeltas          codeanchor.DefDeltas
}

type codeIndexResultReducer struct {
	indexedPaths   []string
	changedCallers map[string]struct{}
	unsupported    map[codeanchor.Lang]int
	defDeltas      codeanchor.DefDeltaAccumulator
}

func newCodeIndexResultReducer() *codeIndexResultReducer {
	return &codeIndexResultReducer{
		changedCallers: make(map[string]struct{}),
		unsupported:    make(map[codeanchor.Lang]int),
	}
}

func validateCodeCandidates(basePath string, candidates []indexingpipe.FileCandidate) error {
	vaultPaths, err := paths.NewVaultPaths(basePath)
	if err != nil || vaultPaths.Root() == "" {
		return fmt.Errorf("code candidate vault root is required")
	}
	for _, candidate := range candidates {
		if candidate.Kind != indexingpipe.FileKindCode {
			return fmt.Errorf("code candidate %q has kind %q, want %q", candidate.RelPath, candidate.Kind, indexingpipe.FileKindCode)
		}
		if !filepath.IsAbs(candidate.AbsPath) {
			return fmt.Errorf("code candidate %q has non-absolute path %q", candidate.RelPath, candidate.AbsPath)
		}
		rel, err := paths.CleanRelPath(candidate.RelPath)
		if err != nil || rel.String() == "" || rel.String() != candidate.RelPath {
			return fmt.Errorf("code candidate has invalid vault-relative path %q", candidate.RelPath)
		}
		actual, err := vaultPaths.RelCodeStrict(candidate.AbsPath)
		if err != nil || actual.String() != rel.String() {
			return fmt.Errorf("code candidate absolute path %q does not match relative path %q", candidate.AbsPath, candidate.RelPath)
		}
	}
	return nil
}

func (r *codeIndexResultReducer) Apply(ctx context.Context, fragment codeIndexResultFragment) {
	if r == nil {
		return
	}
	started := time.Now()
	if fragment.indexedPath != "" {
		partStarted := time.Now()
		r.indexedPaths = append(r.indexedPaths, fragment.indexedPath)
		indexingperf.ObserveLatency(ctx, "codeindex.result_reduce_indexed", time.Since(partStarted))
	}
	if fragment.changedCallerPath != "" {
		partStarted := time.Now()
		r.changedCallers[fragment.changedCallerPath] = struct{}{}
		indexingperf.ObserveLatency(ctx, "codeindex.result_reduce_changed_callers", time.Since(partStarted))
	}
	if fragment.defDeltas.HasChanges() {
		partStarted := time.Now()
		r.defDeltas.Add(fragment.defDeltas)
		indexingperf.ObserveLatency(ctx, "codeindex.result_reduce_defdeltas", time.Since(partStarted))
	}
	if fragment.unsupportedLang != "" {
		r.unsupported[fragment.unsupportedLang]++
	}
	indexingperf.ObserveLatency(ctx, "codeindex.result_reduce", time.Since(started))
}

func (r *codeIndexResultReducer) Finalize(ctx context.Context) codeIndexReducedResult {
	if r == nil {
		return codeIndexReducedResult{}
	}
	started := time.Now()
	out := codeIndexReducedResult{
		indexedPaths: make([]string, 0, len(r.indexedPaths)),
		unsupported:  make(map[codeanchor.Lang]int, len(r.unsupported)),
		defDeltas:    r.defDeltas.Finalize(),
	}
	out.indexedPaths = append(out.indexedPaths, r.indexedPaths...)
	for lang, count := range r.unsupported {
		out.unsupported[lang] = count
	}
	if len(r.changedCallers) > 0 {
		out.changedCallerPaths = make([]string, 0, len(r.changedCallers))
		for path := range r.changedCallers {
			out.changedCallerPaths = append(out.changedCallerPaths, path)
		}
		sort.Strings(out.changedCallerPaths)
	}
	indexingperf.ObserveLatency(ctx, "codeindex.result_finalize", time.Since(started))
	return out
}

func ResolveRoot(vaultPath, root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", nil
	}
	if filepath.Clean(root) == "." {
		if resolved := paths.ResolveSymlinks(vaultPath); resolved != "" {
			return resolved.String(), nil
		}
		return filepath.Clean(vaultPath), nil
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		if resolved := paths.ResolveSymlinks(root); resolved != "" {
			return resolved.String(), nil
		}
		return filepath.Clean(root), nil
	}
	_, abs, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, root)
	if err != nil || abs == "" {
		return "", err
	}
	return abs.String(), nil
}

// IngestNotes is a direct Markdown discovery adapter for legacy code-anchor
// commands. Ownership-routed callers use IngestMarkdownCandidates.
func IngestNotes(ctx context.Context, service *codeanchor.Service, root string) (NoteIngestResult, error) {
	matcher := obsidian.LoadVaultIgnoreMatcher(root, nil)
	return IngestNotesWithMatcher(ctx, service, root, matcher, nil)
}

// IngestNotesForVault is a Markdown syntax compatibility adapter for code
// anchors. Ownership-routed callers must use IngestMarkdownCandidates instead.
// Collection vaults only ingest Markdown notes matched by Includes, then apply
// ignore/exclude rules.
func IngestNotesForVault(ctx context.Context, service *codeanchor.Service, vaultDef obsidian.VaultDefinition, matcher *ignore.Matcher, progress *indexingpipe.ProgressCallbacks) (NoteIngestResult, error) {
	root := strings.TrimSpace(vaultDef.BasePath())
	return ingestNotes(ctx, service, root, matcher, progress, func(relPath string) bool {
		return obsidian.NotePathMatchesSelection(vaultDef, relPath)
	})
}

// IngestNotesWithMatcher is the direct Markdown discovery adapter. Full and
// live ownership paths must pass selected candidates to IngestMarkdownCandidates.
func IngestNotesWithMatcher(ctx context.Context, service *codeanchor.Service, root string, matcher *ignore.Matcher, progress *indexingpipe.ProgressCallbacks) (NoteIngestResult, error) {
	return ingestNotes(ctx, service, root, matcher, progress, nil)
}

func ingestNotes(ctx context.Context, service *codeanchor.Service, root string, matcher *ignore.Matcher, progress *indexingpipe.ProgressCallbacks, includePath func(string) bool) (NoteIngestResult, error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return NoteIngestResult{}, nil
	}
	if matcher == nil {
		matcher = obsidian.LoadVaultIgnoreMatcher(root, nil)
	}
	hardMatcher := obsidian.LoadVaultHardIgnoreMatcher(root)

	vaultPaths, _ := paths.NewVaultPaths(root)
	defaultIgnored := make(map[string]struct{})
	for _, name := range ignore.DefaultIgnoreDirnames() {
		if strings.TrimSpace(name) == "" {
			continue
		}
		defaultIgnored[name] = struct{}{}
	}

	canonicalize := func(path string) string {
		if resolved := paths.ResolveSymlinks(path); resolved != "" {
			return resolved.String()
		}
		return filepath.Clean(path)
	}

	seen := make(map[string]struct{})
	return ingestMarkdownCandidateRun(ctx, service, func(ctx context.Context, shouldRead func(indexingpipe.FileCandidate) bool, onSkip func(indexingpipe.FileCandidate), process func(indexingpipe.FilePayload) error) error {
		return indexingpipe.ProcessFiles(ctx, indexingpipe.ProcessOptions{
			Root:               root,
			Matcher:            matcher,
			AlternativeMatcher: hardMatcher,
			WorkerCount:        ClampIndexWorkers(runtime.GOMAXPROCS(0)),
			Progress:           progress,
		}, func(path string, d os.DirEntry, modTime int64) (indexingpipe.FileCandidate, bool, error) {
			if strings.ToLower(filepath.Ext(path)) != ".md" {
				return indexingpipe.FileCandidate{}, false, nil
			}
			name := d.Name()
			if _, ok := defaultIgnored[name]; ok {
				return indexingpipe.FileCandidate{}, false, nil
			}
			rel := ""
			if relPath, err := vaultPaths.RelNotePathStrict(path); err == nil {
				rel = string(paths.NormalizeNotePath(relPath.String()))
			}
			if includePath != nil {
				// Collection vaults must apply Includes before ignore/exclude cleanup;
				// otherwise notes outside the active collection can leak into anchor and
				// semantic state, then survive as stale index rows.
				if rel == "" || !includePath(rel) {
					return indexingpipe.FileCandidate{}, false, nil
				}
			}
			visibilityMatcher := matcher
			if ignore.IsSystemContextPath(rel) {
				visibilityMatcher = hardMatcher
			}
			if visibilityMatcher != nil && visibilityMatcher.IsIgnored(rel, false) {
				return indexingpipe.FileCandidate{}, false, nil
			}
			seen[canonicalize(path)] = struct{}{}
			if rel != "" {
				seen[rel] = struct{}{}
			}
			return indexingpipe.FileCandidate{AbsPath: path, RelPath: rel, ModTime: modTime, Kind: indexingpipe.FileKindNote}, true, nil
		}, shouldRead, onSkip, process)
	}, markdownIngestPolicy{cleanupStale: true, deleteBuildErrors: true}, seen, nil)
}

func ingestMarkdownCandidateRun(ctx context.Context, service *codeanchor.Service, run markdownCandidateRun, policy markdownIngestPolicy, seen map[string]struct{}, sourceForCandidate markdownSourceLookup) (NoteIngestResult, error) {
	ctx = codeanchor.WithBatchIndexing(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	storedNoteMeta, _ := service.NoteIndexMeta(ctx)
	storedNotePaths := make(map[string]struct{})
	if stored, listErr := service.NotePaths(ctx); listErr == nil {
		for _, p := range stored {
			storedNotePaths[string(paths.NormalizeNotePath(p))] = struct{}{}
		}
	}
	var storedNotePathsMu sync.Mutex

	var count atomic.Int64
	var skipped atomic.Int64
	var deleted atomic.Int64
	var hasAnchors atomic.Bool
	var changedPathsMu sync.Mutex
	var changedPaths []string
	var touchedNoteMtimesMu sync.Mutex
	touchedNoteMtimes := make(map[string]int64)
	var deletedPathsMu sync.Mutex
	var deletedPaths []string
	var buildErrorsMu sync.Mutex
	var buildErrors []NoteIngestBuildError
	workerCount := ClampIndexWorkers(runtime.GOMAXPROCS(0))

	writeCh := make(chan codeanchor.NoteIndexWork, workerCount*2)
	errCh := make(chan error, 1)
	writeQueue := writeQueueFromContext(ctx)

	var writeWG sync.WaitGroup
	if writeQueue == nil {
		writeWG.Add(1)
		go func() {
			defer writeWG.Done()
			RunAdaptiveBatchWriter(ctx, cancel, writeCh, errCh, func(batch []codeanchor.NoteIndexWork) error {
				return service.ApplyNoteIndexBatch(ctx, batch)
			})
		}()
	}

	err := run(ctx, func(candidate indexingpipe.FileCandidate) bool {
		if candidate.ForceRead {
			return true
		}
		if candidate.RelPath == "" || storedNoteMeta == nil {
			return true
		}
		meta, ok := storedNoteMeta[candidate.RelPath]
		return !(ok && meta.IndexerVersion == codeanchor.NoteIndexerVersion && candidate.ModTime > 0 && meta.Mtime >= candidate.ModTime)
	}, func(indexingpipe.FileCandidate) {
		skipped.Add(1)
	}, func(payload indexingpipe.FilePayload) error {
		meta, hasMeta := codeanchor.NoteIndexMeta{}, false
		if payload.Candidate.RelPath != "" && storedNoteMeta != nil {
			meta, hasMeta = storedNoteMeta[payload.Candidate.RelPath]
		}
		contentHash := HashContentBytes(payload.Content)
		if !payload.Candidate.ForceRead && hasMeta && meta.IndexerVersion == codeanchor.NoteIndexerVersion && meta.ContentHash == contentHash {
			// Content-identical notes still refresh mtimes in metadata so future
			// mtime guards do not keep re-reading them.
			skipped.Add(1)
			if payload.Candidate.RelPath != "" && payload.Candidate.ModTime > 0 && meta.Mtime != payload.Candidate.ModTime {
				touchedNoteMtimesMu.Lock()
				if existing, ok := touchedNoteMtimes[payload.Candidate.RelPath]; !ok || existing < payload.Candidate.ModTime {
					touchedNoteMtimes[payload.Candidate.RelPath] = payload.Candidate.ModTime
				}
				touchedNoteMtimesMu.Unlock()
			}
			return nil
		}
		processStarted := time.Now()
		sourcePath := payload.Candidate.RelPath
		if sourcePath == "" {
			sourcePath = payload.Candidate.AbsPath
		}
		source := notemeta.NewContentOnlyNoteSourceSnapshot(sourcePath, string(payload.Content), payload.Candidate.ModTime)
		if sourceForCandidate != nil {
			if preloaded, ok := sourceForCandidate(payload.Candidate); ok {
				source = preloaded
			}
		}
		if source.Format != noteformat.FormatID("markdown") {
			return fmt.Errorf("Markdown candidate %q has source format %q", payload.Candidate.RelPath, source.Format)
		}
		buildCtx := ctx
		if payload.Candidate.ForceRead {
			buildCtx = codeanchor.WithForceReindex(buildCtx)
		}
		work, err := service.BuildNoteIndexWorkFromSource(buildCtx, source)
		indexingperf.ObserveLatency(ctx, "process.note", time.Since(processStarted))
		if err != nil {
			buildErrorsMu.Lock()
			buildErrors = append(buildErrors, NoteIngestBuildError{
				Path:  sourcePath,
				Error: err.Error(),
			})
			buildErrorsMu.Unlock()
			indexingperf.AddCount(ctx, "noteindex.build_error", 1)
			if policy.deleteBuildErrors && payload.Candidate.RelPath != "" {
				if delErr := service.DeleteNote(ctx, payload.Candidate.RelPath); delErr != nil {
					return delErr
				}
				normalizedRel := string(paths.NormalizeNotePath(payload.Candidate.RelPath))
				storedNotePathsMu.Lock()
				_, wasStored := storedNotePaths[normalizedRel]
				if wasStored {
					delete(storedNotePaths, normalizedRel)
				}
				storedNotePathsMu.Unlock()
				if wasStored {
					deleted.Add(1)
					deletedPathsMu.Lock()
					deletedPaths = append(deletedPaths, normalizedRel)
					deletedPathsMu.Unlock()
				}
			}
			return nil
		}
		if work != nil {
			work.ContentHash = contentHash
			work.Mtime = payload.Candidate.ModTime
		}
		count.Add(1)
		if payload.Candidate.RelPath != "" {
			changedPathsMu.Lock()
			changedPaths = append(changedPaths, payload.Candidate.RelPath)
			changedPathsMu.Unlock()
		}
		if work == nil {
			return nil
		}
		if len(work.Note.DefinedAnchors) > 0 {
			hasAnchors.Store(true)
		}
		if writeQueue != nil {
			if err := writeQueue.SubmitNoteIndexWork(ctx, *work); err != nil {
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case writeCh <- *work:
			return nil
		}
	})
	if writeQueue == nil {
		close(writeCh)
		writeWG.Wait()
	} else if err := writeQueue.FlushAndWait(ctx); err != nil {
		return NoteIngestResult{}, err
	}

	makeResult := func() NoteIngestResult {
		return NoteIngestResult{
			Count:        int(count.Load()),
			Unchanged:    int(skipped.Load()),
			Deleted:      int(deleted.Load()),
			HasAnchors:   hasAnchors.Load(),
			ChangedPaths: changedPaths,
			DeletedPaths: deletedPaths,
			BuildErrors:  buildErrors,
		}
	}

	select {
	case writeErr := <-errCh:
		if writeErr != nil {
			return makeResult(), writeErr
		}
	default:
	}
	if err != nil {
		return makeResult(), err
	}
	if len(touchedNoteMtimes) > 0 {
		if touchErr := service.TouchNotePaths(ctx, touchedNoteMtimes); touchErr != nil {
			return makeResult(), touchErr
		}
	}

	if policy.cleanupStale {
		stored, listErr := service.NotePaths(ctx)
		if listErr == nil && len(stored) > 0 {
			for _, p := range stored {
				if _, ok := seen[p]; ok {
					continue
				}
				if delErr := service.DeleteNote(ctx, p); delErr != nil {
					return makeResult(), delErr
				}
				deleted.Add(1)
				deletedPathsMu.Lock()
				deletedPaths = append(deletedPaths, string(paths.NormalizeNotePath(p)))
				deletedPathsMu.Unlock()
			}
		}
	}

	return makeResult(), nil
}

// ValidateNotes validates Markdown code-anchor syntax only. It is an explicit
// parser compatibility path, not a format-neutral ownership operation.
func ValidateNotes(ctx context.Context, root string) (int, int, error) {
	return ValidateMarkdownCodeAnchorSyntaxWithWriter(ctx, root, os.Stderr)
}

// ValidateNotesWithWriter preserves the established command adapter. New
// callers should name the Markdown compatibility boundary directly.
func ValidateNotesWithWriter(ctx context.Context, root string, diagnostics io.Writer) (int, int, error) {
	return ValidateMarkdownCodeAnchorSyntaxWithWriter(ctx, root, diagnostics)
}

// ValidateMarkdownCodeAnchorSyntaxWithWriter walks legacy Markdown files for
// code-anchor syntax. It selects Markdown by the historical .md compatibility
// rule and does not inspect or validate any future provider format.
func ValidateMarkdownCodeAnchorSyntaxWithWriter(ctx context.Context, root string, diagnostics io.Writer) (int, int, error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return 0, 0, nil
	}
	var checked, invalid int
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		err = validateMarkdownCodeAnchorSyntax(path, string(content))
		checked++
		if err != nil {
			invalid++
			if diagnostics != nil {
				fmt.Fprintf(diagnostics, "Invalid code anchors in %s: %v\n", path, err)
			}
		}
		return nil
	})
	return checked, invalid, err
}

// validateMarkdownCodeAnchorSyntax parses a file already selected by the
// Markdown-only validation walk.
func validateMarkdownCodeAnchorSyntax(path, content string) error {
	_, err := codeanchor.ParseNote(path, content)
	return err
}

func IndexRoot(ctx context.Context, service *codeanchor.Service, basePath, root string, matcher *ignore.Matcher, progress *indexingpipe.ProgressCallbacks) (IndexResult, error) {
	vaultPaths, _ := paths.NewVaultPaths(basePath)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return IndexResult{Skipped: make(map[codeanchor.Lang]int), Unsupported: make(map[codeanchor.Lang]int)}, nil
	}
	return indexCandidateRun(ctx, service, basePath, codeIngestPolicy{}, func(ctx context.Context, res *IndexResult, shouldRead func(indexingpipe.FileCandidate) bool, onSkip func(indexingpipe.FileCandidate), process func(indexingpipe.FilePayload) error) error {
		return indexingpipe.ProcessFiles(ctx, indexingpipe.ProcessOptions{
			Root:        root,
			Matcher:     matcher,
			WorkerCount: ClampIndexWorkers(runtime.GOMAXPROCS(0)),
			Progress:    progress,
		}, func(path string, _ os.DirEntry, modTime int64) (indexingpipe.FileCandidate, bool, error) {
			lang := DetectCodeLang(path)
			if lang == "" {
				return indexingpipe.FileCandidate{}, false, nil
			}
			if !service.HasIndexer(lang) {
				res.Skipped[lang]++
				return indexingpipe.FileCandidate{}, false, nil
			}
			relPath := ""
			if rel, err := vaultPaths.RelCodeStrict(path); err == nil {
				relPath = string(paths.NormalizeCode(rel.String()))
			}
			if relPath != "" {
				res.SeenPaths = append(res.SeenPaths, relPath)
			} else {
				res.SeenPaths = append(res.SeenPaths, filepath.ToSlash(path))
			}
			return indexingpipe.FileCandidate{AbsPath: path, RelPath: relPath, ModTime: modTime, Kind: indexingpipe.FileKindCode, Lang: lang}, true, nil
		}, shouldRead, onSkip, process)
	})
}

// IndexCandidates indexes only the supplied, preclassified code candidates.
// It deliberately performs no walk, extension detection, or stale-path
// cleanup; ownership orchestration owns those decisions. Lang is passed to the
// code indexer exactly as supplied by the candidate.
func IndexCandidates(ctx context.Context, service *codeanchor.Service, basePath string, candidates []indexingpipe.FileCandidate, progress *indexingpipe.ProgressCallbacks) (IndexResult, error) {
	if err := validateCodeCandidates(basePath, candidates); err != nil {
		return IndexResult{}, err
	}
	return indexCandidateRun(ctx, service, basePath, codeIngestPolicy{failCandidateErrors: true}, func(ctx context.Context, res *IndexResult, shouldRead func(indexingpipe.FileCandidate) bool, onSkip func(indexingpipe.FileCandidate), process func(indexingpipe.FilePayload) error) error {
		for _, candidate := range candidates {
			if candidate.RelPath != "" {
				res.SeenPaths = append(res.SeenPaths, candidate.RelPath)
			} else {
				res.SeenPaths = append(res.SeenPaths, filepath.ToSlash(candidate.AbsPath))
			}
		}
		return indexingpipe.ProcessCandidates(ctx, candidates, indexingpipe.CandidateProcessOptions{
			WorkerCount: ClampIndexWorkers(runtime.GOMAXPROCS(0)),
			Progress:    progress,
			OnReadError: func(_ context.Context, candidate indexingpipe.FileCandidate, err error) error {
				return &CodeCandidateReadError{Path: candidate.RelPath, Err: err}
			},
		}, shouldRead, onSkip, process)
	})
}

func indexCandidateRun(ctx context.Context, service *codeanchor.Service, basePath string, policy codeIngestPolicy, run codeCandidateRun) (IndexResult, error) {
	ctx = codeanchor.WithBatchIndexing(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	res := IndexResult{
		Skipped:     make(map[codeanchor.Lang]int),
		Unsupported: make(map[codeanchor.Lang]int),
	}
	vaultPaths, _ := paths.NewVaultPaths(basePath)
	var indexed atomic.Int64
	var unchanged atomic.Int64
	var lastPath atomic.Value
	lastPath.Store("")
	done := make(chan struct{})
	defer close(done)

	go func() {
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				cur := indexed.Load()
				lp, _ := lastPath.Load().(string)
				if lp != "" {
					if rel, relErr := vaultPaths.RelCodeStrict(lp); relErr == nil && rel.String() != "" {
						lp = rel.String()
					}
				}
				if lp == "" {
					fmt.Fprintf(os.Stderr, "[code] Still indexing (indexed=%d)…\n", cur)
				} else {
					fmt.Fprintf(os.Stderr, "[code] Still indexing (indexed=%d) last=%s\n", cur, filepath.ToSlash(lp))
				}
			}
		}
	}()

	workerCount := ClampIndexWorkers(runtime.GOMAXPROCS(0))
	writeCh := make(chan codeanchor.CodeIndexWork, workerCount*2)
	var semanticCh chan codeanchor.CodeIndexWork
	errCh := make(chan error, 1)
	resultCh := make(chan codeIndexResultFragment, workerCount*2)
	resultDone := make(chan codeIndexReducedResult, 1)
	go func() {
		reducer := newCodeIndexResultReducer()
		for fragment := range resultCh {
			reducer.Apply(ctx, fragment)
			indexingperf.SetGauge(ctx, "codeindex.result_queue_depth", int64(len(resultCh)))
		}
		resultDone <- reducer.Finalize(ctx)
	}()
	writeQueue := writeQueueFromContext(ctx)
	semanticSubmitter := codeSemanticBatchSubmitterFromContext(ctx)
	semanticWorkSubmitter := codeSemanticWorkSubmitterFromContext(ctx)
	var writeWG sync.WaitGroup
	if writeQueue == nil {
		writeWG.Add(1)
		go func() {
			defer writeWG.Done()
			RunAdaptiveBatchWriter(ctx, cancel, writeCh, errCh, func(batch []codeanchor.CodeIndexWork) error {
				return service.ApplyCodeIndexBatch(ctx, batch)
			})
		}()
	}
	var semanticWG sync.WaitGroup
	if semanticSubmitter != nil && semanticWorkSubmitter == nil {
		semanticCh = make(chan codeanchor.CodeIndexWork, workerCount*2)
		semanticWG.Add(1)
		go func() {
			defer semanticWG.Done()
			RunAdaptiveBatchWriter(ctx, cancel, semanticCh, errCh, func(batch []codeanchor.CodeIndexWork) error {
				return semanticSubmitter.SubmitPreparedCodeBatch(ctx, batch)
			})
		}()
	}
	err := run(ctx, &res, nil, nil, func(payload indexingpipe.FilePayload) error {
		lastPath.Store(payload.Candidate.AbsPath)
		workerStarted := time.Now()
		defer func() {
			indexingperf.ObserveLatency(ctx, "codeindex.worker_total", time.Since(workerStarted))
		}()
		buildStarted := time.Now()
		// BuildCodeIndexWork owns parser/indexer work only; write and semantic
		// lanes below must stay asynchronous so file workers do not serialize on
		// SQLite commits or embedding-provider backpressure.
		buildCtx := ctx
		if payload.Candidate.ForceRead {
			buildCtx = codeanchor.WithForceReindex(buildCtx)
		}
		work, err := service.BuildCodeIndexWork(buildCtx, payload.Candidate.Lang, payload.Candidate.AbsPath, payload.Content)
		buildDur := time.Since(buildStarted)
		indexingperf.ObserveLatency(ctx, "process.code", buildDur)
		indexingperf.ObserveLatency(ctx, "codeindex.worker_build", buildDur)
		if err != nil {
			if policy.failCandidateErrors {
				return &CodeCandidateBuildError{Path: payload.Candidate.RelPath, Err: err}
			}
			if errors.Is(err, codeanchor.ErrUnsupportedLanguage) {
				resultSubmitStarted := time.Now()
				fragment := codeIndexResultFragment{unsupportedLang: payload.Candidate.Lang}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case resultCh <- fragment:
					indexingperf.ObserveLatency(ctx, "codeindex.result_submit_wait", time.Since(resultSubmitStarted))
					indexingperf.SetGauge(ctx, "codeindex.result_queue_depth", int64(len(resultCh)))
				}
				return nil
			}
			if errors.Is(err, context.DeadlineExceeded) {
				if payload.Candidate.RelPath != "" {
					fmt.Fprintf(os.Stderr, "[code] Timeout indexing %s\n", payload.Candidate.RelPath)
				} else {
					fmt.Fprintf(os.Stderr, "[code] Timeout indexing %s\n", filepath.ToSlash(payload.Candidate.AbsPath))
				}
				return nil
			}
			return nil
		}
		postBuildStarted := time.Now()
		postBuildLocalStarted := postBuildStarted
		relPath := payload.Candidate.RelPath
		fragment := codeIndexResultFragment{}
		if work == nil {
			unchanged.Add(1)
			resultSubmitStarted := time.Now()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case resultCh <- fragment:
				indexingperf.ObserveLatency(ctx, "codeindex.result_submit_wait", time.Since(resultSubmitStarted))
				indexingperf.SetGauge(ctx, "codeindex.result_queue_depth", int64(len(resultCh)))
			}
			indexingperf.ObserveLatency(ctx, "codeindex.worker_submit_local", time.Since(postBuildLocalStarted))
			return nil
		}
		indexed.Add(1)
		if relPath == "" {
			relPath = filepath.ToSlash(payload.Candidate.AbsPath)
		}
		if relPath != "" {
			fragment.indexed = true
			fragment.indexedPath = relPath
			if work.ReplaceIndex && work.HasRefSignals {
				fragment.changedCallerPath = relPath
			}
			if work.DefDeltas.HasChanges() {
				fragment.defDeltas = work.DefDeltas
			}
		}
		resultSubmitStarted := time.Now()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case resultCh <- fragment:
			indexingperf.ObserveLatency(ctx, "codeindex.result_submit_wait", time.Since(resultSubmitStarted))
			indexingperf.SetGauge(ctx, "codeindex.result_queue_depth", int64(len(resultCh)))
		}
		indexingperf.ObserveLatency(ctx, "codeindex.worker_submit_local", time.Since(postBuildLocalStarted))
		if semanticSubmitter != nil {
			semanticSubmitStarted := time.Now()
			if semanticWorkSubmitter != nil {
				if err := semanticWorkSubmitter.SubmitCodeIndexWork(ctx, *work); err != nil {
					return err
				}
			} else {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case semanticCh <- *work:
				}
			}
			semanticSubmitDur := time.Since(semanticSubmitStarted)
			indexingperf.ObserveLatency(ctx, "codeindex.semantic_submit_wait", semanticSubmitDur)
			indexingperf.ObserveLatency(ctx, "codeindex.worker_submit_semantic", semanticSubmitDur)
		}
		if writeQueue != nil {
			writeSubmitStarted := time.Now()
			err := writeQueue.SubmitCodeIndexWork(ctx, *work)
			writeSubmitDur := time.Since(writeSubmitStarted)
			indexingperf.ObserveLatency(ctx, "codeindex.write_submit_wait", writeSubmitDur)
			indexingperf.ObserveLatency(ctx, "codeindex.worker_submit_write", writeSubmitDur)
			indexingperf.ObserveLatency(ctx, "codeindex.worker_submit", time.Since(postBuildStarted))
			return err
		}
		writeSubmitStarted := time.Now()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case writeCh <- *work:
			writeSubmitDur := time.Since(writeSubmitStarted)
			indexingperf.ObserveLatency(ctx, "codeindex.write_submit_wait", writeSubmitDur)
			indexingperf.ObserveLatency(ctx, "codeindex.worker_submit_write", writeSubmitDur)
			indexingperf.ObserveLatency(ctx, "codeindex.worker_submit", time.Since(postBuildStarted))
			return nil
		}
	})
	close(resultCh)
	if writeQueue == nil {
		writeDrainStarted := time.Now()
		close(writeCh)
		writeWG.Wait()
		indexingperf.ObserveLatency(ctx, "codeindex.write_drain_wait", time.Since(writeDrainStarted))
	} else {
		writeFlushStarted := time.Now()
		flushErr := writeQueue.FlushAndWait(ctx)
		indexingperf.ObserveLatency(ctx, "codeindex.write_flush_wait", time.Since(writeFlushStarted))
		if flushErr != nil && err == nil {
			err = flushErr
		}
	}
	if semanticCh != nil {
		semanticDrainStarted := time.Now()
		close(semanticCh)
		semanticWG.Wait()
		indexingperf.ObserveLatency(ctx, "codeindex.semantic_drain_wait", time.Since(semanticDrainStarted))
	}
	reduced := <-resultDone
	select {
	case writeErr := <-errCh:
		if writeErr != nil {
			return res, writeErr
		}
	default:
	}
	res.ChangedCallerPaths = reduced.changedCallerPaths
	res.DefDeltas = reduced.defDeltas
	MergeLangCounts(res.Unsupported, reduced.unsupported)
	res.Indexed = int(indexed.Load())
	res.Unchanged = int(unchanged.Load())
	res.IndexedPaths = reduced.indexedPaths
	return res, err
}

func MergeLangCounts(dst, src map[codeanchor.Lang]int) {
	for lang, n := range src {
		dst[lang] += n
	}
}

func FormatLangCounts(counts map[codeanchor.Lang]int) string {
	if len(counts) == 0 {
		return ""
	}
	langs := make([]string, 0, len(counts))
	for lang := range counts {
		langs = append(langs, string(lang))
	}
	sort.Strings(langs)
	parts := make([]string, 0, len(langs))
	for _, l := range langs {
		lang := codeanchor.Lang(l)
		parts = append(parts, fmt.Sprintf("%s (%d)", l, counts[lang]))
	}
	return strings.Join(parts, ", ")
}

func DetectCodeLang(path string) codeanchor.Lang {
	ext := strings.ToLower(filepath.Ext(path))
	if codefile.IsTypeScriptJavaScriptExtension(ext) {
		return codeanchor.LangTS
	}
	switch ext {
	case ".py":
		return codeanchor.LangPy
	case ".go":
		return codeanchor.LangGo
	case ".cs":
		return codeanchor.LangCs
	case ".php":
		return codeanchor.LangPhp
	default:
		return ""
	}
}

func PopulateTailIndexFromRoots(idx *codeanchor.PathTailIndex, roots []string) {
	if idx == nil || len(roots) == 0 {
		return
	}

	extOK := make(map[string]struct{}, len(codefile.TypeScriptJavaScriptExtensions()))
	for _, ext := range codefile.TypeScriptJavaScriptExtensions() {
		extOK[ext] = struct{}{}
	}

	for _, root := range roots {
		if root == "" {
			continue
		}
		matcher := ignore.LoadUnifiedMatcher(root, nil)
		rootPaths, _ := paths.NewVaultPaths(root)
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				if matcher != nil {
					if rel, err := rootPaths.RelStrict(path); err == nil && rel.String() != "" {
						if matcher.IsIgnoredShallow(rel.String(), true) {
							return filepath.SkipDir
						}
					}
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if matcher != nil {
				if rel, err := rootPaths.RelStrict(path); err == nil && rel.String() != "" {
					if matcher.IsIgnoredShallow(rel.String(), false) {
						return nil
					}
				}
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if _, ok := extOK[ext]; ok {
				idx.Add(path)
			}
			return nil
		})
	}
}
