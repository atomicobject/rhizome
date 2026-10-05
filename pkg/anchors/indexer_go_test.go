package codeanchor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func codeRef(t *testing.T, root, absPath string) paths.CodePathRef {
	t.Helper()
	rootPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	rel, err := rootPaths.RelCodeStrict(absPath)
	require.NoError(t, err)
	return paths.CodePathRef{Rel: rel, Abs: paths.ResolveSymlinks(absPath)}
}

func TestGoIndexer_SymbolsAndCalls_WithModuleImportPath(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	depDir := filepath.Join(root, "dep")
	require.NoError(t, os.MkdirAll(depDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(depDir, "dep.go"), []byte(`
package dep

type Service struct{}

func Target() {}

func (s *Service) Method() {}
`), 0o644))

	mainDir := filepath.Join(root, "main")
	require.NoError(t, os.MkdirAll(mainDir, 0o755))
	mainPath := filepath.Join(mainDir, "main.go")
	code := []byte(`
package main

import d "example.com/mod/dep"

func main() {
	d.Target()
}
`)
	require.NoError(t, os.WriteFile(mainPath, code, 0o644))

	idx := NewGoIndexer()
	ref := codeRef(t, root, mainPath)
	summary, err := idx.IndexFile(code, ref)
	require.NoError(t, err)

	require.Equal(t, LangGo, summary.Lang)
	require.Equal(t, ref.Rel.String(), summary.FilePath)

	// Local symbol main should resolve to current import path: example.com/mod/main
	require.Len(t, summary.Symbols, 1)
	require.Equal(t, "example.com/mod/main", summary.Symbols[0].Pkg)
	require.Equal(t, "main", summary.Symbols[0].Name)
	require.Greater(t, summary.Symbols[0].StartLine, int64(0))
	require.Greater(t, summary.Symbols[0].EndLine, int64(0))
	require.NotEmpty(t, summary.Symbols[0].Signature)

	// Call to imported package function should resolve to dep import path.
	require.Len(t, summary.Calls, 1)
	require.Equal(t, SymbolRef{Lang: LangGo, Pkg: "example.com/mod/dep", Name: "Target"}, summary.Calls[0].CalleeSymbol)
}

func TestGoIndexer_UnwrapsGenericInstantiation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	depDir := filepath.Join(root, "dep")
	require.NoError(t, os.MkdirAll(depDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(depDir, "dep.go"), []byte(`
package dep

func Generic[T any]() {}
`), 0o644))

	mainDir := filepath.Join(root, "main")
	require.NoError(t, os.MkdirAll(mainDir, 0o755))
	mainPath := filepath.Join(mainDir, "main.go")
	code := []byte(`
package main

import "example.com/mod/dep"

func main() {
	dep.Generic[int]()
}
`)

	idx := NewGoIndexer()
	summary, err := idx.IndexFile(code, codeRef(t, root, mainPath))
	require.NoError(t, err)

	require.Len(t, summary.Calls, 1)
	require.Equal(t, SymbolRef{Lang: LangGo, Pkg: "example.com/mod/dep", Name: "Generic"}, summary.Calls[0].CalleeSymbol)
}

func TestGoIndexer_IndexesCompositeLiteralsAsCalls(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	depDir := filepath.Join(root, "dep")
	require.NoError(t, os.MkdirAll(depDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(depDir, "dep.go"), []byte(`
package dep

type Service struct{}
`), 0o644))

	mainDir := filepath.Join(root, "main")
	require.NoError(t, os.MkdirAll(mainDir, 0o755))
	mainPath := filepath.Join(mainDir, "main.go")
	code := []byte(`
package main

import "example.com/mod/dep"

func main() {
	_ = dep.Service{}
}
`)

	idx := NewGoIndexer()
	summary, err := idx.IndexFile(code, codeRef(t, root, mainPath))
	require.NoError(t, err)

	require.NotEmpty(t, summary.Calls)
	ref := codeRef(t, root, mainPath)
	requireCallContains(t, summary.Calls, ref.Rel.String(), SymbolRef{Lang: LangGo, Pkg: "example.com/mod/dep", Name: "Service"})
}

