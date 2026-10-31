package identity_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
)

func TestMetaAndSignIn(t *testing.T) {
	e := newEnv(t, options{usage: usageReader{memberSub: {Requests: 3}}})
	var meta wire.Meta
	e.decode(e.do("GET", wire.RouteMeta, "", nil), &meta)
	if meta.ClientID != clientID || meta.AuthorizationEndpoint != e.idp.srv.URL+"/authorize" ||
		meta.Scopes != "openid email" || meta.HostedDomainHint != "atomicobject.com" || meta.APIVersion != 1 {
		t.Fatalf("meta = %+v", meta)
	}

	g := e.signIn(memberSub, member)
	if !strings.HasPrefix(g.AccessToken, wire.AccessTokenPrefix+g.SessionID+".") ||
		!strings.HasPrefix(g.RefreshToken, wire.RefreshTokenPrefix+g.SessionID+".") {
		t.Fatalf("token format: %+v", g)
	}
	now := e.clock.Now()
	if g.Email != member || !g.AccessExpiresAt.Equal(now.Add(10*time.Minute)) ||
		!g.SessionExpiresAt.Equal(now.Add(30*24*time.Hour)) || g.Limits.VoyageConcurrency != 4 {
		t.Fatalf("grant = %+v", g)
	}
	// Only hashes are stored.
	var access, refresh []byte
	if err := e.db.QueryRow(`SELECT access_hash, refresh_hash FROM sessions WHERE id = ?`, g.SessionID).Scan(&access, &refresh); err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256([]byte(g.AccessToken)); !bytes.Equal(access, sum[:]) {
		t.Fatal("stored access hash is not the token's SHA-256")
	}

	var info wire.SessionInfo
	e.decode(e.do("GET", wire.RouteSession, g.AccessToken, nil), &info)
	if info.SessionID != g.SessionID || info.Email != member || info.DeviceName != "test laptop" ||
		info.Today.Usage.Requests != 3 || info.Today.Limits.Requests != 100 {
		t.Fatalf("session info = %+v", info)
	}
	var p core.Principal
	e.decode(e.do("GET", "/test/protected", g.AccessToken, nil), &p)
	if p.Sub != memberSub || p.SessionID != g.SessionID || p.Admin || p.Limits.Requests != 100 {
		t.Fatalf("principal = %+v", p)
	}
}

func TestSignInRejectsBadIDTokens(t *testing.T) {
	e := newEnv(t, options{})
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	past := e.clock.Now().Add(-time.Hour).Unix()
	cases := []struct {
		name      string
		overrides map[string]any
		forge     bool
		nonce     string
		want      wire.ErrorCode
	}{
		{name: "forged signature", forge: true, want: wire.CodeBadRequest},
		{name: "wrong aud", overrides: map[string]any{"aud": "another-client", "azp": "another-client"}, want: wire.CodeBadRequest},
		{name: "wrong azp", overrides: map[string]any{"aud": []string{clientID, "another-client"}, "azp": "another-client"}, want: wire.CodeBadRequest},
		{name: "wrong iss", overrides: map[string]any{"iss": "https://accounts.google.com"}, want: wire.CodeBadRequest},
		{name: "expired", overrides: map[string]any{"exp": past, "iat": past - 60}, want: wire.CodeBadRequest},
		{name: "bad nonce", nonce: "another-nonce", want: wire.CodeBadRequest},
		{name: "missing nonce claim", overrides: map[string]any{"nonce": nil}, want: wire.CodeBadRequest},
		{name: "email not verified", overrides: map[string]any{"email_verified": false}, want: wire.CodeNotEligible},
		{name: "hd missing", overrides: map[string]any{"hd": nil}, want: wire.CodeNotEligible},
		{name: "hd not accepted", overrides: map[string]any{"hd": "gmail.com"}, want: wire.CodeNotEligible},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := "code-" + strings.ReplaceAll(tc.name, " ", "-")
			e.idp.expect(code, memberSub, member, tc.overrides)
			e.idp.mu.Lock()
			if tc.forge {
				e.idp.signer = other
			}
			e.idp.mu.Unlock()
			defer func() { e.idp.mu.Lock(); e.idp.signer = e.idp.key; e.idp.mu.Unlock() }()
			req := signInRequest(code)
			if tc.nonce != "" {
				req.Nonce = tc.nonce
			}
			r := e.do("POST", wire.RouteAuthGoogle, "", req)
			e.t = t
			e.wantCode(r, tc.want)
		})
	}
	e.t = t
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rejected sign-ins created %d sessions (%v)", n, err)
	}
	// The request's hd hint is never trusted: there is no such field to send.
	e.wantCode(e.do("POST", wire.RouteAuthGoogle, "", []byte(`{"code":"x","code_verifier":"v","redirect_uri":"http://127.0.0.1:1/","nonce":"n","hd":"atomicobject.com"}`)), wire.CodeBadRequest)
	// An unknown or replayed code is rejected by the issuer.
	e.wantCode(e.do("POST", wire.RouteAuthGoogle, "", signInRequest("never-issued")), wire.CodeBadRequest)
	bad := signInRequest(goodCode)
	bad.RedirectURI = "https://attacker.example/callback"
	e.wantCode(e.do("POST", wire.RouteAuthGoogle, "", bad), wire.CodeBadRequest)
	e.wantCode(e.do("POST", wire.RouteAuthGoogle, "", bytes.Repeat([]byte(" "), wire.MaxAuthRequestBytes+1)), wire.CodeTooLarge)
}

