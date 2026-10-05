package eventstream

import (
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

const drainWindow = 2 * time.Second

// Stream decouples protocol readers from consumers with an unbounded queue.
type Stream struct {
	mu      sync.Mutex
	queue   []harness.Event
	wake    chan struct{}
	closing chan struct{}
	events  chan harness.Event
	once    sync.Once
	closed  bool
	until   time.Time
}

func New() *Stream {
	s := &Stream{
		wake:    make(chan struct{}, 1),
		closing: make(chan struct{}),
		events:  make(chan harness.Event, 128),
	}
	go s.publish()
	return s
}

func (s *Stream) Events() <-chan harness.Event { return s.events }

func (s *Stream) Emit(event harness.Event) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.queue = append(s.queue, event)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return true
}

func (s *Stream) Close() {
	s.close()
}

// Abort closes a stream after a transport failure. Callers queue the terminal
// event first; queued events receive the same bounded drain window as Close.
func (s *Stream) Abort() {
	s.close()
}

func (s *Stream) close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.until = time.Now().Add(drainWindow)
		s.mu.Unlock()
		close(s.closing)
		select {
		case s.wake <- struct{}{}:
		default:
		}
	})
}

func (s *Stream) publish() {
	defer close(s.events)
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			select {
			case <-s.wake:
			case <-s.closing:
			}
			continue
		}
		event := s.queue[0]
		closed, until := s.closed, s.until
		s.mu.Unlock()

		if !closed {
			select {
			case s.events <- event:
				s.removeFirst()
			case <-s.closing:
			}
			continue
		}
		remaining := time.Until(until)
		if remaining <= 0 {
			return
		}
		timer := time.NewTimer(remaining)
		select {
		case s.events <- event:
			if !timer.Stop() {
				<-timer.C
			}
			s.removeFirst()
		case <-timer.C:
			return
		}
	}
}

func (s *Stream) removeFirst() {
	s.mu.Lock()
	s.queue[0] = harness.Event{}
	s.queue = s.queue[1:]
	s.mu.Unlock()
}