func TestGoIndexer_SkipsBuiltinsAndConversions(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	mainDir := filepath.Join(root, "main")
	require.NoError(t, os.MkdirAll(mainDir, 0o755))
	mainPath := filepath.Join(mainDir, "main.go")
	code := []byte(`
package main

func Foo() {}

func main() {
	s := []int{1, 2, 3}
	_ = len(s)
	_ = make([]int, 0)
	_ = int(123)
	min(1, 2)
	max(1, 2)
	clear(s)
	Foo()
}
`)
	require.NoError(t, os.WriteFile(mainPath, code, 0o644))

	idx := NewGoIndexer()
	summary, err := idx.IndexFile(code, codeRef(t, root, mainPath))
	require.NoError(t, err)

	ref := codeRef(t, root, mainPath)
	requireCallContains(t, summary.Calls, ref.Rel.String(), SymbolRef{Lang: LangGo, Pkg: "example.com/mod/main", Name: "Foo"})

	bad := map[string]bool{
		"len":   true,
		"make":  true,
		"int":   true,
		"min":   true,
		"max":   true,
		"clear": true,
	}
	for _, cs := range summary.Calls {
		require.False(t, bad[cs.CalleeSymbol.Name], "should not index builtins/conversions as calls: %s", cs.CalleeSymbol.Name)
	}
}

func requireCallContains(t *testing.T, calls []CallSite, file string, callee SymbolRef) {
	t.Helper()
	for _, cs := range calls {
		if cs.File == file && cs.CalleeSymbol == callee {
			return
		}
	}
	require.Failf(t, "expected call not found", "file=%s callee=%+v calls=%+v", file, callee, calls)
}

func TestGoIndexer_InterfaceEmbedding(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	mainDir := filepath.Join(root, "ports")
	require.NoError(t, os.MkdirAll(mainDir, 0o755))
	mainPath := filepath.Join(mainDir, "store.go")
	code := []byte(`
package ports

import "io"

type ReadWriteCloser interface {
	io.Reader
	io.Writer
	Close() error
}

type LocalReader interface {
	Reader
	ReadAll() []byte
}

type Reader interface {
	Read(p []byte) (n int, err error)
}
`)
	require.NoError(t, os.WriteFile(mainPath, code, 0o644))

	idx := NewGoIndexer()
	summary, err := idx.IndexFile(code, codeRef(t, root, mainPath))
	require.NoError(t, err)

	// Should have 3 interfaces
	require.Len(t, summary.Symbols, 3)
	for _, sym := range summary.Symbols {
		require.Equal(t, SymInterface, sym.Kind)
	}

	// ReadWriteCloser embeds io.Reader and io.Writer
	require.Len(t, summary.Supers, 3)
	superMap := make(map[string]string)
	for _, s := range summary.Supers {
		superMap[s.ChildFQN+"->"+s.ParentFQN] = s.ParentFQN
	}
	require.Contains(t, superMap, "example.com/mod/ports.ReadWriteCloser->io.Reader")
	require.Contains(t, superMap, "example.com/mod/ports.ReadWriteCloser->io.Writer")
	// LocalReader embeds local Reader interface
	require.Contains(t, superMap, "example.com/mod/ports.LocalReader->example.com/mod/ports.Reader")
}

