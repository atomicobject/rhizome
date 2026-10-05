package indexing

import "sync"

type finalDrainProgress struct {
	bar ProgressBar

	mu        sync.Mutex
	total     int
	completed int
}

func newFinalDrainProgress(bar ProgressBar) *finalDrainProgress {
	return &finalDrainProgress{bar: withProgressBar(bar)}
}

func (p *finalDrainProgress) reset() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total = 0
	p.completed = 0
}

func (p *finalDrainProgress) addWork(count int) {
	if p == nil || count <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total += count
	p.updateLocked()
}

func (p *finalDrainProgress) completeWork(count int) {
	if p == nil || count <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completed += count
	p.updateLocked()
}

func (p *finalDrainProgress) Start(total int) {
	p.addWork(total)
}

func (p *finalDrainProgress) Advance(delta int) {
	p.completeWork(delta)
}

func (p *finalDrainProgress) updateLocked() {
	total := p.total
	if total <= 0 {
		total = 1
	}
	if p.completed > total {
		p.completed = total
	}
	p.bar.Update(p.completed, total)
}
