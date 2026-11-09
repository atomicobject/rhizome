package web

import (
	"net/http"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

// SPEC-0136 R8: every note-ownership check in one scalar read shares one
// request-scoped links cache, so each directory resolves once per request; the
// next request starts a fresh cache, so a retargeted link is seen.
func TestGraphQLScalarReadSharesOneSymlinkCachePerRequest(t *testing.T) {
	srv, _ := newGraphQLReadCacheServer(t)
	var mu sync.Mutex
	var seen []*paths.SymlinkCache
	checked := map[string]bool{}
	srv.cfg.NotePathOwned = func(path string, links *paths.SymlinkCache) bool {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, links)
		checked[path] = true
		return true
	}
	request := func(query string) []*paths.SymlinkCache {
		mu.Lock()
		seen = nil
		mu.Unlock()
		reply := postGraphQL(t, srv, map[string]any{"query": query})
		require.Equal(t, http.StatusOK, reply.status, string(reply.body))
		require.NotContains(t, reply.decoded, "errors", string(reply.body))
		mu.Lock()
		defer mu.Unlock()
		return append([]*paths.SymlinkCache(nil), seen...)
	}

	first := request(`{ spec(first: 50) { path summary } plan(first: 50) { path title } research(first: 50) { path } }`)
	require.GreaterOrEqual(t, len(checked), 3, "the read checks ownership for each selected note")
	require.NotNil(t, first[0])
	for _, links := range first {
		require.Same(t, first[0], links, "one request shares one links cache")
	}

	second := request(`{ spec(first: 49) { path summary } }`)
	require.NotEmpty(t, second)
	require.NotSame(t, first[0], second[0], "a cache never outlives its request")
}
