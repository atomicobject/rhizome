package agentcode

import (
	"context"
	"errors"
	"io"
	"sync"
)

// Queue ownership keeps cancellation, capacity and receipt ordering atomic.
type requestQueue struct {
	mu     sync.Mutex
	frames []rpcFrame
	wake   chan struct{}
	closed bool
	err    error
}

func newRequestQueue() *requestQueue { return &requestQueue{wake: make(chan struct{}, 1)} }
func (q *requestQueue) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
func (q *requestQueue) push(frame rpcFrame) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || len(q.frames) >= maxQueue {
		return false
	}
	q.frames = append(q.frames, frame)
	q.notify()
	return true
}
func (q *requestQueue) remove(id string) (rpcFrame, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, frame := range q.frames {
		frameID, _ := requestID(frame.ID)
		if frame.Method == "call" && frameID == id {
			copy(q.frames[i:], q.frames[i+1:])
			q.frames[len(q.frames)-1] = rpcFrame{}
			q.frames = q.frames[:len(q.frames)-1]
			return frame, true
		}
	}
	return rpcFrame{}, false
}
func (q *requestQueue) close(err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	if err != nil {
		q.frames = nil
		if !errors.Is(err, io.EOF) {
			q.err = err
		}
	}
	q.notify()
}
func (q *requestQueue) next(ctx context.Context) (rpcFrame, bool, error) {
	for {
		q.mu.Lock()
		if q.err != nil {
			err := q.err
			q.mu.Unlock()
			return rpcFrame{}, false, err
		}
		if err := ctx.Err(); err != nil {
			q.mu.Unlock()
			return rpcFrame{}, false, err
		}
		if len(q.frames) > 0 {
			frame := q.frames[0]
			q.frames[0] = rpcFrame{}
			q.frames = q.frames[1:]
			q.mu.Unlock()
			return frame, true, nil
		}
		closed, err := q.closed, q.err
		q.mu.Unlock()
		if closed {
			return rpcFrame{}, false, err
		}
		select {
		case <-ctx.Done():
			return rpcFrame{}, false, ctx.Err()
		case <-q.wake:
		}
	}
}
