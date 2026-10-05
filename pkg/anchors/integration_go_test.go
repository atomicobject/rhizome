//go:build cgo && integration
// +build cgo,integration

package codeanchor_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestIntegration_GoCallAnchorMatchesCallerFile(t *testing.T) {
	root := t.TempDir()

	// Minimal Go module
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	// callee package
	depDir := filepath.Join(root, "dep")
	require.NoError(t, os.MkdirAll(depDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(depDir, "dep.go"), []byte(`
package dep

func Target() {}
`), 0o644))

	// caller package
	mainDir := filepath.Join(root, "main")
	require.NoError(t, os.MkdirAll(mainDir, 0o755))
	mainPath := filepath.Join(mainDir, "main.go")
	mainCode := `package main

import "example.com/mod/dep"

func main() {
	dep.Target()
}
`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainCode), 0o644))

	// Note defining an anchor for dep.Target (matches definition + callers)
	notePath := filepath.Join(root, "notes", "go-call.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	noteContent := `---
title: "Go call anchor"
code-anchors:
  go:
    - label: dep-target-callers
      symbol: example.com/mod/dep.Target
---
`
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewGoIndexer()},
		codeanchor.WithBasePath(root),
		codeanchor.WithoutWarmCache(),
		codeanchor.WithWriteAccess(),
	)

	// Index callee first (so call edges can be created when indexing caller)
	depPath := filepath.Join(depDir, "dep.go")
	depCode, err := os.ReadFile(depPath)
	require.NoError(t, err)
	err = svc.IndexCodeFile(context.Background(), codeanchor.LangGo, depPath, depCode)
	require.NoError(t, err)

	_, err = svc.IngestNoteSource(context.Background(), integrationNoteSource(t, root, notePath, noteContent))
	require.NoError(t, err)
	err = svc.IndexCodeFile(context.Background(), codeanchor.LangGo, mainPath, []byte(mainCode))
	require.NoError(t, err)
	require.NoError(t, svc.RecomputeAnchorScopes(context.Background()))

	fc, err := svc.NotesForFile(context.Background(), mainPath)
	require.NoError(t, err)
	require.Contains(t, fc.Anchors, "dep-target-callers")
	require.Len(t, fc.Notes, 1)
}
