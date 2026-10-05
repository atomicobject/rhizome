//go:build integration
// +build integration

package integration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestSearchIntents_P3(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

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
			EnableIntel:  true,
			EnableGraph:  false,
			EnableRefs:   true,
			MaxPerOwner:  1,
		},
	}

	anchors, err := store.IntelAnchorsBySymbol(ctx, "push_updates", 5)
	require.NoError(t, err)
	require.NotEmpty(t, anchors)
	defID := anchors[0].AnchorID
	require.NotEmpty(t, defID)
	root := filepath.ToSlash(ws.VaultRoot)

	t.Run("go_to_def_top1", func(t *testing.T) {
		spec := search.QuerySpec{
			Text:   "push_updates",
			Intent: search.IntentGoToDef,
			Limits: search.Limits{Total: 5},
		}
		plan, err := planner.Plan(ctx, spec)
		require.NoError(t, err)

		svc := &search.Service{Retrievers: plan.Retrievers, Ranker: plan.Ranker}
		resp, err := svc.Search(ctx, spec)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Results)
		require.Equal(t, defID, resp.Results[0].AnchorID)
	})

	t.Run("find_usages_top5", func(t *testing.T) {
		spec := search.QuerySpec{
			Text:   "push_updates",
			Intent: search.IntentFindUsages,
			Limits: search.Limits{Total: 5},
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
		require.Contains(t, paths, "src/todoapp/services/tasks.py")
		require.Contains(t, paths, "scripts/worker.py")
	})

	t.Run("tests_for_code_top5", func(t *testing.T) {
		spec := search.QuerySpec{
			Seeds:  []knowledge.Handle{knowledge.FileHandle("src/todoapp/services/tasks.py")},
			Intent: search.IntentTestsForCode,
			Limits: search.Limits{Total: 5},
		}
		plan, err := planner.Plan(ctx, spec)
		require.NoError(t, err)

		svc := &search.Service{Retrievers: plan.Retrievers, Ranker: plan.Ranker}
		resp, err := svc.Search(ctx, spec)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Results)

		found := false
		for _, r := range resp.Results {
			path := filepath.ToSlash(r.Path)
			if root != "" && strings.HasPrefix(path, root+"/") {
				path = strings.TrimPrefix(path, root+"/")
			}
			if path == "src/todoapp/services/tasks_test.py" {
				found = true
				break
			}
		}
		require.True(t, found, "expected tests_for_code to return tasks_test.py")
	})

	t.Run("callers_top5", func(t *testing.T) {
		spec := search.QuerySpec{
			Text:   "push_updates",
			Intent: search.IntentCallers,
			Limits: search.Limits{Total: 5},
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
		require.Contains(t, paths, "src/todoapp/services/tasks.py")
		require.Contains(t, paths, "scripts/worker.py")
	})

	t.Run("callees_top5", func(t *testing.T) {
		spec := search.QuerySpec{
			Text:   "add_task",
			Intent: search.IntentCallees,
			Limits: search.Limits{Total: 5},
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
		require.Contains(t, paths, "src/todoapp/services/sync.py")
	})
}