func TestGoIndexer_StructEmbedding(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	baseDir := filepath.Join(root, "base")
	require.NoError(t, os.MkdirAll(baseDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "base.go"), []byte(`
package base

type BaseService struct{}
type Handler struct{}
`), 0o644))

	svcDir := filepath.Join(root, "svc")
	require.NoError(t, os.MkdirAll(svcDir, 0o755))
	svcPath := filepath.Join(svcDir, "service.go")
	code := []byte(`
package svc

import "example.com/mod/base"

type Service struct {
	*base.BaseService
	base.Handler
	name string
}

type LocalService struct {
	Service
}
`)
	require.NoError(t, os.WriteFile(svcPath, code, 0o644))

	idx := NewGoIndexer()
	summary, err := idx.IndexFile(code, codeRef(t, root, svcPath))
	require.NoError(t, err)

	// Should have 2 structs (Service and LocalService)
	structCount := 0
	for _, sym := range summary.Symbols {
		if sym.Kind == SymStruct {
			structCount++
		}
	}
	require.Equal(t, 2, structCount)

	// Service embeds *base.BaseService and base.Handler
	// LocalService embeds Service
	require.Len(t, summary.Supers, 3)
	superMap := make(map[string]string)
	for _, s := range summary.Supers {
		superMap[s.ChildFQN+"->"+s.ParentFQN] = s.ParentFQN
	}
	require.Contains(t, superMap, "example.com/mod/svc.Service->example.com/mod/base.BaseService")
	require.Contains(t, superMap, "example.com/mod/svc.Service->example.com/mod/base.Handler")
	require.Contains(t, superMap, "example.com/mod/svc.LocalService->example.com/mod/svc.Service")
}

func TestGoIndexer_StructFields(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.24\n"), 0o644))

	modelDir := filepath.Join(root, "models")
	require.NoError(t, os.MkdirAll(modelDir, 0o755))
	modelPath := filepath.Join(modelDir, "task.go")
	code := []byte(`
package models

type Task struct {
	ID        string
	Title     string
	Count     int
	isPrivate bool
	internal  string
}
`)
	require.NoError(t, os.WriteFile(modelPath, code, 0o644))

	idx := NewGoIndexer()
	summary, err := idx.IndexFile(code, codeRef(t, root, modelPath))
	require.NoError(t, err)

	// Should have: Task struct + 3 exported fields (ID, Title, Count)
	// isPrivate and internal are unexported, so they should be skipped.
	require.Len(t, summary.Symbols, 4)

	fqns := make(map[string]Symbol)
	for _, s := range summary.Symbols {
		fqns[s.FQN] = s
	}

	// Task struct itself
	require.Contains(t, fqns, "example.com/mod/models.Task")
	require.Equal(t, SymStruct, fqns["example.com/mod/models.Task"].Kind)

	// Exported fields
	require.Contains(t, fqns, "example.com/mod/models.Task.ID")
	require.Equal(t, SymField, fqns["example.com/mod/models.Task.ID"].Kind)
	require.Equal(t, "ID string", fqns["example.com/mod/models.Task.ID"].Signature)

	require.Contains(t, fqns, "example.com/mod/models.Task.Title")
	require.Equal(t, SymField, fqns["example.com/mod/models.Task.Title"].Kind)

	require.Contains(t, fqns, "example.com/mod/models.Task.Count")
	require.Equal(t, SymField, fqns["example.com/mod/models.Task.Count"].Kind)

	// Unexported fields should NOT be included
	require.NotContains(t, fqns, "example.com/mod/models.Task.isPrivate")
	require.NotContains(t, fqns, "example.com/mod/models.Task.internal")
}

func TestGoIndexer_ReverseIndexFallbacks_MethodNames(t *testing.T) {
	idx := &GoIndexer{}
	deltas := DefDeltas{
		AddedSymbols: []SymbolRef{
			{Lang: LangGo, Pkg: "github.com/example/project.Foo", Name: "Do"},
			{Lang: LangGo, Pkg: "github.com/example/project", Name: "TopLevel"},
		},
	}
	fallbacks := idx.ReverseIndexFallbacks(deltas)

	var names []string
	for _, fb := range fallbacks {
		names = append(names, fb.Ref.Name)
	}
	require.Contains(t, names, "Do")
	require.NotContains(t, names, "TopLevel")
}
