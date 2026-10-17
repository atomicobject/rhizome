package codeintel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestResolveRoot_DotResolvesVaultPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	resolved, err := ResolveRoot(root, ".")
	require.NoError(t, err)
	require.NotEmpty(t, resolved)
	require.DirExists(t, resolved)
}

type recordingSemanticSubmitter struct {
	mu         sync.Mutex
	batches    []int
	delay      time.Duration
	callCount  int
	totalWorks int
}

func (s *recordingSemanticSubmitter) SubmitPreparedCodeBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	if s.delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.delay):
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callCount++
	s.batches = append(s.batches, len(batch))
	s.totalWorks += len(batch)
	return nil
}

func (s *recordingSemanticSubmitter) snapshot() (calls int, total int, maxBatch int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, size := range s.batches {
		if size > maxBatch {
			maxBatch = size
		}
	}
	return s.callCount, s.totalWorks, maxBatch
}

type directRecordingSemanticSubmitter struct {
	mu         sync.Mutex
	workCalls  int
	batchCalls int
}

func (s *directRecordingSemanticSubmitter) SubmitPreparedCodeBatch(context.Context, []codeanchor.CodeIndexWork) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls++
	return nil
}

func (s *directRecordingSemanticSubmitter) SubmitCodeIndexWork(context.Context, codeanchor.CodeIndexWork) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workCalls++
	return nil
}

func (s *directRecordingSemanticSubmitter) snapshot() (workCalls int, batchCalls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workCalls, s.batchCalls
}

type delayedLanguageIndexer struct {
	inner codeanchor.LanguageIndexer
	delay time.Duration
}

func (d delayedLanguageIndexer) IndexFile(content []byte, path paths.CodePathRef) (codeanchor.FileSummary, error) {
	if d.delay > 0 {
		time.Sleep(d.delay)
	}
	return d.inner.IndexFile(content, path)
}

func (d delayedLanguageIndexer) Lang() codeanchor.Lang {
	return d.inner.Lang()
}

type delayedDirectSemanticSubmitter struct {
	delay time.Duration
}

func (s delayedDirectSemanticSubmitter) SubmitPreparedCodeBatch(context.Context, []codeanchor.CodeIndexWork) error {
	return nil
}

func (s delayedDirectSemanticSubmitter) SubmitCodeIndexWork(context.Context, codeanchor.CodeIndexWork) error {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return nil
}

type delayedWriteQueue struct {
	delay time.Duration
}

func (q delayedWriteQueue) SubmitCodeIndexWork(context.Context, codeanchor.CodeIndexWork) error {
	if q.delay > 0 {
		time.Sleep(q.delay)
	}
	return nil
}

func (q delayedWriteQueue) SubmitNoteIndexWork(context.Context, codeanchor.NoteIndexWork) error {
	return nil
}

func (q delayedWriteQueue) FlushAndWait(context.Context) error {
	return nil
}

type bulkSymbolIndexer struct {
	lang        codeanchor.Lang
	symbolCount int
}

func (b bulkSymbolIndexer) IndexFile(_ []byte, path paths.CodePathRef) (codeanchor.FileSummary, error) {
	base := strings.TrimSuffix(filepath.Base(path.Rel.String()), filepath.Ext(path.Rel.String()))
	symbols := make([]codeanchor.Symbol, 0, b.symbolCount)
	for i := 0; i < b.symbolCount; i++ {
		symbols = append(symbols, codeanchor.Symbol{
			Name: fmt.Sprintf("%s_%03d", base, i),
			Kind: "function",
			Lang: b.lang,
			Pkg:  path.Rel.String(),
		})
	}
	return codeanchor.FileSummary{
		Lang:        b.lang,
		ParseStatus: codeanchor.ParseOK,
		Symbols:     symbols,
	}, nil
}

func (b bulkSymbolIndexer) Lang() codeanchor.Lang {
	return b.lang
}

func TestIndexCandidates_BatchesEarlySemanticSubmission(t *testing.T) {
	root := t.TempDir()
	codeRoot := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(codeRoot, 0o755))
	for i := 0; i < 20; i++ {
		path := filepath.Join(codeRoot, fmt.Sprintf("file%d.go", i))
		require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("package pkg\nfunc Foo%d() {}\n", i)), 0o644))
	}

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{bulkSymbolIndexer{lang: codeanchor.LangGo, symbolCount: 8}},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)

	submitter := &recordingSemanticSubmitter{delay: 20 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ctx = WithCodeSemanticBatchSubmitter(ctx, submitter)

	result, err := IndexCandidates(ctx, svc, newCandidateTestWriter(t, svc), root, codeCandidatesForTest(t, root, codeRoot, svc), nil)
	require.NoError(t, err)
	require.Equal(t, 20, result.Indexed)

	calls, total, maxBatch := submitter.snapshot()
	require.Equal(t, 20, total)
	require.GreaterOrEqual(t, maxBatch, 2)
	require.Less(t, calls, 20)
}

func TestIndexCandidates_UsesDirectSemanticWorkSubmitterWhenAvailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	codeRoot := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(codeRoot, 0o755))
	for i := 0; i < 6; i++ {
		path := filepath.Join(codeRoot, fmt.Sprintf("file%d.go", i))
		require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("package pkg\nfunc Foo%d() {}\n", i)), 0o644))
	}

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)

	submitter := &directRecordingSemanticSubmitter{}
	ctx := WithCodeSemanticBatchSubmitter(context.Background(), submitter)

	result, err := IndexCandidates(ctx, svc, newCandidateTestWriter(t, svc), root, codeCandidatesForTest(t, root, codeRoot, svc), nil)
	require.NoError(t, err)
	require.Equal(t, 6, result.Indexed)

	workCalls, batchCalls := submitter.snapshot()
	require.Equal(t, 6, workCalls)
	require.Zero(t, batchCalls)
}

