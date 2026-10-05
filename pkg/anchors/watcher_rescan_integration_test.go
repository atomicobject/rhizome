//go:build cgo && integration
// +build cgo,integration

// Package codeanchor_test contains integration tests for the codeanchor package.
// These tests require CGO for SQLite support.
package codeanchor_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

// TestGoAnchorMatching verifies that code anchors work for Go symbol definitions
// and call sites. This tests the core anchor matching logic using manual indexing.
func TestIntegration_GoAnchorMatching(t *testing.T) {
	root := t.TempDir()

	// Minimal Go module for stable import paths.
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.23\n"), 0o644))

	// Code: a callee and a caller.
	pkgDir := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	calleePath := filepath.Join(pkgDir, "callee.go")
	require.NoError(t, os.WriteFile(calleePath, []byte(`package pkg

func Foo() {}
`), 0o644))

	callerPath := filepath.Join(pkgDir, "caller.go")
	require.NoError(t, os.WriteFile(callerPath, []byte(`package pkg

func Bar() {
	Foo()
}
`), 0o644))

	// Note with a function anchor targeting Foo.
	notePath := filepath.Join(root, "Notes", "FooDoc.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	noteContent := `---
summary: "Foo docs"
code-anchors:
  go:
    - label: go.foo
      symbol: example.com/mod/pkg.Foo
---
# Foo
`
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	store, err := sqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Index note
	noteBytes, err := os.ReadFile(notePath)
	require.NoError(t, err)
	_, err = svc.IngestNoteSource(ctx, integrationNoteSource(t, root, notePath, string(noteBytes)))
	require.NoError(t, err)

	// Index code files
	calleeContent, err := os.ReadFile(calleePath)
	require.NoError(t, err)
	err = svc.IndexCodeFile(ctx, codeanchor.LangGo, calleePath, calleeContent)
	require.NoError(t, err)

	callerContent, err := os.ReadFile(callerPath)
	require.NoError(t, err)
	err = svc.IndexCodeFile(ctx, codeanchor.LangGo, callerPath, callerContent)
	require.NoError(t, err)

	// Recompute scopes to propagate anchors
	err = svc.RecomputeAnchorScopes(ctx)
	require.NoError(t, err)

	// Verify symbols were indexed
	calleeSyms, _ := svc.SymbolsForFile(ctx, calleePath)
	require.Contains(t, calleeSyms, "example.com/mod/pkg.Foo", "callee should have Foo symbol")

	callerSyms, _ := svc.SymbolsForFile(ctx, callerPath)
	require.Contains(t, callerSyms, "example.com/mod/pkg.Bar", "caller should have Bar symbol")

	// Verify anchor matches on definition site (callee)
	calleeFC, err := svc.NotesForFile(ctx, calleePath)
	require.NoError(t, err)
	require.Contains(t, calleeFC.Anchors, "go.foo", "definition site should have anchor")
	require.Len(t, calleeFC.Notes, 1)

	// Verify anchor matches on call site (caller)
	callerFC, err := svc.NotesForFile(ctx, callerPath)
	require.NoError(t, err)
	require.Contains(t, callerFC.Anchors, "go.foo", "call site should have anchor")
	require.Len(t, callerFC.Notes, 1)
}
