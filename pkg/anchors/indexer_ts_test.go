//go:build cgo

package codeanchor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func codeRefFromRoot(t *testing.T, root, absPath string) paths.CodePathRef {
	t.Helper()
	rootPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := rootPaths.RelCodeStrict(absPath)
	require.NoError(t, err)
	return paths.CodePathRef{Rel: rel, Abs: paths.ResolveSymlinks(absPath)}
}

func requireTSCallContains(t *testing.T, calls []CallSite, callee SymbolRef) {
	t.Helper()
	for _, cs := range calls {
		if cs.CalleeSymbol == callee {
			return
		}
	}
	require.Failf(t, "expected call not found", "callee=%+v calls=%+v", callee, calls)
}

func TestTSIndexer_AcceptsTypeScriptJavaScriptModuleFormats(t *testing.T) {
	root := t.TempDir()
	idx := NewTSIndexerWithRoot(root)

	tests := []struct {
		ext     string
		content string
	}{
		{ext: ".ts", content: `export function run(): string { return "ok" }`},
		{ext: ".tsx", content: `export function View() { return <main /> }`},
		{ext: ".mts", content: `export function run(): string { return "ok" }`},
		{ext: ".cts", content: `export function run(): string { return "ok" }`},
		{ext: ".js", content: `export function run() { return "ok" }`},
		{ext: ".jsx", content: `export function View() { return <main /> }`},
		{ext: ".mjs", content: `export function run() { return "ok" }`},
		{ext: ".cjs", content: `function run() { return "ok" }; module.exports = { run }`},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			path := filepath.Join(root, "src", "module"+tt.ext)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o644))

			summary, err := idx.IndexFile([]byte(tt.content), codeRefFromRoot(t, root, path))
			require.NoError(t, err)
			require.Equal(t, ParseOK, summary.ParseStatus)
			require.NotEmpty(t, summary.Symbols)
		})
	}
}

