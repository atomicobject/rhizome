package indexingpipe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/stretchr/testify/require"
)

// SPEC-0064 scanner agreement: the code-indexing walker honors include
// boundaries the same way the unified matcher does, while the subtree's own
// .gitignore and built-in default dirnames keep pruning inside it.
func TestProcessFiles_IncludedSubtreeAgreesWithMatcher(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	write(".gitignore", "app/\n")
	write("app/.gitignore", "generated/\n")
	write("app/src/main.go", "package main\n")
	write("app/generated/gen.go", "package gen\n")
	write("app/node_modules/dep/index.js", "js\n")
	write("top.go", "package top\n")
	write(".rhizome/ignore", "# rhizome: included subtrees\n!/app/\n")

	matcher := ignore.LoadUnifiedMatcher(root, nil)

	classify := func(path string, d os.DirEntry, modTime int64) (FileCandidate, bool, error) {
		if !strings.HasSuffix(path, ".go") {
			return FileCandidate{}, false, nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return FileCandidate{}, false, nil
		}
		return FileCandidate{AbsPath: path, RelPath: filepath.ToSlash(rel), Kind: FileKindCode, ModTime: modTime}, true, nil
	}

	var seen []string
	err := ProcessFiles(
		context.Background(),
		ProcessOptions{Root: root, Matcher: matcher, WorkerCount: 1, QueueCapacity: 4},
		classify,
		func(FileCandidate) bool { return true },
		func(FileCandidate) {},
		func(payload FilePayload) error {
			seen = append(seen, payload.Candidate.RelPath)
			return nil
		},
	)
	require.NoError(t, err)

	require.Contains(t, seen, "app/src/main.go", "boundary-included code must be walked")
	require.Contains(t, seen, "top.go")
	require.NotContains(t, seen, "app/generated/gen.go", "subtree's own .gitignore must keep applying")
	for _, rel := range seen {
		require.NotContains(t, rel, "node_modules", "defaults must keep pruning inside included subtree")
	}

	count, err := CountFiles(context.Background(), ProcessOptions{Root: root, Matcher: matcher}, classify)
	require.NoError(t, err)
	require.Equal(t, len(seen), count, "CountFiles must agree with ProcessFiles")
}
