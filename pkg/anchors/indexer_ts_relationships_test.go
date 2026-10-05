//go:build cgo

package codeanchor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func writeTSRelationshipFixture(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func indexTSRelationshipFixture(t *testing.T, idx *TSIndexer, root, rel string) FileSummary {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	summary, err := idx.IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)
	return summary
}

func requireTSRelationshipCall(t *testing.T, summary FileSummary, pkg, name string) {
	t.Helper()
	requireTSCallContains(t, summary.Calls, SymbolRef{Lang: LangTS, Pkg: pkg, Name: name})
}

func requireNoTSRelationshipCall(t *testing.T, summary FileSummary, name string) {
	t.Helper()
	for _, call := range summary.Calls {
		require.NotEqual(t, name, call.CalleeSymbol.Name, "unexpected speculative call: %+v", call)
	}
}

func requireTSRelationshipSymbol(t *testing.T, summary FileSummary, fqn string) {
	t.Helper()
	for _, symbol := range summary.Symbols {
		if symbol.NormalizeFQN() == fqn {
			return
		}
	}
	require.Failf(t, "expected symbol not found", "fqn=%s symbols=%+v", fqn, summary.Symbols)
}

func newTSRelationshipFixture(t *testing.T) (string, *TSIndexer) {
	t.Helper()
	root := t.TempDir()
	writeTSRelationshipFixture(t, root, "package.json", `{"name":"@fixture/app"}`)
	return root, NewTSIndexerWithRoot(root)
}

func TestTSIndexer_ModuleSyntaxRelationships(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	sideEffectPath := writeTSRelationshipFixture(t, root, "src/register.ts", `globalThis.registered = true`)
	toolsPath := writeTSRelationshipFixture(t, root, "src/tools.ts", `export function dynamicWork() {}
export function namespaceWork() {}
export function destructuredWork() {}`)
	defaultPath := writeTSRelationshipFixture(t, root, "src/default.ts", `export default function start() {}`)
	defaultSummary := indexTSRelationshipFixture(t, idx, root, "src/default.ts")
	requireTSRelationshipSymbol(t, defaultSummary, "@fixture/app/src/default.start")

	tests := []struct {
		name, source, targetPath, pkg, symbol string
	}{
		{"dynamic_import", `export async function run() { const tools = await import("./tools"); tools.dynamicWork(); }`, toolsPath, "@fixture/app/src/tools", "dynamicWork"},
		{"default_require", `export function run() { const start = require("./default"); start(); }`, defaultPath, "@fixture/app/src/default", "start"},
		{"namespace_require", `export function run() { const tools = require("./tools"); tools.namespaceWork(); }`, toolsPath, "@fixture/app/src/tools", "namespaceWork"},
		{"destructured_require", `export function run() { const { destructuredWork: alias } = require("./tools"); alias(); }`, toolsPath, "@fixture/app/src/tools", "destructuredWork"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := "src/use-" + tt.name + ".ts"
			writeTSRelationshipFixture(t, root, rel, tt.source)
			summary := indexTSRelationshipFixture(t, idx, root, rel)
			require.Equal(t, ParseOK, summary.ParseStatus)
			require.Equal(t, []ImportEdge{{Module: paths.ResolveSymlinks(tt.targetPath).String()}}, summary.Imports)
			requireTSRelationshipCall(t, summary, tt.pkg, tt.symbol)
		})
	}
	t.Run("side_effect", func(t *testing.T) {
		writeTSRelationshipFixture(t, root, "src/use-side-effect.ts", `import "./register";`)
		summary := indexTSRelationshipFixture(t, idx, root, "src/use-side-effect.ts")
		require.Equal(t, []ImportEdge{{Module: paths.ResolveSymlinks(sideEffectPath).String()}}, summary.Imports)
	})
	t.Run("computed_source_unresolved", func(t *testing.T) {
		writeTSRelationshipFixture(t, root, "src/use-computed.ts", `export async function run(path: string) { await import(path); require(path); }`)
		summary := indexTSRelationshipFixture(t, idx, root, "src/use-computed.ts")
		require.Empty(t, summary.Imports)
	})
}

