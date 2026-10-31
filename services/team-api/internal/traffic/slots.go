package traffic

import (
	"context"
	"sync"
	"time"
)

// gate bounds concurrent work with a bounded wait, such as request bodies
// being read before paid admission.
type gate struct {
	ch   chan struct{}
	wait time.Duration
}

func newGate(n int, wait time.Duration) *gate { return &gate{make(chan struct{}, n), wait} }

func (g *gate) enter(ctx context.Context) (leave func(), ok bool) {
	ctx, cancel := context.WithTimeout(ctx, g.wait)
	defer cancel()
	select {
	case g.ch <- struct{}{}:
		return func() { <-g.ch }, true
	case <-ctx.Done():
		return nil, false
	}
}

// slots bounds concurrent paid calls per person and in total.
type slots struct {
	per    int
	wait   time.Duration
	global chan struct{}
	mu     sync.Mutex
	// ponytail: one channel per person, never removed; bounded by team size.
	people map[string]chan struct{}
}

func newSlots(per, global int, wait time.Duration) *slots {
	return &slots{per: per, wait: wait, global: make(chan struct{}, global), people: map[string]chan struct{}{}}
}

// acquire takes the person's slot, then a global slot, waiting at most s.wait
// in total. Cancellation of ctx ends the wait.
func (s *slots) acquire(ctx context.Context, sub string) (release func(), ok bool) {
	s.mu.Lock()
	person := s.people[sub]
	if person == nil {
		person = make(chan struct{}, s.per)
		s.people[sub] = person
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	select {
	case person <- struct{}{}:
	case <-ctx.Done():
		return nil, false
	}
	select {
	case s.global <- struct{}{}:
	case <-ctx.Done():
		<-person
		return nil, false
	}
	return func() { <-s.global; <-person }, true
}
