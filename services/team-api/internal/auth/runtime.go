package auth

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/providerbudget"
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
	cfg.ProviderDailyNanodollars = maps.Clone(cfg.ProviderDailyNanodollars)
	cfg.MaxProviderDailyNanodollars = maps.Clone(cfg.MaxProviderDailyNanodollars)
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
		ProviderDailyNanodollars: maps.Clone(cfg.ProviderDailyNanodollars),
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
		return cloneRuntime(r.cached), nil
	}
	c, err := r.store.RuntimeConfig(ctx)
	if err != nil {
		return core.Runtime{}, err
	}
	// Startup ceilings also bound previously persisted settings after a restart.
	for provider, limit := range c.ProviderDailyNanodollars {
		c.ProviderDailyNanodollars[provider] = min(limit, r.cfg.MaxProviderDailyNanodollars[provider])
	}
	c.PricingAssumptions = providerbudget.ReviewedPrices()
	c.AcceptedDomains = slices.Clone(r.cfg.AcceptedDomains)
	c.Admins = slices.Clone(r.cfg.Admins)
	r.cached, r.fetched = c, now
	return cloneRuntime(c), nil
}

// Invalidate makes the next read go to the store. Admin updates call it.
func (r *Runtime) Invalidate() {
	r.mu.Lock()
	r.fetched = time.Time{}
	r.mu.Unlock()
}

// eligible reports whether p may hold a session under c. Sign-in, refresh,
// and every paid call use it, so removing an accepted domain from the
// configuration also ends that domain's existing sessions.
func eligible(p store.Person, c core.Runtime) bool {
	if p.Status != wire.PersonActive || !slices.Contains(c.AcceptedDomains, strings.ToLower(p.HostedDomain)) {
		return false
	}
	return c.EligibilityMode == wire.EligibilityAll ||
		(c.EligibilityMode == wire.EligibilityAllowlist && slices.Contains(c.Allowlist, p.Email))
}

// wrapLimited applies the per-address sign-in and refresh limit.
func wrapLimited(l *core.Limiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(core.ClientHost(r)) {
			w.Header().Set("Retry-After", strconv.Itoa(l.RetryAfter()))
			WriteError(w, wire.CodeRateLimited, "too many sign-in or refresh requests")
			return
		}
		next(w, r)
	}
}

func cloneRuntime(c core.Runtime) core.Runtime {
	c.ProviderDailyNanodollars = maps.Clone(c.ProviderDailyNanodollars)
	c.PricingAssumptions = maps.Clone(c.PricingAssumptions)
	c.AcceptedDomains = slices.Clone(c.AcceptedDomains)
	c.Admins = slices.Clone(c.Admins)
	c.Allowlist = slices.Clone(c.Allowlist)
	return c
}
