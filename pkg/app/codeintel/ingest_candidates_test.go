package codeintel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestIngestMarkdownCandidates_UsesOnlySuppliedPreclassifiedMixedCaseMarkdown(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	includedPath := filepath.Join(root, "Decision.MD")
	require.NoError(t, os.WriteFile(includedPath, []byte("# Decision\n"), 0o644))
	// A walk would attempt this file and report a build error. Candidate intake
	// must never discover it because ownership already made that decision.
	require.NoError(t, os.WriteFile(filepath.Join(root, "outside.md"), []byte(`---
code-anchors:
  go:
    - symbol: Unqualified
---
# Outside
`), 0o644))

	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())

	result, err := IngestMarkdownCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{{
		AbsPath: includedPath,
		RelPath: "Decision.MD",
		ModTime: 1,
		Kind:    indexingpipe.FileKindNote,
	}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	require.Empty(t, result.BuildErrors)
	require.Equal(t, []string{"Decision.MD"}, result.ChangedPaths)

	notePaths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"Decision.MD"}, notePaths)
}

func TestIngestMarkdownCandidates_ForceReadProcessesUnchangedContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, []byte("# Note\n"), 0o644))

	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	candidate := indexingpipe.FileCandidate{AbsPath: path, RelPath: "note.md", ModTime: 1, Kind: indexingpipe.FileKindNote}

	first, err := IngestMarkdownCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{candidate}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, first.Count)

	unchanged, err := IngestMarkdownCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{candidate}, nil)
	require.NoError(t, err)
	require.Zero(t, unchanged.Count)
	require.Equal(t, 1, unchanged.Unchanged)

	forced := candidate
	forced.ForceRead = true
	forcedResult, err := IngestMarkdownCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{forced}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, forcedResult.Count)
	require.Zero(t, forcedResult.Unchanged)
}

func TestIngestMarkdownCandidatesWithSources_UsesSealedSourceAndLeavesOthersOnPipeline(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	preloadedPath := filepath.Join(root, "preloaded.md")
	diskPath := filepath.Join(root, "disk.md")
	require.NoError(t, os.WriteFile(preloadedPath, []byte("# Filesystem Version\n"), 0o644))
	require.NoError(t, os.WriteFile(diskPath, []byte("# Disk Version\n"), 0o644))
	source, err := noteformat.NewAuthoredSource(paths.NotePath("preloaded.md"), markdown.New().Descriptor(), []byte("# Sealed Snapshot\n"), 17)
	require.NoError(t, err)
	// The source must remain usable after its filesystem counterpart changes
	// and then becomes unreadable at the candidate path.
	require.NoError(t, os.Rename(preloadedPath, preloadedPath+".moved"))

	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	candidates := []indexingpipe.FileCandidate{
		{AbsPath: preloadedPath, RelPath: "preloaded.md", ModTime: 17, Kind: indexingpipe.FileKindNote},
		{AbsPath: diskPath, RelPath: "disk.md", ModTime: 19, Kind: indexingpipe.FileKindNote},
	}
	var discovered, completed atomic.Int64
	result, err := IngestMarkdownCandidatesWithSources(context.Background(), svc, root, candidates, map[paths.NotePath]noteformat.AuthoredSource{
		"preloaded.md": source,
	}, &indexingpipe.ProgressCallbacks{
		OnDiscovered: func(indexingpipe.FileCandidate) { discovered.Add(1) },
		OnCompleted:  func(indexingpipe.FileCandidate) { completed.Add(1) },
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Count)
	require.Equal(t, int64(2), discovered.Load())
	require.Equal(t, int64(2), completed.Load())

	meta, err := store.IntelNoteIndexMeta(context.Background())
	require.NoError(t, err)
	require.Equal(t, source.ContentHash(), meta["preloaded.md"].ContentHash)
	require.Equal(t, HashContentBytes([]byte("# Disk Version\n")), meta["disk.md"].ContentHash)
}

func TestIngestMarkdownCandidatesWithSources_RejectsInvalidSourceBatchBeforeMutation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, []byte("# Disk\n"), 0o644))
	staleSource, err := noteformat.NewAuthoredSource(paths.NotePath("note.md"), markdown.New().Descriptor(), []byte("# Stale\n"), 2)
	require.NoError(t, err)

	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	_, err = IngestMarkdownCandidatesWithSources(context.Background(), svc, root, []indexingpipe.FileCandidate{{
		AbsPath: path,
		RelPath: "note.md",
		ModTime: 1,
		Kind:    indexingpipe.FileKindNote,
	}}, map[paths.NotePath]noteformat.AuthoredSource{"note.md": staleSource}, nil)
	require.Error(t, err)
	notePaths, listErr := store.NotePaths(context.Background())
	require.NoError(t, listErr)
	require.Empty(t, notePaths)
}

