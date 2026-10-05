package query

import (
	"context"
	"sync"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func relationCacheFixture(t *testing.T) (*loaders, *relationBatchSpyStore) {
	t.Helper()
	env := newCustomQueryTestEnv(t, `type Target @node(paths: ["notes/*.md"]) { name: String }`, nil)
	require.NoError(t, env.store.ReplaceOntologySnapshot(context.Background(), semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/source.md", RelationName: "owner", DstPath: "notes/owner.md", DstType: "Person", Structural: true},
			{SrcPath: "notes/source.md", RelationName: "related", DstPath: "notes/a.md", DstType: "Decision", Provenance: "body_link"},
			{SrcPath: "notes/source.md", RelationName: "related", DstPath: "notes/b.md", DstType: "Decision", Provenance: "body_link"},
			{SrcPath: "notes/source.md", RelationName: "related", DstPath: "notes/c.md", DstType: "Person", Provenance: "backlink"},
			{SrcPath: "notes/source.md", RelationName: "other", DstPath: "notes/d.md", DstType: "Decision", Provenance: "body_link"},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: env.schema.Hash, NotesHash: "test", LoadedAt: 1, Ready: true},
	}))
	store := &relationBatchSpyStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = store
	loaders, err := newLoaders(deps, env.schema)
	require.NoError(t, err)
	return loaders, store
}

func TestRelationLoadersShareConcurrentReadsAndEmptyGroups(t *testing.T) {
	for _, structural := range []bool{true, false} {
		name := "ambient"
		if structural {
			name = "structural"
		}
		t.Run(name, func(t *testing.T) {
			loaders, store := relationCacheFixture(t)
			load := func() (map[string][]semdb.OntologyEdgeRow, error) {
				sources := []string{"notes/source.md", "notes/empty.md"}
				if structural {
					return loaders.structuralEdges(context.Background(), sources, "owner")
				}
				return loaders.ambientEdges(context.Background(), sources, "related", nil, "", 0)
			}
			type outcome struct {
				rows map[string][]semdb.OntologyEdgeRow
				err  error
			}
			results := make(chan outcome, 16)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for range cap(results) {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					rows, err := load()
					results <- outcome{rows, err}
				}()
			}
			close(start)
			workers.Wait()
			close(results)
			for result := range results {
				require.NoError(t, result.err)
				require.Contains(t, result.rows, "notes/empty.md")
				require.Nil(t, result.rows["notes/empty.md"])
				require.NotEmpty(t, result.rows["notes/source.md"])
				result.rows["notes/source.md"][0].DstPath = "mutated.md"
			}
			rows, err := load()
			require.NoError(t, err)
			require.NotEqual(t, "mutated.md", rows["notes/source.md"][0].DstPath)
			require.Equal(t, 1, store.structuralCalls+store.ambientCalls)
		})
	}
}

func TestRelationLoadersKeepFiltersAndLimitsIndependent(t *testing.T) {
	loaders, store := relationCacheFixture(t)
	for _, test := range []struct {
		relation, provenance, targetType string
		limit, count, calls              int
	}{
		{"related", "body_link", "Decision", 0, 2, 1},
		{"related", "body_link", "Decision", 1, 1, 1},
		{"related", "body_link", "Decision", 2, 2, 1},
		{"related", "backlink", "Decision", 1, 0, 2},
		{"related", "backlink", "Decision", 0, 0, 2},
		{"related", "backlink", "Person", 1, 1, 3},
		{"other", "body_link", "Decision", 1, 1, 4},
	} {
		rows, err := loaders.ambientEdges(context.Background(), []string{"notes/source.md"}, test.relation, map[string]struct{}{test.provenance: {}}, test.targetType, test.limit)
		require.NoError(t, err)
		require.Len(t, rows["notes/source.md"], test.count)
		require.Equal(t, test.calls, store.ambientCalls)
	}
	rows, err := loaders.structuralEdges(context.Background(), []string{"notes/source.md"}, "owner")
	require.NoError(t, err)
	require.Equal(t, "notes/owner.md", rows["notes/source.md"][0].DstPath)
	require.Equal(t, 1, store.structuralCalls)
}

func TestRelationLoadersRetryCanceledReads(t *testing.T) {
	loaders, store := relationCacheFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := loaders.structuralEdges(ctx, []string{"notes/source.md"}, "owner")
	require.Error(t, err)
	rows, err := loaders.structuralEdges(context.Background(), []string{"notes/source.md"}, "owner")
	require.NoError(t, err)
	require.Equal(t, "notes/owner.md", rows["notes/source.md"][0].DstPath)
	require.Equal(t, 2, store.structuralCalls)
}
