//go:build cgo && integration
// +build cgo,integration

package codeanchor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

// End-to-end integration for sqlite store + python indexer + scopes.
func TestIntegration_PythonNoteAndCode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() {
		_ = store.Close()
	}()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithBasePath(dir),
		codeanchor.WithWriteAccess(),
	)

	codeDir := filepath.Join(dir, "svc")
	require.NoError(t, os.MkdirAll(codeDir, 0o755))
	codePath := filepath.Join(codeDir, "invoice.py")
	code := `@decorator(tag="billing")
class InvoiceService(BaseService):
    def foo(self):
        charge()`
	require.NoError(t, os.WriteFile(codePath, []byte(code), 0o644))

	// Use empty pkg to match any package (acts as wildcard in anchor matching).
	// This avoids issues with absolute paths producing long module names.
	note := `---
anchors:
  - define:
      label: "BillingDomain"
      kind: "annotation"
      lang: "py"
      annotation:
        symbol:
          pkg: ""
          name: "decorator"
        argFilters:
          tag: "billing"
---
`
	notePath := filepath.Join(dir, "notes", "billing.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(note), 0o644))

	// Ingest note then code, recompute scopes.
	_, err = svc.IngestNoteSource(context.Background(), integrationNoteSource(t, dir, notePath, note))
	require.NoError(t, err)
	err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, codePath, []byte(code))
	require.NoError(t, err)
	require.NoError(t, svc.RecomputeAnchorScopes(context.Background()))

	// Query
	fc, err := svc.NotesForFile(context.Background(), codePath)
	require.NoError(t, err)
	require.Contains(t, fc.Anchors, "BillingDomain")
	require.Len(t, fc.Notes, 1)
	expectedNotePath := string(paths.NormalizeNote("notes/billing.md"))
	require.Equal(t, expectedNotePath, fc.Notes[0].Path)
}

func TestIntegration_DirectoryAnchorMatchesFilesAndDirs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithBasePath(dir),
	)

	codeDir := filepath.Join(dir, "src", "todoapp", "services")
	require.NoError(t, os.MkdirAll(codeDir, 0o755))
	codePath := filepath.Join(codeDir, "tasks.py")
	require.NoError(t, os.WriteFile(codePath, []byte("# code"), 0o644))

	noteContent := `---
title: "Services module"
code-anchors:
  python:
    - dir: src/todoapp/services
---
`
	notePath := filepath.Join(dir, "notes", "services.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	_, err = svc.IngestNoteSource(context.Background(), integrationNoteSource(t, dir, notePath, noteContent))
	require.NoError(t, err)

	// Directory anchor (dir:) is implemented via glob matching; it should match a file under the directory.
	fc, err := svc.NotesForFile(context.Background(), codePath)
	require.NoError(t, err)
	require.Contains(t, fc.Anchors, "src/todoapp/services")
	require.Len(t, fc.Notes, 1)
	expectedNotePath := string(paths.NormalizeNote("notes/services.md"))
	require.Equal(t, expectedNotePath, fc.Notes[0].Path)

	// It should also match when querying the directory itself.
	dirFC, err := svc.NotesForFile(context.Background(), codeDir)
	require.NoError(t, err)
	require.Contains(t, dirFC.Anchors, "src/todoapp/services")
	require.Len(t, dirFC.Notes, 1)
	require.Equal(t, expectedNotePath, dirFC.Notes[0].Path)
}

func TestIntegration_GlobAnchorMatchesNonIndexedFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithBasePath(dir),
	)

	codeDir := filepath.Join(dir, "src", "ui")
	require.NoError(t, os.MkdirAll(codeDir, 0o755))
	codePath := filepath.Join(codeDir, "widget.ts")
	require.NoError(t, os.WriteFile(codePath, []byte("// ts code"), 0o644))

	noteContent := `---
title: "UI docs"
code-anchors:
  python:
    - label: ui-ts
      glob: src/**/*.ts
---
`
	notePath := filepath.Join(dir, "notes", "ui.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(noteContent), 0o644))

	_, err = svc.IngestNoteSource(context.Background(), integrationNoteSource(t, dir, notePath, noteContent))
	require.NoError(t, err)

	// .ts isn't indexed by default, but glob anchors should still attach the note.
	fc, err := svc.NotesForFile(context.Background(), codePath)
	require.NoError(t, err)
	require.Contains(t, fc.Anchors, "ui-ts")
	require.Len(t, fc.Notes, 1)
}

