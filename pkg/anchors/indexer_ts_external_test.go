//go:build cgo

package codeanchor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTSIndexer_ExtractsExternalESMEvidence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "src", "external.ts")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"react":"^19.0.0"},"devDependencies":{"@testing-library/jest-dom":"^7.0.0"},"peerDependencies":{"react-dom":"^19.0.0"},"optionalDependencies":{"optional-lib":"^2.0.0"}}`), 0o644))
	content := []byte(`
import React, { Component as Widget, type ReactNode, useState as state } from "react";
import * as Client from "react-dom/client";
import "@testing-library/jest-dom";
import "optional-lib/register";
import "undeclared/register";
import nodePath from "path";
export function run(value: ReactNode) {
	let widget: Widget;
  React(); state(); React.createElement("div"); Client.createRoot(value); nodePath.basename("x");
  widget = new Widget(); return widget;
}`)
	require.NoError(t, os.WriteFile(path, content, 0o644))

	summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)

	requireExternalImport(t, summary.ExternalEvidence, "react", ExternalEvidenceESMDefault, "default", "React")
	requireExternalImport(t, summary.ExternalEvidence, "react", ExternalEvidenceESMType, "ReactNode", "ReactNode")
	requireExternalImport(t, summary.ExternalEvidence, "react", ExternalEvidenceESMNamed, "useState", "state")
	requireExternalImport(t, summary.ExternalEvidence, "react-dom/client", ExternalEvidenceESMNamespace, "", "Client")
	requireExternalImport(t, summary.ExternalEvidence, "@testing-library/jest-dom", ExternalEvidenceESMSideEffect, "", "")
	requireExternalVersion(t, summary.ExternalEvidence, "react", "package.json", "^19.0.0", ExternalVersionDependency)
	requireExternalVersion(t, summary.ExternalEvidence, "@testing-library/jest-dom", "package.json", "^7.0.0", ExternalVersionDevDependency)
	requireExternalVersion(t, summary.ExternalEvidence, "react-dom/client", "package.json", "^19.0.0", ExternalVersionPeerDependency)
	requireExternalVersion(t, summary.ExternalEvidence, "optional-lib/register", "package.json", "^2.0.0", ExternalVersionOptionalDependency)
	requireExternalVersionAbsent(t, summary.ExternalEvidence, "undeclared/register")
	requireExternalSymbolVersion(t, summary.ExternalEvidence, "react", "useState", "^19.0.0")
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "react", "useState", ExternalTargetSymbol)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "react", "default", ExternalTargetSymbol)
	requireExternalSymbolEvidence(t, summary.ExternalEvidence, "react", "useState", ExternalEvidenceESMNamed)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindTypeRef, "react", "ReactNode", ExternalTargetSymbol)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindTypeRef, "react", "Component", ExternalTargetSymbol)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "react", "Component", ExternalTargetSymbol)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "react-dom/client", "createRoot", ExternalTargetSymbol)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "node:path", "basename", ExternalTargetRuntimeBuiltin)
}

func TestTSIndexer_ExtractsEnumerableCommonJSEvidence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "external.cjs")
	content := []byte(`
const React = require("react");
const { useMemo: memo } = require("react");
const createClient = require("@acme/client").default;
const WidgetLibrary = require("@acme/widgets/react");
require("source-map-support/register");
function run() { memo(); React.createElement("div"); createClient(); return new WidgetLibrary.Widget(); }
`)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)

	requireExternalImport(t, summary.ExternalEvidence, "react", ExternalEvidenceCJSNamespace, "", "React")
	requireExternalImport(t, summary.ExternalEvidence, "react", ExternalEvidenceCJSNamed, "useMemo", "memo")
	requireExternalImport(t, summary.ExternalEvidence, "@acme/client", ExternalEvidenceCJSDefault, "default", "createClient")
	requireExternalImport(t, summary.ExternalEvidence, "source-map-support/register", ExternalEvidenceCJSSideEffect, "", "")
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "@acme/widgets/react", "Widget", ExternalTargetSymbol)
	requireExternalSymbolEvidence(t, summary.ExternalEvidence, "react", "useMemo", ExternalEvidenceCJSNamed)
}

func TestTSIndexer_NodeBuiltinImportsPreserveSyntaxWithRuntimeIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "builtins.ts")
	content := []byte(`
