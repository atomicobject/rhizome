//go:build cgo

package codeanchor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestTSModuleResolver_NodeNextSourceSubstitutions(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "src/service.ts", "export const service = 1")
	writeTSResolverFixture(t, root, "src/view.tsx", "export const View = () => null")
	writeTSResolverFixture(t, root, "src/esm.mts", "export const esm = 1")
	writeTSResolverFixture(t, root, "src/common.cts", "export const common = 1")
	writeTSResolverFixture(t, root, "src/plain.js", "export const plain = 1")

	idx := NewTSIndexerWithRoot(root)
	resolver := idx.resolver
	tests := map[string]string{
		"./service.js": "src/service.ts",
		"./view.jsx":   "src/view.tsx",
		"./esm.mjs":    "src/esm.mts",
		"./common.cjs": "src/common.cts",
		"./plain.js":   "src/plain.js",
	}
	for specifier, want := range tests {
		t.Run(specifier, func(t *testing.T) {
			wantPath := canonicalTSResolverPath(t, filepath.Join(root, want))
			got, ok := resolver.resolve(filepath.Join(root, "src"), specifier)
			if !ok || got != wantPath {
				t.Fatalf("resolve(%q) = %q, %v; want %q, true", specifier, got, ok, wantPath)
			}
		})
	}
}

func TestTSModuleResolver_ExtensionlessAndIndexCoverAllFormats(t *testing.T) {
	root := t.TempDir()
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"} {
		stem := "module_" + strings.TrimPrefix(ext, ".")
		name := stem + ext
		writeTSResolverFixture(t, root, "src/"+name, "export const value = 1")
		writeTSResolverFixture(t, root, "dirs/"+stem+"/index"+ext, "export const value = 1")

		resolver := newTSModuleResolver(root)
		if got, ok := resolver.resolve(filepath.Join(root, "src"), "./"+stem); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "src", name)) {
			t.Fatalf("extensionless %s = %q, %v", ext, got, ok)
		}
		if got, ok := resolver.resolve(filepath.Join(root, "dirs"), "./"+stem); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "dirs", stem, "index"+ext)) {
			t.Fatalf("index %s = %q, %v", ext, got, ok)
		}
	}
}

func TestTSModuleResolver_WorkspaceExportsAndPackageImports(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "package.json", `{"workspaces":["packages/*"]}`)
	writeTSResolverFixture(t, root, "packages/db/package.json", `{
		"name":"@acme/db",
		"exports":{".":{"types":"./src/index.ts","default":"./dist/index.js"},"./models/*":"./src/models/*.ts","./array":["./missing.ts","./src/index.ts"]},
		"imports":{"#internal/*":["./missing/*.ts","./src/internal/*.ts"]}
	}`)
	writeTSResolverFixture(t, root, "packages/db/src/index.ts", "export const createDb = () => null")
	writeTSResolverFixture(t, root, "packages/db/src/models/user.ts", "export interface User {}")
	writeTSResolverFixture(t, root, "packages/db/src/internal/clock.ts", "export const now = Date.now")
	writeTSResolverFixture(t, root, "apps/api/src/main.ts", "export const main = 1")

	resolver := newTSModuleResolver(root)
	tests := map[string]string{
		"@acme/db":             "packages/db/src/index.ts",
		"@acme/db/models/user": "packages/db/src/models/user.ts",
		"@acme/db/array":       "packages/db/src/index.ts",
	}
	for specifier, want := range tests {
		if got, ok := resolver.resolve(filepath.Join(root, "apps/api/src"), specifier); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, want)) {
			t.Fatalf("resolve(%q) = %q, %v; want %q", specifier, got, ok, filepath.Join(root, want))
		}
	}
	if got, ok := resolver.resolve(filepath.Join(root, "packages/db/src"), "#internal/clock"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "packages/db/src/internal/clock.ts")) {
		t.Fatalf("package import = %q, %v", got, ok)
	}
}

