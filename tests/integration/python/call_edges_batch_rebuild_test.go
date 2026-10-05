//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestBatchIndex_RebuildsCallEdgesForUnchangedCallers(t *testing.T) {
	ctx := codeanchor.WithBatchIndexing(context.Background())
	ws := fixture.NewWorkspace(t)

	callerRel := "src/todoapp/services/stale/caller.py"
	calleeRel := "src/todoapp/services/stale/callee.py"
	callerPath := ws.CodePath(callerRel)
	calleePath := ws.CodePath(calleeRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	initialCallee := "import typing\n\n\ndef existing():\n    return 1\n"
	caller := "from todoapp.services.stale import callee\n\n\ndef run():\n    return callee.new_func()\n"
	writeFile(t, calleePath, initialCallee)
	writeFile(t, callerPath, caller)

	store, err := codeanchorsqlite.Open(filepath.Join(ws.CodeRoot, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexerWithRoots([]string{"src"})},
		codeanchor.WithBasePath(ws.CodeRoot),
		codeanchor.WithWriteAccess(),
	)

	workCallee := mustBuildCodeWork(t, svc, ctx, codeanchor.LangPy, calleePath, initialCallee)
	workCaller := mustBuildCodeWork(t, svc, ctx, codeanchor.LangPy, callerPath, caller)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*workCallee, *workCaller}))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{calleePath, callerPath}))

	updatedCallee := "import typing\n\n\ndef existing():\n    return 1\n\n\ndef new_func():\n    return 2\n"
	writeFile(t, calleePath, updatedCallee)
	workCallee = mustBuildCodeWork(t, svc, ctx, codeanchor.LangPy, calleePath, updatedCallee)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*workCallee}))

	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{calleePath, callerPath}))

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "todoapp.services.stale.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, string(paths.NormalizeCode(callerRel)))
}

func TestBatchIndex_RebuildsCallEdgesAfterCallerChange(t *testing.T) {
	ctx := codeanchor.WithBatchIndexing(context.Background())
	ws := fixture.NewWorkspace(t)

	callerRel := "src/todoapp/services/remove/caller.py"
	calleeRel := "src/todoapp/services/remove/callee.py"
	callerPath := ws.CodePath(callerRel)
	calleePath := ws.CodePath(calleeRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	callee := "def target():\n    return 1\n"
	caller := "from todoapp.services.remove import callee\n\n\ndef run():\n    return callee.target()\n"
	writeFile(t, calleePath, callee)
	writeFile(t, callerPath, caller)

	store, err := codeanchorsqlite.Open(filepath.Join(ws.CodeRoot, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{codeanchor.NewPythonIndexerWithRoots([]string{"src"})},
		codeanchor.WithBasePath(ws.CodeRoot),
		codeanchor.WithWriteAccess(),
	)

	workCallee := mustBuildCodeWork(t, svc, ctx, codeanchor.LangPy, calleePath, callee)
	workCaller := mustBuildCodeWork(t, svc, ctx, codeanchor.LangPy, callerPath, caller)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*workCallee, *workCaller}))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerPath}))

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "todoapp.services.remove.callee",
		Name: "target",
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, string(paths.NormalizeCode(callerRel)))

	updatedCaller := "def run():\n    return 1\n"
	writeFile(t, callerPath, updatedCaller)
	workCaller = mustBuildCodeWork(t, svc, ctx, codeanchor.LangPy, callerPath, updatedCaller)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*workCaller}))
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerPath}))

	pathsByCallee, err = store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "todoapp.services.remove.callee",
		Name: "target",
	})
	require.NoError(t, err)
	require.NotContains(t, pathsByCallee, string(paths.NormalizeCode(callerRel)))
}

func mustBuildCodeWork(t *testing.T, svc *codeanchor.Service, ctx context.Context, lang codeanchor.Lang, path string, content string) *codeanchor.CodeIndexWork {
	t.Helper()
	work, err := svc.BuildCodeIndexWork(ctx, lang, path, []byte(content))
	require.NoError(t, err)
	require.NotNil(t, work)
	return work
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