func TestIndexCandidates_WorkerSubmitExcludesBuildTime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	codeRoot := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(codeRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(codeRoot, "file.go"), []byte("package pkg\nfunc Foo() {}\n"), 0o644))

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{delayedLanguageIndexer{
			inner: codeanchor.NewGoIndexer(),
			delay: 300 * time.Millisecond,
		}},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	ctx = indexingperf.WithPhase(ctx, "index_code")
	ctx = WithCodeSemanticBatchSubmitter(ctx, delayedDirectSemanticSubmitter{delay: 40 * time.Millisecond})
	writer := delayedWriteQueue{delay: 50 * time.Millisecond}

	done := indexingperf.StartSpan(ctx, "index_code")
	result, err := IndexCandidates(ctx, svc, writer, root, codeCandidatesForTest(t, root, codeRoot, svc), nil)
	done(err)
	require.NoError(t, err)
	require.Equal(t, 1, result.Indexed)

	summary := collector.RenderSummary()
	workerBuild := requireDurationMetric(t, summary, "worker_build_cum")
	semanticSubmit := requireDurationMetric(t, summary, "semantic_submit_cum")
	writeSubmit := requireDurationMetric(t, summary, "write_submit_cum")
	workerSubmit := requireDurationMetric(t, summary, "worker_submit_cum")
	workerTotal := requireDurationMetric(t, summary, "worker_total_cum")

	require.GreaterOrEqual(t, workerBuild, 250*time.Millisecond)
	require.GreaterOrEqual(t, semanticSubmit, 35*time.Millisecond)
	require.GreaterOrEqual(t, writeSubmit, 45*time.Millisecond)
	require.GreaterOrEqual(t, workerSubmit, semanticSubmit+writeSubmit)
	// workerSubmit must not include build time. Compare against the measured
	// build span instead of an absolute upper bound so slow/Windows CI scheduler
	// jitter in the submit path does not make the invariant flaky.
	require.GreaterOrEqual(t, workerTotal-workerSubmit, workerBuild-75*time.Millisecond)
}

func TestIndexCandidates_ReducerPreservesConcurrentFragments(t *testing.T) {
	root := t.TempDir()
	codeRoot := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(codeRoot, 0o755))
	fileCount := 48
	symbolsPerFile := 24
	for i := 0; i < fileCount; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(codeRoot, fmt.Sprintf("file%03d.go", i)), []byte("package pkg\n"), 0o644))
	}

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	restoreMaxProcs := runtime.GOMAXPROCS(8)
	defer runtime.GOMAXPROCS(restoreMaxProcs)

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{bulkSymbolIndexer{lang: codeanchor.LangGo, symbolCount: symbolsPerFile}},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	ctx = indexingperf.WithPhase(ctx, "index_code")
	done := indexingperf.StartSpan(ctx, "index_code")
	result, err := IndexCandidates(ctx, svc, newCandidateTestWriter(t, svc), root, codeCandidatesForTest(t, root, codeRoot, svc), nil)
	done(err)
	require.NoError(t, err)
	require.Equal(t, fileCount, result.Indexed)
	require.Len(t, result.IndexedPaths, fileCount)
	require.Len(t, result.DefDeltas.AddedSymbols, fileCount*symbolsPerFile)
	require.Empty(t, result.ChangedCallerPaths)
	require.Empty(t, result.Unsupported)

	seen := make(map[string]struct{}, len(result.IndexedPaths))
	for _, path := range result.IndexedPaths {
		require.NotEmpty(t, path)
		seen[path] = struct{}{}
	}
	require.Len(t, seen, fileCount)

	if runtime.GOOS == "windows" {
		// The summary omits zero totals, and the coarse Windows clock can
		// measure a sub-millisecond reduce as zero (seen on CI).
		return
	}
	summary := collector.RenderSummary()
	require.Contains(t, summary, "result_reduce_cum=")
	require.Contains(t, summary, "result_reduce_defdeltas_cum=")
	require.Contains(t, summary, "result_finalize_cum=")
}

func TestAsyncCodeSemanticSubmitterBatchesAndFlushesOnClose(t *testing.T) {
	t.Parallel()

	inner := &recordingSemanticSubmitter{}
	submitter := NewAsyncCodeSemanticSubmitter(context.Background(), inner, nil)
	require.NotNil(t, submitter)

	for i := 0; i < 20; i++ {
		err := submitter.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{
			Path: fmt.Sprintf("pkg/file%d.go", i),
		})
		require.NoError(t, err)
	}

	require.NoError(t, submitter.Close())

	calls, total, maxBatch := inner.snapshot()
	require.Equal(t, 20, total)
	require.GreaterOrEqual(t, maxBatch, 2)
	require.Less(t, calls, 20)
}

func requireDurationMetric(t *testing.T, summary, name string) time.Duration {
	t.Helper()
	marker := name + "="
	idx := strings.Index(summary, marker)
	require.NotEqual(t, -1, idx, "missing metric %s in summary:\n%s", name, summary)
	value := summary[idx+len(marker):]
	if end := strings.IndexByte(value, ' '); end >= 0 {
		value = value[:end]
	}
	dur, err := time.ParseDuration(value)
	require.NoError(t, err, "parse %s=%q", name, value)
	return dur
}