func TestTSModuleResolver_TSConfigPathsExtendsAndJSONC(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "tsconfig.base.json", `{
		// shared aliases
		"compilerOptions": {"baseUrl":".","paths":{"@shared/*":["packages/shared/src/*"],},},
	}`)
	writeTSResolverFixture(t, root, "apps/shared/tsconfig.json", `{"extends":"../../tsconfig.base.json"}`)
	writeTSResolverFixture(t, root, "apps/web/tsconfig.json", `{"extends":"../../tsconfig.base.json","compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/*"]}}}`)
	writeTSResolverFixture(t, root, "packages/shared/src/log.ts", "export const log = () => null")
	writeTSResolverFixture(t, root, "apps/web/src/page.ts", "export const page = 1")
	writeTSResolverFixture(t, root, "apps/shared/src/main.ts", "export const main = 1")

	resolver := newTSModuleResolver(root)
	if got, ok := resolver.resolve(filepath.Join(root, "apps/shared/src"), "@shared/log"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "packages/shared/src/log.ts")) {
		t.Fatalf("inherited path = %q, %v", got, ok)
	}
	if got, ok := resolver.resolve(filepath.Join(root, "apps/web/src"), "@app/page"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "apps/web/src/page.ts")) {
		t.Fatalf("local path = %q, %v", got, ok)
	}
	if got, ok := resolver.resolve(filepath.Join(root, "apps/web/src"), "@shared/log"); ok {
		t.Fatalf("child paths must replace inherited paths, got %q", got)
	}
}

func TestTSModuleResolver_RejectsPathsOutsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	writeTSResolverFixture(t, root, "package.json", `{}`)
	writeTSResolverFixture(t, parent, "outside.ts", "export const nope = 1")
	resolver := newTSModuleResolver(root)
	if got, ok := resolver.resolve(root, "../outside.ts"); ok {
		t.Fatalf("escaped root: %q", got)
	}
}

func TestTSModuleResolver_RejectsEscapingPackageMapsAndDuplicatePackages(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	writeTSResolverFixture(t, parent, "outside.ts", "export const nope = 1")
	writeTSResolverFixture(t, root, "packages/escape/package.json", `{"name":"escape","exports":"../../../outside.ts"}`)
	writeTSResolverFixture(t, root, "packages/a/package.json", `{"name":"duplicate","exports":"../../../outside.ts"}`)
	writeTSResolverFixture(t, root, "packages/b/package.json", `{"name":"duplicate","exports":"./index.ts"}`)
	writeTSResolverFixture(t, root, "packages/b/index.ts", "export const value = 1")
	writeTSResolverFixture(t, root, "packages/root-only/package.json", `{"name":"root-only","exports":"./index.ts"}`)
	writeTSResolverFixture(t, root, "packages/root-only/index.ts", "export const value = 1")
	writeTSResolverFixture(t, root, "packages/root-only/src/private.ts", "export const secret = 1")
	writeTSResolverFixture(t, root, "packages/custom/package.json", `{"name":"custom-only","exports":{".":{"browser":"./browser.ts"}}}`)
	writeTSResolverFixture(t, root, "packages/custom/browser.ts", "export const browser = 1")
	writeTSResolverFixture(t, root, "src/main.ts", "export const main = 1")

	resolver := newTSModuleResolver(root)
	if got, ok := resolver.resolve(filepath.Join(root, "src"), "escape"); ok {
		t.Fatalf("escaping package map resolved to %q", got)
	}
	if got, ok := resolver.resolve(filepath.Join(root, "src"), "duplicate"); ok {
		t.Fatalf("duplicate package name resolved nondeterministically to %q", got)
	}
	if got, ok := resolver.resolve(filepath.Join(root, "src"), "root-only/private"); ok {
		t.Fatalf("unexported package subpath resolved to %q", got)
	}
	if got, ok := resolver.resolve(filepath.Join(root, "src"), "custom-only"); ok {
		t.Fatalf("unsupported package condition resolved speculatively to %q", got)
	}
}

func TestTSModuleResolver_WorkspaceScanSkipsGeneratedPackageCopies(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "packages/library/package.json", `{"name":"@fixture/library","exports":"./src/index.ts"}`)
	writeTSResolverFixture(t, root, "packages/library/src/index.ts", "export const source = 1")
	for _, dir := range []string{"dist", "build", "out", "coverage", "node_modules"} {
		writeTSResolverFixture(t, root, filepath.Join("packages/library", dir, "package.json"), `{"name":"@fixture/library","exports":"./index.js"}`)
		writeTSResolverFixture(t, root, filepath.Join("packages/library", dir, "index.js"), "export const generated = 1")
	}
	writeTSResolverFixture(t, root, "src/main.ts", "export const main = 1")

	resolver := newTSModuleResolver(root)
	want := canonicalTSResolverPath(t, filepath.Join(root, "packages/library/src/index.ts"))
	if got, ok := resolver.resolve(filepath.Join(root, "src"), "@fixture/library"); !ok || got != want {
		t.Fatalf("workspace package with generated copies = %q, %v; want %q", got, ok, want)
	}
}

