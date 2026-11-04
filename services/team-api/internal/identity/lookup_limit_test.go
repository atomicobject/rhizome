package identity_test

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
)

// token returns a well-formed token for session id with a random secret.
func token(prefix, id string) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return prefix + id + "." + base64.RawURLEncoding.EncodeToString(b)
}

// invented returns a well-formed token for a session that does not exist.
func invented(prefix string, n int) string { return token(prefix, "invented"+strconv.Itoa(n)) }

func lookupLimits(l config.SessionLookups) options {
	return options{mutate: func(c *config.Config) { c.SessionLookups = l }}
}

func wantLimited(t *testing.T, e *env, r result) {
	t.Helper()
	e.wantCode(r, wire.CodeRateLimited)
	if r.header.Get("Retry-After") == "" {
		t.Fatal("rate-limited response has no Retry-After")
	}
}

// Invented session IDs miss the authentication cache every time, so a flood
// of them must be refused before it reaches the store, on protected routes,
// logout, and refresh alike, while a signed-in session keeps working.
func TestInventedSessionsAreLimitedBeforeTheStore(t *testing.T) {
	e := newEnv(t, lookupLimits(config.SessionLookups{PerIPBurst: 5, PerIPPerSecond: 1, GlobalBurst: 1000, GlobalPerSecond: 100}))
	e.remote = "203.0.113.7"
	g := e.signIn(memberSub, member)
	if r := e.do("GET", "/test/protected", g.AccessToken, nil); r.status != http.StatusOK {
		t.Fatalf("signed-in request = %d %q", r.status, r.code)
	}
	base := e.sessionReads.Load()

	for i := range 4 {
		e.wantCode(e.do("GET", "/test/protected", invented(wire.AccessTokenPrefix, i), nil), wire.CodeSessionRevoked)
	}
	for i := range 100 {
		wantLimited(t, e, e.do("GET", "/test/protected", invented(wire.AccessTokenPrefix, 100+i), nil))
	}
	wantLimited(t, e, e.do("POST", wire.RouteAuthLogout, invented(wire.AccessTokenPrefix, 999), nil))
	wantLimited(t, e, e.refresh(invented(wire.RefreshTokenPrefix, 999)))
	// Naming a real session with a guessed secret forces a fresh read, so it
	// is limited the same way.
	id := strings.TrimPrefix(strings.SplitN(g.AccessToken, ".", 2)[0], wire.AccessTokenPrefix)
	wantLimited(t, e, e.do("GET", "/test/protected", token(wire.AccessTokenPrefix, id), nil))
	if got := e.sessionReads.Load() - base; got != 4 {
		t.Fatalf("store session reads = %d, want 4 (one bucket token went to the signed-in request)", got)
	}

	// The signed-in session is served from the cache and is not limited.
	for range 50 {
		if r := e.do("GET", "/test/protected", g.AccessToken, nil); r.status != http.StatusOK {
			t.Fatalf("signed-in request during flood = %d %q", r.status, r.code)
		}
	}
	// Another address has its own bucket, and this one refills.
	e.remote = "198.51.100.9"
	e.wantCode(e.do("GET", "/test/protected", invented(wire.AccessTokenPrefix, 1000), nil), wire.CodeSessionRevoked)
	e.remote = "203.0.113.7"
	e.clock.Advance(2 * time.Second)
	if r := e.do("POST", wire.RouteAuthLogout, invented(wire.AccessTokenPrefix, 1001), nil); r.status != http.StatusNoContent {
		t.Fatalf("logout after refill = %d %q", r.status, r.code)
	}
}

// A process-wide bucket caps store reads from many addresses at once.
func TestGlobalSessionLookupLimit(t *testing.T) {
	e := newEnv(t, lookupLimits(config.SessionLookups{PerIPBurst: 100, PerIPPerSecond: 10, GlobalBurst: 3, GlobalPerSecond: 1}))
	limited := 0
	for i := range 20 {
		e.remote = "192.0.2." + strconv.Itoa(i+1)
		if r := e.do("GET", "/test/protected", invented(wire.AccessTokenPrefix, i), nil); r.code == wire.CodeRateLimited {
			limited++
		}
	}
	if got := e.sessionReads.Load(); got != 3 || limited != 17 {
		t.Fatalf("store session reads = %d, limited = %d; want 3 and 17", got, limited)
	}
}

// With the default limits, a signed-in session sends far more requests than
// the per-address burst without being limited, because cache hits are free.
func TestSignedInTrafficIsNotLookupLimited(t *testing.T) {
	e := newEnv(t, options{})
	g := e.signIn(memberSub, member)
	for i := range 500 {
		if i == 250 {
			e.clock.Advance(31 * time.Second) // one cache expiry
		}
		if r := e.do("GET", "/test/protected", g.AccessToken, nil); r.status != http.StatusOK {
			t.Fatalf("request %d = %d %q", i, r.status, r.code)
		}
	}
	if got := e.sessionReads.Load(); got != 2 {
		t.Fatalf("store session reads = %d, want 2", got)
	}
}