func TestIntegration_PythonImportEdges(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexer()},
		codeanchor.WithBasePath(dir),
	)

	// Create Python files with imports.
	srcDir := filepath.Join(dir, "src", "myapp")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))

	utilsPath := filepath.Join(srcDir, "utils.py")
	mainPath := filepath.Join(srcDir, "main.py")

	require.NoError(t, os.WriteFile(utilsPath, []byte(`
def helper():
    return True
`), 0o644))

	require.NoError(t, os.WriteFile(mainPath, []byte(`
from .utils import helper

def run():
    helper()
`), 0o644))

	// Index both files.
	utilsContent, err := os.ReadFile(utilsPath)
	require.NoError(t, err)
	mainContent, err := os.ReadFile(mainPath)
	require.NoError(t, err)

	err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, utilsPath, utilsContent)
	require.NoError(t, err)
	err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, mainPath, mainContent)
	require.NoError(t, err)

	// Rebuild call edges to ensure import edges are created.
	err = svc.RebuildAllCallEdges(context.Background())
	require.NoError(t, err)

	// Query edges and verify import edge exists.
	edges, err := store.IntelEdges(context.Background())
	require.NoError(t, err)

	var importEdges []codeanchor.IntelEdge
	for _, e := range edges {
		if e.Kind == "imports" {
			importEdges = append(importEdges, e)
		}
	}

	require.Len(t, importEdges, 1, "expected 1 import edge from main.py to utils.py")

	// Verify the edge connects the right files by checking the anchor paths.
	srcAnchor, found, err := store.IntelAnchorByID(context.Background(), importEdges[0].SrcID)
	require.NoError(t, err)
	require.True(t, found, "source anchor should exist")
	dstAnchor, found, err := store.IntelAnchorByID(context.Background(), importEdges[0].DstID)
	require.NoError(t, err)
	require.True(t, found, "destination anchor should exist")

	require.Contains(t, srcAnchor.Path, "main.py", "source should be main.py")
	require.Contains(t, dstAnchor.Path, "utils.py", "destination should be utils.py")
}

// TestIntegration_PythonAbsoluteImportEdges tests import edge creation for absolute Python imports
// using module suffix matching (e.g., "app.core.utils" matches "backend/src/app/core/utils.py").
func TestIntegration_PythonAbsoluteImportEdges(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Use source roots matching the directory structure.
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexerWithRoots([]string{"backend/src"})},
		codeanchor.WithBasePath(dir),
	)

	// Create Python files with absolute imports.
	// Structure: backend/src/app/core/utils.py and backend/src/app/domain/service.py
	coreDir := filepath.Join(dir, "backend", "src", "app", "core")
	domainDir := filepath.Join(dir, "backend", "src", "app", "domain")
	require.NoError(t, os.MkdirAll(coreDir, 0o755))
	require.NoError(t, os.MkdirAll(domainDir, 0o755))

	utilsPath := filepath.Join(coreDir, "utils.py")
	servicePath := filepath.Join(domainDir, "service.py")

	require.NoError(t, os.WriteFile(utilsPath, []byte(`
def helper():
    return True
`), 0o644))

	// Use absolute import: "app.core.utils"
	require.NoError(t, os.WriteFile(servicePath, []byte(`
from app.core.utils import helper

def run():
    helper()
`), 0o644))

	// Index both files.
	utilsContent, err := os.ReadFile(utilsPath)
	require.NoError(t, err)
	serviceContent, err := os.ReadFile(servicePath)
	require.NoError(t, err)

	err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, utilsPath, utilsContent)
	require.NoError(t, err)
	err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, servicePath, serviceContent)
	require.NoError(t, err)

	// Rebuild call edges to ensure import edges are created via suffix matching.
	err = svc.RebuildAllCallEdges(context.Background())
	require.NoError(t, err)

	// Query edges and verify import edge exists.
	edges, err := store.IntelEdges(context.Background())
	require.NoError(t, err)

	var importEdges []codeanchor.IntelEdge
	for _, e := range edges {
		if e.Kind == "imports" {
			importEdges = append(importEdges, e)
		}
	}

	require.Len(t, importEdges, 1, "expected 1 import edge from service.py to utils.py")

	// Verify the edge connects the right files by checking the anchor paths.
	srcAnchor, found, err := store.IntelAnchorByID(context.Background(), importEdges[0].SrcID)
	require.NoError(t, err)
	require.True(t, found, "source anchor should exist")
	dstAnchor, found, err := store.IntelAnchorByID(context.Background(), importEdges[0].DstID)
	require.NoError(t, err)
	require.True(t, found, "destination anchor should exist")

	require.Contains(t, srcAnchor.Path, "service.py", "source should be service.py")
	require.Contains(t, dstAnchor.Path, "utils.py", "destination should be utils.py")
}

