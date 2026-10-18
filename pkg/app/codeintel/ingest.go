package codeintel

import (
	"context"
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
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
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
// shared Markdown processing core. Ownership selection remains outside this
// bounded read/build/write path.
type markdownCandidateRun func(
	context.Context,
	func(FileCandidate) bool,
	func(FileCandidate),
	func(FilePayload) error,
) error

// markdownIngestPolicy keeps legacy discovery cleanup separate from explicit
// ownership-routed candidate intake. Candidate callers report parse failures
// but never retire durable ownership or derived state themselves.
// codeCandidateRun supplies already-selected code candidates to the shared
// code processing core. Classification remains an orchestration concern.
type codeCandidateRun func(
	context.Context,
	*IndexResult,
	func(FileCandidate) bool,
	func(FileCandidate),
	func(FilePayload) error,
) error

// codeIngestPolicy separates legacy root-walk tolerance from strict,
// ownership-routed candidate intake. A strict run returns filesystem and work
// build failures so reconciliation cannot acknowledge incomplete code state.
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

func validateCodeCandidates(basePath string, candidates []FileCandidate) error {
	vaultPaths, err := paths.NewVaultPaths(basePath)
	if err != nil || vaultPaths.Root() == "" {
		return fmt.Errorf("code candidate vault root is required")
	}
	for _, candidate := range candidates {
		if candidate.Kind != FileKindCode {
			return fmt.Errorf("code candidate %q has kind %q, want %q", candidate.RelPath, candidate.Kind, FileKindCode)
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
		indexingperf.ObserveLatency(ctx, indexingperf.CodeindexResultReduceIndexed, time.Since(partStarted))
	}
	if fragment.changedCallerPath != "" {
		partStarted := time.Now()
		r.changedCallers[fragment.changedCallerPath] = struct{}{}
		indexingperf.ObserveLatency(ctx, indexingperf.CodeindexResultReduceChangedCallers, time.Since(partStarted))
	}
	if fragment.defDeltas.HasChanges() {
		partStarted := time.Now()
		r.defDeltas.Add(fragment.defDeltas)
		indexingperf.ObserveLatency(ctx, indexingperf.CodeindexResultReduceDefdeltas, time.Since(partStarted))
	}
	if fragment.unsupportedLang != "" {
		r.unsupported[fragment.unsupportedLang]++
	}
	indexingperf.ObserveLatency(ctx, indexingperf.CodeindexResultReduce, time.Since(started))
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
	indexingperf.ObserveLatency(ctx, indexingperf.CodeindexResultFinalize, time.Since(started))
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

func ingestMarkdownCandidateRun(ctx context.Context, service *codeanchor.Service, writeQueue WriteQueue, noteMetadata notemeta.Indexer, run markdownCandidateRun) (NoteIngestResult, error) {
	ctx = codeanchor.WithBatchIndexing(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	storedNoteMeta, err := service.NoteIndexMeta(ctx)
	if err != nil {
		return NoteIngestResult{}, fmt.Errorf("load note index metadata: %w", err)
	}
	var count atomic.Int64
	var skipped atomic.Int64
	var hasAnchors atomic.Bool
	var changedPathsMu sync.Mutex
	var changedPaths []string
	var buildErrorsMu sync.Mutex
	var buildErrors []NoteIngestBuildError
	if writeQueue == nil {
		return NoteIngestResult{}, fmt.Errorf("note index writer is required")
	}
	err = run(ctx, func(candidate FileCandidate) bool {
		if candidate.ForceRead {
			return true
		}
		if candidate.RelPath == "" || storedNoteMeta == nil {
			return true
		}
		meta, ok := storedNoteMeta[candidate.RelPath]
		return !(ok && meta.IndexerVersion == codeanchor.NoteIndexerVersion && candidate.ModTime > 0 && meta.Mtime >= candidate.ModTime)
	}, func(FileCandidate) {
		skipped.Add(1)
	}, func(payload FilePayload) error {
		meta, hasMeta := codeanchor.NoteIndexMeta{}, false
		if payload.Candidate.RelPath != "" && storedNoteMeta != nil {
			meta, hasMeta = storedNoteMeta[payload.Candidate.RelPath]
		}
		contentHash := HashContentBytes(payload.Content)
		if !payload.Candidate.ForceRead && hasMeta && meta.IndexerVersion == codeanchor.NoteIndexerVersion && meta.ContentHash == contentHash {
			// The owning metadata publication refreshes unchanged source mtimes.
			skipped.Add(1)

			return nil
		}
		processStarted := time.Now()
		sourcePath := payload.Candidate.RelPath
		source, err := noteMetadata.ProjectNoteSource(sourcePath, string(payload.Content), payload.Candidate.ModTime)
		if err != nil {
			return err
		}

		if source.Format != noteformat.FormatID("markdown") {
			return fmt.Errorf("Markdown candidate %q has source format %q", payload.Candidate.RelPath, source.Format)
		}
		buildCtx := ctx
		if payload.Candidate.ForceRead {
			buildCtx = codeanchor.WithForceReindex(buildCtx)
		}
		work, err := service.BuildNoteIndexWorkFromSource(buildCtx, source)
		indexingperf.ObserveLatency(ctx, indexingperf.ProcessNote, time.Since(processStarted))
		if err != nil {
			buildErrorsMu.Lock()
			buildErrors = append(buildErrors, NoteIngestBuildError{
				Path:  sourcePath,
				Error: err.Error(),
			})
			buildErrorsMu.Unlock()
			indexingperf.AddCount(ctx, indexingperf.NoteindexBuildError, 1)

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
		return writeQueue.SubmitNoteIndexWork(ctx, *work)
	})
	if flushErr := writeQueue.FlushAndWait(ctx); flushErr != nil {
		return NoteIngestResult{}, flushErr
	}

	makeResult := func() NoteIngestResult {
		return NoteIngestResult{
			Count:        int(count.Load()),
			Unchanged:    int(skipped.Load()),
			HasAnchors:   hasAnchors.Load(),
			ChangedPaths: changedPaths,
			BuildErrors:  buildErrors,
		}
	}

	if err != nil {
		return makeResult(), err
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

// IndexCandidates indexes only the supplied, preclassified code candidates.
// It deliberately performs no walk, extension detection, or stale-path
// cleanup; ownership orchestration owns those decisions. Lang is passed to the
// code indexer exactly as supplied by the candidate.
func IndexCandidates(ctx context.Context, service *codeanchor.Service, writeQueue WriteQueue, basePath string, candidates []FileCandidate, progress *ProgressCallbacks) (IndexResult, error) {
	if err := validateCodeCandidates(basePath, candidates); err != nil {
		return IndexResult{}, err
	}
	return indexCandidateRun(ctx, service, writeQueue, basePath, func(ctx context.Context, res *IndexResult, shouldRead func(FileCandidate) bool, onSkip func(FileCandidate), process func(FilePayload) error) error {
		for _, candidate := range candidates {
			if candidate.RelPath != "" {
				res.SeenPaths = append(res.SeenPaths, candidate.RelPath)
			} else {
				res.SeenPaths = append(res.SeenPaths, filepath.ToSlash(candidate.AbsPath))
			}
		}
		return ProcessCandidates(ctx, candidates, CandidateProcessOptions{
			WorkerCount: ClampIndexWorkers(runtime.GOMAXPROCS(0)),
			Progress:    progress,
			OnReadError: func(_ context.Context, candidate FileCandidate, err error) error {
				return &CodeCandidateReadError{Path: candidate.RelPath, Err: err}
			},
		}, shouldRead, onSkip, process)
	})
}

func indexCandidateRun(ctx context.Context, service *codeanchor.Service, writeQueue WriteQueue, basePath string, run codeCandidateRun) (IndexResult, error) {
	if writeQueue == nil {
		return IndexResult{}, fmt.Errorf("code index writer is required")
	}
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
	var semanticCh chan codeanchor.CodeIndexWork
	errCh := make(chan error, 1)
	resultCh := make(chan codeIndexResultFragment, workerCount*2)
	resultDone := make(chan codeIndexReducedResult, 1)
	go func() {
		reducer := newCodeIndexResultReducer()
		for fragment := range resultCh {
			reducer.Apply(ctx, fragment)
			indexingperf.SetGauge(ctx, indexingperf.CodeindexResultQueueDepth, int64(len(resultCh)))
		}
		resultDone <- reducer.Finalize(ctx)
	}()
	semanticSubmitter := codeSemanticBatchSubmitterFromContext(ctx)
	semanticWorkSubmitter := codeSemanticWorkSubmitterFromContext(ctx)
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
	err := run(ctx, &res, nil, nil, func(payload FilePayload) error {
		lastPath.Store(payload.Candidate.AbsPath)
		workerStarted := time.Now()
		defer func() {
			indexingperf.ObserveLatency(ctx, indexingperf.CodeindexWorkerTotal, time.Since(workerStarted))
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
		indexingperf.ObserveLatency(ctx, indexingperf.ProcessCode, buildDur)
		indexingperf.ObserveLatency(ctx, indexingperf.CodeindexWorkerBuild, buildDur)
		if err != nil {
			return &CodeCandidateBuildError{Path: payload.Candidate.RelPath, Err: err}
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
				indexingperf.ObserveLatency(ctx, indexingperf.CodeindexResultSubmitWait, time.Since(resultSubmitStarted))
				indexingperf.SetGauge(ctx, indexingperf.CodeindexResultQueueDepth, int64(len(resultCh)))
			}
			indexingperf.ObserveLatency(ctx, indexingperf.CodeindexWorkerSubmitLocal, time.Since(postBuildLocalStarted))
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
			indexingperf.ObserveLatency(ctx, indexingperf.CodeindexResultSubmitWait, time.Since(resultSubmitStarted))
			indexingperf.SetGauge(ctx, indexingperf.CodeindexResultQueueDepth, int64(len(resultCh)))
		}
		indexingperf.ObserveLatency(ctx, indexingperf.CodeindexWorkerSubmitLocal, time.Since(postBuildLocalStarted))
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
			indexingperf.ObserveLatency(ctx, indexingperf.CodeindexSemanticSubmitWait, semanticSubmitDur)
			indexingperf.ObserveLatency(ctx, indexingperf.CodeindexWorkerSubmitSemantic, semanticSubmitDur)
		}
		writeSubmitStarted := time.Now()
		err = writeQueue.SubmitCodeIndexWork(ctx, *work)
		writeSubmitDur := time.Since(writeSubmitStarted)
		indexingperf.ObserveLatency(ctx, indexingperf.CodeindexWriteSubmitWait, writeSubmitDur)
		indexingperf.ObserveLatency(ctx, indexingperf.CodeindexWorkerSubmitWrite, writeSubmitDur)
		indexingperf.ObserveLatency(ctx, indexingperf.CodeindexWorkerSubmit, time.Since(postBuildStarted))
		return err
	})
	close(resultCh)
	writeFlushStarted := time.Now()
	flushErr := writeQueue.FlushAndWait(ctx)
	indexingperf.ObserveLatency(ctx, indexingperf.CodeindexWriteFlushWait, time.Since(writeFlushStarted))
	if flushErr != nil && err == nil {
		err = flushErr
	}
	if semanticCh != nil {
		semanticDrainStarted := time.Now()
		close(semanticCh)
		semanticWG.Wait()
		indexingperf.ObserveLatency(ctx, indexingperf.CodeindexSemanticDrainWait, time.Since(semanticDrainStarted))
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