func TestEligibility(t *testing.T) {
	e := newEnv(t, options{mutate: func(c *config.Config) { c.EligibilityMode = wire.EligibilityAllowlist }})
	// Admins are seeded onto the allowlist; others are not.
	e.signIn(adminSub, adminEmail)
	code := "code-outsider"
	e.idp.expect(code, memberSub, member, nil)
	e.wantCode(e.do("POST", wire.RouteAuthGoogle, "", signInRequest(code)), wire.CodeNotEligible)

	admin := e.signIn(adminSub, adminEmail)
	list := []string{adminEmail, "MEMBER@atomicobject.com"}
	e.decode(e.do("PATCH", wire.RouteAdminConfig, admin.AccessToken, wire.AdminConfigUpdate{Allowlist: &list}), &wire.AdminConfig{})
	g := e.signIn(memberSub, member)

	disabled := wire.PersonDisabled
	e.decode(e.do("PATCH", "/v1/admin/people/"+memberSub, admin.AccessToken, wire.AdminPersonUpdate{Status: &disabled}), &wire.AdminPerson{})
	code = "code-disabled"
	e.idp.expect(code, memberSub, member, nil)
	e.wantCode(e.do("POST", wire.RouteAuthGoogle, "", signInRequest(code)), wire.CodeNotEligible)
	e.wantCode(e.refresh(g.RefreshToken), wire.CodeNotEligible)
	if p, _ := e.store.Person(t.Context(), memberSub); p.Status != wire.PersonDisabled {
		t.Fatalf("sign-in overwrote disabled status: %+v", p)
	}
}

func TestRefreshRotationAndGrace(t *testing.T) {
	e := newEnv(t, options{})
	g0 := e.signIn(memberSub, member)

	var g1 wire.SessionGrant
	e.decode(e.refresh(g0.RefreshToken), &g1)
	if g1.AccessToken == g0.AccessToken || g1.RefreshToken == g0.RefreshToken || !g1.SessionExpiresAt.Equal(g0.SessionExpiresAt) {
		t.Fatalf("refresh did not rotate both tokens: %+v", g1)
	}
	e.wantCode(e.do("GET", "/test/protected", g0.AccessToken, nil), wire.CodeAccessRefreshRequired)
	e.decode(e.do("GET", "/test/protected", g1.AccessToken, nil), &core.Principal{})

	// The response was lost; the predecessor recovers within the grace period.
	e.clock.Advance(100 * time.Second)
	var g2 wire.SessionGrant
	e.decode(e.refresh(g0.RefreshToken), &g2)
	e.decode(e.do("GET", "/test/protected", g2.AccessToken, nil), &core.Principal{})

	// Recovery did not extend the deadline, which is 120 s after the first
	// rotation, so the predecessor now revokes the session.
	e.clock.Advance(21 * time.Second)
	e.wantCode(e.refresh(g0.RefreshToken), wire.CodeRefreshReused)
	e.wantCode(e.refresh(g2.RefreshToken), wire.CodeSessionRevoked)
	e.wantCode(e.do("GET", "/test/protected", g2.AccessToken, nil), wire.CodeSessionRevoked)
}

func TestUnknownRefreshTokenCannotRevoke(t *testing.T) {
	e := newEnv(t, options{})
	g := e.signIn(memberSub, member)
	forged := wire.RefreshTokenPrefix + g.SessionID + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	e.wantCode(e.refresh(forged), wire.CodeSessionRevoked)
	e.clock.Advance(time.Hour)
	e.wantCode(e.refresh(forged), wire.CodeSessionRevoked)
	e.decode(e.refresh(g.RefreshToken), &wire.SessionGrant{})
}