func TestTSIndexer_ChainedBarrelRelationships(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	writeTSRelationshipFixture(t, root, "src/leaf.ts", `
export function original() {}
export interface Model { id: string }
`)
	writeTSRelationshipFixture(t, root, "src/alias-one.ts", `export { original as renamed } from "./leaf"`)
	writeTSRelationshipFixture(t, root, "src/alias-two.ts", `export { renamed as final } from "./alias-one"`)
	writeTSRelationshipFixture(t, root, "src/star-one.ts", `export * from "./leaf"`)
	writeTSRelationshipFixture(t, root, "src/star-two.ts", `export * from "./star-one"`)
	writeTSRelationshipFixture(t, root, "src/type-one.ts", `export type { Model as PublicModel } from "./leaf"`)
	writeTSRelationshipFixture(t, root, "src/type-two.ts", `export type { PublicModel as AppModel } from "./type-one"`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `
import { final } from "./alias-two";
import { original } from "./star-two";
import type { AppModel } from "./type-two";

export function run(model: AppModel) {
  final();
  original();
  return model.id;
}
`)

	summary := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireTSRelationshipCall(t, summary, "@fixture/app/src/leaf", "original")
	require.Equal(t, 2, countTSRelationshipCalls(summary, "@fixture/app/src/leaf", "original"), "alias and star chains must converge on the defining symbol")
	require.Contains(t, summary.TypeRefs, TypeRef{
		File:     filepath.ToSlash("src/use.ts"),
		OwnerFQN: "@fixture/app/src/use.run",
		TypeSym:  SymbolRef{Lang: LangTS, Pkg: "@fixture/app/src/leaf", Name: "Model"},
	})
}

func countTSRelationshipCalls(summary FileSummary, pkg, name string) int {
	count := 0
	for _, call := range summary.Calls {
		if call.CalleeSymbol.Pkg == pkg && call.CalleeSymbol.Name == name {
			count++
		}
	}
	return count
}

func TestTSIndexer_BarrelCyclesAndAmbiguityStayUnresolved(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	writeTSRelationshipFixture(t, root, "src/cycle-a.ts", `export * from "./cycle-b"`)
	writeTSRelationshipFixture(t, root, "src/cycle-b.ts", `export * from "./cycle-a"`)
	writeTSRelationshipFixture(t, root, "src/one.ts", `export function duplicate() {}`)
	writeTSRelationshipFixture(t, root, "src/two.ts", `export function duplicate() {}`)
	writeTSRelationshipFixture(t, root, "src/ambiguous.ts", `
export * from "./one";
export * from "./two";
`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `
import { missing } from "./cycle-a";
import { duplicate } from "./ambiguous";
missing();
duplicate();
`)

	summary := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireNoTSRelationshipCall(t, summary, "missing")
	requireNoTSRelationshipCall(t, summary, "duplicate")
}

func TestTSIndexer_BarrelCacheRevalidatesChangedDependencies(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	leaf := writeTSRelationshipFixture(t, root, "src/leaf.ts", `export function first() {}`)
	writeTSRelationshipFixture(t, root, "src/barrel.ts", `export * from "./leaf"`)
	use := writeTSRelationshipFixture(t, root, "src/use.ts", `import { first } from "./barrel"; first();`)

	before := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireTSRelationshipCall(t, before, "@fixture/app/src/leaf", "first")

	require.NoError(t, os.WriteFile(leaf, []byte(`export function secondName() {}`), 0o644))
	require.NoError(t, os.WriteFile(use, []byte(`
import { first, secondName } from "./barrel";
first();
secondName();
`), 0o644))
	after := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireNoTSRelationshipCall(t, after, "first")
	requireTSRelationshipCall(t, after, "@fixture/app/src/leaf", "secondName")
}

func TestTSIndexer_BarrelCacheInvalidatesWithPackageMetadata(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	packageJSON := writeTSRelationshipFixture(t, root, "packages/library/package.json", `{"name":"@fixture/library","exports":"./src/first.ts"}`)
	writeTSRelationshipFixture(t, root, "packages/library/src/first.ts", `export function work() {}`)
	writeTSRelationshipFixture(t, root, "packages/library/src/second.ts", `export function work() {}`)
	writeTSRelationshipFixture(t, root, "src/barrel.ts", `export { work } from "@fixture/library"`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `import { work } from "./barrel"; work();`)

	before := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireTSRelationshipCall(t, before, "@fixture/library/src/first", "work")

	require.NoError(t, os.WriteFile(packageJSON, []byte(`{"name":"@fixture/library","exports":"./src/second.ts"}`), 0o644))
	idx.InvalidateModuleMetadata(packageJSON)
	after := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	require.Equal(t, 0, countTSRelationshipCalls(after, "@fixture/library/src/first", "work"))
	requireTSRelationshipCall(t, after, "@fixture/library/src/second", "work")
}

