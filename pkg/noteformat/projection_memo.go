package noteformat

import (
	"strconv"
	"sync"
)

// A projection is a pure function of the authored source, so reads that
// re-project unchanged files reuse one validated result. Callers always get a
// Copy, so no caller can mutate the shared entry.
const projectionMemoEntries = 1024

type projectionMemo struct {
	mu      sync.Mutex
	max     int
	entries map[string]Projection
}

func newProjectionMemo(max int) *projectionMemo {
	return &projectionMemo{max: max, entries: make(map[string]Projection)}
}

func projectionMemoKey(format FormatID, source AuthoredSource) string {
	return string(format) + "\x00" + source.Path().String() + "\x00" + source.ContentHash() + "\x00" + strconv.FormatInt(source.mtime, 10)
}

func (m *projectionMemo) get(key string) (Projection, bool) {
	if m == nil {
		return Projection{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	projection, ok := m.entries[key]
	return projection, ok
}

func (m *projectionMemo) put(key string, projection Projection) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// ponytail: drops everything when full; an LRU only matters if a working set larger than max thrashes it.
	if len(m.entries) >= m.max {
		clear(m.entries)
	}
	m.entries[key] = projection
}

// ProjectCached is Project for read paths that re-project unchanged files on
// every request. Indexing keeps calling Project so it always runs the projector.
func (r Runtime) ProjectCached(source AuthoredSource) (Projection, error) {
	descriptor, err := r.descriptorForSource(source)
	if err != nil {
		return Projection{}, err
	}
	key := projectionMemoKey(descriptor.ID, source)
	if cached, ok := r.projections.get(key); ok {
		return cached.Copy(), nil
	}
	projection, err := r.Project(source)
	if err != nil {
		return Projection{}, err
	}
	r.projections.put(key, projection.Copy())
	return projection, nil
}