func TestTSModuleResolver_CacheRevalidatesFilesystem(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "src/main.ts", "export const main = 1")
	resolver := newTSModuleResolver(root)
	dir := filepath.Join(root, "src")
	if got, ok := resolver.resolve(dir, "./late.js"); ok {
		t.Fatalf("missing module unexpectedly resolved to %q", got)
	}
	writeTSResolverFixture(t, root, "src/late.ts", "export const late = 1")
	if got, ok := resolver.resolve(dir, "./late.js"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "src/late.ts")) {
		t.Fatalf("module created after miss = %q, %v", got, ok)
	}
	if err := os.Rename(filepath.Join(root, "src/late.ts"), filepath.Join(root, "src/late.ts.deleted")); err != nil {
		t.Fatal(err)
	}
	if got, ok := resolver.resolve(dir, "./late.js"); ok {
		t.Fatalf("deleted cached module resolved to %q", got)
	}
}

func TestTSModuleResolver_AbsoluteSpecifierMustStayInsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	writeTSResolverFixture(t, root, "src/value.ts", "export const value = 1")
	writeTSResolverFixture(t, parent, "outside.ts", "export const outside = 1")
	resolver := newTSModuleResolver(root)
	want := canonicalTSResolverPath(t, filepath.Join(root, "src/value.ts"))
	if got, ok := resolver.resolve(filepath.Join(root, "src"), want); !ok || got != want {
		t.Fatalf("in-root absolute = %q, %v", got, ok)
	}
	if got, ok := resolver.resolve(filepath.Join(root, "src"), filepath.Join(parent, "outside.ts")); ok {
		t.Fatalf("out-of-root absolute resolved to %q", got)
	}
}

func TestCleanTSConfigJSON_PreservesCommaSequencesInsideStrings(t *testing.T) {
	cleaned := cleanTSConfigJSON([]byte(`{"compilerOptions":{"baseUrl":"value,}",},}`))
	var decoded map[string]any
	if err := json.Unmarshal(cleaned, &decoded); err != nil {
		t.Fatalf("cleaned JSON is invalid: %v: %s", err, cleaned)
	}
	options := decoded["compilerOptions"].(map[string]any)
	if got := options["baseUrl"]; got != "value,}" {
		t.Fatalf("string content changed: %q", got)
	}
}

func TestTSModuleResolver_ExtendsDottedBasenameWithOmittedJSON(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "config/base.config.json", `{"compilerOptions":{"baseUrl":"..","paths":{"@src/*":["src/*"]}}}`)
	writeTSResolverFixture(t, root, "tsconfig.json", `{"extends":"./config/base.config"}`)
	writeTSResolverFixture(t, root, "src/value.ts", "export const value = 1")
	resolver := newTSModuleResolver(root)
	if got, ok := resolver.resolve(filepath.Join(root, "src"), "@src/value"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "src/value.ts")) {
		t.Fatalf("dotted config extends = %q, %v", got, ok)
	}
}

func TestTSModuleResolver_ConfigExtendsCycleAndConcurrentCache(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "tsconfig.json", `{"extends":"./config/base.json"}`)
	writeTSResolverFixture(t, root, "config/base.json", `{"extends":"../tsconfig.json"}`)
	writeTSResolverFixture(t, root, "src/value.ts", "export const value = 1")
	resolver := newTSModuleResolver(root)
	want := canonicalTSResolverPath(t, filepath.Join(root, "src/value.ts"))

	var wg sync.WaitGroup
	errs := make(chan string, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, ok := resolver.resolve(filepath.Join(root, "src"), "./value.js")
			if !ok || got != want {
				errs <- got
			}
		}()
	}
	wg.Wait()
	close(errs)
	for got := range errs {
		t.Errorf("concurrent resolution failed: %q", got)
	}
}

