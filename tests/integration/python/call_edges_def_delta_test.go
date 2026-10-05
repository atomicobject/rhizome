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

func TestBatchIndex_DefOnlyChange_RebuildsImpactedCallers(t *testing.T) {
	ctx := codeanchor.WithBatchIndexing(context.Background())
	ws := fixture.NewWorkspace(t)

	callerRel := "src/todoapp/services/reverse/caller.py"
	calleeRel := "src/todoapp/services/reverse/callee.py"
	callerPath := ws.CodePath(callerRel)
	calleePath := ws.CodePath(calleeRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	initialCallee := "import typing\n\n\ndef existing():\n    return 1\n"
	caller := "from todoapp.services.reverse import callee\n\n\ndef run():\n    return callee.new_func()\n"
	writeFile(t, calleePath, initialCallee)
	writeFile(t, callerPath, caller)

	store, err := codeanchorsqlite.Open(filepath.Join(ws.CodeRoot, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	indexer := codeanchor.NewPythonIndexerWithRoots([]string{"src"})
	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{indexer},
		codeanchor.WithBasePath(ws.CodeRoot),
		codeanchor.WithWriteAccess(),
	)

	workCallee := mustBuildCodeWork(t, svc, ctx, codeanchor.LangPy, calleePath, initialCallee)
	workCaller := mustBuildCodeWork(t, svc, ctx, codeanchor.LangPy, callerPath, caller)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*workCallee, *workCaller}))

	updatedCallee := "import typing\n\n\ndef existing():\n    return 1\n\n\ndef new_func():\n    return 2\n"
	writeFile(t, calleePath, updatedCallee)
	updatedWork := mustBuildCodeWork(t, svc, ctx, codeanchor.LangPy, calleePath, updatedCallee)
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*updatedWork}))

	oldRefs := codeanchor.SymbolRefsFromSymbols(workCallee.Summary.Symbols)
	newRefs := codeanchor.SymbolRefsFromSymbols(updatedWork.Summary.Symbols)
	addedSyms, removedSyms := codeanchor.DiffSymbolRefs(oldRefs, newRefs)

	oldModules := codeanchor.ModulesFromDefs(codeanchor.BuildModuleDefRows(calleeRel, workCallee.Summary, indexer))
	newModules := codeanchor.ModulesFromDefs(codeanchor.BuildModuleDefRows(calleeRel, updatedWork.Summary, indexer))
	addedMods, removedMods := codeanchor.DiffModules(oldModules, newModules)

	deltas := codeanchor.DefDeltas{
		AddedSymbols:   addedSyms,
		RemovedSymbols: removedSyms,
		AddedModules:   addedMods,
		RemovedModules: removedMods,
	}

	_, err = svc.RebuildCallEdgesForDefDeltas(ctx, nil, deltas)
	require.NoError(t, err)

	pathsByCallee, err := store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "todoapp.services.reverse.callee",
		Name: "new_func",
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, string(paths.NormalizeCode(callerRel)))
}
