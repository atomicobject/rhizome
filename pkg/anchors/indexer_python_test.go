package codeanchor

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func relCodeRef(path string) paths.CodePathRef {
	return paths.CodePathRef{Rel: paths.NormalizeCode(path)}
}

func TestPythonIndexer_BasicClassFunctionCall(t *testing.T) {
	code := []byte(`
import foo.bar as fb

@decorator(tag="billing")
class InvoiceService(BaseService):
    def compute(self):
        charge()

def charge():
    pass
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("svc/invoice.py"))
	require.NoError(t, err)

	require.Equal(t, LangPy, summary.Lang)
	require.Equal(t, "svc/invoice.py", summary.FilePath)

	// Symbols: class InvoiceService, method compute, func charge
	require.Len(t, summary.Symbols, 3)
	fqns := map[string]Symbol{}
	for _, s := range summary.Symbols {
		fqns[s.FQN] = s
	}
	require.Contains(t, fqns, "svc.invoice.InvoiceService")
	require.Contains(t, fqns, "svc.invoice.InvoiceService.compute")
	require.Contains(t, fqns, "svc.invoice.charge")

	// Inheritance
	require.Len(t, summary.Supers, 1)
	require.Equal(t, SuperEdge{
		ChildFQN:  "svc.invoice.InvoiceService",
		ParentFQN: "svc.invoice.BaseService",
	}, summary.Supers[0])

	// Decorator
	require.Len(t, summary.Annotations, 1)
	ann := summary.Annotations[0]
	require.Equal(t, "svc.invoice.InvoiceService", ann.OwnerFQN)
	require.Equal(t, SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "decorator"}, ann.AnnSymbol)
	require.Equal(t, map[string]string{"tag": "billing"}, ann.Args)

	// Call site (charge)
	require.Len(t, summary.Calls, 1)
	require.Equal(t, SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "charge"}, summary.Calls[0].CalleeSymbol)
}

func TestPythonIndexer_ModulePathAndImports(t *testing.T) {
	code := []byte(`
from myapp.decorators import transactional
from pkg import foo as bar

@transactional(timeout=30, items=[1, 2])
def process():
    bar()
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("src/app/models.py"))
	require.NoError(t, err)

	require.Equal(t, "app.models.process", summary.Symbols[0].FQN)

	require.Len(t, summary.Annotations, 1)
	ann := summary.Annotations[0]
	require.Equal(t, SymbolRef{Lang: LangPy, Pkg: "myapp.decorators", Name: "transactional"}, ann.AnnSymbol)
	require.Equal(t, map[string]string{"timeout": "30", "items": "[1, 2]"}, ann.Args)

	require.Len(t, summary.Calls, 1)
	require.Equal(t, SymbolRef{Lang: LangPy, Pkg: "pkg", Name: "foo"}, summary.Calls[0].CalleeSymbol)
}

func TestPythonIndexer_ModuleDocstring(t *testing.T) {
	code := []byte(`"""
Module docs for analytics utilities.
"""

def run():
    pass
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("svc/analytics.py"))
	require.NoError(t, err)
	require.Contains(t, summary.PackageDoc, "Module docs for analytics utilities.")
}

func TestPythonIndexer_HeaderCommentFallback(t *testing.T) {
	code := []byte(`# Header comment line 1
# Header comment line 2

def run():
    pass
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("svc/headers.py"))
	require.NoError(t, err)
	require.Contains(t, summary.PackageDoc, "Header comment line 1")
}

func TestPythonIndexer_ImportsCanonicalizeSrcShim(t *testing.T) {
	code := []byte(`
from charm.core.events.CEP.definition import CEPDefinition

class EarlyBird(CEPDefinition[int]):
    pass
`)
	root := t.TempDir()
	idx := NewPythonIndexerWithRoots([]string{filepath.Join(root, "backend")})
	path := filepath.Join(root, "backend", "src", "charm", "core", "badges", "rules", "early_bird.py")
	summary, err := idx.IndexFile(code, relCodeRef(path))
	require.NoError(t, err)

	// The module name for this file includes the src/ shim.
	require.Equal(t, "src.charm.core.badges.rules.early_bird", summary.Symbols[0].Pkg)

	// Super edge should also use the src.* package prefix, despite importing `charm...`.
	require.NotEmpty(t, summary.Supers)
	require.Contains(t, summary.Supers, SuperEdge{
		ChildFQN:  "src.charm.core.badges.rules.early_bird.EarlyBird",
		ParentFQN: "src.charm.core.events.CEP.definition.CEPDefinition",
	})
}

