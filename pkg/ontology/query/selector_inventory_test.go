package query

import (
	"context"
	"errors"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

type selectorInventoryStore struct {
	*semdb.Store
	inventoryReads int
	failNext       bool
}

func (s *selectorInventoryStore) CurrentNoteMetadataRows(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	s.inventoryReads++
	if s.failNext {
		s.failNext = false
		return nil, errors.New("temporary inventory failure")
	}
	return s.Store.CurrentNoteMetadataRows(ctx)
}

func selectorInventoryEnv(t *testing.T) *queryTestEnv {
	return newCustomQueryTestEnv(t, `
interface SummaryDoc { name: String }
type Project implements SummaryDoc @node(paths: ["notes/projects/*.md"]) { name: String }
type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) { name: String }
type Alert @node(paths: ["notes/alerts/*.md"]) { name: String }
`, map[string]string{
		"notes/alerts/alpha.md":    "---\ntype: Alert\nname: Alpha\n---\n",
		"notes/decisions/alpha.md": "---\ntype: Decision\nname: Alpha\n---\n",
		"notes/projects/alpha.md":  "---\ntype: Project\nname: Alpha\n---\n",
		"notes/projects/beta.md":   "---\ntype: Project\nname: Beta\n---\n",
	})
}

func TestSelectorInventorySharesSuccessfulReadAndRefreshesNextExecution(t *testing.T) {
	env := selectorInventoryEnv(t)
	prepared, errs := Prepare(env.execSchema, `{
  typed: project(find: "notes", first: 1) { path }
  summary: notes(type: "SummaryDoc", find: "notes", first: 1) {
   nodes { path }
   pageInfo { truncated returnedCount }
  }
  allSummary: notes(type: "SummaryDoc", first: 1) { nodes { path } }
 }`)
	require.Empty(t, errs)
	store := &selectorInventoryStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = store
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{map[string]any{"path": "notes/projects/alpha.md"}}, result.Data["typed"])
	require.Equal(t, map[string]any{
		"nodes":    []any{map[string]any{"path": "notes/decisions/alpha.md"}},
		"pageInfo": map[string]any{"truncated": true, "returnedCount": 1},
	}, result.Data["summary"])
	require.Equal(t, map[string]any{"nodes": []any{map[string]any{"path": "notes/decisions/alpha.md"}}}, result.Data["allSummary"])
	require.Equal(t, 1, store.inventoryReads)

	require.NoError(t, env.store.ApplyNoteMetadataDelta(context.Background(), semdb.NoteMetadataDelta{
		State:        semdb.NoteMetadataState{LoadedAt: 99, Ready: true},
		DeletedPaths: []string{"notes/decisions/alpha.md", "notes/projects/alpha.md"},
	}))
	result = Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{map[string]any{"path": "notes/projects/beta.md"}}, result.Data["typed"])
	require.Equal(t, map[string]any{
		"nodes":    []any{map[string]any{"path": "notes/projects/beta.md"}},
		"pageInfo": map[string]any{"truncated": false, "returnedCount": 1},
	}, result.Data["summary"])
	require.Equal(t, 2, store.inventoryReads)
}

func TestSelectorInventoryDoesNotRememberReadFailure(t *testing.T) {
	env := selectorInventoryEnv(t)
	prepared, errs := Prepare(env.execSchema, `{
  failed: project(find: "notes", first: 1) { path }
  recovered: project(find: "notes", first: 1) { path }
 }`)
	require.Empty(t, errs)
	store := &selectorInventoryStore{Store: env.store, failNext: true}
	deps := env.deps(nil)
	deps.Store = store
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "temporary inventory failure")
	require.Equal(t, []any{map[string]any{"path": "notes/projects/alpha.md"}}, result.Data["recovered"])
	require.Equal(t, 2, store.inventoryReads)
}

func TestSelectorInventoryHonorsCancellationAfterSuccessfulRead(t *testing.T) {
	env := selectorInventoryEnv(t)
	store := &selectorInventoryStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = store
	loaders, err := newLoaders(deps, env.schema)
	require.NoError(t, err)
	rows, err := loaders.selectorMetadataRows(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = loaders.selectorMetadataRows(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, store.inventoryReads)
}

type selectorTypeErrorStore struct{ *semdb.Store }

func (s *selectorTypeErrorStore) OntologyTypesByPaths(ctx context.Context, paths []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	for _, path := range paths {
		if path == "notes/projects/beta.md" {
			return nil, errors.New("later candidate type failure")
		}
	}
	return s.Store.OntologyTypesByPaths(ctx, paths)
}

func TestSelectorInventoryPreservesTypeErrorsBeyondFirstPage(t *testing.T) {
	env := selectorInventoryEnv(t)
	prepared, errs := Prepare(env.execSchema, `{ project(find: "notes", first: 1) { path } }`)
	require.Empty(t, errs)
	deps := env.deps(nil)
	deps.Store = &selectorTypeErrorStore{Store: env.store}
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "later candidate type failure")
}

type changingSelectorStore struct {
	*semdb.Store
	calls int
}

func (s *changingSelectorStore) CurrentNoteMetadataRows(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	s.calls++
	rows, err := s.Store.CurrentNoteMetadataRows(ctx)
	if s.calls > 1 {
		return nil, err
	}
	return rows, err
}

func TestSelectorInventoryKeepsOneSuccessfulSnapshotWithinExecution(t *testing.T) {
	env := selectorInventoryEnv(t)
	prepared, errs := Prepare(env.execSchema, `{
  first: project(find: "notes", first: 1) { path }
  second: project(find: "notes", first: 1) { path }
 }`)
	require.Empty(t, errs)
	deps := env.deps(nil)
	store := &changingSelectorStore{Store: env.store}
	deps.Store = store
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.NotEmpty(t, result.Data["first"])
	require.Equal(t, result.Data["first"], result.Data["second"])
	result = Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Empty(t, result.Data["first"])
	require.Empty(t, result.Data["second"])
}
