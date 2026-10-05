package indexing

import (
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
)

type fixedTotalFileProgress struct {
	bar ProgressBar

	mu              sync.Mutex
	total           int
	completed       int
	edgeDiscovered  map[string]struct{}
	edgeCompleted   map[string]struct{}
	scopeDiscovered map[int64]struct{}
	scopeCompleted  map[int64]struct{}
}

func newFixedTotalFileProgress(bar ProgressBar, total int) *fixedTotalFileProgress {
	return &fixedTotalFileProgress{
		bar:             withProgressBar(bar),
		total:           total,
		edgeDiscovered:  make(map[string]struct{}),
		edgeCompleted:   make(map[string]struct{}),
		scopeDiscovered: make(map[int64]struct{}),
		scopeCompleted:  make(map[int64]struct{}),
	}
}

func (p *fixedTotalFileProgress) callbacks() *indexingpipe.ProgressCallbacks {
	if p == nil {
		return nil
	}
	return &indexingpipe.ProgressCallbacks{
		OnCompleted: p.onCompleted,
	}
}

func (p *fixedTotalFileProgress) onCompleted(indexingpipe.FileCandidate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completed++
	p.updateLocked()
}

func (p *fixedTotalFileProgress) AddEdgeWork(paths []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, path := range normalizeProgressPaths(paths) {
		if _, ok := p.edgeDiscovered[path]; ok {
			continue
		}
		p.edgeDiscovered[path] = struct{}{}
		p.total++
	}
	p.updateLocked()
}

func (p *fixedTotalFileProgress) CompleteEdgeWork(paths []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, path := range normalizeProgressPaths(paths) {
		if _, ok := p.edgeDiscovered[path]; !ok {
			p.edgeDiscovered[path] = struct{}{}
			p.total++
		}
		if _, ok := p.edgeCompleted[path]; ok {
			continue
		}
		p.edgeCompleted[path] = struct{}{}
		p.completed++
	}
	p.updateLocked()
}

func (p *fixedTotalFileProgress) updateLocked() {
	total := p.total
	if total <= 0 {
		total = 1
	}
	if p.completed > total {
		p.completed = total
	}
	p.bar.Update(p.completed, total)
}

func (p *fixedTotalFileProgress) AddScopeWork(ids []int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range normalizeProgressIDs(ids) {
		if _, ok := p.scopeDiscovered[id]; ok {
			continue
		}
		p.scopeDiscovered[id] = struct{}{}
		p.total++
	}
	p.updateLocked()
}

func (p *fixedTotalFileProgress) CompleteScopeWork(ids []int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range normalizeProgressIDs(ids) {
		if _, ok := p.scopeDiscovered[id]; !ok {
			p.scopeDiscovered[id] = struct{}{}
			p.total++
		}
		if _, ok := p.scopeCompleted[id]; ok {
			continue
		}
		p.scopeCompleted[id] = struct{}{}
		p.completed++
	}
	p.updateLocked()
}

func normalizeProgressPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func normalizeProgressIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