func TestPythonIndexer_BuiltinDecorators(t *testing.T) {
	code := []byte(`
class Thing:
    @property
    def name(self):
        return self._name

    @classmethod
    def create(cls):
        return cls()
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("thing.py"))
	require.NoError(t, err)

	require.Len(t, summary.Annotations, 2)
	props := map[string]SymbolRef{}
	for _, ann := range summary.Annotations {
		props[ann.AnnSymbol.Name] = ann.AnnSymbol
	}
	require.Equal(t, SymbolRef{Lang: LangPy, Pkg: "builtins", Name: "property"}, props["property"])
	require.Equal(t, SymbolRef{Lang: LangPy, Pkg: "builtins", Name: "classmethod"}, props["classmethod"])
}

func TestPythonIndexer_RelativeImportsAndLibRoot(t *testing.T) {
	code := []byte(`
from .utils import helper
from ..common import shared

def run():
    helper()
    shared()
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("lib/myapp/tasks/run.py"))
	require.NoError(t, err)

	require.Equal(t, "myapp.tasks.run.run", summary.Symbols[0].FQN)
	require.Len(t, summary.Calls, 2)
	require.ElementsMatch(t, []SymbolRef{
		{Lang: LangPy, Pkg: "myapp.tasks.utils", Name: "helper"},
		{Lang: LangPy, Pkg: "myapp.common", Name: "shared"},
	}, []SymbolRef{summary.Calls[0].CalleeSymbol, summary.Calls[1].CalleeSymbol})
}

func TestPythonIndexer_AbsolutePathUsesSourceRoot(t *testing.T) {
	code := []byte(`
def add_task():
    return True
`)
	root := t.TempDir()
	idx := NewPythonIndexerWithRoots([]string{
		filepath.Join(root, "vault", "src"),
		filepath.Join(root, "vault", "packages"),
	})
	path := filepath.Join(root, "vault", "src", "todoapp", "services", "tasks.py")

	summary, err := idx.IndexFile(code, relCodeRef(path))
	require.NoError(t, err)
	require.Len(t, summary.Symbols, 1)
	require.Equal(t, "todoapp.services.tasks", summary.Symbols[0].Pkg)
	require.Equal(t, "todoapp.services.tasks.add_task", summary.Symbols[0].FQN)
}

func TestPythonIndexer_MethodCallsOnSelfAndClassName(t *testing.T) {
	code := []byte(`
class InvoiceService:
    def charge(self):
        pass

    @classmethod
    def create(cls):
        return cls()

    def compute(self):
        self.charge()

def run():
    InvoiceService.create()
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("svc/invoice.py"))
	require.NoError(t, err)

	require.Len(t, summary.Calls, 3)
	expected := []SymbolRef{
		{Lang: LangPy, Pkg: "svc.invoice", Name: "InvoiceService"},
		{Lang: LangPy, Pkg: "svc.invoice.InvoiceService", Name: "charge"},
		{Lang: LangPy, Pkg: "svc.invoice.InvoiceService", Name: "create"},
	}
	got := make([]SymbolRef, 0, len(summary.Calls))
	for _, call := range summary.Calls {
		got = append(got, call.CalleeSymbol)
	}
	require.ElementsMatch(t, expected, got)
}

func TestPythonIndexer_MethodCallsViaTypedParams(t *testing.T) {
	code := []byte(`
from myapp.store import TaskStore

def run(store: TaskStore):
    store.persist()
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("svc/tasks.py"))
	require.NoError(t, err)

	require.Len(t, summary.Calls, 1)
	require.Equal(t, SymbolRef{Lang: LangPy, Pkg: "myapp.store.TaskStore", Name: "persist"}, summary.Calls[0].CalleeSymbol)
}

