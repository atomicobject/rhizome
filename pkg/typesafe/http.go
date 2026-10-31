package typesafe

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// APIError preserves a non-2xx response. Error deliberately excludes Body,
// which can contain sensitive input echoed by the service. Inspect it explicitly
// when debugging; the client's API key is redacted from it.
type APIError struct {
	StatusCode int
	RequestID  string
	Body       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("typesafe: HTTP %d %s", e.StatusCode, http.StatusText(e.StatusCode))
}

func (e *APIError) Retryable() bool {
	return e.StatusCode == 408 || e.StatusCode == 429 || (e.StatusCode >= 500 && e.StatusCode <= 599)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// The gateway dispatches and settles each idempotency key once. Allocate it
	// before the retry loop, reuse it after an ambiguous outcome (an upstream
	// 5xx passed through, or request_in_progress), and replace it after a
	// definitive rejection (an upstream 408/429, or a gateway rate_limited or
	// upstream_unavailable). Transport failures are not retried at all.
	var idempotencyKey string
	if c.managed != nil && body != nil {
		idempotencyKey = crand.Text()
	}
	freshKey := func() {
		if idempotencyKey != "" {
			idempotencyKey = crand.Text()
		}
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, "", fmt.Errorf("typesafe: create HTTP request: %w", err)
		}
		if c.managed == nil {
			request.Header.Set("Authorization", "Bearer "+c.apiKey)
		} else if idempotencyKey != "" {
			request.Header.Set(wire.HeaderIdempotencyKey, idempotencyKey)
		}
		request.Header.Set("Accept", "application/json")
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		batchAttemptState, _ := ctx.Value(batchAttemptKey{}).(*batchAttempt)
		if batchAttemptState != nil {
			if err := batchAttemptState.gate.wait(ctx); err != nil {
				return nil, "", err
			}
			batchAttemptState.count++
		}
		response, err := c.http.Do(request)
		var gatewayErr *wire.GatewayError
		if err != nil && c.managed != nil && errors.As(err, &gatewayErr) && gatewayErr.Retryable() && attempt < c.maxRetries {
			// Retryable gateway codes guarantee nothing reached TypeSafe.
			if gatewayErr.Code != wire.CodeInProgress {
				freshKey()
			}
			delay := gatewayErr.RetryAfter
			if delay <= 0 {
				delay = backoff(attempt)
			}
			if batchAttemptState != nil && gatewayErr.Code == wire.CodeRateLimited {
				batchAttemptState.gate.pause(delay)
			}
			if waited, waitErr := waitRetry(ctx, delay); !waited {
				if waitErr != nil {
					return nil, "", waitErr
				}
				return nil, "", fmt.Errorf("typesafe: HTTP request: %w", err)
			}
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("typesafe: HTTP request: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
		_ = response.Body.Close()
		if readErr != nil {
			return nil, "", fmt.Errorf("typesafe: read HTTP response: %w", readErr)
		}
		if int64(len(data)) > c.maxResponseBytes {
			return nil, "", &ResponseError{"body exceeds configured size limit"}
		}
		requestID := response.Header.Get("x-typesafe-request-id")
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return data, requestID, nil
		}
		apiErr := &APIError{
			StatusCode: response.StatusCode, RequestID: requestID,
			Body:       string(data),
			RetryAfter: retryAfter(response.Header, time.Now()),
		}
		if c.apiKey != "" {
			apiErr.Body = strings.ReplaceAll(apiErr.Body, c.apiKey, "[REDACTED]")
		}
		if batch, _ := ctx.Value(batchAttemptKey{}).(*batchAttempt); batch != nil && apiErr.StatusCode == 429 {
			delay := apiErr.RetryAfter
			if delay <= 0 {
				delay = backoff(attempt)
			}
			batch.gate.pause(delay)
		}
		if !apiErr.Retryable() || attempt >= c.maxRetries {
			return nil, "", apiErr
		}
		if apiErr.StatusCode < 500 {
			freshKey()
		}
		delay := apiErr.RetryAfter
		if delay <= 0 {
			delay = backoff(attempt)
		}
		if waited, waitErr := waitRetry(ctx, delay); !waited {
			if waitErr != nil {
				return nil, "", waitErr
			}
			return nil, "", apiErr
		}
	}
}

// waitRetry sleeps before a retry. It reports false with a nil error when the
// delay would outlast the call budget (do not retry earlier than requested or
// hold the caller past its budget), and the context error on cancellation.
func waitRetry(ctx context.Context, delay time.Duration) (bool, error) {
	if deadline, ok := ctx.Deadline(); ok && delay >= time.Until(deadline) {
		return false, nil
	}
	timer := time.NewTimer(delay)
	select {
	case <-ctx.Done():
		timer.Stop()
		return false, ctx.Err()
	case <-timer.C:
		return true, nil
	}
}

func retryAfter(header http.Header, now time.Time) time.Duration {
	if value := strings.TrimSpace(header.Get("retry-after-ms")); value != "" {
		if delay, ok := retryDuration(value, time.Millisecond); ok {
			return delay
		}
	}
	value := strings.TrimSpace(header.Get("Retry-After"))
	if delay, ok := retryDuration(value, time.Second); ok {
		return delay
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}

func retryDuration(value string, unit time.Duration) (time.Duration, bool) {
	n, err := strconv.ParseFloat(value, 64)
	// The bound also excludes NaN and infinity and avoids duration overflow.
	if err != nil || !(n > 0 && n <= float64((1<<63-1)/unit)) {
		return 0, false
	}
	return time.Duration(n * float64(unit)), true
}

func backoff(attempt int) time.Duration {
	delay := min(500*time.Millisecond*time.Duration(1<<min(attempt, 4)), 5*time.Second)
	return time.Duration(float64(delay) * (0.75 + rand.Float64()*0.25))
}
