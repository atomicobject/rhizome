package codeintel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIngestNotesWithMatcher_SkipsIgnoredAndVendor(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.md"), []byte("# Keep"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "ignored"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ignored", "skip.md"), []byte("# Skip"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "vendor", "vend.md"), []byte("# Vend"), 0o644))

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))
	matcher := ignore.NewMatcher([]string{"ignored/**"})

	result, err := IngestNotesWithMatcher(context.Background(), svc, root, matcher, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)

	notePaths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Len(t, notePaths, 1)
	require.Equal(t, string(paths.NormalizeNote("keep.md")), notePaths[0])
}

func TestIngestNotesWithMatcher_PreservesMixedCaseMarkdownIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repositoryNote := filepath.Join(root, "Decision.MD")
	require.NoError(t, os.WriteFile(repositoryNote, []byte("# Decision"), 0o644))

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))
	result, err := IngestNotesWithMatcher(context.Background(), svc, root, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	require.Equal(t, []string{"Decision.MD"}, result.ChangedPaths)

	notePaths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"Decision.MD"}, notePaths)
	require.NotContains(t, notePaths, "Decision.MD.md")
}

func TestIngestNotesWithMatcher_PreservesLowercaseMarkdownIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "decision.md"), []byte("# Decision"), 0o644))

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))
	result, err := IngestNotesWithMatcher(context.Background(), svc, root, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	require.Equal(t, []string{"decision.md"}, result.ChangedPaths)
}

func TestIngestNotesWithMatcher_ReportsBuildErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "good.md"), []byte("# Good"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "bad.md"), []byte(`---
code-anchors:
  go:
    - symbol: Unqualified
---
# Bad
`), 0o644))

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))
	result, err := IngestNotesWithMatcher(context.Background(), svc, root, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	require.Len(t, result.BuildErrors, 1)
	require.Equal(t, "bad.md", result.BuildErrors[0].Path)
	require.Contains(t, result.BuildErrors[0].Error, "symbol must be fully-qualified")

	notePaths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"good.md"}, notePaths)
}

func TestIngestNotesWithMatcher_BuildErrorPrunesPreviouslyIndexedNote(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	notePath := filepath.Join(root, "bad.md")
	require.NoError(t, os.WriteFile(notePath, []byte("# Was Good\n"), 0o644))

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))
	require.NoError(t, store.UpsertNote(context.Background(), codeanchor.Note{
		Path:  "bad.md",
		Title: "Was Good",
	}))

	require.NoError(t, os.WriteFile(notePath, []byte(`---
code-anchors:
  go:
    - symbol: Unqualified
---
# Bad
`), 0o644))

	second, err := IngestNotesWithMatcher(context.Background(), svc, root, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 0, second.Count)
	require.Equal(t, 1, second.Deleted)
	require.Equal(t, []string{"bad.md"}, second.DeletedPaths)
	require.Len(t, second.BuildErrors, 1)

	notePaths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Empty(t, notePaths)
}

func TestIngestNotesWithMatcher_SkipsMtimeOnlyChangesByHash(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	notePath := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(notePath, []byte("# Note"), 0o644))

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))
	matcher := ignore.NewMatcher(nil)

	first, err := IngestNotesWithMatcher(context.Background(), svc, root, matcher, nil)
	require.NoError(t, err)
	require.Equal(t, 1, first.Count)

	before, err := store.IntelNoteMtimes(context.Background())
	require.NoError(t, err)
	require.NotZero(t, before["note.md"])

	newMtime := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(notePath, newMtime, newMtime))

	second, err := IngestNotesWithMatcher(context.Background(), svc, root, matcher, nil)
	require.NoError(t, err)
	require.Equal(t, 0, second.Count)
	require.Equal(t, 1, second.Unchanged)

	after, err := store.IntelNoteMtimes(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, after["note.md"], newMtime.Unix())
}

func TestIngestNotesWithMatcher_PrunesDeletedNotesOnNoOpPass(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	noteA := filepath.Join(root, "a.md")
	noteB := filepath.Join(root, "b.md")
	require.NoError(t, os.WriteFile(noteA, []byte("# A\nbody"), 0o644))
	require.NoError(t, os.WriteFile(noteB, []byte("# B\nbody"), 0o644))

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))

	first, err := IngestNotesWithMatcher(context.Background(), svc, root, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 2, first.Count)
	require.Equal(t, 0, first.Deleted)

	require.NoError(t, os.Remove(noteB))

	second, err := IngestNotesWithMatcher(context.Background(), svc, root, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 1, second.Deleted)
	require.Equal(t, 0, second.Count)

	notePaths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"a.md"}, notePaths)

	sections, err := store.IntelDocSections(context.Background())
	require.NoError(t, err)
	for _, sec := range sections {
		require.NotEqual(t, "b.md", sec.Path)
	}
}

