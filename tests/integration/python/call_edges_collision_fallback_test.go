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

func TestBatchIndex_ParseFallbackAndNoCrossLangCollision(t *testing.T) {
	ctx := codeanchor.WithBatchIndexing(context.Background())
	ws := fixture.NewWorkspace(t)

	calleeRel := "src/todoapp/services/collision/callee.py"
	callerRel := "src/todoapp/services/collision/caller.py"
	calleePath := ws.CodePath(calleeRel)
	callerPath := ws.CodePath(callerRel)
	require.NoError(t, os.MkdirAll(filepath.Dir(callerPath), 0o755))

	callee := "def target():\n    return 1\n"
	caller := "from todoapp.services.collision import callee\n\n\ndef run():\n    return callee.target()\n"
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
		Pkg:  "todoapp.services.collision.callee",
		Name: "target",
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, string(paths.NormalizeCode(callerRel)))

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"src/goapp/caller.go": {{
			SrcPath:  "src/goapp/caller.go",
			OwnerFQN: "",
			RefKind:  codeanchor.RefKindCalls,
			DstLang:  codeanchor.LangGo,
			DstPkg:   "goapp",
			DstName:  "target",
			DstFQN:   "goapp.target",
		}},
	}))

	deltas := codeanchor.DefDeltas{
		AddedSymbols: []codeanchor.SymbolRef{{
			Lang: codeanchor.LangPy,
			Pkg:  "todoapp.services.collision.callee",
			Name: "target",
		}},
	}
	summary, err := svc.RebuildCallEdgesForDefDeltas(ctx, nil, deltas)
	require.NoError(t, err)
	require.Equal(t, 1, summary.ImpactedCallers)

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		string(paths.NormalizeCode(callerRel)): {},
	}))
	require.NoError(t, store.ReplaceIntelImportRefsForPathsBatch(ctx, map[string][]codeanchor.ImportRefRow{
		string(paths.NormalizeCode(callerRel)): {},
	}))
	require.NoError(t, store.ReplaceIntelModuleDefsForPathsBatch(ctx, map[string][]codeanchor.ModuleDefRow{
		string(paths.NormalizeCode(callerRel)): {},
	}))

	badCaller := "from todoapp.services.collision import (\n\n"
	writeFile(t, callerPath, badCaller)
	require.NoError(t, svc.RebuildCallEdgesForPaths(ctx, []string{callerRel}))

	pathsByCallee, err = store.CallFilesByCallee(ctx, &codeanchor.SymbolRef{
		Lang: codeanchor.LangPy,
		Pkg:  "todoapp.services.collision.callee",
		Name: "target",
	})
	require.NoError(t, err)
	require.Contains(t, pathsByCallee, string(paths.NormalizeCode(callerRel)))
}
