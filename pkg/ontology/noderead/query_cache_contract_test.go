package noderead

import (
	"context"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func relationCacheFixture(t *testing.T) (*Scope, *queryRelationBatchStore) {
	t.Helper()
	scope := neighborhoodCatalogScope(t, nil, []semdb.OntologyEdgeRow{
		{SrcPath: "notes/source.md", RelationName: "owner", DstPath: "notes/owner.md", DstType: "Person", Structural: true},
		{SrcPath: "notes/source.md", RelationName: "related", DstPath: "notes/a.md", DstType: "Decision", Provenance: "body_link"},
		{SrcPath: "notes/source.md", RelationName: "related", DstPath: "notes/b.md", DstType: "Decision", Provenance: "body_link"},
		{SrcPath: "notes/source.md", RelationName: "related", DstPath: "notes/c.md", DstType: "Person", Provenance: "backlink"},
		{SrcPath: "notes/source.md", RelationName: "other", DstPath: "notes/d.md", DstType: "Decision", Provenance: "body_link"},
	})
	store := &queryRelationBatchStore{ReadStore: scope.service.Store}
	scope.service.Store = store
	return scope, store
}

func TestScopeRelationCacheSharesConcurrentReadsAndEmptyGroups(t *testing.T) {
	for _, structural := range []bool{true, false} {
		name, relation := "ambient", "related"
		if structural {
			name, relation = "structural", "owner"
		}
		t.Run(name, func(t *testing.T) {
			scope, store := relationCacheFixture(t)
			request := NeighborhoodRequest{Sources: queryRelationRefs([]string{"notes/source.md", "notes/empty.md"}), RelationNames: []string{relation}, IncludeStructural: structural, IncludeAmbient: !structural}
			load := func() (NeighborhoodResult, error) { return scope.Edges(context.Background(), request) }
			type outcome struct {
				result NeighborhoodResult
				err    error
			}
			results := make(chan outcome, 16)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for range cap(results) {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					result, err := load()
					results <- outcome{result, err}
				}()
			}
			close(start)
			workers.Wait()
			close(results)
			for output := range results {
				require.NoError(t, output.err)
				require.Contains(t, output.result.BySource, "notes/empty.md")
				require.Empty(t, output.result.BySource["notes/empty.md"].Edges)
				require.NotEmpty(t, output.result.BySource["notes/source.md"].Edges)
				require.NotEqual(t, "mutated.md", output.result.Edges[0].Target.NotePath)
				require.NotEqual(t, "mutated.md", output.result.Edges[0].Edge.DstPath)
				output.result.Edges[0].Target.NotePath = "mutated.md"
				output.result.Edges[0].Edge.DstPath = "mutated.md"
			}
			result, err := load()
			require.NoError(t, err)
			require.NotEqual(t, "mutated.md", result.Edges[0].Target.NotePath)
			require.NotEqual(t, "mutated.md", result.Edges[0].Edge.DstPath)
			require.Equal(t, 1, store.calls)
		})
	}
}

func TestScopeRelationCacheKeepsFiltersAndLimitsIndependent(t *testing.T) {
	scope, _ := relationCacheFixture(t)
	for _, test := range []struct {
		relation, provenance, targetType string
		limit, count                     int
	}{
		{"related", "body_link", "Decision", 0, 2},
		{"related", "body_link", "Decision", 1, 1},
		{"related", "body_link", "Decision", 2, 2},
		{"related", "backlink", "Decision", 1, 0},
		{"related", "backlink", "Decision", 0, 0},
		{"related", "backlink", "Person", 1, 1},
		{"other", "body_link", "Decision", 1, 1},
	} {
		result, err := scope.Edges(context.Background(), NeighborhoodRequest{
			Sources: queryRelationRefs([]string{"notes/source.md"}), RelationNames: []string{test.relation},
			Provenance: map[string]struct{}{test.provenance: {}}, TargetTypes: []string{test.targetType},
			IncludeAmbient: true, FirstPerSource: test.limit,
		})
		require.NoError(t, err)
		require.Len(t, result.BySource["notes/source.md"].Edges, test.count)
	}
	result, err := scope.Edges(context.Background(), NeighborhoodRequest{Sources: queryRelationRefs([]string{"notes/source.md"}), RelationNames: []string{"owner"}, IncludeStructural: true})
	require.NoError(t, err)
	require.Equal(t, "notes/owner.md", result.Edges[0].Target.NotePath)
}

func TestScopeRelationCacheRetriesCanceledStoreReads(t *testing.T) {
	scope, store := relationCacheFixture(t)
	store.cancelNext = true
	request := NeighborhoodRequest{Sources: queryRelationRefs([]string{"notes/source.md"}), RelationNames: []string{"owner"}, IncludeStructural: true}
	_, err := scope.Edges(context.Background(), request)
	require.ErrorIs(t, err, context.Canceled)
	for range 2 {
		result, err := scope.Edges(context.Background(), request)
		require.NoError(t, err)
		require.Equal(t, "notes/owner.md", result.Edges[0].Target.NotePath)
	}
	require.Equal(t, 2, store.calls)
}

func queryRelationRefs(paths []string) []ontology.NodeRef {
	refs := make([]ontology.NodeRef, len(paths))
	for i, path := range paths {
		refs[i] = ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote}
	}
	return refs
}

type queryRelationBatchStore struct {
	ReadStore
	calls      int
	cancelNext bool
	sources    [][]readmodel.EdgeEndpoint
}

func (s *queryRelationBatchStore) OntologyNeighborhood(ctx context.Context, query readmodel.NeighborhoodQuery) ([]semdb.OntologyEdgeRow, error) {
	s.calls++
	s.sources = append(s.sources, append([]readmodel.EdgeEndpoint(nil), query.Sources...))
	if s.cancelNext {
		s.cancelNext = false
		return nil, context.Canceled
	}
	return s.ReadStore.OntologyNeighborhood(ctx, query)
}