import pathDefault from "path";
import { readFile } from "node:fs/promises";
import * as os from "node:os";
import "node:test";
const crypto = require("crypto");
function run() { pathDefault.join("a", "b"); readFile("x"); os.platform(); crypto.randomUUID(); }
`)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)

	requireExternalImportTargetKind(t, summary.ExternalEvidence, "path", ExternalEvidenceESMDefault, ExternalTargetRuntimeBuiltin)
	requireExternalImportTargetKind(t, summary.ExternalEvidence, "node:fs/promises", ExternalEvidenceESMNamed, ExternalTargetRuntimeBuiltin)
	requireExternalImportTargetKind(t, summary.ExternalEvidence, "node:os", ExternalEvidenceESMNamespace, ExternalTargetRuntimeBuiltin)
	requireExternalImportTargetKind(t, summary.ExternalEvidence, "node:test", ExternalEvidenceESMSideEffect, ExternalTargetRuntimeBuiltin)
	requireExternalImportTargetKind(t, summary.ExternalEvidence, "crypto", ExternalEvidenceCJSNamespace, ExternalTargetRuntimeBuiltin)
	for _, got := range summary.ExternalEvidence.Symbols {
		if strings.HasPrefix(got.Target.Module, "node:") {
			require.Equal(t, ExternalTargetRuntimeBuiltin, got.Target.Kind)
		}
	}
}

func TestTSIndexer_TypeOnlyAndValueImportsShareCanonicalRawTarget(t *testing.T) {
	root := t.TempDir()
	indexer := NewTSIndexerWithRoot(root)
	typePath := filepath.Join(root, "type-use.ts")
	valuePath := filepath.Join(root, "value-use.ts")
	typeContent := []byte(`import type { Widget } from "@acme/widgets"; export function use(value: Widget) { return value; }`)
	valueContent := []byte(`import { Widget } from "@acme/widgets"; export function make() { return new Widget(); }`)
	require.NoError(t, os.WriteFile(typePath, typeContent, 0o644))
	require.NoError(t, os.WriteFile(valuePath, valueContent, 0o644))

	typeSummary, err := indexer.IndexFile(typeContent, codeRefFromRoot(t, root, typePath))
	require.NoError(t, err)
	valueSummary, err := indexer.IndexFile(valueContent, codeRefFromRoot(t, root, valuePath))
	require.NoError(t, err)
	typeInput := requireExternalRawSymbol(t, typeSummary.ExternalEvidence, "@acme/widgets", "Widget")
	valueInput := requireExternalRawSymbol(t, valueSummary.ExternalEvidence, "@acme/widgets", "Widget")
	require.Equal(t, typeInput.Raw, valueInput.Raw)
	require.Equal(t, typeInput.Target, valueInput.Target)
	require.Equal(t, ExternalTargetSymbol, typeInput.Target.Kind)
	require.Equal(t, ExternalEvidenceESMType, typeInput.Evidence.Kind)
	require.Equal(t, ExternalEvidenceESMNamed, valueInput.Evidence.Kind)
}

func TestTSIndexer_DefaultUseSharesImportTargetAndStaysSeparateFromNamedExport(t *testing.T) {
	root := t.TempDir()
	indexer := NewTSIndexerWithRoot(root)
	defaultPath := filepath.Join(root, "default.ts")
	namedPath := filepath.Join(root, "named.ts")
	defaultContent := []byte(`import React from "react"; export function a() { return React(); }`)
	namedContent := []byte(`import { React } from "react"; export function b() { return React(); }`)
	require.NoError(t, os.WriteFile(defaultPath, defaultContent, 0o644))
	require.NoError(t, os.WriteFile(namedPath, namedContent, 0o644))

	defaultSummary, err := indexer.IndexFile(defaultContent, codeRefFromRoot(t, root, defaultPath))
	require.NoError(t, err)
	namedSummary, err := indexer.IndexFile(namedContent, codeRefFromRoot(t, root, namedPath))
	require.NoError(t, err)
	defaultInput := requireExternalRawSymbol(t, defaultSummary.ExternalEvidence, "react", "default")
	namedInput := requireExternalRawSymbol(t, namedSummary.ExternalEvidence, "react", "React")
	defaultImport := requireExternalImportInput(t, defaultSummary.ExternalEvidence, "react", ExternalEvidenceESMDefault)
	require.Equal(t, defaultImport.Target, defaultInput.Target)
	require.NotEqual(t, defaultInput.Raw, namedInput.Raw)
	require.NotEqual(t, defaultInput.Target, namedInput.Target)
	require.Equal(t, "default", defaultInput.Target.SymbolPath)
	require.Equal(t, "React", namedInput.Target.SymbolPath)
	require.Equal(t, ExternalEvidenceESMDefault, defaultInput.Evidence.Kind)
	require.Equal(t, ExternalEvidenceESMNamed, namedInput.Evidence.Kind)
}

func TestTSIndexer_TypeAndDefaultImportsFromSameModuleDoNotCrossBind(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "markdown.tsx")
	content := []byte(`
