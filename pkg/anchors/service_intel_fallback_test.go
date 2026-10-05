//go:build cgo

package codeanchor_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

func TestService_IntelCallEdges_PythonFallbackWhenPkgMismatch(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "intel-call-fallback.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	summaries := map[string]codeanchor.FileSummary{
		"src/todoapp/services/sync.py": {
			FilePath: "src/todoapp/services/sync.py",
			Lang:     codeanchor.LangPy,
			Symbols: []codeanchor.Symbol{{
				Lang: codeanchor.LangPy,
				Kind: codeanchor.SymFunc,
				File: "src/todoapp/services/sync.py",
				Pkg:  "todoapp.services.sync",
				Name: "push_updates",
			}},
		},
		"src/todoapp/services/tasks.py": {
			FilePath: "src/todoapp/services/tasks.py",
			Lang:     codeanchor.LangPy,
			Symbols: []codeanchor.Symbol{{
				Lang: codeanchor.LangPy,
				Kind: codeanchor.SymFunc,
				File: "src/todoapp/services/tasks.py",
				Pkg:  "todoapp.services.tasks",
				Name: "add_task",
			}},
			Calls: []codeanchor.CallSite{{
				CalleeSymbol: codeanchor.SymbolRef{
					Lang: codeanchor.LangPy,
					Pkg:  "todoapp.services.tasks",
					Name: "push_updates",
				},
				OwnerFQN: "todoapp.services.tasks.add_task",
			}},
		},
	}

	svc := codeanchor.NewServiceWithOptions(
		store,
		[]codeanchor.LanguageIndexer{&summaryIndexer{lang: codeanchor.LangPy, summaries: summaries}},
		codeanchor.WithBasePath(tmp),
		codeanchor.WithWriteAccess(),
	)

	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, filepath.Join(tmp, filepath.FromSlash("src/todoapp/services/sync.py")), nil))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangPy, filepath.Join(tmp, filepath.FromSlash("src/todoapp/services/tasks.py")), nil))

	anchors, err := store.IntelAnchorsBySymbol(ctx, "push_updates", 5)
	require.NoError(t, err)
	require.NotEmpty(t, anchors)

	var calleeID string
	for _, a := range anchors {
		if a.FQN == "todoapp.services.sync.push_updates" {
			calleeID = a.AnchorID
			break
		}
	}
	require.NotEmpty(t, calleeID)

	callers, err := store.CallerAnchorsByCalleeIDs(ctx, []string{calleeID}, 10)
	require.NoError(t, err)

	found := false
	for _, a := range callers[calleeID] {
		if a.Path == "src/todoapp/services/tasks.py" {
			found = true
			break
		}
	}
	require.True(t, found, "expected fallback call edge from tasks.py to push_updates")
}
