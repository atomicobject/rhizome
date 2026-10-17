package watchhub

import (
	"github.com/atomicobject/rhizome/pkg/paths"
	"time"
)

func (h *Hub) dispatchLoop() {
	defer h.work.Done()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	if !timer.Stop() {
		<-timer.C
	}
	var ready <-chan time.Time
	for {
		select {
		case <-h.pendingStop:
			return
		case <-h.dispatchWake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(h.debounce)
			ready = timer.C
		case <-ready:
			ready = nil
			h.flush()
		}
	}
}

func (h *Hub) enqueue(ev WatchEvent) {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	h.pendingMu.Lock()
	// Use case-insensitive key on Windows/macOS to avoid duplicate entries for
	// the same file. Merge op bits so a rapid remove+create sequence can be
	// interpreted by subscribers as a recreation.
	key := paths.CaseKey(ev.RelPath)
	if existing, ok := h.pending[key]; ok {
		existing.Op |= ev.Op
		if ev.IsDir {
			existing.IsDir = true
		}
		if existing.AbsPath == "" {
			existing.AbsPath = ev.AbsPath
		}
		existing.Source = ev.Source
		h.pending[key] = existing
	} else {
		h.pending[key] = ev
	}
	h.pendingMu.Unlock()

	select {
	case h.dispatchWake <- struct{}{}:
	default:
	}
}

func (h *Hub) flush() {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	var batch []WatchEvent
	h.pendingMu.Lock()
	if len(h.pending) == 0 {
		h.pendingMu.Unlock()
		return
	}
	batch = make([]WatchEvent, 0, len(h.pending))
	for _, ev := range h.pending {
		batch = append(batch, ev)
	}
	h.pending = make(map[string]WatchEvent)
	h.pendingMu.Unlock()

	h.dispatch(batch)
}

func (h *Hub) dispatch(events []WatchEvent) {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	h.subsMu.RLock()
	subs := make([]*subscription, 0, len(h.subs))
	for _, sub := range h.subs {
		subs = append(subs, sub)
	}
	h.subsMu.RUnlock()

	for _, sub := range subs {
		if sub == nil || sub.handler == nil || !sub.active.Load() {
			continue
		}
		filtered := make([]WatchEvent, 0, len(events))
		for _, ev := range events {
			if sub.filter.Matches(ev) {
				filtered = append(filtered, ev)
			}
		}
		if len(filtered) == 0 {
			continue
		}
		if sub.active.Load() {
			sub.handler(h.context(), filtered)
		}
	}
}
