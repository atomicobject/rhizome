package noderead

import (
	"context"
	"sync"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopeApplyInvalidationDoesNotRepublishInFlightNotePathCache(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := applyCacheFixture(t)
	reader := &applyCacheBlockingNoteReader{
		Note:    &obsidian.Note{},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	scope := NewService(vault, reader, store, schema).NewScope(ctx, ScopeOptions{})

	built := make(chan *obsidian.NotePathCache, 1)
	go func() { built <- scope.notePathCache(ctx) }()
	waitApplyCacheBarrier(t, reader.started)

	// Apply invalidation can occur while a path-cache build has released the
	// scope lock but has not yet published its old note list.
	scope.invalidateAfterApply()
	close(reader.release)
	stale := <-built
	require.NotNil(t, stale)

	fresh := scope.notePathCache(ctx)
	path, ok := fresh.ResolveNote("new")
	require.True(t, ok, "the next read must rebuild instead of reusing the stale in-flight cache")
	require.Equal(t, "specs/new.md", path)
}

func TestScopeApplyInvalidationDoesNotRepublishInFlightGraphCaches(t *testing.T) {
	t.Run("graph", func(t *testing.T) {
		ctx, scope, store := applyCacheGraphScope(t)
		request := GraphRequest{Profile: GraphProfileOntologyNative}
		done := make(chan graphCall, 1)
		go func() {
			result, err := scope.Graph(ctx, request)
			done <- graphCall{result: result, err: err}
		}()
		waitApplyCacheBarrier(t, store.started)

		scope.invalidateAfterApply()
		close(store.release)
		stale := <-done
		require.NoError(t, stale.err)
		require.NotEmpty(t, stale.result.Edges)

		fresh, err := scope.Graph(ctx, request)
		require.NoError(t, err)
		require.GreaterOrEqual(t, store.calls(), 2, "the next Graph read must not reuse the stale in-flight result")
		require.Equal(t, stale.result, fresh, "the store is unchanged; only cache publication is under test")
	})

	t.Run("graph facts", func(t *testing.T) {
		ctx, scope, store := applyCacheGraphScope(t)
		request := GraphFactsRequest{IncludeOntology: true, IncludeEmbedded: true}
		done := make(chan graphFactsCall, 1)
		go func() {
			result, err := scope.GraphFacts(ctx, request)
			done <- graphFactsCall{result: result, err: err}
		}()
		waitApplyCacheBarrier(t, store.started)

		scope.invalidateAfterApply()
		close(store.release)
		stale := <-done
		require.NoError(t, stale.err)
		require.NotEmpty(t, stale.result.Edges)

		fresh, err := scope.GraphFacts(ctx, request)
		require.NoError(t, err)
		require.GreaterOrEqual(t, store.calls(), 2, "the next GraphFacts read must not reuse the stale in-flight result")
		require.Equal(t, stale.result, fresh, "the store is unchanged; only cache publication is under test")
	})
}

type applyCacheBlockingNoteReader struct {
	*obsidian.Note
	started chan struct{}
	release chan struct{}

	mu    sync.Mutex
	calls int
}

func (r *applyCacheBlockingNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	if call == 1 {
		close(r.started)
		<-r.release
		return []string{"specs/product.md"}, nil
	}
	return []string{"specs/product.md", "specs/new.md"}, nil
}

type applyCacheBlockingGraphStore struct {
	*semdb.Store
	started chan struct{}
	release chan struct{}

	mu        sync.Mutex
	edgeCalls int
}

func (s *applyCacheBlockingGraphStore) GraphOntologyEdges(ctx context.Context, query readmodel.GraphEdgeQuery) ([]readmodel.GraphTypedEdgeRow, error) {
	rows, err := s.Store.GraphOntologyEdges(ctx, query)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.edgeCalls++
	call := s.edgeCalls
	s.mu.Unlock()
	if call == 1 {
		close(s.started)
		<-s.release
	}
	return rows, nil
}

func (s *applyCacheBlockingGraphStore) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edgeCalls
}

type graphCall struct {
	result GraphResult
	err    error
}

type graphFactsCall struct {
	result GraphFactsResult
	err    error
}

func applyCacheGraphScope(t *testing.T) (context.Context, *Scope, *applyCacheBlockingGraphStore) {
	t.Helper()
	ctx := context.Background()
	vault, realStore, schema := applyCacheFixture(t)
	store := &applyCacheBlockingGraphStore{
		Store:   realStore,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := NewService(vault, &obsidian.Note{}, store, schema)
	return ctx, service.NewScope(ctx, ScopeOptions{}), store
}

func applyCacheFixture(t *testing.T) (obsidian.VaultDefinition, *semdb.Store, *ontology.Schema) {
	t.Helper()
	return buildFixture(t, "type ProductSpec @node(paths: [\"specs/product.md\"]) {\n  stories: StoriesSection @contains(level: H2, heading: \"Stories\")\n}\n\ntype StoriesSection implements Section { stories: [UserStory!] @contains(level: H3) }\n\ntype UserStory implements Section @node(locator: EMBEDDED) { related: ProductSpec @link }\n", "# Product\n\n## Stories\n\n### Story A\nrelated:: [[specs/product]]\n")
}

func waitApplyCacheBarrier(t *testing.T, barrier <-chan struct{}) {
	t.Helper()
	select {
	case <-barrier:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for in-flight cache read")
	}
}
