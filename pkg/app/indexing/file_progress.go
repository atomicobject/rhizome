package indexing

import (
	"sync"

	"github.com/atomicobject/rhizome/pkg/app/codeintel"
)

type fixedTotalFileProgress struct {
	bar ProgressBar

	mu        sync.Mutex
	total     int
	completed int
}

func newFixedTotalFileProgress(bar ProgressBar, total int) *fixedTotalFileProgress {
	return &fixedTotalFileProgress{
		bar:   withProgressBar(bar),
		total: total,
	}
}

func (p *fixedTotalFileProgress) callbacks() *codeintel.ProgressCallbacks {
	if p == nil {
		return nil
	}
	return &codeintel.ProgressCallbacks{
		OnCompleted: p.onCompleted,
	}
}

func (p *fixedTotalFileProgress) onCompleted(codeintel.FileCandidate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completed++
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