func TestPythonIndexer_DataclassFields(t *testing.T) {
	code := []byte(`
from dataclasses import dataclass

@dataclass
class Task:
    id: str
    title: str
    count: int = 0
    _private: str = ""
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("models/task.py"))
	require.NoError(t, err)

	// Should have: Task class + 3 public fields (id, title, count)
	// _private is skipped because it starts with underscore
	require.Len(t, summary.Symbols, 4)

	fqns := map[string]Symbol{}
	for _, s := range summary.Symbols {
		fqns[s.FQN] = s
	}

	// Class itself
	require.Contains(t, fqns, "models.task.Task")
	require.Equal(t, SymClass, fqns["models.task.Task"].Kind)

	// Fields
	require.Contains(t, fqns, "models.task.Task.id")
	require.Equal(t, SymField, fqns["models.task.Task.id"].Kind)
	require.Equal(t, "id: str", fqns["models.task.Task.id"].Signature)

	require.Contains(t, fqns, "models.task.Task.title")
	require.Equal(t, SymField, fqns["models.task.Task.title"].Kind)

	require.Contains(t, fqns, "models.task.Task.count")
	require.Equal(t, SymField, fqns["models.task.Task.count"].Kind)

	// _private should NOT be included
	require.NotContains(t, fqns, "models.task.Task._private")
}

func TestPythonIndexer_ProtocolDetection(t *testing.T) {
	code := []byte(`
from typing import Protocol

class TaskStore(Protocol):
    def persist(self, task: dict) -> None: ...
    def all(self) -> list: ...
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("ports/store.py"))
	require.NoError(t, err)

	// Should have: TaskStore interface + 2 methods
	require.Len(t, summary.Symbols, 3)

	fqns := map[string]Symbol{}
	for _, s := range summary.Symbols {
		fqns[s.FQN] = s
	}

	// Class should be detected as interface (Protocol)
	require.Contains(t, fqns, "ports.store.TaskStore")
	require.Equal(t, SymInterface, fqns["ports.store.TaskStore"].Kind)

	// Methods
	require.Contains(t, fqns, "ports.store.TaskStore.persist")
	require.Equal(t, SymMethod, fqns["ports.store.TaskStore.persist"].Kind)
}

func TestPythonIndexer_ABCDetection(t *testing.T) {
	code := []byte(`
from abc import ABC, abstractmethod

class BaseRepository(ABC):
    @abstractmethod
    def save(self, entity) -> None:
        pass
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("repos/base.py"))
	require.NoError(t, err)

	fqns := map[string]Symbol{}
	for _, s := range summary.Symbols {
		fqns[s.FQN] = s
	}

	// ABC class should be detected as interface
	require.Contains(t, fqns, "repos.base.BaseRepository")
	require.Equal(t, SymInterface, fqns["repos.base.BaseRepository"].Kind)
}

func TestPythonIndexer_InitPyReExports(t *testing.T) {
	code := []byte(`
from .tasks import TaskService
from .sync import SyncManager as SM
from .utils import helper, validator
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("src/services/__init__.py"))
	require.NoError(t, err)

	// Should have 4 re-exported symbols: TaskService, SM, helper, validator
	require.Len(t, summary.Symbols, 4)

	fqns := map[string]Symbol{}
	for _, s := range summary.Symbols {
		fqns[s.FQN] = s
	}

	require.Contains(t, fqns, "services.TaskService")
	require.Contains(t, fqns, "services.SM")
	require.Contains(t, fqns, "services.helper")
	require.Contains(t, fqns, "services.validator")

	// Check signature contains source info
	require.Contains(t, fqns["services.TaskService"].Signature, "re-export from services.tasks.TaskService")
	require.Contains(t, fqns["services.SM"].Signature, "re-export from services.sync.SyncManager")
}

