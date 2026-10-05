package web

import (
	"context"
	"sync"

	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

const defaultScopedGraphCacheEntries = 32

type graphCacheEntry struct {
	key  string
	resp GraphResponse
}

type graphCacheCall struct {
	done chan struct{}
	resp GraphResponse
	err  error
}

// graphResponseCache keeps hot graph responses in memory and deduplicates
// concurrent requests for the same graph build.
type graphResponseCache struct {
	mu        sync.Mutex
	global    *graphCacheEntry
	scoped    map[string]*graphCacheEntry
	order     []string
	inflight  map[string]*graphCacheCall
	maxScoped int
	closed    bool

	ctx    context.Context
	cancel context.CancelFunc
	builds sync.WaitGroup
}

func newGraphResponseCache(maxScoped int) *graphResponseCache {
	if maxScoped <= 0 {
		maxScoped = defaultScopedGraphCacheEntries
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &graphResponseCache{
		scoped:    make(map[string]*graphCacheEntry, maxScoped),
		inflight:  make(map[string]*graphCacheCall),
		maxScoped: maxScoped,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Close cancels the cache lifetime context and waits for in-flight builds to
// return, so no build goroutine outlives the resources it reads from.
func (c *graphResponseCache) Close() {
	c.cancel()
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.builds.Wait()
}

// spawn runs fn under the cache lifetime context and counts it in the Close
// drain, so graph-scoped background work cannot outlive the stores it reads.
// It reports false, without running fn, once the cache is closed.
func (c *graphResponseCache) spawn(fn func(ctx context.Context)) bool {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return false
	}
	c.builds.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.builds.Done()
		fn(c.ctx)
	}()
	return true
}

func (c *graphResponseCache) getOrBuild(ctx context.Context, key string, global bool, build func(ctx context.Context) (GraphResponse, error)) (GraphResponse, error) {
	c.mu.Lock()
	if global {
		if c.global != nil && c.global.key == key {
			resp := cloneGraphResponse(c.global.resp)
			c.mu.Unlock()
			return resp, nil
		}
	} else if entry, ok := c.scoped[key]; ok {
		resp := cloneGraphResponse(entry.resp)
		c.mu.Unlock()
		return resp, nil
	}

	call, ok := c.inflight[key]
	if !ok {
		if c.closed {
			c.mu.Unlock()
			return GraphResponse{}, context.Canceled
		}
		call = &graphCacheCall{done: make(chan struct{})}
		c.inflight[key] = call
		c.builds.Add(1)
		go c.run(call, key, global, build)
	}
	c.mu.Unlock()

	select {
	case <-call.done:
		if call.err != nil {
			return GraphResponse{}, call.err
		}
		return cloneGraphResponse(call.resp), nil
	case <-ctx.Done():
		return GraphResponse{}, ctx.Err()
	}
}

// run performs one shared build under the cache lifetime context. It is
// deliberately detached from every waiter's context: the build outlives any
// single request, so nothing request-scoped may ride the context it receives.
func (c *graphResponseCache) run(call *graphCacheCall, key string, global bool, build func(ctx context.Context) (GraphResponse, error)) {
	defer c.builds.Done()

	resp, err := build(c.ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		call.resp = cloneGraphResponse(resp)
		if global {
			c.global = &graphCacheEntry{key: key, resp: call.resp}
		} else {
			c.putScopedLocked(key, call.resp)
		}
	}
	call.err = err
	delete(c.inflight, key)
	close(call.done)
}

func (c *graphResponseCache) putScopedLocked(key string, resp GraphResponse) {
	if entry, ok := c.scoped[key]; ok {
		entry.resp = resp
		return
	}
	if len(c.scoped) >= c.maxScoped && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.scoped, oldest)
	}
	c.scoped[key] = &graphCacheEntry{key: key, resp: resp}
	c.order = append(c.order, key)
}

func cloneGraphResponse(resp GraphResponse) GraphResponse {
	out := resp
	if len(resp.Nodes) > 0 {
		out.Nodes = append([]GraphNode(nil), resp.Nodes...)
		for i := range out.Nodes {
			if out.Nodes[i].NodeRef != nil {
				ref := *out.Nodes[i].NodeRef
				out.Nodes[i].NodeRef = &ref
			}
		}
	}
	if len(resp.Edges) > 0 {
		out.Edges = append([]GraphEdge(nil), resp.Edges...)
	}
	if resp.Diagnostics != nil {
		diagnostics := *resp.Diagnostics
		diagnostics.Nodes = append([]noderead.GraphNodeDiagnostic(nil), diagnostics.Nodes...)
		diagnostics.Edges = append([]noderead.GraphEdgeDiagnostic(nil), diagnostics.Edges...)
		diagnostics.Skips = append([]noderead.GraphSkipDiagnostic(nil), diagnostics.Skips...)
		diagnostics.Dedupe = append([]noderead.GraphDedupeDiagnostic(nil), diagnostics.Dedupe...)
		out.Diagnostics = &diagnostics
	}
	return out
}