func TestTSIndexer_IndexesJSXAsCall(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "components"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	compPath := filepath.Join(root, "tsapp", "src", "components", "SomeComponent.tsx")
	appPath := filepath.Join(root, "tsapp", "src", "app", "App.tsx")

	require.NoError(t, os.WriteFile(compPath, []byte(`export function SomeComponent(){ return <div /> }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(appPath, []byte(`
import { SomeComponent } from "../components/SomeComponent";
export function App() { return <SomeComponent />; }
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	appContent, err := os.ReadFile(appPath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(appContent, codeRefFromRoot(t, root, appPath))
	require.NoError(t, err)

	require.NotEmpty(t, summary.Calls, "expected JSX usage to produce call-sites; got none")

	found := false
	for _, cs := range summary.Calls {
		if cs.CalleeSymbol.Pkg == "@polyglot/tsapp/src/components/SomeComponent" && cs.CalleeSymbol.Name == "SomeComponent" {
			found = true
			break
		}
	}
	require.True(t, found, "expected call-site to resolve to @polyglot/tsapp/src/components.SomeComponent; got: %+v", summary.Calls)
}

func TestTSIndexer_IndexesHOCAssignedComponentAsSymbol(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	path := filepath.Join(root, "tsapp", "src", "Wrapped.tsx")
	require.NoError(t, os.WriteFile(path, []byte(`
import { memo } from "react";
export const Wrapped = memo(() => <div />);
`+"\n"), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)

	found := false
	for _, sym := range summary.Symbols {
		if sym.Pkg == "@polyglot/tsapp/src/Wrapped" && sym.Name == "Wrapped" {
			found = true
			break
		}
	}
	require.True(t, found, "expected Wrapped to be indexed as a symbol; got: %+v", summary.Symbols)
}

func TestTSIndexer_FileDocComment(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	path := filepath.Join(root, "tsapp", "src", "Doc.ts")
	require.NoError(t, os.WriteFile(path, []byte(`/**
 * File-level docs for Doc.ts
 */
export function run() {}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)
	require.Equal(t, ParseOK, summary.ParseStatus)
	require.Contains(t, summary.PackageDoc, "File-level docs for Doc.ts")
	requireTSRelationshipSymbol(t, summary, "@polyglot/tsapp/src/Doc.run")
}

func TestTSIndexer_FileHeaderLineComments(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	path := filepath.Join(root, "tsapp", "src", "Header.ts")
	require.NoError(t, os.WriteFile(path, []byte(`// Header comment line 1
// Header comment line 2
export function run() {}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)
	require.Contains(t, summary.PackageDoc, "Header comment line 1")
}

func TestTSIndexer_TailIndexFallbackResolvesAliasLikeImport(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "components"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	componentPath := filepath.Join(root, "tsapp", "src", "components", "SomeComponent.tsx")
	appPath := filepath.Join(root, "tsapp", "src", "app", "App.tsx")

	require.NoError(t, os.WriteFile(componentPath, []byte(`export function SomeComponent(){ return <div /> }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(appPath, []byte(`
import { SomeComponent } from "@/src/components/SomeComponent";
export function App() { return <SomeComponent />; }
`+"\n"), 0o644))

	tail := NewPathTailIndex(5)
	tail.Add(componentPath)

	idx := NewTSIndexerWithRootAndTailIndex(root, tail)
	require.NotNil(t, idx)

	appContent, err := os.ReadFile(appPath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(appContent, codeRefFromRoot(t, root, appPath))
	require.NoError(t, err)

	found := false
	for _, cs := range summary.Calls {
		if cs.CalleeSymbol.Pkg == "@polyglot/tsapp/src/components/SomeComponent" && cs.CalleeSymbol.Name == "SomeComponent" {
			found = true
			break
		}
	}
	require.True(t, found, "expected call-site to resolve via tail index fallback; got: %+v", summary.Calls)
}

func TestTSIndexer_DefaultExportNamedFunctionEmitsFileBaseAlias(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	defPath := filepath.Join(root, "tsapp", "src", "Foo.ts")
	usePath := filepath.Join(root, "tsapp", "src", "App.ts")

	require.NoError(t, os.WriteFile(defPath, []byte(`export default function Named() {}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(usePath, []byte(`
import Foo from "./Foo";
Foo();
`+"\n"), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	defContent, err := os.ReadFile(defPath)
	require.NoError(t, err)
	defSummary, err := idx.IndexFile(defContent, codeRefFromRoot(t, root, defPath))
	require.NoError(t, err)

	pkg := "@polyglot/tsapp/src/Foo"
	foundNamed := false
	foundFoo := false
	for _, sym := range defSummary.Symbols {
		if sym.Pkg != pkg {
			continue
		}
		if sym.Name == "Named" {
			foundNamed = true
		}
		if sym.Name == "Foo" {
			foundFoo = true
		}
	}
	require.True(t, foundNamed, "expected default export declared name to be indexed")
	require.True(t, foundFoo, "expected file-base alias symbol to be indexed for default exports")

	useContent, err := os.ReadFile(usePath)
	require.NoError(t, err)
	useSummary, err := idx.IndexFile(useContent, codeRefFromRoot(t, root, usePath))
	require.NoError(t, err)

	foundCall := false
	for _, cs := range useSummary.Calls {
		if cs.CalleeSymbol.Pkg == pkg && cs.CalleeSymbol.Name == "Foo" {
			foundCall = true
			break
		}
	}
	require.True(t, foundCall, "expected default import call to resolve to file-base alias; got: %+v", useSummary.Calls)
}

func TestTSIndexer_DefaultExportNamedClassEmitsFileBaseAlias(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	defPath := filepath.Join(root, "tsapp", "src", "Thing.ts")
	usePath := filepath.Join(root, "tsapp", "src", "App.ts")

	require.NoError(t, os.WriteFile(defPath, []byte(`export default class NamedThing {}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(usePath, []byte(`
import Thing from "./Thing";
new Thing();
`+"\n"), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	defContent, err := os.ReadFile(defPath)
	require.NoError(t, err)
	defSummary, err := idx.IndexFile(defContent, codeRefFromRoot(t, root, defPath))
	require.NoError(t, err)

	pkg := "@polyglot/tsapp/src/Thing"
	foundNamed := false
	foundThing := false
	for _, sym := range defSummary.Symbols {
		if sym.Pkg != pkg {
			continue
		}
		if sym.Name == "NamedThing" {
			foundNamed = true
		}
		if sym.Name == "Thing" {
			foundThing = true
		}
	}
	require.True(t, foundNamed, "expected default export declared name to be indexed")
	require.True(t, foundThing, "expected file-base alias symbol to be indexed for default exports")

	useContent, err := os.ReadFile(usePath)
	require.NoError(t, err)
	useSummary, err := idx.IndexFile(useContent, codeRefFromRoot(t, root, usePath))
	require.NoError(t, err)

	foundNew := false
	for _, cs := range useSummary.Calls {
		if cs.CalleeSymbol.Pkg == pkg && cs.CalleeSymbol.Name == "Thing" {
			foundNew = true
			break
		}
	}
	require.True(t, foundNew, "expected new-expression to resolve to file-base alias; got: %+v", useSummary.Calls)
}

func TestTSIndexer_IndexesNewExpressionAsCall(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "models"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	modelPath := filepath.Join(root, "tsapp", "src", "models", "User.ts")
	appPath := filepath.Join(root, "tsapp", "src", "app", "App.ts")

	require.NoError(t, os.WriteFile(modelPath, []byte(`export class User {}`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(appPath, []byte(`
import { User } from "../models/User";
export function run() { new User(); }
`+"\n"), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	appContent, err := os.ReadFile(appPath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(appContent, codeRefFromRoot(t, root, appPath))
	require.NoError(t, err)

	found := false
	for _, cs := range summary.Calls {
		if cs.CalleeSymbol.Pkg == "@polyglot/tsapp/src/models/User" && cs.CalleeSymbol.Name == "User" {
			found = true
			break
		}
	}
	require.True(t, found, "expected new-expression to produce call-site for User; got: %+v", summary.Calls)
}

func TestTSIndexer_ImportsEdges(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "components"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "utils"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	buttonPath := filepath.Join(root, "tsapp", "src", "components", "Button.tsx")
	helperPath := filepath.Join(root, "tsapp", "src", "utils", "helpers.ts")
	appPath := filepath.Join(root, "tsapp", "src", "App.tsx")

	require.NoError(t, os.WriteFile(buttonPath, []byte(`export function Button() { return <button />; }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(helperPath, []byte(`export function formatDate(d: Date) { return d.toISOString(); }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(appPath, []byte(`
import { Button } from "./components/Button";
import { formatDate } from "./utils/helpers";
import React from "react";  // npm package - should be ignored

export function App() {
    return <Button />;
}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	appContent, err := os.ReadFile(appPath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(appContent, codeRefFromRoot(t, root, appPath))
	require.NoError(t, err)

	// Should have import edges for local imports (Button, helpers), but not for npm packages (react).
	// TypeScript stores resolved file paths (not pkg names) for database lookup.
	require.Len(t, summary.Imports, 2, "expected 2 import edges for local imports; got: %+v", summary.Imports)

	// Check that import edges contain resolved file paths.
	modules := make(map[string]bool)
	for _, imp := range summary.Imports {
		modules[imp.Module] = true
	}

	// Expect file paths (normalized absolute paths with symlinks resolved).
	// The indexer normalizes paths, so we need to do the same for comparison.
	normalizedButtonPath := paths.ResolveSymlinks(buttonPath).String()
	normalizedHelperPath := paths.ResolveSymlinks(helperPath).String()
	require.True(t, modules[normalizedButtonPath], "expected import edge for Button file path; got: %+v", modules)
	require.True(t, modules[normalizedHelperPath], "expected import edge for helpers file path; got: %+v", modules)
}

func TestTSIndexer_AliasImportsVsNpmPackages(t *testing.T) {
	// Test that path aliases are resolved via tail index while npm packages are ignored.
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "components", "ui"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@myapp/tsapp\"}\n"), 0o644))

	dialogPath := filepath.Join(root, "tsapp", "src", "components", "ui", "dialog.tsx")
	appPath := filepath.Join(root, "tsapp", "src", "App.tsx")

	require.NoError(t, os.WriteFile(dialogPath, []byte(`export function Dialog() { return <div />; }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(appPath, []byte(`
import React from "react";                           // npm - should be ignored
import { Dialog } from "@components/ui/dialog";      // alias with 3+ segments - should resolve
import { something } from "@angular/core";           // npm scoped - should be ignored
import { other } from "@types/node";                 // npm @types - should be ignored
`), 0o644))

	// Build tail index with absolute paths (simulating production behavior).
	tailIdx := NewPathTailIndex(5)
	err := filepath.Walk(filepath.Join(root, "tsapp"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && info.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			ext := filepath.Ext(path)
			if ext == ".ts" || ext == ".tsx" {
				tailIdx.Add(path)
			}
		}
		return nil
	})
	require.NoError(t, err)

	idx := NewTSIndexerWithRootAndTailIndex(root, tailIdx)
	require.NotNil(t, idx)

	appContent, err := os.ReadFile(appPath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(appContent, codeRefFromRoot(t, root, appPath))
	require.NoError(t, err)

	// Should have exactly 1 import edge (for the alias import that resolved to dialog.tsx).
	// npm packages (react, @angular/core, @types/node) should NOT create import edges.
	require.Len(t, summary.Imports, 1, "expected 1 import edge for alias import; got: %+v", summary.Imports)

	// The import should be the resolved path to dialog.tsx.
	normalizedDialogPath := paths.ResolveSymlinks(dialogPath).String()
	require.Equal(t, normalizedDialogPath, summary.Imports[0].Module)
}

func TestTSIndexer_TypeRefs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "models"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "services"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	userPath := filepath.Join(root, "tsapp", "src", "models", "User.ts")
	servicePath := filepath.Join(root, "tsapp", "src", "services", "UserService.ts")

	require.NoError(t, os.WriteFile(userPath, []byte(`export interface User { name: string; }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(servicePath, []byte(`
import { User } from "../models/User";

export function getUser(id: string): User {
    return { name: "Test" };
}

export function updateUser(user: User): void {
    console.log(user);
}

export const findUsers = (query: string): User[] => {
    return [];
};
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	serviceContent, err := os.ReadFile(servicePath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(serviceContent, codeRefFromRoot(t, root, servicePath))
	require.NoError(t, err)

	// Should have type refs for User in parameter types and return types.
	// Built-in types (string, void) should be filtered out.
	require.NotEmpty(t, summary.TypeRefs, "expected type refs for User type annotations")

	userPkg := "@polyglot/tsapp/src/models/User"
	var userRefs []TypeRef
	for _, tr := range summary.TypeRefs {
		if tr.TypeSym.Pkg == userPkg && tr.TypeSym.Name == "User" {
			userRefs = append(userRefs, tr)
		}
	}

	// We expect type refs from:
	// - getUser return type (User)
	// - updateUser parameter (user: User)
	// - findUsers return type (User[])
	require.GreaterOrEqual(t, len(userRefs), 3, "expected at least 3 type refs for User; got: %+v", summary.TypeRefs)
}

func TestTSIndexer_TypeRefs_GenericTypes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "models"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "services"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	userPath := filepath.Join(root, "tsapp", "src", "models", "User.ts")
	responsePath := filepath.Join(root, "tsapp", "src", "models", "Response.ts")
	servicePath := filepath.Join(root, "tsapp", "src", "services", "ApiService.ts")

	require.NoError(t, os.WriteFile(userPath, []byte(`export interface User { name: string; }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(responsePath, []byte(`export interface ApiResponse<T> { data: T; }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(servicePath, []byte(`
import { User } from "../models/User";
import { ApiResponse } from "../models/Response";

export function fetchUser(id: string): ApiResponse<User> {
    return { data: { name: "Test" } };
}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	serviceContent, err := os.ReadFile(servicePath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(serviceContent, codeRefFromRoot(t, root, servicePath))
	require.NoError(t, err)

	// Should have type refs for both ApiResponse and User in generic types.
	require.NotEmpty(t, summary.TypeRefs, "expected type refs for generic type annotations")

	foundApiResponse := false
	foundUser := false
	for _, tr := range summary.TypeRefs {
		if tr.TypeSym.Pkg == "@polyglot/tsapp/src/models/Response" && tr.TypeSym.Name == "ApiResponse" {
			foundApiResponse = true
		}
		if tr.TypeSym.Pkg == "@polyglot/tsapp/src/models/User" && tr.TypeSym.Name == "User" {
			foundUser = true
		}
	}

	require.True(t, foundApiResponse, "expected type ref for ApiResponse; got: %+v", summary.TypeRefs)
	require.True(t, foundUser, "expected type ref for User in generic type argument; got: %+v", summary.TypeRefs)
}

func TestTSIndexer_OptionalChainCalls(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	apiPath := filepath.Join(root, "tsapp", "src", "api.ts")
	appPath := filepath.Join(root, "tsapp", "src", "App.ts")
	require.NoError(t, os.WriteFile(apiPath, []byte(`
export function maybeRun() { return true; }
export const StoriesApi = {
  detail() { return true; },
};
`), 0o644))
	require.NoError(t, os.WriteFile(appPath, []byte(`
import { maybeRun, StoriesApi } from "./api";

export function App() {
  maybeRun?.();
  StoriesApi?.detail();
}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	content, err := os.ReadFile(appPath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, appPath))
	require.NoError(t, err)

	requireTSCallContains(t, summary.Calls, SymbolRef{Lang: LangTS, Pkg: "@polyglot/tsapp/src/api", Name: "maybeRun"})
	requireTSCallContains(t, summary.Calls, SymbolRef{Lang: LangTS, Pkg: "@polyglot/tsapp/src/api", Name: "detail"})
}

func TestTSIndexer_MemberRefs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	constsPath := filepath.Join(root, "tsapp", "src", "constants.ts")
	usePath := filepath.Join(root, "tsapp", "src", "use.ts")
	require.NoError(t, os.WriteFile(constsPath, []byte(`
export const Stage = {
  Flags: {
    Tilt: "tilt",
  },
};
`), 0o644))
	require.NoError(t, os.WriteFile(usePath, []byte(`
import { Stage } from "./constants";

export function readStageFlag() {
  return Stage.Flags.Tilt;
}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	content, err := os.ReadFile(usePath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, usePath))
	require.NoError(t, err)

	require.Contains(t, summary.MemberRefs, MemberRef{
		File:     "tsapp/src/use.ts",
		OwnerFQN: "@polyglot/tsapp/src/use.readStageFlag",
		Sym: SymbolRef{
			Lang: LangTS,
			Pkg:  "@polyglot/tsapp/src/constants",
			Name: "Tilt",
		},
	})
}

func TestTSIndexer_TypeRefs_BeyondFunctionSignatures(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "models"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "state"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	modelPath := filepath.Join(root, "tsapp", "src", "models", "Scene.ts")
	statePath := filepath.Join(root, "tsapp", "src", "state", "sceneState.ts")
	require.NoError(t, os.WriteFile(modelPath, []byte(`
export interface Scene { id: string; }
export interface SceneConfig { active: Scene; }
`), 0o644))
	require.NoError(t, os.WriteFile(statePath, []byte(`
import { Scene, SceneConfig } from "../models/Scene";

export interface SceneBox {
  current: Scene;
}

export type SceneState = {
  scene: Scene;
};

export function readScene() {
  const current: Scene = { id: "x" };
  const config = { active: current } satisfies SceneConfig;
  return config;
}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	content, err := os.ReadFile(statePath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, statePath))
	require.NoError(t, err)

	modelPkg := "@polyglot/tsapp/src/models/Scene"
	foundScene := 0
	foundConfig := 0
	for _, tr := range summary.TypeRefs {
		if tr.TypeSym.Pkg != modelPkg {
			continue
		}
		if tr.TypeSym.Name == "Scene" {
			foundScene++
		}
		if tr.TypeSym.Name == "SceneConfig" {
			foundConfig++
		}
	}

	require.GreaterOrEqual(t, foundScene, 3, "expected Scene refs from interface/type alias/const annotation; got %+v", summary.TypeRefs)
	require.GreaterOrEqual(t, foundConfig, 1, "expected SceneConfig ref from satisfies; got %+v", summary.TypeRefs)
}

func TestTSIndexer_Supers(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	sharedPath := filepath.Join(root, "tsapp", "src", "shared.ts")
	path := filepath.Join(root, "tsapp", "src", "types.ts")
	require.NoError(t, os.WriteFile(sharedPath, []byte(`
export class BaseThing {}
export interface Runnable {}
export interface Serializable {}
`), 0o644))
	require.NoError(t, os.WriteFile(path, []byte(`
import { BaseThing, Runnable, Serializable } from "./shared";

export class Worker extends BaseThing implements Runnable, Serializable {}
export interface StageWorker extends Runnable {}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	content, err := os.ReadFile(path)
	require.NoError(t, err)

	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)

	require.Contains(t, summary.Supers, SuperEdge{
		ChildFQN:  "@polyglot/tsapp/src/types.Worker",
		ParentFQN: "@polyglot/tsapp/src/shared.BaseThing",
	})
	require.Contains(t, summary.Supers, SuperEdge{
		ChildFQN:  "@polyglot/tsapp/src/types.Worker",
		ParentFQN: "@polyglot/tsapp/src/shared.Runnable",
	})
	require.Contains(t, summary.Supers, SuperEdge{
		ChildFQN:  "@polyglot/tsapp/src/types.StageWorker",
		ParentFQN: "@polyglot/tsapp/src/shared.Runnable",
	})
}

func TestTSIndexer_BarrelReExports(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	storiesPath := filepath.Join(root, "tsapp", "src", "lib", "stories.ts")
	workerPath := filepath.Join(root, "tsapp", "src", "lib", "worker.ts")
	sharedPath := filepath.Join(root, "tsapp", "src", "lib", "shared.ts")
	indexPath := filepath.Join(root, "tsapp", "src", "lib", "index.ts")
	appPath := filepath.Join(root, "tsapp", "src", "lib", "app.ts")
	require.NoError(t, os.WriteFile(storiesPath, []byte(`export function detail() { return true; }`), 0o644))
	require.NoError(t, os.WriteFile(workerPath, []byte(`export class Worker {}`), 0o644))
	require.NoError(t, os.WriteFile(sharedPath, []byte(`export const shared = true;`), 0o644))
	require.NoError(t, os.WriteFile(indexPath, []byte(`
export { detail } from "./stories";
export { Worker as StageWorker } from "./worker";
export * from "./shared";
`), 0o644))
	require.NoError(t, os.WriteFile(appPath, []byte(`
import { detail, StageWorker } from "./index";

export function run() {
  detail();
  return new StageWorker();
}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	indexContent, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	indexSummary, err := idx.IndexFile(indexContent, codeRefFromRoot(t, root, indexPath))
	require.NoError(t, err)
	require.Len(t, indexSummary.Imports, 3, "expected re-exported modules to count as imports; got %+v", indexSummary.Imports)
	reExportSignatures := map[string]string{}
	for _, symbol := range indexSummary.Symbols {
		if symbol.Signature != "" {
			reExportSignatures[symbol.Name] = symbol.Signature
		}
	}
	require.Equal(t, "re-export from @polyglot/tsapp/src/lib/stories.detail", reExportSignatures["detail"])
	require.Equal(t, "re-export from @polyglot/tsapp/src/lib/worker.Worker", reExportSignatures["StageWorker"])

	appContent, err := os.ReadFile(appPath)
	require.NoError(t, err)
	appSummary, err := idx.IndexFile(appContent, codeRefFromRoot(t, root, appPath))
	require.NoError(t, err)

	requireTSCallContains(t, appSummary.Calls, SymbolRef{Lang: LangTS, Pkg: "@polyglot/tsapp/src/lib/stories", Name: "detail"})
	requireTSCallContains(t, appSummary.Calls, SymbolRef{Lang: LangTS, Pkg: "@polyglot/tsapp/src/lib/worker", Name: "Worker"})
}

func TestTSIndexer_DestructuredOwnersUseStableFunctionFQNs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	path := filepath.Join(root, "tsapp", "src", "hooks.ts")
	require.NoError(t, os.WriteFile(path, []byte(`
import { MusicRuntime } from "./music";

export function useSceneMusic() {
  const { currentSceneMusic, playCurrentSceneMusic, playSoundEffect } = MusicRuntime;
  playCurrentSceneMusic?.();
  return currentSceneMusic ?? playSoundEffect;
}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	content, err := os.ReadFile(path)
	require.NoError(t, err)

	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)

	rows := BuildSymbolRefRows("tsapp/src/hooks.ts", summary)
	require.NotEmpty(t, rows)
	for _, row := range rows {
		require.Equal(t, "@polyglot/tsapp/src/hooks.useSceneMusic", row.OwnerFQN)
		require.NotContains(t, row.OwnerFQN, "{")
	}
}

func TestTSIndexer_TypeRefs_ClassMethods(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "models"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "services"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	userPath := filepath.Join(root, "tsapp", "src", "models", "User.ts")
	servicePath := filepath.Join(root, "tsapp", "src", "services", "UserService.ts")

	require.NoError(t, os.WriteFile(userPath, []byte(`export class User { name: string = ""; }`+"\n"), 0o644))
	require.NoError(t, os.WriteFile(servicePath, []byte(`
import { User } from "../models/User";

export class UserService {
    getUser(id: string): User {
        return new User();
    }

    saveUser(user: User): void {
        console.log(user);
    }
}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	serviceContent, err := os.ReadFile(servicePath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(serviceContent, codeRefFromRoot(t, root, servicePath))
	require.NoError(t, err)

	// Should have type refs for User in class method parameters and return types.
	require.NotEmpty(t, summary.TypeRefs, "expected type refs for User in class methods")

	userPkg := "@polyglot/tsapp/src/models/User"
	var userRefs []TypeRef
	for _, tr := range summary.TypeRefs {
		if tr.TypeSym.Pkg == userPkg && tr.TypeSym.Name == "User" {
			userRefs = append(userRefs, tr)
		}
	}

	// We expect type refs from:
	// - getUser return type (User)
	// - saveUser parameter (user: User)
	require.GreaterOrEqual(t, len(userRefs), 2, "expected at least 2 type refs for User in class methods; got: %+v", summary.TypeRefs)
}

func TestTSIndexer_TypeRefs_NamespaceImport(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "models"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src", "services"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	modelsPath := filepath.Join(root, "tsapp", "src", "models", "index.ts")
	servicePath := filepath.Join(root, "tsapp", "src", "services", "UserService.ts")

	require.NoError(t, os.WriteFile(modelsPath, []byte(`
export interface User { name: string; }
export interface Order { id: number; }
`), 0o644))

	// Use namespace import: import * as Models from ...
	require.NoError(t, os.WriteFile(servicePath, []byte(`
import * as Models from "../models";

export function getUser(id: string): Models.User {
    return { name: "test" };
}

export function processOrder(order: Models.Order): void {
    console.log(order);
}
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	serviceContent, err := os.ReadFile(servicePath)
	require.NoError(t, err)

	summary, err := idx.IndexFile(serviceContent, codeRefFromRoot(t, root, servicePath))
	require.NoError(t, err)

	// Should have type refs for User and Order via namespace import.
	require.NotEmpty(t, summary.TypeRefs, "expected type refs for namespace-imported types")

	// The types should be resolved to the models package (index.ts), not the current package.
	// Since we import from "../models", it resolves to "../models/index.ts".
	modelsPkg := "@polyglot/tsapp/src/models/index"
	foundUser := false
	foundOrder := false
	for _, tr := range summary.TypeRefs {
		if tr.TypeSym.Pkg == modelsPkg && tr.TypeSym.Name == "User" {
			foundUser = true
		}
		if tr.TypeSym.Pkg == modelsPkg && tr.TypeSym.Name == "Order" {
			foundOrder = true
		}
	}

	require.True(t, foundUser, "expected Models.User to resolve to %s.User; got: %+v", modelsPkg, summary.TypeRefs)
	require.True(t, foundOrder, "expected Models.Order to resolve to %s.Order; got: %+v", modelsPkg, summary.TypeRefs)
}

func TestTSIndexer_IndexesTopLevelObjectLiteralMethodsAsSymbols(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tsapp", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsapp", "package.json"), []byte("{\"name\":\"@polyglot/tsapp\"}\n"), 0o644))

	path := filepath.Join(root, "tsapp", "src", "api.ts")
	require.NoError(t, os.WriteFile(path, []byte(`
export const StoriesApi = {
  detail: async (storyId: string) => ({ storyId }),
  rename(storyId: string, name: string) {
    return { storyId, name };
  },
};
`), 0o644))

	idx := NewTSIndexerWithRoot(root)
	require.NotNil(t, idx)

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, sym := range summary.Symbols {
		if sym.Pkg == "@polyglot/tsapp/src/api" {
			names[sym.Name] = true
		}
	}

	require.True(t, names["detail"], "expected object-literal arrow method to be indexed; got: %+v", summary.Symbols)
	require.True(t, names["rename"], "expected object-literal method shorthand to be indexed; got: %+v", summary.Symbols)
}
