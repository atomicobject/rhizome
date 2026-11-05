package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

type boundedInterfaceOverlayStore struct {
	*semdb.Store
	plans []readmodel.NodeQueryPlan
}

func (s *boundedInterfaceOverlayStore) OntologyPathsByType(ctx context.Context, name string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("unbounded committed read for %s", name)
	}
	return s.Store.OntologyPathsByType(ctx, name, limit)
}

func (s *boundedInterfaceOverlayStore) OntologyNodesByTypePlan(ctx context.Context, plan readmodel.NodeQueryPlan) ([]readmodel.NodeRow, error) {
	s.plans = append(s.plans, plan)
	return s.Store.OntologyNodesByTypePlan(ctx, plan)
}

func TestInterfaceRootOverlayKeepsCommittedReadBounded(t *testing.T) {
	env := meetingRootEnv(t)
	store := &boundedInterfaceOverlayStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = store
	deps.ReadOverlay = &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
		"coaching/0.md": "---\nsessiondate: 2026-02-01\ncustomer: Acme\n---\n# Staged meeting\n",
	}}
	prepared, errs := Prepare(env.execSchema, `{ meeting(first: 1) { date ... on NoteNode { path } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{map[string]any{"date": "2026-02-01", "path": "coaching/0.md"}}, result.Data["meeting"])
	require.NotEmpty(t, store.plans)
	for _, plan := range store.plans {
		require.Positive(t, plan.Limit)
		require.LessOrEqual(t, plan.Limit, 10001)
		require.Equal(t, []string{"CoachingSession", "GeneralMeeting"}, plan.TypeNames)
	}
}

func TestInterfaceRootPublicCandidateLimits(t *testing.T) {
	notes := make(map[string]string, 10001)
	for i := 0; i < 10001; i++ {
		path := fmt.Sprintf("general/%05d.md", i)
		body := "---\ndate: 2026-01-01\nclient: Acme\n---\n"
		if i%2 != 0 {
			path = fmt.Sprintf("coaching/%05d.md", i)
			body = "---\nsessiondate: 2026-01-01\ncustomer: Acme\n---\n"
		}
		notes[path] = body
	}
	env := newCustomQueryTestEnv(t, meetingRootSchema, notes)
	result := executeMeetingRoot(t, env, `{ meeting(first: 1) { date ... on NoteNode { path } } }`)
	require.Equal(t, []any{map[string]any{"date": "2026-01-01", "path": "coaching/00001.md"}}, result.Data["meeting"])
	require.Equal(t, "capped", result.Extensions["typedRoots"].(map[string]typedRootCoverage)["meeting"].Status)

	t.Run("residual candidate window reports uncertainty", func(t *testing.T) {
		result := executeMeetingRoot(t, env, `{ meeting(first: 1, filters: [{field: "clientRef", op: contains, value: "absent"}]) { date } }`)
		require.Empty(t, result.Data["meeting"])
		page := result.Extensions["typedRoots"].(map[string]typedRootCoverage)["meeting"]
		require.Equal(t, "unknown_candidate_bound", page.Status)
		require.Nil(t, page.HasMore)
		require.Nil(t, page.NextOffset)
	})
	t.Run("overlay rejects excessive committed candidates", func(t *testing.T) {
		deps := env.deps(nil)
		deps.ReadOverlay = &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"coaching/new.md": "---\nsessiondate: 2026-02-01\n---\n",
		}}
		prepared, errs := Prepare(env.execSchema, `{ meeting(first: 1) { date } }`)
		require.Empty(t, errs)
		result := Execute(context.Background(), deps, env.schema, prepared)
		require.NotEmpty(t, result.Errors)
		require.Contains(t, result.Errors[0].Message, "overlay type listing exceeds 10000 committed candidates")
		require.Empty(t, result.Extensions)
	})
}
