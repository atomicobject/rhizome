package agentchat

import "sync"

type Broker struct {
	mu          sync.Mutex
	subscribers map[string]map[chan Event]struct{}
	closed      bool
}

func NewBroker() *Broker {
	return &Broker{subscribers: map[string]map[chan Event]struct{}{}}
}

func (b *Broker) Subscribe(sessionID string) (<-chan Event, func()) {
	ch := make(chan Event, 32)
	b.mu.Lock()
	if b.closed {
		close(ch)
		b.mu.Unlock()
		return ch, func() {}
	}
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = map[chan Event]struct{}{}
	}
	b.subscribers[sessionID][ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		removed := false
		if subs := b.subscribers[sessionID]; subs != nil {
			if _, ok := subs[ch]; ok {
				delete(subs, ch)
				removed = true
			}
			if len(subs) == 0 {
				delete(b.subscribers, sessionID)
			}
		}
		b.mu.Unlock()
		if removed {
			close(ch)
		}
	}
}

func (b *Broker) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers[event.SessionID] {
		select {
		case ch <- event:
		default:
			if event.ID < 0 {
				continue
			}
			delete(b.subscribers[event.SessionID], ch)
			close(ch)
		}
	}
	if len(b.subscribers[event.SessionID]) == 0 {
		delete(b.subscribers, event.SessionID)
	}
}

func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for _, subscribers := range b.subscribers {
		for ch := range subscribers {
			close(ch)
		}
	}
	b.subscribers = nil
}