import type { Components as MarkdownComponents } from "react-markdown";
import ReactMarkdown from "react-markdown";
const components: MarkdownComponents = {};
export function Pane() { return <ReactMarkdown components={components}>body</ReactMarkdown>; }
`)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)

	markdown := requireExternalRawSymbol(t, summary.ExternalEvidence, "react-markdown", "default")
	require.Equal(t, ExternalEvidenceESMDefault, markdown.Evidence.Kind)
	require.Equal(t, "default", markdown.Target.SymbolPath)
	require.Equal(t, ExternalTargetSymbol, markdown.Target.Kind)
	require.Equal(t, requireExternalImportInput(t, summary.ExternalEvidence, "react-markdown", ExternalEvidenceESMDefault).Target, markdown.Target)
	componentsInput := requireExternalRawSymbol(t, summary.ExternalEvidence, "react-markdown", "Components")
	require.Equal(t, ExternalEvidenceESMType, componentsInput.Evidence.Kind)
	require.Equal(t, "Components", componentsInput.Target.SymbolPath)

	handlesByRaw := make(map[RawSymbolTargetKey]string)
	for _, input := range summary.ExternalEvidence.Symbols {
		if prior, ok := handlesByRaw[input.Raw]; ok {
			require.Equal(t, prior, input.Target.Handle, input.Raw)
		}
		handlesByRaw[input.Raw] = input.Target.Handle
	}
}

func TestTSIndexer_ExtractsEachCommonJSDeclaratorOnce(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"alpha-lib":"^1.0.0","beta-lib":"^2.0.0"}}`), 0o644))
	path := filepath.Join(root, "multi.cjs")
	content := []byte(`const alpha = require("alpha-lib"), beta = require("beta-lib");
function run() { alpha(); beta(); }
`)
	require.NoError(t, os.WriteFile(path, content, 0o644))

	summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)
	requireExternalImport(t, summary.ExternalEvidence, "alpha-lib", ExternalEvidenceCJSNamespace, "", "alpha")
	requireExternalImport(t, summary.ExternalEvidence, "beta-lib", ExternalEvidenceCJSNamespace, "", "beta")
	requireExternalVersion(t, summary.ExternalEvidence, "alpha-lib", "package.json", "^1.0.0", ExternalVersionDependency)
	requireExternalVersion(t, summary.ExternalEvidence, "beta-lib", "package.json", "^2.0.0", ExternalVersionDependency)
	require.Len(t, summary.ExternalEvidence.Imports, 2)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "alpha-lib", "alpha", ExternalTargetSymbol)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "beta-lib", "beta", ExternalTargetSymbol)
}