func TestNoteSourceSnapshotFullAndLiveIngestConverge(t *testing.T) {
	root := t.TempDir()
	fullStore, err := semdb.Open(filepath.Join(root, "full.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = fullStore.Close() })
	liveStore, err := semdb.Open(filepath.Join(root, "live.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = liveStore.Close() })

	fullSvc := codeanchor.NewServiceWithOptions(fullStore, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	liveSvc := codeanchor.NewServiceWithOptions(liveStore, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	matcher := ignore.NewMatcher(nil)
	ctx := context.Background()

	assertConverged := func(path string, content string, mtime int64) {
		t.Helper()
		_, err := IngestNotesWithMatcher(ctx, fullSvc, root, matcher, nil)
		require.NoError(t, err)
		_, err = liveSvc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot(path, content, mtime))
		require.NoError(t, err)

		fullPaths, err := fullStore.NotePaths(ctx)
		require.NoError(t, err)
		livePaths, err := liveStore.NotePaths(ctx)
		require.NoError(t, err)
		require.Equal(t, fullPaths, livePaths)

		fullMeta, err := fullStore.IntelNoteIndexMeta(ctx)
		require.NoError(t, err)
		liveMeta, err := liveStore.IntelNoteIndexMeta(ctx)
		require.NoError(t, err)
		require.Equal(t, fullMeta, liveMeta)

		fullSections, err := fullStore.IntelDocSections(ctx)
		require.NoError(t, err)
		liveSections, err := liveStore.IntelDocSections(ctx)
		require.NoError(t, err)
		require.Equal(t, fullSections, liveSections)
	}

	path := filepath.Join(root, "note.md")
	content := "# Note\n\ncreated"
	mtime := time.Unix(1_700_000_000, 0)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
	assertConverged("note.md", content, mtime.Unix())

	content = "# Note\n\nupdated"
	mtime = mtime.Add(time.Minute)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
	assertConverged("note.md", content, mtime.Unix())

	renamed := filepath.Join(root, "renamed.md")
	require.NoError(t, os.Rename(path, renamed))
	_, err = IngestNotesWithMatcher(ctx, fullSvc, root, matcher, nil)
	require.NoError(t, err)
	require.NoError(t, liveSvc.DeleteNote(ctx, "note.md"))
	_, err = liveSvc.IngestNoteSource(ctx, notemeta.NewContentOnlyNoteSourceSnapshot("renamed.md", content, mtime.Unix()))
	require.NoError(t, err)
	fullPaths, err := fullStore.NotePaths(ctx)
	require.NoError(t, err)
	livePaths, err := liveStore.NotePaths(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"renamed.md"}, fullPaths)
	require.Equal(t, fullPaths, livePaths)

	require.NoError(t, os.Remove(renamed))
	_, err = IngestNotesWithMatcher(ctx, fullSvc, root, matcher, nil)
	require.NoError(t, err)
	require.NoError(t, liveSvc.DeleteNote(ctx, "renamed.md"))
	fullPaths, err = fullStore.NotePaths(ctx)
	require.NoError(t, err)
	livePaths, err = liveStore.NotePaths(ctx)
	require.NoError(t, err)
	require.Empty(t, fullPaths)
	require.Equal(t, fullPaths, livePaths)
}

func TestIngestNotesForVault_PrunesOutOfScopeCollectionNotes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inScope := filepath.Join(root, "docs", "keep.md")
	outOfScope := filepath.Join(root, "scratch", "drop.md")
	contextDoc := filepath.Join(root, "src", "service", "CONTEXT.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(inScope), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(outOfScope), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(contextDoc), 0o755))
	require.NoError(t, os.WriteFile(inScope, []byte("# Keep\nbody"), 0o644))
	require.NoError(t, os.WriteFile(outOfScope, []byte("# Drop\nbody"), 0o644))
	require.NoError(t, os.WriteFile(contextDoc, []byte("# Service context\nbody"), 0o644))

	dbPath := filepath.Join(root, "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))

	seed, err := IngestNotesWithMatcher(context.Background(), svc, root, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 3, seed.Count)

	vaultDef := obsidian.VaultDefinition{
		Root:     root,
		Includes: []string{"docs/**/*.md"},
	}
	result, err := IngestNotesForVault(context.Background(), svc, vaultDef, obsidian.LoadVaultIgnoreMatcher(root, nil), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Deleted)
	require.Equal(t, []string{"scratch/drop.md"}, result.DeletedPaths)

	notePaths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"docs/keep.md", "src/service/CONTEXT.md"}, notePaths)
}

func TestIngestNotesForVault_DirExcludeKeepsOnlySystemContext(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	publicNote := filepath.Join(root, "public.md")
	privateNote := filepath.Join(root, "private", "ordinary.md")
	contextDoc := filepath.Join(root, "private", "CONTEXT.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(privateNote), 0o755))
	require.NoError(t, os.WriteFile(publicNote, []byte("# Public\nbody"), 0o644))
	require.NoError(t, os.WriteFile(privateNote, []byte("# Private\nbody"), 0o644))
	require.NoError(t, os.WriteFile(contextDoc, []byte("# Private context\nbody"), 0o644))

	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root))

	seed, err := IngestNotesWithMatcher(context.Background(), svc, root, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 3, seed.Count)

	vaultDef := obsidian.VaultDefinition{
		Root:     root,
		Includes: []string{"**/*.md"},
		Excludes: []string{"private/"},
	}
	result, err := IngestNotesForVault(context.Background(), svc, vaultDef, obsidian.LoadVaultIgnoreMatcher(root, vaultDef.Excludes), nil)
	require.NoError(t, err)
	require.Equal(t, []string{"private/ordinary.md"}, result.DeletedPaths)

	notePaths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"private/CONTEXT.md", "public.md"}, notePaths)
}

