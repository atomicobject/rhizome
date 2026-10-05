package embeddings

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultFallbackRequests      = 3
	defaultFallbackRequestWindow = time.Second
	defaultFallbackTokens        = 80000
	defaultFallbackTokenWindow   = time.Minute
)

type openAIRateLimitSnapshot struct {
	remainingRequests int
	remainingTokens   int
	resetRequests     time.Duration
	resetTokens       time.Duration
	updatedAt         time.Time
	hasRequests       bool
	hasTokens         bool
	fromHeaders       bool
}

// openAIRateLimiter paces request starts based on OpenAI rate-limit response headers.
//
// It is intentionally conservative: it spreads the remaining budget evenly over the
// reset window and falls back to a fixed local cap when headers are missing.
type openAIRateLimiter struct {
	mu sync.Mutex
	// now exists to make pacing logic testable.
	now func() time.Time

	snap      openAIRateLimitSnapshot
	nextReqAt time.Time
	nextTokAt time.Time
}

func newOpenAIRateLimiter() *openAIRateLimiter {
	now := time.Now()
	return &openAIRateLimiter{
		now: time.Now,
		snap: openAIRateLimitSnapshot{
			remainingRequests: defaultFallbackRequests,
			remainingTokens:   defaultFallbackTokens,
			resetRequests:     defaultFallbackRequestWindow,
			resetTokens:       defaultFallbackTokenWindow,
			updatedAt:         now,
			hasRequests:       true,
			hasTokens:         true,
		},
	}
}

func (l *openAIRateLimiter) snapshot() openAIRateLimitSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snap
}

func (l *openAIRateLimiter) updateFromHeaders(h http.Header) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	snap := l.snap

	var reqChanged, tokChanged bool
	if v, ok := headerInt(h, "x-ratelimit-remaining-requests"); ok {
		snap.remainingRequests = v
		snap.hasRequests = true
		reqChanged = true
	}
	if d, ok := headerDuration(h, "x-ratelimit-reset-requests"); ok {
		snap.resetRequests = d
		reqChanged = true
	}
	if v, ok := headerInt(h, "x-ratelimit-remaining-tokens"); ok {
		snap.remainingTokens = v
		snap.hasTokens = true
		tokChanged = true
	}
	if d, ok := headerDuration(h, "x-ratelimit-reset-tokens"); ok {
		snap.resetTokens = d
		tokChanged = true
	}

	if reqChanged || tokChanged {
		snap.updatedAt = now
		snap.fromHeaders = true
	}
	if reqChanged {
		l.nextReqAt = time.Time{}
	}
	if tokChanged {
		l.nextTokAt = time.Time{}
	}
	if reqChanged || tokChanged {
		l.snap = snap
	}
}

// reserve returns how long the caller should wait before starting a request,
// and performs an optimistic reservation against the latest snapshot.
func (l *openAIRateLimiter) reserve(estimatedTokens int) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	var delay time.Duration

	// Requests pacing: spread remaining requests evenly across the remaining window.
	if l.snap.hasRequests && l.snap.resetRequests > 0 {
		if l.snap.remainingRequests <= 0 {
			resetAt := l.snap.updatedAt.Add(l.snap.resetRequests)
			if now.Before(resetAt) {
				delay = maxDuration(delay, resetAt.Sub(now))
			}
		} else {
			interval := l.snap.resetRequests / time.Duration(l.snap.remainingRequests)
			if interval < 0 {
				interval = 0
			}
			if l.nextReqAt.IsZero() || l.nextReqAt.Before(now) {
				l.nextReqAt = now
			}
			if l.nextReqAt.After(now) {
				delay = maxDuration(delay, l.nextReqAt.Sub(now))
			}
			base := l.nextReqAt
			if base.Before(now) {
				base = now
			}
			l.nextReqAt = base.Add(interval)
			l.snap.remainingRequests--
		}
	}

	// Tokens pacing: similar idea, but scale the interval by the request's size.
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}
	if l.snap.hasTokens && l.snap.resetTokens > 0 {
		if l.snap.remainingTokens <= 0 {
			resetAt := l.snap.updatedAt.Add(l.snap.resetTokens)
			if now.Before(resetAt) {
				delay = maxDuration(delay, resetAt.Sub(now))
			}
		} else {
			interval := time.Duration(int64(l.snap.resetTokens) * int64(estimatedTokens) / int64(l.snap.remainingTokens))
			if interval < 0 {
				interval = 0
			}
			if l.nextTokAt.IsZero() || l.nextTokAt.Before(now) {
				l.nextTokAt = now
			}
			if l.nextTokAt.After(now) {
				delay = maxDuration(delay, l.nextTokAt.Sub(now))
			}
			base := l.nextTokAt
			if base.Before(now) {
				base = now
			}
			l.nextTokAt = base.Add(interval)
			l.snap.remainingTokens -= estimatedTokens
		}
	}

	return delay
}

func (l *openAIRateLimiter) wait(ctx context.Context, estimatedTokens int) error {
	delay := l.reserve(estimatedTokens)
	if delay <= 0 {
		return nil
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func headerInt(h http.Header, key string) (int, bool) {
	raw := strings.TrimSpace(h.Get(key))
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

func headerDuration(h http.Header, key string) (time.Duration, bool) {
	raw := strings.TrimSpace(h.Get(key))
	if raw == "" {
		return 0, false
	}
	d, err := time.ParseDuration(raw)
	if err == nil {
		return d, true
	}
	// Best-effort: some systems may return seconds as an integer.
	if secs, err2 := strconv.Atoi(raw); err2 == nil {
		return time.Duration(secs) * time.Second, true
	}
	return 0, false
}

func maxDuration(a, b time.Duration) time.Duration {
	if b > a {
		return b
	}
	return a
}