func TestPythonIndexer_ImportEdgesCollected(t *testing.T) {
	code := []byte(`
import os
import sys
from myapp.core import service
from myapp.utils import helper, logger
from .local import something
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("lib/myapp/tasks/run.py"))
	require.NoError(t, err)

	// Should collect: os, sys, myapp.core, myapp.utils, myapp.tasks.local
	require.NotEmpty(t, summary.Imports, "should collect import edges")

	modules := make(map[string]bool)
	for _, imp := range summary.Imports {
		modules[imp.Module] = true
	}

	require.True(t, modules["os"], "should include 'os' import")
	require.True(t, modules["sys"], "should include 'sys' import")
	require.True(t, modules["myapp.core"], "should include 'myapp.core' import")
	require.True(t, modules["myapp.utils"], "should include 'myapp.utils' import")
	require.True(t, modules["myapp.tasks.local"], "should include resolved relative import")
}

func TestPythonIndexer_ImportEdgesDeduplication(t *testing.T) {
	code := []byte(`
from myapp.core import Thing
from myapp.core import OtherThing
from myapp.core import YetAnother
import os
import os
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("app/main.py"))
	require.NoError(t, err)

	// Count occurrences of each module
	counts := make(map[string]int)
	for _, imp := range summary.Imports {
		counts[imp.Module]++
	}

	// myapp.core appears 3 times in source but should be deduplicated to 1
	require.Equal(t, 1, counts["myapp.core"], "myapp.core should be deduplicated")
	// os appears 2 times in source but should be deduplicated to 1
	require.Equal(t, 1, counts["os"], "os should be deduplicated")
}