func TestTSIndexer_WorkspacePackageImportPreservesSourceFQN(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "package.json", `{"workspaces":["apps/*","packages/*"]}`)
	writeTSResolverFixture(t, root, "packages/db/package.json", `{"name":"@acme/db","exports":{".":"./src/index.ts"}}`)
	writeTSResolverFixture(t, root, "packages/db/src/index.ts", "export function createDb() { return {}; }")
	appPath := filepath.Join(root, "apps/api/src/main.ts")
	appContent := []byte(`import { createDb } from "@acme/db"; export const db = createDb();`)
	writeTSResolverFixture(t, root, "apps/api/src/main.ts", string(appContent))

	summary, err := NewTSIndexerWithRoot(root).IndexFile(appContent, codeRefFromRoot(t, root, appPath))
	if err != nil {
		t.Fatal(err)
	}
	want := SymbolRef{Lang: LangTS, Pkg: "@acme/db/src/index", Name: "createDb"}
	for _, call := range summary.Calls {
		if call.CalleeSymbol == want {
			return
		}
	}
	t.Fatalf("workspace call target missing: want %+v, calls %+v", want, summary.Calls)
}

func TestTSModuleResolver_ReevaluatesSourcePrecedenceAndDeletion(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "src/module.js", "export const format = 'js'")
	resolver := newTSModuleResolver(root)
	dir := filepath.Join(root, "src")
	jsPath := canonicalTSResolverPath(t, filepath.Join(root, "src/module.js"))
	if got, ok := resolver.resolve(dir, "./module.js"); !ok || got != jsPath {
		t.Fatalf("initial JS resolution = %q, %v", got, ok)
	}
	writeTSResolverFixture(t, root, "src/module.ts", "export const format = 'ts'")
	tsPath := canonicalTSResolverPath(t, filepath.Join(root, "src/module.ts"))
	if got, ok := resolver.resolve(dir, "./module.js"); !ok || got != tsPath {
		t.Fatalf("higher-precedence TS creation = %q, %v", got, ok)
	}
	if err := os.Rename(filepath.Join(root, "src/module.ts"), filepath.Join(root, "src/module.ts.deleted")); err != nil {
		t.Fatal(err)
	}
	if got, ok := resolver.resolve(dir, "./module.js"); !ok || got != jsPath {
		t.Fatalf("fallback after TS deletion = %q, %v", got, ok)
	}
}

func TestTSModuleResolver_RevalidatesConfigExtendsChain(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "tsconfig.json", `{"extends":"./config/base.json"}`)
	writeTSResolverFixture(t, root, "config/base.json", `{"compilerOptions":{"baseUrl":"..","paths":{"@value":["src/a.ts"]}}}`)
	writeTSResolverFixture(t, root, "src/a.ts", "export const value = 'a'")
	writeTSResolverFixture(t, root, "src/b.ts", "export const value = 'b'")
	resolver := newTSModuleResolver(root)
	dir := filepath.Join(root, "src")
	configPath := filepath.Join(root, "config/base.json")
	before, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := resolver.resolve(dir, "@value"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "src/a.ts")) {
		t.Fatalf("initial extended alias = %q, %v", got, ok)
	}
	writeTSResolverFixture(t, root, "config/base.json", `{"compilerOptions":{"baseUrl":"..","paths":{"@value":["src/b.ts"]}}}`)
	// Same-size edits need a distinct stamp to exercise dependency revalidation.
	advanced := before.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(configPath, advanced, advanced); err != nil {
		t.Fatal(err)
	}
	if got, ok := resolver.resolve(dir, "@value"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "src/b.ts")) {
		t.Fatalf("edited extended alias = %q, %v", got, ok)
	}
	writeTSResolverFixture(t, root, "config/other.json", `{"compilerOptions":{"baseUrl":".."}}`)
	writeTSResolverFixture(t, root, "tsconfig.json", `{"extends":"./config/other.json"}`)
	if got, ok := resolver.resolve(dir, "src/a"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "src/a.ts")) {
		t.Fatalf("edited extends/baseUrl = %q, %v", got, ok)
	}
}