// TestIntegration_PythonMultipleImportEdges tests that multiple import edges are created
// when a file imports from multiple modules.
func TestIntegration_PythonMultipleImportEdges(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexerWithRoots([]string{"backend/src"})},
		codeanchor.WithBasePath(dir),
	)

	// Create multiple Python modules.
	coreDir := filepath.Join(dir, "backend", "src", "app", "core")
	domainDir := filepath.Join(dir, "backend", "src", "app", "domain")
	require.NoError(t, os.MkdirAll(coreDir, 0o755))
	require.NoError(t, os.MkdirAll(domainDir, 0o755))

	// Core modules.
	cachePath := filepath.Join(coreDir, "cache.py")
	eventsPath := filepath.Join(coreDir, "events.py")
	// Domain module that imports both.
	servicePath := filepath.Join(domainDir, "service.py")

	require.NoError(t, os.WriteFile(cachePath, []byte(`
class Cache:
    pass
`), 0o644))

	require.NoError(t, os.WriteFile(eventsPath, []byte(`
class Event:
    pass
`), 0o644))

	require.NoError(t, os.WriteFile(servicePath, []byte(`
from app.core.cache import Cache
from app.core.events import Event

class Service:
    def __init__(self):
        self.cache = Cache()
        self.event = Event()
`), 0o644))

	// Index all files.
	for _, p := range []string{cachePath, eventsPath, servicePath} {
		content, err := os.ReadFile(p)
		require.NoError(t, err)
		err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, p, content)
		require.NoError(t, err)
	}

	// Rebuild call edges.
	err = svc.RebuildAllCallEdges(context.Background())
	require.NoError(t, err)

	// Query edges.
	edges, err := store.IntelEdges(context.Background())
	require.NoError(t, err)

	var importEdges []codeanchor.IntelEdge
	for _, e := range edges {
		if e.Kind == "imports" {
			importEdges = append(importEdges, e)
		}
	}

	require.Len(t, importEdges, 2, "expected 2 import edges from service.py to cache.py and events.py")

	// Verify all import edges come from service.py.
	srcAnchor, found, err := store.IntelAnchorByID(context.Background(), importEdges[0].SrcID)
	require.NoError(t, err)
	require.True(t, found)
	require.Contains(t, srcAnchor.Path, "service.py")

	srcAnchor2, found, err := store.IntelAnchorByID(context.Background(), importEdges[1].SrcID)
	require.NoError(t, err)
	require.True(t, found)
	require.Contains(t, srcAnchor2.Path, "service.py")

	// Verify destinations are the two core modules.
	dstPaths := make(map[string]bool)
	for _, e := range importEdges {
		dstAnchor, found, err := store.IntelAnchorByID(context.Background(), e.DstID)
		require.NoError(t, err)
		require.True(t, found)
		dstPaths[dstAnchor.Path] = true
	}
	require.True(t, dstPaths["backend/src/app/core/cache.py"] || containsPath(dstPaths, "cache.py"),
		"expected import edge to cache.py")
	require.True(t, dstPaths["backend/src/app/core/events.py"] || containsPath(dstPaths, "events.py"),
		"expected import edge to events.py")
}