func TestTSIndexer_ExternalEvidencePreservesLocalPrecedenceAndUnknowns(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "packages", "local", "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "app", "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"workspaces":["packages/*"]}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "packages", "local", "package.json"), []byte(`{"name":"@fixture/local","exports":"./src/index.ts"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "packages", "local", "src", "index.ts"), []byte(`export function localCall(){}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app", "tsconfig.json"), []byte(`{"compilerOptions":{"paths":{"@missing/*":["missing/*"]}}}`), 0o644))
	path := filepath.Join(root, "app", "src", "use.ts")
	content := []byte(`import { localCall } from "@fixture/local"; import { nope } from "@missing/value"; function run(x:any){ localCall(); nope(); x.render(); require(x); }`)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)
	require.Empty(t, summary.ExternalEvidence.Symbols)
	require.Empty(t, summary.ExternalEvidence.Imports)
}

func TestTSIndexer_ExtractsCuratedRuntimeGlobalEvidence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "globals.js")
	content := []byte(`function run(url) { console.log(JSON.stringify(url)); fetch(url); return new URL(url); }`)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindMemberRef, "javascript", "console.log", ExternalTargetRuntimeGlobal)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "javascript", "fetch", ExternalTargetRuntimeGlobal)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "javascript", "URL", ExternalTargetRuntimeGlobal)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindMemberRef, "javascript", "JSON.stringify", ExternalTargetRuntimeGlobal)
}

func TestTSIndexer_ShadowedRuntimeGlobalRemainsUnknown(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shadowed.js")
	content := []byte(`function run(URL) { return new URL("local"); }`)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)
	for _, got := range summary.ExternalEvidence.Symbols {
		require.NotEqual(t, "URL", got.Target.SymbolPath)
	}
}

func TestTSIndexer_ParameterDefaultRuntimeGlobalIsNotShadowed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "default.ts")
	content := []byte(`function run(response = fetch("/api"), fetch = localFetch) { return response; }`)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	summary, err := NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)

	// The call expression in the first default initializer is a use, not a
	// binding. The later parameter named fetch is an actual binding, however,
	// so keep the two cases separate in their own functions.
	for _, got := range summary.ExternalEvidence.Symbols {
		require.NotEqual(t, "fetch", got.Target.SymbolPath)
	}

	content = []byte(`function run(response = fetch("/api")) { return response; }`)
	summary, err = NewTSIndexerWithRoot(root).IndexFile(content, codeRefFromRoot(t, root, path))
	require.NoError(t, err)
	requireExternalSymbol(t, summary.ExternalEvidence, RefKindCalls, "javascript", "fetch", ExternalTargetRuntimeGlobal)
}

func requireExternalImport(t *testing.T, batch ExternalEvidenceBatch, module string, kind ExternalEvidenceKind, imported, local string) {
	t.Helper()
	for _, got := range batch.Imports {
		if got.Module == module && got.Evidence.Kind == kind && got.Evidence.ImportedName == imported && got.Evidence.LocalName == local {
			return
		}
	}
	require.Failf(t, "external import evidence missing", "%s %s %s %s: %+v", module, kind, imported, local, batch.Imports)
}

func requireExternalImportTargetKind(t *testing.T, batch ExternalEvidenceBatch, module string, evidenceKind ExternalEvidenceKind, targetKind ExternalTargetKind) {
	t.Helper()
	wantModule := canonicalNPMModule(module)
	for _, got := range batch.Imports {
		if got.Target.Module == wantModule && got.Evidence.Kind == evidenceKind {
			require.Equal(t, targetKind, got.Target.Kind)
			return
		}
	}
	require.Failf(t, "external import target missing", "%s %s: %+v", module, evidenceKind, batch.Imports)
}

func requireExternalImportInput(t *testing.T, batch ExternalEvidenceBatch, module string, evidenceKind ExternalEvidenceKind) ExternalImportEvidenceInput {
	t.Helper()
	for _, got := range batch.Imports {
		if got.Target.Module == canonicalNPMModule(module) && got.Evidence.Kind == evidenceKind {
			return got
		}
	}
	require.Failf(t, "external import missing", "%s %s: %+v", module, evidenceKind, batch.Imports)
	return ExternalImportEvidenceInput{}
}

func requireExternalRawSymbol(t *testing.T, batch ExternalEvidenceBatch, module, symbol string) ExternalSymbolEvidenceInput {
	t.Helper()
	for _, got := range batch.Symbols {
		if got.Raw.DstPkg == module && got.Raw.DstName == symbol {
			return got
		}
	}
	require.Failf(t, "external raw symbol missing", "%s %s: %+v", module, symbol, batch.Symbols)
	return ExternalSymbolEvidenceInput{}
}

func requireExternalSymbol(t *testing.T, batch ExternalEvidenceBatch, refKind RefKind, module, symbol string, targetKind ExternalTargetKind) {
	t.Helper()
	for _, got := range batch.Symbols {
		if got.RefKind == refKind && got.Target.Module == module && got.Target.SymbolPath == symbol && got.Target.Kind == targetKind {
			return
		}
	}
	require.Failf(t, "external symbol evidence missing", "%s %s %s %s: %+v", refKind, module, symbol, targetKind, batch.Symbols)
}

func requireExternalSymbolEvidence(t *testing.T, batch ExternalEvidenceBatch, module, symbol string, kind ExternalEvidenceKind) {
	t.Helper()
	for _, got := range batch.Symbols {
		if got.Target.Module == module && got.Target.SymbolPath == symbol && got.Evidence.Kind == kind {
			return
		}
	}
	require.Failf(t, "external symbol evidence kind missing", "%s %s %s: %+v", module, symbol, kind, batch.Symbols)
}

func requireExternalVersion(t *testing.T, batch ExternalEvidenceBatch, module, manifest, declaredRange string, scope ExternalVersionScope) {
	t.Helper()
	for _, got := range batch.Imports {
		if got.Module == module && got.Evidence.Version != nil {
			require.Equal(t, manifest, got.Evidence.Version.ManifestPath)
			require.Equal(t, declaredRange, got.Evidence.Version.DeclaredRange)
			require.Equal(t, scope, got.Evidence.Version.Scope)
			return
		}
	}
	require.Failf(t, "external version provenance missing", "%s: %+v", module, batch.Imports)
}

func requireExternalVersionAbsent(t *testing.T, batch ExternalEvidenceBatch, module string) {
	t.Helper()
	for _, got := range batch.Imports {
		if got.Module == module {
			require.Nil(t, got.Evidence.Version)
			return
		}
	}
	require.Failf(t, "external import missing", "%s: %+v", module, batch.Imports)
}

func requireExternalSymbolVersion(t *testing.T, batch ExternalEvidenceBatch, module, symbol, declaredRange string) {
	t.Helper()
	for _, got := range batch.Symbols {
		if got.Target.Module == module && got.Target.SymbolPath == symbol {
			require.NotNil(t, got.Evidence.Version)
			require.Equal(t, declaredRange, got.Evidence.Version.DeclaredRange)
			return
		}
	}
	require.Failf(t, "external symbol version missing", "%s %s: %+v", module, symbol, batch.Symbols)
}
