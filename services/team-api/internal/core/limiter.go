package core

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// maxBuckets is the bucket count above which Allow drops buckets that have
// refilled completely; a full bucket behaves exactly like a missing one.
const maxBuckets = 10000

// Limiter is a token bucket per key. Callers key it by ClientHost, by a
// person's sub, or by a constant for a process-wide bucket.
type Limiter struct {
	burst float64
	rate  float64 // tokens per second
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter allows burst requests per key at once, refilling perSecond.
func NewLimiter(burst, perSecond float64, now func() time.Time) *Limiter {
	return &Limiter{burst: burst, rate: perSecond, now: now, buckets: map[string]*bucket{}}
}

// Allow takes one token from key's bucket and reports whether one was there.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.buckets) > maxBuckets {
		for k, b := range l.buckets {
			if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
				delete(l.buckets, k)
			}
		}
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RetryAfter is the Retry-After value, in whole seconds, for a refused request.
func (l *Limiter) RetryAfter() int { return int(1/l.rate) + 1 }

// ClientHost is the caller's address for per-address limits. The server
// rewrites RemoteAddr from X-Forwarded-For only for trusted ALB peers.
func ClientHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