func TestResolveRoot_DotResolvesVaultPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	resolved, err := ResolveRoot(root, ".")
	require.NoError(t, err)
	require.NotEmpty(t, resolved)
	require.DirExists(t, resolved)
}

func TestIndexRootSeenPathsAreVaultRelative(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	codeRoot := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(codeRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(codeRoot, "main.go"), []byte("package pkg\nfunc Foo() {}\n"), 0o644))

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

	result, err := IndexRoot(context.Background(), svc, root, codeRoot, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, []string{"pkg/main.go"}, result.SeenPaths)
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

func TestIndexRoot_BatchesEarlySemanticSubmission(t *testing.T) {
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

	result, err := IndexRoot(ctx, svc, root, codeRoot, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 20, result.Indexed)

	calls, total, maxBatch := submitter.snapshot()
	require.Equal(t, 20, total)
	require.GreaterOrEqual(t, maxBatch, 2)
	require.Less(t, calls, 20)
}

func TestIndexRoot_UsesDirectSemanticWorkSubmitterWhenAvailable(t *testing.T) {
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

	result, err := IndexRoot(ctx, svc, root, codeRoot, ignore.NewMatcher(nil), nil)
	require.NoError(t, err)
	require.Equal(t, 6, result.Indexed)

	workCalls, batchCalls := submitter.snapshot()
	require.Equal(t, 6, workCalls)
	require.Zero(t, batchCalls)
}

func TestIndexRoot_WorkerSubmitExcludesBuildTime(t *testing.T) {
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
	ctx = WithWriteQueue(ctx, delayedWriteQueue{delay: 50 * time.Millisecond})

	done := indexingperf.StartSpan(ctx, "index_code")
	result, err := IndexRoot(ctx, svc, root, codeRoot, ignore.NewMatcher(nil), nil)
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

func TestIndexRoot_ReducerPreservesConcurrentFragments(t *testing.T) {
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
	result, err := IndexRoot(ctx, svc, root, codeRoot, ignore.NewMatcher(nil), nil)
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

type failingNoteBatchStore struct {
	*semdb.Store
	err error
}

func (s *failingNoteBatchStore) UpsertNotesWithCleanupBatch(ctx context.Context, notes []codeanchor.NoteWithKeepLabels) (codeanchor.AnchorUpsertResult, error) {
	return codeanchor.AnchorUpsertResult{}, s.err
}

func TestIngestNotesWithMatcher_ReturnsBatchWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note"), 0o644))
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	errBoom := errors.New("batch write failed")
	svc := codeanchor.NewServiceWithOptions(&failingNoteBatchStore{Store: store, err: errBoom}, nil, codeanchor.WithBasePath(root))
	_, err = IngestNotesWithMatcher(context.Background(), svc, root, nil, nil)
	require.ErrorIs(t, err, errBoom)
	notePaths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Empty(t, notePaths)
}