func TestTSIndexer_BarrelCacheInvalidatesWithTSConfigMetadata(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	tsconfig := writeTSRelationshipFixture(t, root, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@leaf":["src/first.ts"]}}}`)
	writeTSRelationshipFixture(t, root, "src/first.ts", `export function work() {}`)
	writeTSRelationshipFixture(t, root, "src/second.ts", `export function work() {}`)
	writeTSRelationshipFixture(t, root, "src/barrel.ts", `export { work } from "@leaf"`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `import { work } from "./barrel"; work();`)

	before := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireTSRelationshipCall(t, before, "@fixture/app/src/first", "work")

	require.NoError(t, os.WriteFile(tsconfig, []byte(`{"compilerOptions":{"baseUrl":".","paths":{"@leaf":["src/second.ts"]}}}`), 0o644))
	idx.InvalidateModuleMetadata(tsconfig)
	after := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	require.Equal(t, 0, countTSRelationshipCalls(after, "@fixture/app/src/first", "work"))
	requireTSRelationshipCall(t, after, "@fixture/app/src/second", "work")
}

func TestTSIndexer_FailedBarrelResolutionRetriesAfterTargetCreation(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	writeTSRelationshipFixture(t, root, "src/barrel.ts", `export * from "./late"`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `import { work } from "./barrel"; work();`)

	before := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireNoTSRelationshipCall(t, before, "work")

	writeTSRelationshipFixture(t, root, "src/late.ts", `export function work() {}`)
	after := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireTSRelationshipCall(t, after, "@fixture/app/src/late", "work")
}

func TestTSIndexer_CompoundCallableCandidates(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	writeTSRelationshipFixture(t, root, "src/candidates.ts", `
export function override() {}
export function fallback() {}
export function primary() {}
`)
	writeTSRelationshipFixture(t, root, "src/cycle-a.ts", `export * from "./cycle-b"`)
	writeTSRelationshipFixture(t, root, "src/cycle-b.ts", `export * from "./cycle-a"`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `
import { override, fallback, primary } from "./candidates";
import { missing } from "./cycle-a";

export function run(enabled: boolean) {
  (override ?? fallback)();
  (enabled && primary)();
  (enabled ? primary : fallback)();
  (enabled && (primary || fallback))();
  (primary ?? missing)();
  (enabled ? missing : fallback)();
}
`)

	summary := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	require.Equal(t, 1, countTSRelationshipCalls(summary, "@fixture/app/src/candidates", "override"))
	require.Equal(t, 4, countTSRelationshipCalls(summary, "@fixture/app/src/candidates", "fallback"))
	require.Equal(t, 4, countTSRelationshipCalls(summary, "@fixture/app/src/candidates", "primary"))
	requireNoTSRelationshipCall(t, summary, "enabled")
	requireNoTSRelationshipCall(t, summary, "missing")
}

func TestTSIndexer_NamespaceBarrelRelationship(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	writeTSRelationshipFixture(t, root, "src/leaf.ts", `export function original() {}`)
	writeTSRelationshipFixture(t, root, "src/namespace.ts", `export * as tools from "./leaf"`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `
import { tools } from "./namespace";
tools.original();
`)

	leafSummary := indexTSRelationshipFixture(t, idx, root, "src/leaf.ts")
	requireTSRelationshipSymbol(t, leafSummary, "@fixture/app/src/leaf.original")
	useSummary := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireTSRelationshipCall(t, useSummary, "@fixture/app/src/leaf", "original")
}

func TestTSIndexer_ImportedMembersAndUnknownReceivers(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	writeTSRelationshipFixture(t, root, "src/client.ts", `
export default function createClient() {}
export function save() {}
`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `
import client from "./client";
import * as api from "./client";

export function save() {}
export function run(unknownClient: { save(): void }) {
  client.save();
  api.save();
  unknownClient.save();
}
`)

	summary := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	require.Equal(t, 2, countTSRelationshipCalls(summary, "@fixture/app/src/client", "save"), "default and namespace imported members must resolve")
	require.Equal(t, 0, countTSRelationshipCalls(summary, "@fixture/app/src/use", "save"), "unknown receivers must not collide with a same-named local symbol")
}

func TestTSIndexer_ScopedLexicalCallablesAndDuplicateNames(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	writeTSRelationshipFixture(t, root, "src/use.ts", `
export function lifecycle() {
  const clearDrainTimer = () => {};
  function closeOnce() { clearDrainTimer(); }
  const write = function() { closeOnce(); };
  write();
}

export function setup() {
  const stopContainer = async () => {};
  stopContainer();
}

export function first() {
  const duplicate = () => {};
  duplicate();
}

export function second() {
  const duplicate = () => {};
  duplicate();
}

export function ambiguous(flag: boolean) {
  if (flag) {
    const shadow = () => {};
    shadow();
  } else {
    const shadow = () => {};
    shadow();
  }
}
`)

	summary := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	for _, name := range []string{
		"lifecycle.clearDrainTimer",
		"lifecycle.closeOnce",
		"lifecycle.write",
		"setup.stopContainer",
		"first.duplicate",
		"second.duplicate",
	} {
		requireTSRelationshipSymbol(t, summary, "@fixture/app/src/use."+name)
		requireTSRelationshipCall(t, summary, "@fixture/app/src/use", name)
	}
	require.Equal(t, 0, countTSRelationshipCalls(summary, "@fixture/app/src/use", "duplicate"), "nested duplicate names must not collapse to a file-level target")
	require.Equal(t, 0, countTSRelationshipCalls(summary, "@fixture/app/src/use", "shadow"), "conservatively ambiguous lexical names must not fall back to a file-level target")
}

func TestTSIndexer_StaticClassMethodRelationships(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	writeTSRelationshipFixture(t, root, "src/errors.ts", `
export class IntegrationSetupError extends Error {
  static from(stage: string, error: unknown) {
    return new IntegrationSetupError(stage, error);
  }
}
`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `
import { IntegrationSetupError } from "./errors";
IntegrationSetupError.from("database-clone", new Error());
`)

	errors := indexTSRelationshipFixture(t, idx, root, "src/errors.ts")
	requireTSRelationshipSymbol(t, errors, "@fixture/app/src/errors.IntegrationSetupError.from")
	use := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	requireTSRelationshipCall(t, use, "@fixture/app/src/errors", "IntegrationSetupError.from")
}

func TestTSIndexer_LexicalCallableAliasKeepsOnlyKnownFallbacks(t *testing.T) {
	root, idx := newTSRelationshipFixture(t)
	writeTSRelationshipFixture(t, root, "src/auth.ts", `export function createAuth() {}`)
	writeTSRelationshipFixture(t, root, "src/use.ts", `
import { createAuth } from "./auth";

export function buildApp(options: { createAuth?: typeof createAuth }) {
  const authFactory = options.createAuth ?? createAuth;
  authFactory();
}
`)

	summary := indexTSRelationshipFixture(t, idx, root, "src/use.ts")
	require.Equal(t, 1, countTSRelationshipCalls(summary, "@fixture/app/src/auth", "createAuth"))
	require.Equal(t, 0, countTSRelationshipCalls(summary, "@fixture/app/src/use", "authFactory"), "the alias must not become a speculative local target")
}

func TestTSIndexer_WorkspaceValueRelationshipsPreferRuntimeExport(t *testing.T) {
	root := t.TempDir()
	writeTSRelationshipFixture(t, root, "packages/library/package.json", `{
		"name":"@fixture/library",
		"exports":{".":{"types":"./dist/index.d.ts","source":"./src/index.ts","import":"./dist/index.js","default":"./dist/index.js"}}
	}`)
	writeTSRelationshipFixture(t, root, "packages/library/dist/index.d.ts", "export declare function run(): void")
	writeTSRelationshipFixture(t, root, "packages/library/dist/index.js", "export function run() {}")
	runtimePath := writeTSRelationshipFixture(t, root, "packages/library/src/index.ts", "export function run() {}")
	writeTSRelationshipFixture(t, root, "apps/web/src/main.ts", `import { run } from "@fixture/library"; run();`)

	idx := NewTSIndexerWithRoot(root)
	summary := indexTSRelationshipFixture(t, idx, root, "apps/web/src/main.ts")
	require.Contains(t, summary.Imports, ImportEdge{Module: paths.ResolveSymlinks(runtimePath).String()})
	requireTSRelationshipCall(t, summary, "@fixture/library/src/index", "run")
}
