package indexing

import (
	"strings"
	"sync"
)

const (
	unifiedProgressTotal = 1000
	mainPhaseEnd         = 950
	finalDrainEnd        = 978
	streamFlushEnd       = 982
	anchorScopesEnd      = 986
	metadataSyncEnd      = 989
	ontologyEnd          = 992
	graphEnd             = 994
	finalPhaseEnd        = unifiedProgressTotal
)

type integratedProgress struct {
	base    ProgressBar
	verbose bool

	mu       sync.Mutex
	segStart int
	segEnd   int
	lastDone int
}

func newIntegratedProgress(base ProgressBar, verbose bool) *integratedProgress {
	p := &integratedProgress{
		base:    withProgressBar(base),
		verbose: verbose,
		segEnd:  mainPhaseEnd,
	}
	p.SetStage("Indexing code")
	p.base.Update(0, unifiedProgressTotal)
	return p
}

func (p *integratedProgress) Println(msg string) {
	if p == nil {
		return
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	if p.verbose || strings.Contains(strings.ToLower(msg), "warning") {
		p.base.Println(msg)
	}
}

func (p *integratedProgress) Update(done, total int) {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	overall := p.segStart
	if total > 0 && p.segEnd > p.segStart {
		span := p.segEnd - p.segStart
		if done < 0 {
			done = 0
		}
		if done > total {
			done = total
		}
		overall = p.segStart + (done*span)/total
	}
	if overall < p.lastDone {
		overall = p.lastDone
	}
	p.lastDone = overall
	p.base.Update(overall, unifiedProgressTotal)
}

func (p *integratedProgress) SetStage(label string) {
	if p == nil {
		return
	}
	if setter, ok := p.base.(interface{ SetLabel(string) }); ok {
		setter.SetLabel(label)
	}
}

func (p *integratedProgress) SetSegment(start, end int) {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > unifiedProgressTotal {
		end = unifiedProgressTotal
	}
	p.segStart = start
	p.segEnd = end
	if p.lastDone < start {
		p.lastDone = start
		p.base.Update(p.lastDone, unifiedProgressTotal)
	}
}

func (p *integratedProgress) SetTailStep(label string, step, total int) {
	if p == nil {
		return
	}
	p.SetStage(label)
	if total <= 0 {
		total = 1
	}
	if step < 0 {
		step = 0
	}
	if step > total {
		step = total
	}
	p.SetSegment(mainPhaseEnd, finalPhaseEnd)
	p.Update(step, total)
}

func (p *integratedProgress) Finish(label string) {
	if p == nil {
		return
	}
	if strings.TrimSpace(label) != "" {
		p.SetStage(label)
	}
	p.mu.Lock()
	p.segStart = finalPhaseEnd
	p.segEnd = finalPhaseEnd
	p.lastDone = unifiedProgressTotal
	p.mu.Unlock()
	p.base.Update(unifiedProgressTotal, unifiedProgressTotal)
}

func (p *integratedProgress) AdvanceTo(label string, value int) {
	if p == nil {
		return
	}
	if strings.TrimSpace(label) != "" {
		p.SetStage(label)
	}
	if value < 0 {
		value = 0
	}
	if value > unifiedProgressTotal {
		value = unifiedProgressTotal
	}
	p.mu.Lock()
	p.segStart = value
	p.segEnd = value
	if p.lastDone < value {
		p.lastDone = value
	}
	p.mu.Unlock()
	p.base.Update(value, unifiedProgressTotal)
}