func TestTSModuleResolver_ArbitraryJSONInvalidationRefreshesConfigWithoutWorkspaceRescan(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "tsconfig.json", `{"extends":"./config/compiler-options.json"}`)
	writeTSResolverFixture(t, root, "config/compiler-options.json", `{"compilerOptions":{"baseUrl":"..","paths":{"@value":["src/a.ts"]}}}`)
	writeTSResolverFixture(t, root, "src/a.ts", "export const value = 'a'")
	writeTSResolverFixture(t, root, "src/b.ts", "export const value = 'b'")
	idx := NewTSIndexerWithRoot(root)
	resolver := idx.resolver
	dir := filepath.Join(root, "src")
	if got, ok := resolver.resolve(dir, "@value"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "src/a.ts")) {
		t.Fatalf("initial arbitrary JSON extends = %q, %v", got, ok)
	}
	_, _ = resolver.resolve(dir, "external-dependency")
	resolver.packageMu.RLock()
	beforeScans := resolver.packageScanCount
	resolver.packageMu.RUnlock()

	configPath := filepath.Join(root, "config/compiler-options.json")
	before, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeTSResolverFixture(t, root, "config/compiler-options.json", `{"compilerOptions":{"baseUrl":"..","paths":{"@value":["src/b.ts"]}}}`)
	// Watcher invalidation must also refresh edits with an unchanged stat stamp.
	if err := os.Chtimes(configPath, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	watcher := &Watcher{service: NewService(nil, idx)}
	watcher.handlePath(context.Background(), configPath)
	if got, ok := resolver.resolve(dir, "@value"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "src/b.ts")) {
		t.Fatalf("updated arbitrary JSON extends = %q, %v", got, ok)
	}
	_, _ = resolver.resolve(dir, "another-external-dependency")
	resolver.packageMu.RLock()
	afterScans := resolver.packageScanCount
	resolver.packageMu.RUnlock()
	if afterScans != beforeScans {
		t.Fatalf("arbitrary JSON invalidation rescanned workspace packages: %d -> %d", beforeScans, afterScans)
	}
}

func TestTSModuleResolver_ConfigDependencyFastPathRefreshesMetadataAfterEquivalentRewrite(t *testing.T) {
	root := t.TempDir()
	content := `{"compilerOptions":{"baseUrl":"."}}`
	writeTSResolverFixture(t, root, "tsconfig.json", content)
	writeTSResolverFixture(t, root, "src/value.ts", "export const value = 1")
	resolver := newTSModuleResolver(root)
	configPath := filepath.Join(resolver.root, "tsconfig.json")
	if _, ok := resolver.resolve(filepath.Join(root, "src"), "src/value"); !ok {
		t.Fatal("initial config resolution failed")
	}
	resolver.configMu.Lock()
	before := resolver.configs[configPath].dependencies[configPath]
	resolver.configMu.Unlock()
	if err := os.Chmod(configPath, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolver.resolve(filepath.Join(root, "src"), "src/value"); !ok {
		t.Fatal("unchanged config cache hit tried to reread an unreadable dependency")
	}
	if err := os.Chmod(configPath, 0o644); err != nil {
		t.Fatal(err)
	}
	advanced := before.modTime.Add(2 * time.Second)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(configPath, advanced, advanced); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolver.resolve(filepath.Join(root, "src"), "src/value"); !ok {
		t.Fatal("resolution after equivalent config rewrite failed")
	}
	resolver.configMu.Lock()
	after := resolver.configs[configPath].dependencies[configPath]
	resolver.configMu.Unlock()
	if !after.modTime.Equal(advanced) {
		t.Fatalf("dependency metadata was not refreshed: got %v, want %v", after.modTime, advanced)
	}
}

func TestTSModuleResolver_JSConfigExactPathStandaloneBaseURLAndCycleAlias(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "apps/js/jsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"appAlias":["src/app.js"]}}}`)
	writeTSResolverFixture(t, root, "apps/js/src/app.js", "export const app = 1")
	writeTSResolverFixture(t, root, "apps/base/tsconfig.json", `{"compilerOptions":{"baseUrl":"."}}`)
	writeTSResolverFixture(t, root, "apps/base/lib/value.ts", "export const value = 1")
	writeTSResolverFixture(t, root, "apps/cycle/tsconfig.json", `{"extends":"./config/a.json"}`)
	writeTSResolverFixture(t, root, "apps/cycle/config/a.json", `{"extends":"../tsconfig.json","compilerOptions":{"baseUrl":"..","paths":{"@cycle":["src/value.ts"]}}}`)
	writeTSResolverFixture(t, root, "apps/cycle/src/value.ts", "export const cycle = 1")
	resolver := newTSModuleResolver(root)
	if got, ok := resolver.resolve(filepath.Join(root, "apps/js/src"), "appAlias"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "apps/js/src/app.js")) {
		t.Fatalf("jsconfig exact path = %q, %v", got, ok)
	}
	if got, ok := resolver.resolve(filepath.Join(root, "apps/base/lib"), "lib/value"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "apps/base/lib/value.ts")) {
		t.Fatalf("standalone baseUrl = %q, %v", got, ok)
	}
	if got, ok := resolver.resolve(filepath.Join(root, "apps/cycle/src"), "@cycle"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "apps/cycle/src/value.ts")) {
		t.Fatalf("alias through extends cycle = %q, %v", got, ok)
	}
}

