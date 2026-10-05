package web

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	vaultignore "github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/stretchr/testify/require"
)

func TestCachedGraphResponse_ReusesGlobalAndScopedEntries(t *testing.T) {
	ctx := context.Background()
	srv := newGraphCacheServer(t)

	globalCalls := 0
	buildGlobal := func(ctx context.Context) (GraphResponse, error) {
		globalCalls++
		return GraphResponse{Nodes: []GraphNode{{
			ID:      "embedded:story-a",
			Kind:    "embedded",
			Label:   "Story A",
			NodeRef: &ontology.NodeRef{NotePath: "docs/spec.md", NodeID: "story-a", Kind: ontology.NodeKindEmbedded},
		}}}, nil
	}

	first, err := srv.cachedGraphResponse(ctx, "global|limit=2000|depth=2", true, buildGlobal)
	require.NoError(t, err)
	second, err := srv.cachedGraphResponse(ctx, "global|limit=2000|depth=2", true, buildGlobal)
	require.NoError(t, err)
	require.Equal(t, 1, globalCalls)
	require.Equal(t, first, second)

	// A caller mutating its returned node ref must not change the cached graph.
	first.Nodes[0].NodeRef.NodeID = "story-b"
	third, err := srv.cachedGraphResponse(ctx, "global|limit=2000|depth=2", true, buildGlobal)
	require.NoError(t, err)
	require.Equal(t, 1, globalCalls)
	require.Equal(t, "story-a", third.Nodes[0].NodeRef.NodeID)
	require.Equal(t, "story-a", second.Nodes[0].NodeRef.NodeID)

	scopedCalls := 0
	buildScoped := func(ctx context.Context) (GraphResponse, error) {
		scopedCalls++
		return GraphResponse{Nodes: []GraphNode{{ID: "code:src/a.go", Kind: "code", Label: "a.go"}}}, nil
	}

	_, err = srv.cachedGraphResponse(ctx, "local|path=src/a.go|limit=200|depth=1", false, buildScoped)
	require.NoError(t, err)
	_, err = srv.cachedGraphResponse(ctx, "local|path=src/a.go|limit=200|depth=1", false, buildScoped)
	require.NoError(t, err)
	_, err = srv.cachedGraphResponse(ctx, "expand|module=src|limit=600", false, buildScoped)
	require.NoError(t, err)
	require.Equal(t, 2, scopedCalls)
}

func TestCachedGraphResponse_ChangesWhenIgnoreMatcherChanges(t *testing.T) {
	ctx := context.Background()
	srv := newGraphCacheServer(t)

	calls := 0
	build := func(ctx context.Context) (GraphResponse, error) {
		calls++
		return GraphResponse{Nodes: []GraphNode{{ID: "note:a", Kind: "note", Label: "a"}}}, nil
	}

	srv.runtime.SetIgnoreMatcher(vaultignore.NewMatcher([]string{"ignored-one/"}))
	_, err := srv.cachedGraphResponse(ctx, "global|limit=2000|depth=2", true, build)
	require.NoError(t, err)
	_, err = srv.cachedGraphResponse(ctx, "global|limit=2000|depth=2", true, build)
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	srv.runtime.SetIgnoreMatcher(vaultignore.NewMatcher([]string{"ignored-two/"}))
	_, err = srv.cachedGraphResponse(ctx, "global|limit=2000|depth=2", true, build)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
