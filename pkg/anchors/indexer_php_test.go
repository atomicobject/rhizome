//go:build cgo
// +build cgo

package codeanchor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func phpCodeRef(t *testing.T, root, absPath string) paths.CodePathRef {
	t.Helper()
	rootPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := rootPaths.RelCodeStrict(absPath)
	require.NoError(t, err)
	return paths.CodePathRef{Rel: rel, Abs: paths.ResolveSymlinks(absPath)}
}

// TestPHPIndexer_NamespacedClassAndMethod covers FQN for namespaced classes and methods.
func TestPHPIndexer_NamespacedClassAndMethod(t *testing.T) {
	content := []byte(`<?php
namespace Acme\Billing;

class Invoice {
    public function total(): int {
        return $this->lineTotal();
    }

    public function lineTotal(): int {
        return 0;
    }
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	require.NotNil(t, idx)

	absPath := filepath.Join(root, "php", "Invoice.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, LangPhp, summary.Lang)
	require.Equal(t, ParseOK, summary.ParseStatus)

	// Class symbol
	var classSym, totalSym, lineTotalSym *Symbol
	for i := range summary.Symbols {
		s := &summary.Symbols[i]
		switch s.Name {
		case "Invoice":
			classSym = s
		case "total":
			totalSym = s
		case "lineTotal":
			lineTotalSym = s
		}
	}
	require.NotNil(t, classSym, "expected class symbol")
	require.Equal(t, SymClass, classSym.Kind)
	require.Equal(t, "Acme\\Billing", classSym.Pkg)
	require.Equal(t, "Acme\\Billing\\Invoice", classSym.FQN)
	require.Equal(t, "php/Invoice.php", classSym.File)

	require.NotNil(t, totalSym, "expected method total")
	require.Equal(t, SymMethod, totalSym.Kind)
	require.Equal(t, "Acme\\Billing\\Invoice::total", totalSym.FQN)

	require.NotNil(t, lineTotalSym)
	require.Equal(t, "Acme\\Billing\\Invoice::lineTotal", lineTotalSym.FQN)

	// Method call: $this->lineTotal() shows up as member_call
	var foundCall bool
	for _, c := range summary.Calls {
		if c.CalleeSymbol.Name == "lineTotal" && c.OwnerFQN == "Acme\\Billing\\Invoice::total" {
			foundCall = true
			require.Equal(t, "php/Invoice.php", c.File)
		}
	}
	require.True(t, foundCall, "expected member call to lineTotal: %+v", summary.Calls)
}

// TestPHPIndexer_GlobalFunction covers procedural (no namespace) global function.
func TestPHPIndexer_GlobalFunction(t *testing.T) {
	content := []byte(`<?php
function greet($name) {
    return "Hello, " . $name;
}

greet("World");
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "global.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	require.Len(t, summary.Symbols, 1)
	require.Equal(t, "greet", summary.Symbols[0].Name)
	require.Equal(t, SymFunc, summary.Symbols[0].Kind)
	require.Equal(t, "", summary.Symbols[0].Pkg)
	require.Equal(t, "greet", summary.Symbols[0].FQN)

	// Global call site: caller has empty OwnerFQN (no enclosing function).
	// Calls happen at file top-level so we don't track them — only calls inside function bodies are collected.
	// Validate function-internal call: greet calls string concat operator — not a function call we track.
	// So calls list should be empty here.
}

// TestPHPIndexer_Interface covers interface declaration.
func TestPHPIndexer_Interface(t *testing.T) {
	content := []byte(`<?php
namespace Acme;

interface Notifier {
    public function notify(string $msg): void;
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "Notifier.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var iface *Symbol
	for i := range summary.Symbols {
		if summary.Symbols[i].Name == "Notifier" {
			iface = &summary.Symbols[i]
		}
	}
	require.NotNil(t, iface)
	require.Equal(t, SymInterface, iface.Kind)
	require.Equal(t, "Acme", iface.Pkg)
	require.Equal(t, "Acme\\Notifier", iface.FQN)
}

// TestPHPIndexer_Trait verifies traits are indexed as SymClass.
func TestPHPIndexer_Trait(t *testing.T) {
	content := []byte(`<?php
namespace Acme;

trait Loggable {
    public function logIt(): void {
        Logger::info("hi");
    }
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "Loggable.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var trait *Symbol
	for i := range summary.Symbols {
		if summary.Symbols[i].Name == "Loggable" {
			trait = &summary.Symbols[i]
		}
	}
	require.NotNil(t, trait)
	require.Equal(t, SymClass, trait.Kind, "traits indexed as SymClass per v1 decision")
	require.Equal(t, "Acme\\Loggable", trait.FQN)
	require.Equal(t, "trait Loggable", trait.Signature)
}

// TestPHPIndexer_UseImport verifies `use` declarations become ImportEdge.
func TestPHPIndexer_UseImport(t *testing.T) {
	content := []byte(`<?php
namespace App;

use Vendor\Package\Helper;
use Vendor\Package\Util;
use Vendor\Package\Original as HelperAlias;

class Worker {
    public function run(HelperAlias $helper): void {
        Helper::go();
        HelperAlias::go();
    }
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "Worker.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var modules []string
	for _, imp := range summary.Imports {
		modules = append(modules, imp.Module)
	}
	require.Contains(t, modules, "Vendor\\Package\\Helper")
	require.Contains(t, modules, "Vendor\\Package\\Util")
	require.Contains(t, modules, "Vendor\\Package\\Original")

	var foundAliasedTypeRef, foundAliasedCall bool
	for _, ref := range summary.TypeRefs {
		if ref.TypeSym.Pkg == "Vendor\\Package" && ref.TypeSym.Name == "Original" {
			foundAliasedTypeRef = true
		}
	}
	for _, call := range summary.Calls {
		if call.CalleeSymbol.Pkg == "Vendor\\Package\\Original" && call.CalleeSymbol.Name == "go" {
			foundAliasedCall = true
		}
	}
	require.True(t, foundAliasedTypeRef, "expected aliased type ref: %+v", summary.TypeRefs)
	require.True(t, foundAliasedCall, "expected aliased static call: %+v", summary.Calls)
}

// TestPHPIndexer_IncludeRequire verifies include/require as ImportEdge, including
// dynamic includes built from constant + literal concatenation chains.
func TestPHPIndexer_IncludeRequire(t *testing.T) {
	content := []byte(`<?php
include 'helpers.php';
require_once 'config/app.php';
require __DIR__ . '/dynamic.php';
require ABSPATH . 'wp-cron.php';
require __DIR__ . '/inc/' . 'helpers.php';
require __DIR__ . '/inc/' . $name;

function dummy() {}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "boot.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var modules []string
	for _, imp := range summary.Imports {
		modules = append(modules, imp.Module)
	}
	require.Contains(t, modules, "helpers.php", "bare string literal include")
	require.Contains(t, modules, "config/app.php", "string-literal require_once")
	require.Contains(t, modules, "{__DIR__}/dynamic.php", "magic-constant concat captured with placeholder")
	require.Contains(t, modules, "{ABSPATH}wp-cron.php", "named-constant concat captured with placeholder")
	require.Contains(t, modules, "{__DIR__}/inc/helpers.php", "two-literal concat joins fragments")
	require.Contains(t, modules, "{__DIR__}/inc/{var}", "variable-suffixed include surfaces partial path")
}

// TestPHPIndexer_FunctionCall covers free-function call collection.
func TestPHPIndexer_FunctionCall(t *testing.T) {
	content := []byte(`<?php
namespace App;

function doWork() {
    helper();
    \Vendor\Pkg\thing();
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "work.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var foundHelper, foundQualified bool
	for _, c := range summary.Calls {
		if c.CalleeSymbol.Name == "helper" && c.OwnerFQN == "App\\doWork" {
			foundHelper = true
		}
		if c.CalleeSymbol.Name == "thing" && strings.Contains(c.CalleeSymbol.Pkg, "Vendor") {
			foundQualified = true
		}
	}
	require.True(t, foundHelper, "expected helper() call: %+v", summary.Calls)
	require.True(t, foundQualified, "expected qualified Vendor\\Pkg\\thing() call: %+v", summary.Calls)
}

// TestPHPIndexer_PHPDocComment verifies /** */ comments preceding declarations attach as DocComment.
func TestPHPIndexer_PHPDocComment(t *testing.T) {
	content := []byte(`<?php
namespace App;

/**
 * The Worker handles jobs.
 */
class Worker {
    /**
     * Run a single job.
     */
    public function run(): void {}
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "DocWorker.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var classDoc, methodDoc string
	for _, s := range summary.Symbols {
		switch s.Name {
		case "Worker":
			classDoc = s.DocComment
		case "run":
			methodDoc = s.DocComment
		}
	}
	require.Contains(t, classDoc, "The Worker handles jobs")
	require.Contains(t, methodDoc, "Run a single job")
}

// TestPHPIndexer_NoNamespaceFile covers a global-scope file with classes (no namespace).
func TestPHPIndexer_NoNamespaceFile(t *testing.T) {
	content := []byte(`<?php
class LegacyHelper {
    public function help() {
        return 42;
    }
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "Legacy.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var cls *Symbol
	for i := range summary.Symbols {
		if summary.Symbols[i].Name == "LegacyHelper" {
			cls = &summary.Symbols[i]
		}
	}
	require.NotNil(t, cls)
	require.Equal(t, "", cls.Pkg, "no namespace = empty Pkg")
	require.Equal(t, "LegacyHelper", cls.FQN, "FQN falls back to bare name")
}

// TestPHPIndexer_ParseRecovery covers malformed PHP and ensures we don't crash.
func TestPHPIndexer_ParseRecovery(t *testing.T) {
	content := []byte(`<?php
namespace App;

class Half {
    public function start(

class Other {
    public function ok() { return 1; }
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "broken.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)
	require.Equal(t, ParseErrored, summary.ParseStatus)
	// Best effort: we should still pick up at least one symbol despite parse errors.
	require.NotEmpty(t, summary.Symbols)
}

// TestPHPIndexer_StaticCall verifies static method calls collect with the called method name.
func TestPHPIndexer_StaticCall(t *testing.T) {
	content := []byte(`<?php
namespace App;

class Caller {
    public function run() {
        \Acme\Logger::info("hi");
        Helper::go();
    }
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "Caller.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var foundInfo, foundGo bool
	for _, c := range summary.Calls {
		if c.CalleeSymbol.Name == "info" && c.OwnerFQN == "App\\Caller::run" {
			foundInfo = true
		}
		if c.CalleeSymbol.Name == "go" && c.OwnerFQN == "App\\Caller::run" {
			foundGo = true
		}
	}
	require.True(t, foundInfo, "expected static call Logger::info: %+v", summary.Calls)
	require.True(t, foundGo, "expected static call Helper::go: %+v", summary.Calls)
}

// TestPHPIndexer_StringArgCallback_BasicHook verifies that a string-literal argument that
// looks like a PHP identifier emits a CallSite for the named handler, in addition to the
// outer call. This is the WP `add_action('init', 'my_handler')` / Laravel route pattern.
func TestPHPIndexer_StringArgCallback_BasicHook(t *testing.T) {
	content := []byte(`<?php
function bootstrap() {
    add_action('init', 'my_handler');
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "hooks.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var foundOuter, foundHandler bool
	for _, c := range summary.Calls {
		if c.OwnerFQN != "bootstrap" {
			continue
		}
		switch c.CalleeSymbol.Name {
		case "add_action":
			foundOuter = true
		case "my_handler":
			foundHandler = true
			require.Equal(t, LangPhp, c.CalleeSymbol.Lang)
			require.Empty(t, c.CalleeSymbol.Pkg, "unqualified identifier has empty Pkg")
		}
	}
	require.True(t, foundOuter, "expected outer add_action call: %+v", summary.Calls)
	require.True(t, foundHandler, "expected inner my_handler call from string arg: %+v", summary.Calls)
}

// TestPHPIndexer_StringArgCallback_QualifiedName verifies that backslash-namespaced
// identifiers passed as string args resolve into Pkg/Name split.
func TestPHPIndexer_StringArgCallback_QualifiedName(t *testing.T) {
	content := []byte(`<?php
function bootstrap() {
    add_action('init', 'My\Namespace\handler');
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "hooks_qualified.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var found bool
	for _, c := range summary.Calls {
		if c.OwnerFQN == "bootstrap" && c.CalleeSymbol.Name == "handler" {
			found = true
			require.Contains(t, c.CalleeSymbol.Pkg, "My", "expected Pkg to contain namespace: %+v", c)
			require.Contains(t, c.CalleeSymbol.Pkg, "Namespace")
		}
	}
	require.True(t, found, "expected qualified handler split into Pkg/Name: %+v", summary.Calls)
}

// TestPHPIndexer_StringArgCallback_NotIdentifier verifies that string arguments that
// aren't valid PHP identifiers (spaces, format specifiers, punctuation) do NOT emit
// callee CallSites.
func TestPHPIndexer_StringArgCallback_NotIdentifier(t *testing.T) {
	content := []byte(`<?php
function fmt() {
    printf("hello %s\n", "with space");
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "printf.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	for _, c := range summary.Calls {
		if c.OwnerFQN != "fmt" {
			continue
		}
		require.NotEqual(t, "hello %s\n", c.CalleeSymbol.Name, "format string must not emit callee")
		require.NotEqual(t, "with space", c.CalleeSymbol.Name, "string with space must not emit callee")
	}
	// printf itself should still appear as the outer call.
	var foundPrintf bool
	for _, c := range summary.Calls {
		if c.OwnerFQN == "fmt" && c.CalleeSymbol.Name == "printf" {
			foundPrintf = true
		}
	}
	require.True(t, foundPrintf, "expected outer printf call still present: %+v", summary.Calls)
}

// TestPHPIndexer_MagicMethods verifies PHP magic methods (__construct, __call, __invoke,
// __toString, __get, __set, etc.) are emitted as SymMethod with FQN class::__name. The
// catalog 2026-06-12 listed magic methods as a potential real-gap; this test pins the
// v1 behavior as already-supported (method_declaration walker applies no name filter).
func TestPHPIndexer_MagicMethods(t *testing.T) {
	content := []byte(`<?php
namespace App;

class Container {
    public function __construct(private array $bindings = []) {}
    public function __call(string $name, array $args) {}
    public function __invoke($key) {}
    public function __toString(): string { return ''; }
    public function __get($name) {}
    public function __set($name, $value) {}
    public function __isset($name) {}
    public function __destruct() {}
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "Container.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	want := map[string]bool{
		"__construct": false, "__call": false, "__invoke": false,
		"__toString": false, "__get": false, "__set": false,
		"__isset": false, "__destruct": false,
	}
	for _, s := range summary.Symbols {
		if s.Kind != SymMethod {
			continue
		}
		if _, ok := want[s.Name]; ok {
			require.Equal(t, "App\\Container", s.Pkg, "magic method should carry class as Pkg: %+v", s)
			require.Equal(t, "App\\Container::"+s.Name, s.FQN, "magic method FQN shape: %+v", s)
			want[s.Name] = true
		}
	}
	for name, found := range want {
		require.Truef(t, found, "magic method %s not emitted as SymMethod", name)
	}
}

// TestPHPIndexer_StringArgCallback_NonStringArg verifies that variable arguments
// don't trigger string-arg callee emission.
func TestPHPIndexer_StringArgCallback_NonStringArg(t *testing.T) {
	content := []byte(`<?php
function bootstrap($handler) {
    add_action('init', $handler);
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "hooks_var.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	for _, c := range summary.Calls {
		if c.OwnerFQN != "bootstrap" {
			continue
		}
		// Per AST-shape rule, 'init' (a clean identifier string) WILL emit a CallSite —
		// that's an accepted false positive. The variable $handler must not.
		require.NotEqual(t, "handler", c.CalleeSymbol.Name, "variable handler must not emit callee (would need value at runtime)")
	}
}

// TestPHPIndexer_FileScopeCalls verifies that calls at file scope (not inside any
// function/method/class body) are extracted as CallSites with the namespace (or empty
// string) as their owner FQN. Surfaced as a gap by the 2026-06-15 wp-tst assessment:
// WordPress's `default-filters.php` (600 lines of file-scope `add_action(...)` /
// `add_filter(...)`) produced zero `intel_symbol_refs` rows because `phpCollectCalls`
// only fires from inside `function_definition` / `method_declaration` switch cases.
// Pinned by SPEC-0070 US5.
func TestPHPIndexer_FileScopeCalls(t *testing.T) {
	content := []byte(`<?php
add_action('init', 'my_init_handler');
add_filter('the_title', 'my_title_filter');

function my_init_handler() {}
function my_title_filter($title) { return $title; }
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "default-filters-style.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var foundAddAction, foundAddFilter, foundInitHandler, foundTitleFilter bool
	addActionCount, addFilterCount, initHandlerCount, titleFilterCount := 0, 0, 0, 0
	for _, c := range summary.Calls {
		switch c.CalleeSymbol.Name {
		case "add_action":
			foundAddAction = true
			addActionCount++
			require.Empty(t, c.OwnerFQN, "file-scope add_action owner should be empty (no namespace)")
		case "add_filter":
			foundAddFilter = true
			addFilterCount++
			require.Empty(t, c.OwnerFQN, "file-scope add_filter owner should be empty (no namespace)")
		case "my_init_handler":
			foundInitHandler = true
			initHandlerCount++
		case "my_title_filter":
			foundTitleFilter = true
			titleFilterCount++
		}
	}
	require.True(t, foundAddAction, "expected file-scope add_action call: %+v", summary.Calls)
	require.True(t, foundAddFilter, "expected file-scope add_filter call: %+v", summary.Calls)
	require.True(t, foundInitHandler, "expected file-scope string-arg callee my_init_handler: %+v", summary.Calls)
	require.True(t, foundTitleFilter, "expected file-scope string-arg callee my_title_filter: %+v", summary.Calls)

	require.Equal(t, 1, addActionCount, "add_action must not double-emit (got %d)", addActionCount)
	require.Equal(t, 1, addFilterCount, "add_filter must not double-emit (got %d)", addFilterCount)
	require.Equal(t, 1, initHandlerCount, "my_init_handler string-arg callee must not double-emit (got %d)", initHandlerCount)
	require.Equal(t, 1, titleFilterCount, "my_title_filter string-arg callee must not double-emit (got %d)", titleFilterCount)

	var initSym, titleSym *Symbol
	for i := range summary.Symbols {
		s := &summary.Symbols[i]
		switch s.Name {
		case "my_init_handler":
			initSym = s
		case "my_title_filter":
			titleSym = s
		}
	}
	require.NotNil(t, initSym, "function symbol my_init_handler must still be extracted")
	require.NotNil(t, titleSym, "function symbol my_title_filter must still be extracted")
}

// TestPHPIndexer_FileScopeCalls_Namespaced verifies file-scope calls in a namespaced
// file carry the namespace as their owner FQN (symmetric with free-function Pkg/FQN).
func TestPHPIndexer_FileScopeCalls_Namespaced(t *testing.T) {
	content := []byte(`<?php
namespace Acme\Theme;

add_action('init', 'my_handler');
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "namespaced-bootstrap.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var foundAddAction, foundHandler bool
	for _, c := range summary.Calls {
		switch c.CalleeSymbol.Name {
		case "add_action":
			foundAddAction = true
			require.Equal(t, "Acme\\Theme", c.OwnerFQN, "file-scope call should carry namespace as owner: %+v", c)
		case "my_handler":
			foundHandler = true
			require.Equal(t, "Acme\\Theme", c.OwnerFQN, "file-scope string-arg callee should carry namespace as owner: %+v", c)
		}
	}
	require.True(t, foundAddAction, "expected namespaced file-scope add_action call: %+v", summary.Calls)
	require.True(t, foundHandler, "expected namespaced file-scope string-arg callee: %+v", summary.Calls)
}

// TestPHPIndexer_FileScopeCalls_NoRegression verifies that adding file-scope call
// extraction does not double-emit calls inside function/method bodies. The structured
// walker already collects those via phpCollectCalls(body, fqn, ...); the file-scope
// pass must skip into function/method/class boundaries.
func TestPHPIndexer_FileScopeCalls_NoRegression(t *testing.T) {
	content := []byte(`<?php
function foo() {
    bar();
    add_action('init', 'inner_handler');
}

add_filter('the_title', 'top_handler');
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "mixed-scope.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	barCount, innerAddActionCount, innerHandlerCount := 0, 0, 0
	topAddFilterCount, topHandlerCount := 0, 0
	for _, c := range summary.Calls {
		switch c.CalleeSymbol.Name {
		case "bar":
			barCount++
			require.Equal(t, "foo", c.OwnerFQN, "function-body call should retain function owner: %+v", c)
		case "add_action":
			innerAddActionCount++
			require.Equal(t, "foo", c.OwnerFQN, "add_action inside foo() should owner=foo: %+v", c)
		case "inner_handler":
			innerHandlerCount++
			require.Equal(t, "foo", c.OwnerFQN, "string-arg callee inside foo() should owner=foo: %+v", c)
		case "add_filter":
			topAddFilterCount++
			require.Empty(t, c.OwnerFQN, "file-scope add_filter should have empty owner: %+v", c)
		case "top_handler":
			topHandlerCount++
			require.Empty(t, c.OwnerFQN, "file-scope string-arg callee should have empty owner: %+v", c)
		}
	}
	require.Equal(t, 1, barCount, "bar() inside foo() must not double-emit (got %d)", barCount)
	require.Equal(t, 1, innerAddActionCount, "add_action inside foo() must not double-emit (got %d)", innerAddActionCount)
	require.Equal(t, 1, innerHandlerCount, "inner_handler must not double-emit (got %d)", innerHandlerCount)
	require.Equal(t, 1, topAddFilterCount, "file-scope add_filter must not double-emit (got %d)", topAddFilterCount)
	require.Equal(t, 1, topHandlerCount, "file-scope top_handler must not double-emit (got %d)", topHandlerCount)
}

// TestPHPIndexer_PropertyDeclaration verifies class fields become SymField entries.
func TestPHPIndexer_PropertyDeclaration(t *testing.T) {
	content := []byte(`<?php
namespace App;

class Bag {
    public string $name = "";
    private int $count;
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "Bag.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	var nameField, countField *Symbol
	for i := range summary.Symbols {
		s := &summary.Symbols[i]
		if s.Kind == SymField {
			switch s.Name {
			case "name":
				nameField = s
			case "count":
				countField = s
			}
		}
	}
	require.NotNil(t, nameField, "expected $name field: %+v", summary.Symbols)
	require.Equal(t, "App\\Bag::$name", nameField.FQN)
	require.NotNil(t, countField, "expected $count field")
	require.Equal(t, "App\\Bag::$count", countField.FQN)
}

func TestPHPIndexer_RealWorldOOEdges(t *testing.T) {
	content := []byte(`<?php
namespace App\Http;

use App\Domain\Contracts\SyncGateway;
use App\Domain\Events\SyncRequested;
use App\Domain\Services\BaseController;
use App\Domain\Services\SyncClient;
use App\Framework\Route;

#[Route('/tasks/{id}', methods: ['POST'])]
class TaskController extends BaseController implements SyncGateway {
    public function __construct(private SyncClient $client) {}

    public function store(TaskStatus $status): void {
        if ($status === TaskStatus::Pending) {
            $this->client->pushUpdates("controller-store");
            SyncClient::fromContainer()->pushUpdates("static-factory");
            SyncRequested::dispatch($status);
        }
    }
}

enum TaskStatus: string {
    case Pending = 'pending';
    case Done = 'done';
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "TaskController.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	require.Contains(t, phpSymbolFQNs(summary.Symbols), "App\\Http\\TaskController")
	require.Contains(t, phpSymbolFQNs(summary.Symbols), "App\\Http\\TaskController::store")
	require.Contains(t, phpSymbolFQNs(summary.Symbols), "App\\Http\\TaskStatus")
	require.Contains(t, phpSymbolFQNs(summary.Symbols), "App\\Http\\TaskStatus::Pending")
	require.Contains(t, phpSymbolFQNs(summary.Symbols), "App\\Http\\TaskStatus::Done")

	require.Contains(t, summary.Supers, SuperEdge{
		ChildFQN:  "App\\Http\\TaskController",
		ParentFQN: "App\\Domain\\Services\\BaseController",
	})
	require.Contains(t, summary.Supers, SuperEdge{
		ChildFQN:  "App\\Http\\TaskController",
		ParentFQN: "App\\Domain\\Contracts\\SyncGateway",
	})

	require.Contains(t, summary.Annotations, AnnotationUse{
		OwnerFQN:  "App\\Http\\TaskController",
		AnnSymbol: SymbolRef{Lang: LangPhp, Pkg: "App\\Framework", Name: "Route"},
		Args: map[string]string{
			"0":       "/tasks/{id}",
			"methods": "POST",
		},
	})

	require.Contains(t, summary.TypeRefs, TypeRef{
		File:     "php/TaskController.php",
		OwnerFQN: "App\\Http\\TaskController::__construct",
		TypeSym:  SymbolRef{Lang: LangPhp, Pkg: "App\\Domain\\Services", Name: "SyncClient"},
	})
	require.Contains(t, summary.TypeRefs, TypeRef{
		File:     "php/TaskController.php",
		OwnerFQN: "App\\Http\\TaskController::store",
		TypeSym:  SymbolRef{Lang: LangPhp, Pkg: "App\\Http", Name: "TaskStatus"},
	})

	require.Contains(t, summary.Calls, CallSite{
		File:     "php/TaskController.php",
		OwnerFQN: "App\\Http\\TaskController::store",
		CalleeSymbol: SymbolRef{
			Lang:   LangPhp,
			Pkg:    "App\\Domain\\Services\\SyncClient",
			Name:   "pushUpdates",
			Member: true,
		},
	})
	require.Contains(t, summary.Calls, CallSite{
		File:     "php/TaskController.php",
		OwnerFQN: "App\\Http\\TaskController::store",
		CalleeSymbol: SymbolRef{
			Lang:   LangPhp,
			Pkg:    "App\\Domain\\Events\\SyncRequested",
			Name:   "dispatch",
			Member: true,
		},
	})
	require.Contains(t, summary.MemberRefs, MemberRef{
		File:     "php/TaskController.php",
		OwnerFQN: "App\\Http\\TaskController::store",
		Sym:      SymbolRef{Lang: LangPhp, Pkg: "App\\Http\\TaskStatus", Name: "Pending", Member: true},
	})
}

func TestPHPIndexer_LocalNewVariableMethodCall(t *testing.T) {
	content := []byte(`<?php
namespace App\Worker;

use App\Services\SyncClient;

class Worker {
    public function run(): void {
        $client = new SyncClient();
        $client->pushUpdates("worker-run");
    }
}
`)
	root := t.TempDir()
	idx := NewPHPIndexer()
	absPath := filepath.Join(root, "php", "Worker.php")
	summary, err := idx.IndexFile(content, phpCodeRef(t, root, absPath))
	require.NoError(t, err)

	require.Contains(t, summary.Calls, CallSite{
		File:     "php/Worker.php",
		OwnerFQN: "App\\Worker\\Worker::run",
		CalleeSymbol: SymbolRef{
			Lang:   LangPhp,
			Pkg:    "App\\Services\\SyncClient",
			Name:   "pushUpdates",
			Member: true,
		},
	})
}

func phpSymbolFQNs(symbols []Symbol) []string {
	out := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		out = append(out, sym.NormalizeFQN())
	}
	return out
}