func TestPythonIndexer_ImportEdgesFallbackOnParseError(t *testing.T) {
	code := []byte(`
import os

def broken(
from myapp.core import service
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("lib/myapp/tasks/run.py"))
	require.NoError(t, err)

	modules := make(map[string]bool)
	for _, imp := range summary.Imports {
		modules[imp.Module] = true
	}

	require.True(t, modules["os"], "should include 'os' import")
	require.True(t, modules["myapp.core"], "should include 'myapp.core' from fallback scan")
}

func TestPythonIndexer_CallEdgesFallbackOnParseError(t *testing.T) {
	code := []byte(`
def broken(
def do_work():
    push_updates({"ok": True})
`)
	idx := NewPythonIndexerWithRoots([]string{"src", "packages", "scripts"})
	summary, err := idx.IndexFile(code, relCodeRef("src/todoapp/services/tasks.py"))
	require.NoError(t, err)
	require.NotEmpty(t, summary.Calls)

	found := false
	for _, call := range summary.Calls {
		if call.CalleeSymbol.Name == "push_updates" {
			require.Equal(t, "", call.CalleeSymbol.Pkg)
			found = true
			break
		}
	}
	require.True(t, found, "expected fallback call edge for push_updates")
}

func TestPythonIndexer_CallEdgesFallbackWhenImportsMissing(t *testing.T) {
	idx := NewPythonIndexerWithRoots([]string{"src"})
	code := `
def add_task():
    sync.push_updates()
`
	summary, err := idx.IndexFile([]byte(code), paths.CodePathRef{Rel: "src/todoapp/services/tasks.py"})
	require.NoError(t, err)
	require.NotEmpty(t, summary.Calls)

	found := false
	for _, call := range summary.Calls {
		if call.CalleeSymbol.Name == "push_updates" && call.CalleeSymbol.Pkg == "" {
			found = true
			break
		}
	}
	require.True(t, found, "expected symbol-only call edge when imports are missing")
}

func TestPythonIndexer_InitPyRelativeImportResolution(t *testing.T) {
	// In __init__.py, the module name is the package itself, so relative imports
	// resolve differently: `.foo` in pkg/__init__.py resolves to `pkg.foo`,
	// not `pkg.pkg.foo`.
	//
	// Default PythonIndexer has ["src", "lib"] as source roots, so
	// path "src/services/__init__.py" produces package "services".
	code := []byte(`
from .tasks import TaskService
from ..common import shared
`)

	idx := NewPythonIndexer()

	// Test __init__.py file (package is "services" with default src/ root stripped)
	initSummary, err := idx.IndexFile(code, relCodeRef("src/services/__init__.py"))
	require.NoError(t, err)

	initModules := make(map[string]bool)
	for _, imp := range initSummary.Imports {
		initModules[imp.Module] = true
	}

	// In __init__.py: .tasks resolves to services.tasks (same level as services package)
	require.True(t, initModules["services.tasks"], "__init__.py should resolve .tasks to services.tasks")
	// In __init__.py: ..common goes up one level from "services" - but since services has no
	// parent in the package hierarchy, packageFromQualified returns "services", giving "services.common"
	require.True(t, initModules["services.common"], "__init__.py should resolve ..common to services.common")

	// Test regular .py file with same imports - use a deeper package to show the difference
	regularCode := []byte(`
from .utils import helper
from ..common import shared
`)
	// Regular file: lib/myapp/tasks/runner.py has package myapp.tasks.runner
	regularSummary, err := idx.IndexFile(regularCode, relCodeRef("lib/myapp/tasks/runner.py"))
	require.NoError(t, err)

	regularModules := make(map[string]bool)
	for _, imp := range regularSummary.Imports {
		regularModules[imp.Module] = true
	}

	// In regular file: .utils resolves to myapp.tasks.utils (sibling of runner)
	require.True(t, regularModules["myapp.tasks.utils"], "regular file should resolve .utils to myapp.tasks.utils")
	// In regular file: ..common goes up 2 levels from myapp.tasks.runner → myapp.tasks → myapp, then appends common
	require.True(t, regularModules["myapp.common"], "regular file should resolve ..common to myapp.common")
}

func TestResolveRelativeModuleForInitPy(t *testing.T) {
	tests := []struct {
		name       string
		currentPkg string
		mod        string
		expected   string
	}{
		{
			name:       "single dot in __init__.py",
			currentPkg: "myapp.services",
			mod:        ".tasks",
			expected:   "myapp.services.tasks",
		},
		{
			name:       "double dot in __init__.py",
			currentPkg: "myapp.services",
			mod:        "..common",
			expected:   "myapp.common",
		},
		{
			name:       "triple dot in __init__.py",
			currentPkg: "myapp.services.internal",
			mod:        "...utils",
			expected:   "myapp.utils",
		},
		{
			name:       "single dot bare in __init__.py",
			currentPkg: "myapp.services",
			mod:        ".",
			expected:   "myapp.services",
		},
		{
			name:       "absolute import unchanged",
			currentPkg: "myapp.services",
			mod:        "os.path",
			expected:   "os.path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := resolveRelativeModuleForInitPy(tc.currentPkg, tc.mod)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestResolveRelativeModule_RegularFile(t *testing.T) {
	// Test the regular resolveRelativeModule for non-__init__.py files
	tests := []struct {
		name       string
		currentPkg string
		mod        string
		expected   string
	}{
		{
			name:       "single dot in regular file",
			currentPkg: "myapp.services.runner",
			mod:        ".tasks",
			expected:   "myapp.services.tasks",
		},
		{
			name:       "double dot in regular file",
			currentPkg: "myapp.services.runner",
			mod:        "..common",
			expected:   "myapp.common",
		},
		{
			name:       "triple dot in regular file",
			currentPkg: "myapp.services.internal.runner",
			mod:        "...utils",
			expected:   "myapp.utils",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := resolveRelativeModule(tc.currentPkg, tc.mod)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestPythonIndexer_TypeRefs(t *testing.T) {
	code := []byte(`
from myapp.models import User, Order

def get_user(id: int) -> User:
    pass

def process_order(order: Order, user: User) -> None:
    pass

class Service:
    def find(self, id: int) -> User:
        pass
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("src/handlers.py"))
	require.NoError(t, err)

	// Should have type refs for User and Order
	require.NotEmpty(t, summary.TypeRefs, "expected type refs to be extracted")

	// Collect type ref FQNs
	typeRefFQNs := make(map[string]struct{})
	for _, tr := range summary.TypeRefs {
		fqn := tr.TypeSym.Pkg + "." + tr.TypeSym.Name
		typeRefFQNs[fqn] = struct{}{}
	}

	// Should reference User and Order from myapp.models
	require.Contains(t, typeRefFQNs, "myapp.models.User", "expected User type ref")
	require.Contains(t, typeRefFQNs, "myapp.models.Order", "expected Order type ref")
}

func TestPythonIndexer_TypeRefs_ReturnType(t *testing.T) {
	code := []byte(`
from myapp.models import Result

def compute() -> Result:
    return Result()
`)
	idx := NewPythonIndexer()
	summary, err := idx.IndexFile(code, relCodeRef("src/compute.py"))
	require.NoError(t, err)

	require.NotEmpty(t, summary.TypeRefs, "expected type refs from return type")

	// Check that Result is captured
	found := false
	for _, tr := range summary.TypeRefs {
		if tr.TypeSym.Name == "Result" {
			found = true
			require.Equal(t, "myapp.models", tr.TypeSym.Pkg)
			require.Equal(t, "compute.compute", tr.OwnerFQN)
		}
	}
	require.True(t, found, "expected Result type ref")
}