func containsPath(paths map[string]bool, suffix string) bool {
	for p := range paths {
		if filepath.Base(p) == suffix {
			return true
		}
	}
	return false
}

func TestIntegration_TypeScriptImportEdges(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Build tail index with all TS files.
	tailIdx := codeanchor.NewPathTailIndex(5)

	// Create TS files.
	srcDir := filepath.Join(dir, "src", "components")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))

	buttonPath := filepath.Join(srcDir, "Button.tsx")
	appPath := filepath.Join(dir, "src", "App.tsx")

	require.NoError(t, os.WriteFile(buttonPath, []byte(`export function Button() { return <button />; }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(appPath, []byte(`
import { Button } from "./components/Button";
import React from "react";  // npm - should be ignored
export function App() { return <Button />; }
`), 0o644))

	// Add files to tail index (simulating what happens during indexing).
	tailIdx.Add(buttonPath)
	tailIdx.Add(appPath)

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewTSIndexerWithRootAndTailIndex(dir, tailIdx)},
		codeanchor.WithBasePath(dir),
		codeanchor.WithPathTailIndex(tailIdx),
	)

	// Index both files.
	buttonContent, err := os.ReadFile(buttonPath)
	require.NoError(t, err)
	appContent, err := os.ReadFile(appPath)
	require.NoError(t, err)

	err = svc.IndexCodeFile(context.Background(), codeanchor.LangTS, buttonPath, buttonContent)
	require.NoError(t, err)
	err = svc.IndexCodeFile(context.Background(), codeanchor.LangTS, appPath, appContent)
	require.NoError(t, err)

	// Rebuild call edges to ensure import edges are created.
	err = svc.RebuildAllCallEdges(context.Background())
	require.NoError(t, err)

	// Query edges and verify import edge exists.
	edges, err := store.IntelEdges(context.Background())
	require.NoError(t, err)

	var importEdges []codeanchor.IntelEdge
	for _, e := range edges {
		if e.Kind == "imports" {
			importEdges = append(importEdges, e)
		}
	}

	require.Len(t, importEdges, 1, "expected 1 import edge from App.tsx to Button.tsx")

	// Verify the edge connects the right files by checking the anchor paths.
	srcAnchor, found, err := store.IntelAnchorByID(context.Background(), importEdges[0].SrcID)
	require.NoError(t, err)
	require.True(t, found, "source anchor should exist")
	dstAnchor, found, err := store.IntelAnchorByID(context.Background(), importEdges[0].DstID)
	require.NoError(t, err)
	require.True(t, found, "destination anchor should exist")

	require.Contains(t, srcAnchor.Path, "App.tsx", "source should be App.tsx")
	require.Contains(t, dstAnchor.Path, "Button.tsx", "destination should be Button.tsx")
}

// TestIntegration_PythonTypeRefEdges tests that type_ref edges are created
// when a file uses type annotations referencing types from other modules.
func TestIntegration_PythonTypeRefEdges(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexerWithRoots([]string{"src"})},
		codeanchor.WithBasePath(dir),
	)

	// Create Python files with type annotations.
	modelsDir := filepath.Join(dir, "src", "models")
	svcDir := filepath.Join(dir, "src", "services")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))
	require.NoError(t, os.MkdirAll(svcDir, 0o755))

	userPath := filepath.Join(modelsDir, "user.py")
	servicePath := filepath.Join(svcDir, "user_service.py")

	require.NoError(t, os.WriteFile(userPath, []byte(`
class User:
    def __init__(self, name: str):
        self.name = name
`), 0o644))

	require.NoError(t, os.WriteFile(servicePath, []byte(`
from models.user import User

def get_user(id: int) -> User:
    return User("test")

def update_user(user: User) -> None:
    pass
`), 0o644))

	// Index both files.
	userContent, err := os.ReadFile(userPath)
	require.NoError(t, err)
	serviceContent, err := os.ReadFile(servicePath)
	require.NoError(t, err)

	err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, userPath, userContent)
	require.NoError(t, err)
	err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, servicePath, serviceContent)
	require.NoError(t, err)

	// Rebuild edges (includes type_ref edges now).
	err = svc.RebuildAllCallEdges(context.Background())
	require.NoError(t, err)

	// Query edges and verify type_ref edges exist.
	edges, err := store.IntelEdges(context.Background())
	require.NoError(t, err)

	var typeRefEdges []codeanchor.IntelEdge
	for _, e := range edges {
		if e.Kind == "type_ref" {
			typeRefEdges = append(typeRefEdges, e)
		}
	}

	require.NotEmpty(t, typeRefEdges, "expected type_ref edges from user_service.py to User class")

	// Verify at least one edge connects to the User class.
	foundUserRef := false
	for _, e := range typeRefEdges {
		dstAnchor, found, err := store.IntelAnchorByID(context.Background(), e.DstID)
		require.NoError(t, err)
		if found && dstAnchor.Symbol == "User" {
			foundUserRef = true
			break
		}
	}
	require.True(t, foundUserRef, "expected type_ref edge targeting User class; got edges: %+v", typeRefEdges)
}

// TestIntegration_TypeScriptTypeRefEdges tests that type_ref edges are created
// when a TypeScript file uses type annotations referencing types from other modules.
func TestIntegration_TypeScriptTypeRefEdges(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Create package.json for TypeScript project.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tsapp", "src", "models"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tsapp", "src", "services"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tsapp", "package.json"), []byte(`{"name":"@test/tsapp"}`), 0o644))

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewTSIndexerWithRoot(dir)},
		codeanchor.WithBasePath(dir),
	)

	userPath := filepath.Join(dir, "tsapp", "src", "models", "User.ts")
	servicePath := filepath.Join(dir, "tsapp", "src", "services", "UserService.ts")

	require.NoError(t, os.WriteFile(userPath, []byte(`export interface User { name: string; }`), 0o644))
	require.NoError(t, os.WriteFile(servicePath, []byte(`
import { User } from "../models/User";

export function getUser(id: string): User {
    return { name: "test" };
}

export function updateUser(user: User): void {
    console.log(user);
}
`), 0o644))

	// Index both files.
	userContent, err := os.ReadFile(userPath)
	require.NoError(t, err)
	serviceContent, err := os.ReadFile(servicePath)
	require.NoError(t, err)

	ctx := context.Background()
	err = svc.IndexCodeFile(ctx, codeanchor.LangTS, userPath, userContent)
	require.NoError(t, err)
	err = svc.IndexCodeFile(ctx, codeanchor.LangTS, servicePath, serviceContent)
	require.NoError(t, err)

	// Verify symbols were indexed.
	userSyms, err := svc.SymbolsForFile(ctx, userPath)
	require.NoError(t, err)
	require.NotEmpty(t, userSyms, "TypeScript indexer must produce User.ts symbols")
	serviceSyms, err := svc.SymbolsForFile(ctx, servicePath)
	require.NoError(t, err)
	require.NotEmpty(t, serviceSyms, "TypeScript indexer must produce UserService.ts symbols")

	// Get all anchors before rebuilding edges to verify they exist.
	allAnchors, err := store.IntelAnchors(ctx)
	require.NoError(t, err)
	t.Logf("Intel code anchors before RebuildAllCallEdges: %d anchors", len(allAnchors))
	for _, a := range allAnchors {
		t.Logf("  anchor: lang=%s kind=%s path=%s symbol=%s fqn=%s", a.Lang, a.Kind, a.Path, a.Symbol, a.FQN)
	}

	// Rebuild edges (includes type_ref edges now).
	err = svc.RebuildAllCallEdges(ctx)
	require.NoError(t, err)

	// Query edges and verify type_ref edges exist.
	edges, err := store.IntelEdges(ctx)
	require.NoError(t, err)
	t.Logf("Total edges after RebuildAllCallEdges: %d", len(edges))
	for _, e := range edges {
		t.Logf("  edge: kind=%s src=%s dst=%s", e.Kind, e.SrcID, e.DstID)
	}

	var typeRefEdges []codeanchor.IntelEdge
	for _, e := range edges {
		if e.Kind == "type_ref" {
			typeRefEdges = append(typeRefEdges, e)
		}
	}

	require.NotEmpty(t, typeRefEdges, "expected type_ref edges from UserService.ts to User interface")

	// Verify at least one edge connects to the User interface.
	foundUserRef := false
	for _, e := range typeRefEdges {
		dstAnchor, found, err := store.IntelAnchorByID(context.Background(), e.DstID)
		require.NoError(t, err)
		if found && dstAnchor.Symbol == "User" {
			foundUserRef = true
			break
		}
	}
	require.True(t, foundUserRef, "expected type_ref edge targeting User interface; got edges: %+v", typeRefEdges)
}

func TestIntegration_TypeScriptObjectLiteralMethodCallEdges(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tsapp", "package.json"), []byte(`{"name":"@test/tsapp"}`), 0o644))

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewTSIndexerWithRoot(dir)},
		codeanchor.WithBasePath(dir),
	)

	apiPath := filepath.Join(dir, "tsapp", "src", "api.ts")
	usePath := filepath.Join(dir, "tsapp", "src", "App.ts")

	require.NoError(t, os.WriteFile(apiPath, []byte(`
export const StoriesApi = {
  detail: async (storyId: string) => ({ storyId }),
  rename(storyId: string, name: string) {
    return { storyId, name };
  },
};
`), 0o644))
	require.NoError(t, os.WriteFile(usePath, []byte(`
import { StoriesApi } from "./api";

export function App() {
  StoriesApi.detail("story-1");
  StoriesApi.rename("story-1", "Next");
}
`), 0o644))

	apiContent, err := os.ReadFile(apiPath)
	require.NoError(t, err)
	useContent, err := os.ReadFile(usePath)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, apiPath, apiContent))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, usePath, useContent))
	require.NoError(t, svc.RebuildAllCallEdges(ctx))

	edges, err := store.IntelEdges(ctx)
	require.NoError(t, err)

	foundDetail := false
	foundRename := false
	for _, e := range edges {
		if e.Kind != "calls" {
			continue
		}
		dstAnchor, found, err := store.IntelAnchorByID(ctx, e.DstID)
		require.NoError(t, err)
		if !found || dstAnchor.Path != "tsapp/src/api.ts" {
			continue
		}
		if dstAnchor.Symbol == "detail" {
			foundDetail = true
		}
		if dstAnchor.Symbol == "rename" {
			foundRename = true
		}
	}

	require.True(t, foundDetail, "expected call edge targeting api.detail; got edges: %+v", edges)
	require.True(t, foundRename, "expected call edge targeting api.rename; got edges: %+v", edges)
}

func TestIntegration_TypeScriptExpandedIntelEdges(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sem.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tsapp", "src", "models"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tsapp", "src", "services"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tsapp", "package.json"), []byte(`{"name":"@test/tsapp"}`), 0o644))

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewTSIndexerWithRoot(dir)},
		codeanchor.WithBasePath(dir),
	)

	modelsPath := filepath.Join(dir, "tsapp", "src", "models", "Scene.ts")
	workerPath := filepath.Join(dir, "tsapp", "src", "models", "Worker.ts")
	barrelPath := filepath.Join(dir, "tsapp", "src", "models", "index.ts")
	servicePath := filepath.Join(dir, "tsapp", "src", "services", "SceneService.ts")

	require.NoError(t, os.WriteFile(modelsPath, []byte(`
export interface Scene {
  id: string;
}

export const SceneApi = {
  detail(scene: Scene) {
    return scene.id;
  },
};

export function detail(scene: Scene) {
  return scene.id;
}
`), 0o644))
	require.NoError(t, os.WriteFile(workerPath, []byte(`
import { Scene } from "./Scene";

export interface Runnable {}
export class SceneWorker implements Runnable {
  current: Scene;

  constructor(scene: Scene) {
    this.current = scene;
  }
}
`), 0o644))
	require.NoError(t, os.WriteFile(barrelPath, []byte(`
export { detail, SceneApi, Scene } from "./Scene";
export { SceneWorker } from "./Worker";
export * from "./Worker";
`), 0o644))
	require.NoError(t, os.WriteFile(servicePath, []byte(`
import { detail, SceneApi, Scene, SceneWorker } from "../models";

export function useSceneMusic() {
  const { currentSceneMusic, playCurrentSceneMusic } = SceneApi;
  const active: Scene = { id: detail({ id: "one" }) } satisfies Scene;
  const worker = new SceneWorker(active);
  const selectedDetail = SceneApi.detail;
  playCurrentSceneMusic?.();
  return {
    selectedDetail,
    currentSceneMusic,
    worker,
  };
}
`), 0o644))

	ctx := context.Background()
	for _, path := range []string{modelsPath, workerPath, barrelPath, servicePath} {
		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, path, content))
	}
	require.NoError(t, svc.RebuildAllCallEdges(ctx))

	edges, err := store.IntelEdges(ctx)
	require.NoError(t, err)

	var foundMemberRef bool
	var foundTypeRef bool
	var foundBarrelCall bool
	for _, e := range edges {
		dstAnchor, found, err := store.IntelAnchorByID(ctx, e.DstID)
		require.NoError(t, err)
		if !found {
			continue
		}
		switch e.Kind {
		case "member_ref":
			if dstAnchor.Path == "tsapp/src/models/Scene.ts" && dstAnchor.Symbol == "detail" {
				foundMemberRef = true
			}
		case "type_ref":
			if dstAnchor.Path == "tsapp/src/models/Scene.ts" && dstAnchor.Symbol == "Scene" {
				foundTypeRef = true
			}
		case "calls":
			if dstAnchor.Path == "tsapp/src/models/Scene.ts" && dstAnchor.Symbol == "detail" {
				foundBarrelCall = true
			}
		}
	}

	require.True(t, foundMemberRef, "expected member_ref edge to SceneSchema.shape.tilt; got %+v", edges)
	require.True(t, foundTypeRef, "expected type_ref edge to Scene; got %+v", edges)
	require.True(t, foundBarrelCall, "expected barrel import call to resolve to Scene.detail; got %+v", edges)

	ancestors, err := store.Ancestors(ctx, "@test/tsapp/src/models/Worker.SceneWorker")
	require.NoError(t, err)
	require.Contains(t, ancestors, "@test/tsapp/src/models/Worker.Runnable")

	refRowsByPath, err := store.SymbolRefsByPaths(ctx, []string{"tsapp/src/services/SceneService.ts"})
	require.NoError(t, err)
	require.NotEmpty(t, refRowsByPath["tsapp/src/services/SceneService.ts"])
	for _, row := range refRowsByPath["tsapp/src/services/SceneService.ts"] {
		require.NotContains(t, row.OwnerFQN, "{")
		require.Equal(t, "@test/tsapp/src/services/SceneService.useSceneMusic", row.OwnerFQN)
	}
}

// TestIntegration_ValidateAnchors_SuffixMatching tests that ValidateAnchors
// correctly identifies FQN mismatches and suggests corrections via suffix matching.
func TestIntegration_ValidateAnchors_SuffixMatching(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "validate.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Use an indexer with NO source roots, so the full path becomes the module path.
	// This gives us predictable FQNs like "myapp.models.user.User".
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexerWithRoots(nil)},
		codeanchor.WithBasePath(dir),
		codeanchor.WithWriteAccess(),
	)

	// Create code file at myapp/models/user.py.
	// With no source roots, FQN will be: myapp.models.user.User
	codeDir := filepath.Join(dir, "myapp", "models")
	require.NoError(t, os.MkdirAll(codeDir, 0o755))
	codePath := filepath.Join(codeDir, "user.py")
	code := `class User:
    """User model."""
    pass
`
	require.NoError(t, os.WriteFile(codePath, []byte(code), 0o644))

	// Index the code file.
	err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, codePath, []byte(code))
	require.NoError(t, err)

	// Create a note with an anchor using a PARTIAL FQN (suffix of the full FQN).
	// The indexed FQN is "myapp.models.user.User", but we use just "models.user.User".
	// This should NOT match exactly (no symbol with that exact FQN),
	// but should suggest "myapp.models.user.User" via suffix matching.
	notePartialFQN := `---
code-anchors:
  py:
    - label: user-model-partial
      ref: models.user.User
---
User model docs with partial FQN.
`
	notePath := filepath.Join(dir, "notes", "user-partial.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(notePartialFQN), 0o644))

	_, err = svc.IngestNoteSource(context.Background(), integrationNoteSource(t, dir, notePath, notePartialFQN))
	require.NoError(t, err)
	require.NoError(t, svc.RecomputeAnchorScopes(context.Background()))

	// Run validation - should find suffix match and suggest correction.
	results, err := svc.ValidateAnchors(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1, "expected 1 validation result")

	result := results[0]
	require.Equal(t, "user-model-partial", result.Anchor.Label)
	require.Equal(t, codeanchor.ValidationSuffix, result.Status, "expected suffix_match status")
	require.NotEmpty(t, result.SuffixMatches, "expected suffix matches")

	// The suggested FQN should end with the expected path-based suffix.
	// Note: with no source roots, the FQN includes the temp directory prefix.
	foundCorrectFQN := false
	for _, fqn := range result.SuffixMatches {
		if strings.HasSuffix(fqn, "myapp.models.user.User") {
			foundCorrectFQN = true
			break
		}
	}
	require.True(t, foundCorrectFQN, "expected suffix match ending in 'myapp.models.user.User'; got: %v", result.SuffixMatches)
}

// TestIntegration_ValidateAnchors_ValidAnchor tests that ValidateAnchors
// correctly identifies valid anchors with exact FQN matches.
func TestIntegration_ValidateAnchors_ValidAnchor(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "validate_valid.db")
	store, err := sqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Use the temp directory as a source root so the indexer strips it from paths,
	// producing consistent FQNs like "service.MyService" across all platforms.
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexerWithRoots([]string{dir})},
		codeanchor.WithBasePath(dir),
		codeanchor.WithWriteAccess(),
	)

	// Create a simple code file at the root level.
	// Use a relative path from the base to ensure cross-platform FQN consistency.
	codePath := filepath.Join(dir, "service.py")
	code := `class MyService:
    """Service implementation."""
    pass
`
	require.NoError(t, os.WriteFile(codePath, []byte(code), 0o644))

	// Index the code file. With the temp dir as source root, FQN will be "service.MyService".
	err = svc.IndexCodeFile(context.Background(), codeanchor.LangPy, codePath, []byte(code))
	require.NoError(t, err)

	// Query to find the actual indexed FQN.
	syms, err := store.SymbolsBySuffix(context.Background(), "MyService", codeanchor.LangPy, 1)
	require.NoError(t, err)
	require.NotEmpty(t, syms, "expected to find MyService symbol")
	actualFQN := syms[0].FQN
	t.Logf("Indexed FQN: %s", actualFQN)
	require.Equal(t, "service.MyService", actualFQN)

	// Create a note with an anchor using the CORRECT FQN.
	noteCorrectFQN := `---
code-anchors:
  py:
    - label: my-service
      ref: ` + actualFQN + `
---
Service docs with correct FQN.
`
	notePath := filepath.Join(dir, "notes", "service.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(noteCorrectFQN), 0o644))

	_, err = svc.IngestNoteSource(context.Background(), integrationNoteSource(t, dir, notePath, noteCorrectFQN))
	require.NoError(t, err)
	require.NoError(t, svc.RecomputeAnchorScopes(context.Background()))

	// Run validation - should be valid.
	results, err := svc.ValidateAnchors(context.Background())
	require.NoError(t, err)
	require.Len(t, results, 1, "expected 1 validation result")

	result := results[0]
	require.Equal(t, "my-service", result.Anchor.Label)
	t.Logf("Validation status: %s, matched: %d", result.Status, result.MatchedCount)
	require.Equal(t, codeanchor.ValidationValid, result.Status, "expected valid status")
	require.GreaterOrEqual(t, result.MatchedCount, 1, "expected at least 1 matched symbol")
}

func integrationNoteSource(t *testing.T, root, path, content string) notemeta.NoteSourceSnapshot {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	require.NoError(t, err)
	return notemeta.NewContentOnlyNoteSourceSnapshot(filepath.ToSlash(rel), content, 0)
}
