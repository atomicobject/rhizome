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

func TestInterfaceRootOverlayReportsCommittedCandidateLimit(t *testing.T) {
	env := meetingRootEnv(t)
	deps := env.deps(nil)
	deps.Store = &interfaceCappedStore{Store: env.store}
	deps.ReadOverlay = scalarParityOverlays(t, env)[0]
	prepared, errs := Prepare(env.execSchema, `{ meeting(first: 1) { date } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "overlay type listing exceeds 10000 committed candidates")
	require.Empty(t, result.Extensions)
}
