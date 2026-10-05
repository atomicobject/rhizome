//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestPython_CallEdgesFallback_ParseError(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)

	rel := filepath.ToSlash("src/todoapp/services/broken_callers.py")
	abs := ws.CodePath(rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(`
from todoapp.services.sync import (

def broken(task):
    push_updates(task)
`), 0o644))

	ws.IndexCodeAnchors(t, ctx)

	content, err := os.ReadFile(abs)
	require.NoError(t, err)
	require.NoError(t, ws.CodeAnchor.IndexCodeFile(ctx, codeanchor.LangPy, abs, content))

	store, err := codeanchorsqlite.Open(ws.DBPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	searcher := &semantic.Searcher{IntelStore: store}
	planner := searchplanner.Planner{
		Deps: searchplanner.Deps{
			Semantic:   searcher,
			IntelStore: store,
			VaultPath:  ws.VaultRoot,
			VaultDef:   ws.VaultDef,
			NoteReader: ws.NoteMgr,
		},
		Options: searchplanner.Options{
			EnableVector: false,
			EnableIntel:  false,
			EnableGraph:  false,
			EnableRefs:   false,
			MaxPerOwner:  10,
		},
	}

	root := filepath.ToSlash(ws.VaultRoot)
	assertContainsPath := func(t *testing.T, intent search.Intent) {
		t.Helper()
		spec := search.QuerySpec{
			Text:   "push_updates",
			Intent: intent,
			Limits: search.Limits{Total: 10},
		}
		plan, err := planner.Plan(ctx, spec)
		require.NoError(t, err)

		svc := &search.Service{Retrievers: plan.Retrievers, Ranker: plan.Ranker}
		resp, err := svc.Search(ctx, spec)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Results)

		paths := map[string]struct{}{}
		for _, r := range resp.Results {
			path := filepath.ToSlash(r.Path)
			if root != "" && strings.HasPrefix(path, root+"/") {
				path = strings.TrimPrefix(path, root+"/")
			}
			paths[path] = struct{}{}
		}
		require.Contains(t, paths, rel)
	}

	t.Run("find_usages", func(t *testing.T) {
		assertContainsPath(t, search.IntentFindUsages)
	})
	t.Run("callers", func(t *testing.T) {
		assertContainsPath(t, search.IntentCallers)
	})
}
