package auth

import (
	"context"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

// RuntimeTTL bounds how stale the runtime configuration may be, so the kill
// switch takes effect within 15 seconds.
const RuntimeTTL = 15 * time.Second

// Runtime is the core.RuntimeSource backed by the runtime_config table.
// AcceptedDomains and Admins always come from server configuration.
type Runtime struct {
	store store.Store
	cfg   config.Config
	now   func() time.Time

	mu      sync.Mutex
	cached  core.Runtime
	fetched time.Time
}

// NewRuntime seeds the table from cfg on first start.
func NewRuntime(ctx context.Context, st store.Store, cfg config.Config, now func() time.Time) (*Runtime, error) {
	allow := slices.Clone(cfg.Allowlist)
	for _, a := range cfg.Admins {
		if !slices.Contains(allow, a) {
			allow = append(allow, a)
		}
	}
	err := st.InitRuntimeConfig(ctx, wire.AdminConfig{
		PaidCallsEnabled: true, VoyageEnabled: true, TypeSafeEnabled: true,
		EligibilityMode: cfg.EligibilityMode, Allowlist: allow,
		PersonLimits: cfg.PersonLimits, GlobalLimits: cfg.GlobalLimits,
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{store: st, cfg: cfg, now: now}, nil
}

func (r *Runtime) Runtime(ctx context.Context) (core.Runtime, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if !r.fetched.IsZero() && now.Sub(r.fetched) < RuntimeTTL {
		return r.cached, nil
	}
	c, err := r.store.RuntimeConfig(ctx)
	if err != nil {
		return core.Runtime{}, err
	}
	c.AcceptedDomains = slices.Clone(r.cfg.AcceptedDomains)
	c.Admins = slices.Clone(r.cfg.Admins)
	r.cached, r.fetched = c, now
	return c, nil
}

// Invalidate makes the next read go to the store. Admin updates call it.
func (r *Runtime) Invalidate() {
	r.mu.Lock()
	r.fetched = time.Time{}
	r.mu.Unlock()
}

// eligible reports whether p may hold a session under c.
func eligible(p store.Person, c core.Runtime) bool {
	if p.Status != wire.PersonActive {
		return false
	}
	return c.EligibilityMode == wire.EligibilityAll ||
		(c.EligibilityMode == wire.EligibilityAllowlist && slices.Contains(c.Allowlist, p.Email))
}

// limiter is a token bucket per remote address for the auth endpoints.
// ponytail: keyed by RemoteAddr, so behind a load balancer every client
// shares one bucket; key on the trusted forwarded address when deployed.
type limiter struct {
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

func newLimiter(burst, perSecond float64, now func() time.Time) *limiter {
	return &limiter{burst: burst, rate: perSecond, now: now, buckets: map[string]*bucket{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.buckets) > 10000 {
		for k, b := range l.buckets {
			if now.Sub(b.last) > time.Hour {
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

func (l *limiter) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !l.allow(host) {
			w.Header().Set("Retry-After", strconv.Itoa(int(1/l.rate)+1))
			WriteError(w, wire.CodeRateLimited, "too many sign-in or refresh requests")
			return
		}
		next(w, r)
	}
}