func TestTSModuleResolver_RevalidatesPackageMetadataAndDiscoversNewPackage(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "src/main.ts", "export const main = 1")
	writeTSResolverFixture(t, root, "packages/db/package.json", `{"name":"db","exports":"./src/a.ts","imports":{"#value":"./src/a.ts"}}`)
	writeTSResolverFixture(t, root, "packages/db/src/a.ts", "export const value = 'a'")
	writeTSResolverFixture(t, root, "packages/db/src/b.ts", "export const value = 'b'")
	idx := NewTSIndexerWithRoot(root)
	resolver := idx.resolver
	rootDir := filepath.Join(root, "src")
	packageDir := filepath.Join(root, "packages/db/src")
	if got, ok := resolver.resolve(rootDir, "db"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "packages/db/src/a.ts")) {
		t.Fatalf("initial package export = %q, %v", got, ok)
	}
	writeTSResolverFixture(t, root, "packages/db/package.json", `{"name":"db","exports":"./src/b.ts","imports":{"#value":"./src/b.ts"}}`)
	if got, ok := resolver.resolve(rootDir, "db"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "packages/db/src/b.ts")) {
		t.Fatalf("edited package export = %q, %v", got, ok)
	}
	if got, ok := resolver.resolve(packageDir, "#value"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "packages/db/src/b.ts")) {
		t.Fatalf("edited package import = %q, %v", got, ok)
	}
	if got, ok := resolver.resolve(rootDir, "new-package"); ok {
		t.Fatalf("new package resolved before creation: %q", got)
	}
	writeTSResolverFixture(t, root, "packages/new/package.json", `{"name":"new-package","exports":"./index.ts"}`)
	writeTSResolverFixture(t, root, "packages/new/index.ts", "export const value = 1")
	resolver.packageMu.RLock()
	beforeInvalidation := resolver.packageScanCount
	resolver.packageMu.RUnlock()
	watcher := &Watcher{service: NewService(nil, idx)}
	watcher.handlePath(context.Background(), filepath.Join(root, "packages/new/package.json"))
	if got, ok := resolver.resolve(rootDir, "new-package"); !ok || got != canonicalTSResolverPath(t, filepath.Join(root, "packages/new/index.ts")) {
		t.Fatalf("new package after metadata invalidation = %q, %v", got, ok)
	}
	resolver.packageMu.RLock()
	afterInvalidation := resolver.packageScanCount
	resolver.packageMu.RUnlock()
	if afterInvalidation != beforeInvalidation+1 {
		t.Fatalf("metadata invalidation scans = %d -> %d; want exactly one refresh", beforeInvalidation, afterInvalidation)
	}
	if _, ok := resolver.resolve(rootDir, "new-package"); !ok {
		t.Fatal("new package disappeared after refreshed registry")
	}
	resolver.packageMu.RLock()
	afterSecondResolution := resolver.packageScanCount
	resolver.packageMu.RUnlock()
	if afterSecondResolution != afterInvalidation {
		t.Fatalf("clean registry rescanned again: %d -> %d", afterInvalidation, afterSecondResolution)
	}
}

func TestTSModuleResolver_BoundsExternalMissRescans(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "src/main.ts", "export const main = 1")
	resolver := newTSModuleResolver(root)
	dir := filepath.Join(root, "src")
	for round := range 5 {
		for i := range 100 {
			_, _ = resolver.resolve(dir, fmt.Sprintf("external-%d-%d", round, i))
		}
	}
	resolver.packageMu.RLock()
	scans := resolver.packageScanCount
	resolver.packageMu.RUnlock()
	if scans != 1 {
		t.Fatalf("500 external misses caused %d workspace scans; want 1 until lifecycle invalidation", scans)
	}
}