func TestIngestMarkdownCandidates_BuildErrorDoesNotRetireOwnership(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "bad.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
code-anchors:
  go:
    - symbol: Unqualified
---
# Bad
`), 0o644))

	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	require.NoError(t, store.UpsertNote(context.Background(), codeanchor.Note{Path: "bad.md", Title: "Existing"}))

	result, err := IngestMarkdownCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{{
		AbsPath: path,
		RelPath: "bad.md",
		ModTime: 1,
		Kind:    indexingpipe.FileKindNote,
	}}, nil)
	require.NoError(t, err)
	require.Zero(t, result.Count)
	require.Zero(t, result.Deleted)
	require.Empty(t, result.DeletedPaths)
	require.Len(t, result.BuildErrors, 1)

	notePaths, listErr := store.NotePaths(context.Background())
	require.NoError(t, listErr)
	require.Equal(t, []string{"bad.md"}, notePaths)
}

func TestIngestMarkdownCandidates_MissingCandidateFailsWithoutMutation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missingPath := filepath.Join(root, "missing.md")
	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())

	result, err := IngestMarkdownCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{{
		AbsPath: missingPath,
		RelPath: "missing.md",
		ModTime: 1,
		Kind:    indexingpipe.FileKindNote,
	}}, nil)
	require.Error(t, err)
	var readErr *MarkdownCandidateReadError
	require.True(t, errors.As(err, &readErr))
	require.Equal(t, "missing.md", readErr.Path)
	require.Zero(t, result.Count)
	require.Zero(t, result.Deleted)

	notePaths, listErr := store.NotePaths(context.Background())
	require.NoError(t, listErr)
	require.Empty(t, notePaths)
}

func TestIngestMarkdownCandidates_RejectsWholeInvalidBatchBeforeMutation(t *testing.T) {
	root := t.TempDir()
	includedPath := filepath.Join(root, "Decision.MD")
	outsidePath := filepath.Join(root, "Outside.MD")
	require.NoError(t, os.WriteFile(includedPath, []byte("# Decision\n"), 0o644))
	require.NoError(t, os.WriteFile(outsidePath, []byte("# Outside\n"), 0o644))

	valid := indexingpipe.FileCandidate{AbsPath: includedPath, RelPath: "Decision.MD", ModTime: 1, Kind: indexingpipe.FileKindNote}
	tests := []struct {
		name    string
		invalid indexingpipe.FileCandidate
	}{
		{name: "wrong kind", invalid: indexingpipe.FileCandidate{AbsPath: outsidePath, RelPath: "Outside.MD", Kind: indexingpipe.FileKindCode}},
		{name: "absolute relative path", invalid: indexingpipe.FileCandidate{AbsPath: outsidePath, RelPath: outsidePath, Kind: indexingpipe.FileKindNote}},
		{name: "parent relative path", invalid: indexingpipe.FileCandidate{AbsPath: outsidePath, RelPath: "../Outside.MD", Kind: indexingpipe.FileKindNote}},
		{name: "mismatched paths", invalid: indexingpipe.FileCandidate{AbsPath: outsidePath, RelPath: "Decision.MD", Kind: indexingpipe.FileKindNote}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openCandidateTestStore(t, t.TempDir())
			svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())

			_, err := IngestMarkdownCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{valid, tt.invalid}, nil)
			require.Error(t, err)
			notePaths, listErr := store.NotePaths(context.Background())
			require.NoError(t, listErr)
			require.Empty(t, notePaths)
		})
	}
}

func TestIndexCandidates_UsesSuppliedLangWithoutDiscoveryOrExtensionChecks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	includedPath := filepath.Join(root, "src", "main.not-code")
	require.NoError(t, os.MkdirAll(filepath.Dir(includedPath), 0o755))
	require.NoError(t, os.WriteFile(includedPath, []byte("package src\nfunc Main() {}\n"), 0o644))
	// This normal Go file proves the candidate path does not walk siblings.
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "outside.go"), []byte("package src\nfunc Outside() {}\n"), 0o644))

	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)
	candidates := []indexingpipe.FileCandidate{{
		AbsPath: includedPath,
		RelPath: "src/main.not-code",
		ModTime: 1,
		Kind:    indexingpipe.FileKindCode,
		Lang:    codeanchor.LangGo,
	}}

	result, err := IndexCandidates(context.Background(), svc, root, candidates, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Indexed)
	require.Equal(t, []string{"src/main.not-code"}, result.SeenPaths)
	require.Equal(t, []string{"src/main.not-code"}, result.IndexedPaths)

	mtimes, err := store.IntelCodeMtimes(context.Background())
	require.NoError(t, err)
	require.Contains(t, mtimes, "src/main.not-code")
	require.NotContains(t, mtimes, "src/outside.go")

	second, err := IndexCandidates(context.Background(), svc, root, candidates, nil)
	require.NoError(t, err)
	require.Zero(t, second.Indexed)
	require.Equal(t, 1, second.Unchanged)
}

func TestIndexCandidates_ForceReadProcessesUnchangedCode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("package src\nfunc Main() {}\n"), 0o644))

	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)
	candidate := indexingpipe.FileCandidate{AbsPath: path, RelPath: "src/main.go", ModTime: 1, Kind: indexingpipe.FileKindCode, Lang: codeanchor.LangGo}

	first, err := IndexCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{candidate}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, first.Indexed)

	unchanged, err := IndexCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{candidate}, nil)
	require.NoError(t, err)
	require.Zero(t, unchanged.Indexed)
	require.Equal(t, 1, unchanged.Unchanged)

	forced := candidate
	forced.ForceRead = true
	forcedResult, err := IndexCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{forced}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, forcedResult.Indexed)
	require.Zero(t, forcedResult.Unchanged)
}

func TestIndexCandidates_MissingCandidateFailsWithoutMutation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missingPath := filepath.Join(root, "src", "missing.go")
	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)

	result, err := IndexCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{{
		AbsPath: missingPath,
		RelPath: "src/missing.go",
		ModTime: 1,
		Kind:    indexingpipe.FileKindCode,
		Lang:    codeanchor.LangGo,
	}}, nil)
	require.Error(t, err)
	var readErr *CodeCandidateReadError
	require.True(t, errors.As(err, &readErr))
	require.Equal(t, "src/missing.go", readErr.Path)
	require.Zero(t, result.Indexed)

	mtimes, listErr := store.IntelCodeMtimes(context.Background())
	require.NoError(t, listErr)
	require.Empty(t, mtimes)
}

func TestIndexCandidates_BuildFailureFailsWithoutMutation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("package src\n"), 0o644))
	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{failingLanguageIndexer{lang: codeanchor.LangGo, err: errors.New("parse failed")}},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)

	result, err := IndexCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{{
		AbsPath: path,
		RelPath: "src/main.go",
		ModTime: 1,
		Kind:    indexingpipe.FileKindCode,
		Lang:    codeanchor.LangGo,
	}}, nil)
	require.Error(t, err)
	var buildErr *CodeCandidateBuildError
	require.True(t, errors.As(err, &buildErr))
	require.Equal(t, "src/main.go", buildErr.Path)
	require.ErrorContains(t, buildErr, "parse failed")
	require.Zero(t, result.Indexed)

	mtimes, listErr := store.IntelCodeMtimes(context.Background())
	require.NoError(t, listErr)
	require.Empty(t, mtimes)
}

func TestIndexRoot_BuildFailureRemainsTolerant(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	codeRoot := filepath.Join(root, "src")
	path := filepath.Join(codeRoot, "main.go")
	require.NoError(t, os.MkdirAll(codeRoot, 0o755))
	require.NoError(t, os.WriteFile(path, []byte("package src\n"), 0o644))
	store := openCandidateTestStore(t, root)
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{failingLanguageIndexer{lang: codeanchor.LangGo, err: errors.New("parse failed")}},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)

	result, err := IndexRoot(context.Background(), svc, root, codeRoot, nil, nil)
	require.NoError(t, err)
	require.Zero(t, result.Indexed)

	mtimes, listErr := store.IntelCodeMtimes(context.Background())
	require.NoError(t, listErr)
	require.Empty(t, mtimes)
}

type failingLanguageIndexer struct {
	lang codeanchor.Lang
	err  error
}

func (f failingLanguageIndexer) IndexFile([]byte, paths.CodePathRef) (codeanchor.FileSummary, error) {
	return codeanchor.FileSummary{}, f.err
}

func (f failingLanguageIndexer) Lang() codeanchor.Lang {
	return f.lang
}

func TestIndexCandidates_RejectsWholeInvalidBatchBeforeMutation(t *testing.T) {
	root := t.TempDir()
	includedPath := filepath.Join(root, "src", "main.not-code")
	outsidePath := filepath.Join(root, "src", "outside.not-code")
	require.NoError(t, os.MkdirAll(filepath.Dir(includedPath), 0o755))
	require.NoError(t, os.WriteFile(includedPath, []byte("package src\nfunc Main() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(outsidePath, []byte("package src\nfunc Outside() {}\n"), 0o644))

	valid := indexingpipe.FileCandidate{AbsPath: includedPath, RelPath: "src/main.not-code", ModTime: 1, Kind: indexingpipe.FileKindCode, Lang: codeanchor.LangGo}
	tests := []struct {
		name    string
		invalid indexingpipe.FileCandidate
	}{
		{name: "wrong kind", invalid: indexingpipe.FileCandidate{AbsPath: outsidePath, RelPath: "src/outside.not-code", Kind: indexingpipe.FileKindNote, Lang: codeanchor.LangGo}},
		{name: "absolute relative path", invalid: indexingpipe.FileCandidate{AbsPath: outsidePath, RelPath: outsidePath, Kind: indexingpipe.FileKindCode, Lang: codeanchor.LangGo}},
		{name: "parent relative path", invalid: indexingpipe.FileCandidate{AbsPath: outsidePath, RelPath: "../src/outside.not-code", Kind: indexingpipe.FileKindCode, Lang: codeanchor.LangGo}},
		{name: "mismatched paths", invalid: indexingpipe.FileCandidate{AbsPath: outsidePath, RelPath: "src/main.not-code", Kind: indexingpipe.FileKindCode, Lang: codeanchor.LangGo}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openCandidateTestStore(t, t.TempDir())
			svc := codeanchor.NewServiceWithOptions(
				store,
				[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
				codeanchor.WithBasePath(root),
				codeanchor.WithoutWarmCache(),
			)

			_, err := IndexCandidates(context.Background(), svc, root, []indexingpipe.FileCandidate{valid, tt.invalid}, nil)
			require.Error(t, err)
			mtimes, listErr := store.IntelCodeMtimes(context.Background())
			require.NoError(t, listErr)
			require.Empty(t, mtimes)
		})
	}
}

func openCandidateTestStore(t testing.TB, root string) *semdb.Store {
	t.Helper()
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}