func TestConcurrentRefreshRotatesOnce(t *testing.T) {
	e := newEnv(t, options{})
	g := e.signIn(memberSub, member)
	const n = 20
	var wg sync.WaitGroup
	results := make([]result, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = e.refresh(g.RefreshToken)
		}()
	}
	wg.Wait()

	var gen int64
	var current, prev []byte
	var deadline int64
	if err := e.db.QueryRow(`SELECT generation, refresh_hash, prev_refresh_hash, prev_refresh_deadline FROM sessions WHERE id = ?`, g.SessionID).
		Scan(&gen, &current, &prev, &deadline); err != nil {
		t.Fatal(err)
	}
	original := sha256.Sum256([]byte(g.RefreshToken))
	if gen != n || !bytes.Equal(prev, original[:]) || deadline != e.clock.Now().Add(120*time.Second).UnixNano() {
		t.Fatalf("after %d refreshes: generation %d, predecessor is original %t, deadline %d", n, gen, bytes.Equal(prev, original[:]), deadline)
	}
	var latest string
	for _, r := range results {
		var grant wire.SessionGrant
		e.decode(r, &grant)
		if sum := sha256.Sum256([]byte(grant.RefreshToken)); bytes.Equal(sum[:], current) {
			if latest != "" {
				t.Fatal("two responses carry the current refresh token")
			}
			latest = grant.RefreshToken
		}
	}
	if latest == "" {
		t.Fatal("no response carries the current refresh token")
	}
	e.decode(e.refresh(latest), &wire.SessionGrant{})
}

func TestTokenConfusion(t *testing.T) {
	e := newEnv(t, options{})
	a := e.signIn(memberSub, member)
	b := e.signIn(adminSub, adminEmail)
	e.wantCode(e.refresh(a.AccessToken), wire.CodeSessionRevoked)
	e.wantCode(e.do("GET", "/test/protected", a.RefreshToken, nil), wire.CodeSessionRevoked)
	e.wantCode(e.refresh(a.RefreshToken[:len(a.RefreshToken)-3]), wire.CodeSessionRevoked)
	e.wantCode(e.do("GET", "/test/protected", a.AccessToken[:len(a.AccessToken)-3], nil), wire.CodeSessionRevoked)
	e.wantCode(e.do("GET", "/test/protected", "", nil), wire.CodeSessionRevoked)

	// Session B's ID with session A's valid secret.
	_, secretA, _ := strings.Cut(a.AccessToken, ".")
	if r := e.do("GET", "/test/protected", wire.AccessTokenPrefix+b.SessionID+"."+secretA, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("spliced access token got %d", r.status)
	}
	_, refreshA, _ := strings.Cut(a.RefreshToken, ".")
	e.wantCode(e.refresh(wire.RefreshTokenPrefix+b.SessionID+"."+refreshA), wire.CodeSessionRevoked)
	// Neither session was harmed.
	e.decode(e.do("GET", "/test/protected", a.AccessToken, nil), &core.Principal{})
	e.decode(e.do("GET", "/test/protected", b.AccessToken, nil), &core.Principal{})
	e.decode(e.refresh(b.RefreshToken), &wire.SessionGrant{})
}

func TestSessionLifetimes(t *testing.T) {
	e := newEnv(t, options{mutate: func(c *config.Config) {
		c.Sessions.IdleTTL = config.Duration(2 * time.Hour)
		c.Sessions.AbsoluteTTL = config.Duration(3 * time.Hour)
	}})
	g := e.signIn(memberSub, member)
	start := e.clock.Now()
	for range 2 {
		e.clock.Advance(80 * time.Minute)
		var next wire.SessionGrant
		e.decode(e.refresh(g.RefreshToken), &next)
		if !next.SessionExpiresAt.Equal(start.Add(3 * time.Hour)) {
			t.Fatalf("refresh moved the absolute expiry to %v", next.SessionExpiresAt)
		}
		g = next
	}
	e.clock.Advance(15 * time.Minute) // the access token expired at 170 minutes
	e.wantCode(e.do("GET", "/test/protected", g.AccessToken, nil), wire.CodeAccessRefreshRequired)
	e.decode(e.refresh(g.RefreshToken), &g)
	if !g.AccessExpiresAt.Equal(g.SessionExpiresAt) {
		t.Fatalf("access token %v is not capped at the session expiry %v", g.AccessExpiresAt, g.SessionExpiresAt)
	}
	e.clock.Advance(5 * time.Minute) // exactly at the absolute limit
	e.wantCode(e.refresh(g.RefreshToken), wire.CodeSessionExpired)
	e.wantCode(e.do("GET", "/test/protected", g.AccessToken, nil), wire.CodeSessionExpired)

	// Idle expiry: no refresh for the idle period.
	idle := e.signIn(memberSub, member)
	e.clock.Advance(2 * time.Hour)
	e.wantCode(e.refresh(idle.RefreshToken), wire.CodeSessionExpired)
	e.wantCode(e.do("GET", "/test/protected", idle.AccessToken, nil), wire.CodeSessionExpired)
}

