package typesafe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// BatchItem identifies an independent state and its questions.
type BatchItem struct {
	ID      string  `json:"id"`
	Request Request `json:"request"`
}

// BatchResult preserves successes even when another item fails. Uncertain means
// a request was attempted but no validated answer was obtained; do not replay it
// automatically. Not-started items are safe to submit later.
type BatchResult struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Response   *Response `json:"response,omitempty"`
	RequestID  string    `json:"requestId,omitempty"`
	Attempts   int       `json:"attempts"`
	Error      string    `json:"error,omitempty"`
	StatusCode int       `json:"statusCode,omitempty"`
	Retryable  bool      `json:"retryable,omitempty"`
}

// EvaluateBatch evaluates up to 128 states using 1..32 workers (default eight).
// Results preserve input order. Retries use the same policy as Evaluate, with a
// shared throttle pause for this batch. Cancellation returns all completed items.
// Callers own checkpoints; the client does not persist state or retry a batch.
func (c *Client) EvaluateBatch(ctx context.Context, items []BatchItem, concurrency int) ([]BatchResult, error) {
	if len(items) == 0 || len(items) > 128 {
		return nil, fmt.Errorf("typesafe: batch requires 1 to 128 items")
	}
	if concurrency == 0 {
		concurrency = 8
	}
	if concurrency < 1 || concurrency > 32 {
		return nil, fmt.Errorf("typesafe: concurrency must be 1 to 32")
	}
	seen := map[string]bool{}
	results := make([]BatchResult, len(items))
	for i, item := range items {
		if item.ID == "" || seen[item.ID] {
			return nil, fmt.Errorf("typesafe: batch IDs must be nonempty and unique")
		}
		seen[item.ID] = true
		results[i] = BatchResult{ID: item.ID, Status: "not_started"}
	}
	gate := &batchGate{}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(concurrency, len(items)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				attempt := &batchAttempt{gate: gate}
				response, err := c.Evaluate(context.WithValue(ctx, batchAttemptKey{}, attempt), items[i].Request)
				result := BatchResult{ID: items[i].ID, Attempts: attempt.count}
				if err == nil {
					result.Status = "succeeded"
					result.Response = &response
					result.RequestID = response.RequestID
				} else {
					result.Error = err.Error()
					var apiErr *APIError
					switch {
					case errors.As(err, &apiErr):
						result.Status = "failed"
						result.StatusCode = apiErr.StatusCode
						result.Retryable = apiErr.Retryable()
						result.RequestID = apiErr.RequestID
					case attempt.count > 0:
						result.Status = "uncertain"
					case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
						result.Status = "not_started"
					default:
						result.Status = "failed"
					}
				}
				results[i] = result
			}
		}()
	}
	for i := range items {
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- i:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
	return results, nil
}

type batchAttemptKey struct{}
type batchAttempt struct {
	gate  *batchGate
	count int
}
type batchGate struct {
	mu    sync.Mutex
	until time.Time
}

func (g *batchGate) pause(delay time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	until := time.Now().Add(delay)
	if until.After(g.until) {
		g.until = until
	}
}
func (g *batchGate) wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		delay := time.Until(g.until)
		g.mu.Unlock()
		if delay <= 0 {
			return ctx.Err()
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}