func TestTSModuleResolver_DeterministicOverlappingWildcardPrecedence(t *testing.T) {
	root := t.TempDir()
	writeTSResolverFixture(t, root, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"alias/*foo":["src/suffix.ts"],"alias/foo*":["src/prefix.ts"]}}}`)
	writeTSResolverFixture(t, root, "src/suffix.ts", "export const value = 1")
	writeTSResolverFixture(t, root, "src/prefix.ts", "export const value = 1")
	writeTSResolverFixture(t, root, "src/main.ts", "export const main = 1")
	writeTSResolverFixture(t, root, "packages/pattern/package.json", `{"name":"pattern-package","exports":{"./*foo":"./src/suffix.ts","./foo*":"./src/prefix.ts"}}`)
	writeTSResolverFixture(t, root, "packages/pattern/src/suffix.ts", "export const value = 1")
	writeTSResolverFixture(t, root, "packages/pattern/src/prefix.ts", "export const value = 1")
	resolver := newTSModuleResolver(root)
	want := canonicalTSResolverPath(t, filepath.Join(root, "src/prefix.ts"))
	wantPackage := canonicalTSResolverPath(t, filepath.Join(root, "packages/pattern/src/prefix.ts"))
	for range 100 {
		if got, ok := resolver.resolve(filepath.Join(root, "src"), "alias/foo"); !ok || got != want {
			t.Fatalf("overlapping wildcard precedence = %q, %v; want %q", got, ok, want)
		}
		if got, ok := resolver.resolve(filepath.Join(root, "src"), "pattern-package/foo"); !ok || got != wantPackage {
			t.Fatalf("overlapping package wildcard precedence = %q, %v; want %q", got, ok, wantPackage)
		}
	}
}

func TestTSModuleResolver_ExtensionPriorityIsDeterministic(t *testing.T) {
	root := t.TempDir()
	for _, ext := range codefile.TypeScriptJavaScriptExtensions() {
		writeTSResolverFixture(t, root, "src/collision"+ext, "export const value = 1")
		writeTSResolverFixture(t, root, "src/directory/index"+ext, "export const value = 1")
	}
	resolver := newTSModuleResolver(root)
	dir := filepath.Join(root, "src")
	wantFile := canonicalTSResolverPath(t, filepath.Join(root, "src/collision.ts"))
	wantIndex := canonicalTSResolverPath(t, filepath.Join(root, "src/directory/index.ts"))
	for range 100 {
		if got, ok := resolver.resolve(dir, "./collision"); !ok || got != wantFile {
			t.Fatalf("extension priority = %q, %v", got, ok)
		}
		if got, ok := resolver.resolve(dir, "./directory"); !ok || got != wantIndex {
			t.Fatalf("index priority = %q, %v", got, ok)
		}
	}
}

func TestTSIndexer_TailFallbackRejectsOutsideRootAndStalePaths(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	writeTSResolverFixture(t, root, "src/main.ts", "export const main = 1")
	writeTSResolverFixture(t, parent, "outside/components/Button.ts", "export const Button = 1")
	tail := NewPathTailIndex(5)
	tail.Add(filepath.Join(parent, "outside/components/Button.ts"))
	idx := NewTSIndexerWithRootAndTailIndex(root, tail)
	if got, ok := idx.resolveByTailIndex(filepath.Join(root, "src"), "@/components/Button"); ok {
		t.Fatalf("outside-root tail resolved to %q", got)
	}
	writeTSResolverFixture(t, root, "components/Local.ts", "export const Local = 1")
	local := filepath.Join(root, "components/Local.ts")
	tail.Add(local)
	if err := os.Rename(local, local+".deleted"); err != nil {
		t.Fatal(err)
	}
	if got, ok := idx.resolveByTailIndex(filepath.Join(root, "src"), "@/components/Local"); ok {
		t.Fatalf("stale tail resolved to %q", got)
	}
}

func writeTSResolverFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func canonicalTSResolverPath(t *testing.T, path string) string {
	t.Helper()
	return paths.ResolveSymlinks(path).String()
}