func TestRevocationWithinCacheWindow(t *testing.T) {
	e := newEnv(t, options{})
	admin := e.signIn(adminSub, adminEmail)
	g := e.signIn(memberSub, member)
	other := e.signIn(memberSub, member)
	e.decode(e.do("GET", "/test/protected", g.AccessToken, nil), &core.Principal{})

	if r := e.do("POST", "/v1/admin/sessions/"+g.SessionID+"/revoke", admin.AccessToken, nil); r.status != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", r.status, r.body)
	}
	e.clock.Advance(30 * time.Second)
	e.wantCode(e.do("GET", "/test/protected", g.AccessToken, nil), wire.CodeSessionRevoked)
	e.wantCode(e.refresh(g.RefreshToken), wire.CodeSessionRevoked)
	e.decode(e.do("GET", "/test/protected", other.AccessToken, nil), &core.Principal{})

	disabled := wire.PersonDisabled
	e.decode(e.do("PATCH", "/v1/admin/people/"+memberSub, admin.AccessToken, wire.AdminPersonUpdate{Status: &disabled}), &wire.AdminPerson{})
	e.clock.Advance(30 * time.Second)
	e.wantCode(e.do("GET", "/test/protected", other.AccessToken, nil), wire.CodeNotEligible)
}

func TestLogout(t *testing.T) {
	e := newEnv(t, options{})
	g := e.signIn(memberSub, member)
	e.decode(e.do("GET", "/test/protected", g.AccessToken, nil), &core.Principal{})
	if r := e.do("POST", wire.RouteAuthLogout, g.AccessToken, nil); r.status != http.StatusNoContent {
		t.Fatalf("logout = %d %s", r.status, r.body)
	}
	e.wantCode(e.do("GET", "/test/protected", g.AccessToken, nil), wire.CodeSessionRevoked)
	e.wantCode(e.refresh(g.RefreshToken), wire.CodeSessionRevoked)
	if r := e.do("POST", wire.RouteAuthLogout, g.AccessToken, nil); r.status != http.StatusNoContent {
		t.Fatalf("second logout = %d", r.status)
	}
}

func TestMinimumClientVersion(t *testing.T) {
	e := newEnv(t, options{mutate: func(c *config.Config) { c.MinClientVersion = "1.2.0" }})
	e.ua = "rzm/1.2.0"
	g := e.signIn(memberSub, member)
	for ua, ok := range map[string]bool{"rzm/1.1.9": false, "curl/8.0": false, "rzm/dev": true, "rzm/1.10.0 (darwin)": true, "rzm/v1.2.0-rc1": true} {
		e.ua = ua
		r := e.do("GET", "/test/protected", g.AccessToken, nil)
		if ok != (r.status == http.StatusOK) || (!ok && r.code != wire.CodeUpgradeRequired) {
			t.Errorf("%s: got %d %q", ua, r.status, r.code)
		}
	}
	e.ua = "rzm/1.0.0"
	e.wantCode(e.refresh(g.RefreshToken), wire.CodeUpgradeRequired)
	e.wantCode(e.do("POST", wire.RouteAuthGoogle, "", signInRequest("any")), wire.CodeUpgradeRequired)
}

func TestAuthRateLimit(t *testing.T) {
	e := newEnv(t, options{})
	var limited result
	for range 61 {
		limited = e.refresh("garbage")
	}
	e.wantCode(limited, wire.CodeRateLimited)
	if limited.header.Get("Retry-After") == "" {
		t.Fatal("rate-limited response has no Retry-After")
	}
	e.clock.Advance(5 * time.Second)
	e.wantCode(e.refresh("garbage"), wire.CodeSessionRevoked)
}

func TestDiagnosticsOmitSecrets(t *testing.T) {
	dir := t.TempDir()
	rec, err := diagnostics.Open(dir, diagnostics.Options{Role: "team-api"})
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, options{rec: rec})
	admin := e.signIn(adminSub, adminEmail)
	g := e.signIn(memberSub, member)
	var next wire.SessionGrant
	e.decode(e.refresh(g.RefreshToken), &next)
	e.do("POST", "/v1/admin/sessions/"+g.SessionID+"/revoke", admin.AccessToken, nil)
	e.do("POST", wire.RouteAuthLogout, admin.AccessToken, nil)
	rec.Close()

	var all []byte
	files, _ := filepath.Glob(filepath.Join(dir, ".rhizome", "diagnostics", "events", "*.jsonl"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		all = append(all, b...)
	}
	for _, want := range []string{"auth.signin", "auth.refresh", "auth.logout", "admin.session.revoke", g.SessionID, "duration_ms"} {
		if !bytes.Contains(all, []byte(want)) {
			t.Errorf("diagnostics lack %q", want)
		}
	}
	for _, secret := range []string{"rzt_", "@atomicobject.com", goodCode, "code-", "verifier-123"} {
		if bytes.Contains(all, []byte(secret)) {
			t.Errorf("diagnostics contain %q", secret)
		}
	}
}
