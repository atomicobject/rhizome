package identity_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/identity"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

const (
	clientID   = "test-client"
	testNonce  = "nonce-1"
	goodCode   = "good-code"
	memberSub  = "sub-member"
	member     = "member@atomicobject.com"
	adminSub   = "sub-admin"
	adminEmail = "admin@atomicobject.com"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// issuer is a minimal OIDC provider: discovery, JWKS, and a token endpoint
// that returns an RS256 ID token built from claims for the presented code.
type issuer struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey // published in the JWKS
	clock *clock

	mu     sync.Mutex
	signer *rsa.PrivateKey
	claims map[string]map[string]any // by authorization code
}

func newIssuer(t *testing.T, c *clock) *issuer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	i := &issuer{key: key, signer: key, clock: c, claims: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": i.srv.URL, "authorization_endpoint": i.srv.URL + "/authorize",
			"token_endpoint": i.srv.URL + "/token", "jwks_uri": i.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &i.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		i.mu.Lock()
		claims, ok := i.claims[r.PostForm.Get("code")]
		signer := i.signer
		i.mu.Unlock()
		if !ok || r.PostForm.Get("code_verifier") == "" || r.PostForm.Get("grant_type") != "authorization_code" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: signer, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
		if err != nil {
			t.Error(err)
			return
		}
		payload, _ := json.Marshal(claims)
		obj, err := sig.Sign(payload)
		if err != nil {
			t.Error(err)
			return
		}
		idToken, _ := obj.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "idp-access", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken})
	})
	i.srv = httptest.NewServer(mux)
	t.Cleanup(i.srv.Close)
	return i
}

// expect registers code to yield an ID token for sub/email with overrides
// applied; a nil override deletes the claim.
func (i *issuer) expect(code, sub, email string, overrides map[string]any) {
	now := i.clock.Now()
	claims := map[string]any{
		"iss": i.srv.URL, "aud": clientID, "azp": clientID, "sub": sub, "email": email,
		"email_verified": true, "hd": "atomicobject.com", "nonce": testNonce,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	maps.Copy(claims, overrides)
	for k, v := range overrides {
		if v == nil {
			delete(claims, k)
		}
	}
	i.mu.Lock()
	i.claims[code] = claims
	i.mu.Unlock()
}

// usageReader reports the same usage for every date.
type usageReader map[string]wire.DailyUsage

func (u usageReader) Usage(context.Context, string) (wire.DailyUsage, map[string]wire.DailyUsage, error) {
	return u["GLOBAL"], u, nil
}

type env struct {
	t     *testing.T
	clock *clock
	idp   *issuer
	db    *sql.DB
	store store.Store
	srv   *httptest.Server
	id    *identity.Identity
	ua    string
}

type options struct {
	mutate func(*config.Config)
	rec    *diagnostics.Recorder
	usage  core.UsageReader
}

func newEnv(t *testing.T, opts options) *env {
	t.Helper()
	c := &clock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	idp := newIssuer(t, c)
	cfgJSON, _ := json.Marshal(map[string]any{
		"mode": "dev", "listen": "127.0.0.1:0", "data_dir": t.TempDir(),
		"issuer": idp.srv.URL, "client_id": clientID, "hd_hint": "atomicobject.com",
		"accepted_domains": []string{"atomicobject.com"}, "admins": []string{adminEmail},
		"eligibility_mode": "all",
		"person_limits":    wire.DailyLimits{Requests: 100, InputBytes: 1000, ChargeCredits: 10},
		"global_limits":    wire.DailyLimits{Requests: 1000, InputBytes: 10000, ChargeCredits: 100},
	})
	cfg, err := config.Parse(cfgJSON)
	if err != nil {
		t.Fatal(err)
	}
	if opts.mutate != nil {
		opts.mutate(&cfg)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	id, err := identity.New(cfg, db, opts.usage, opts.rec, c.Now)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	id.Routes(mux)
	// A stand-in for a provider route, to observe the middleware's principal.
	mux.Handle("GET /test/protected", id.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := core.PrincipalFrom(r.Context())
		json.NewEncoder(w).Encode(p)
	})))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{t: t, clock: c, idp: idp, db: db, store: store.NewSQLite(db), srv: srv, id: id, ua: "rzm/1.0.0"}
}

type result struct {
	status int
	code   wire.ErrorCode
	header http.Header
	body   []byte
}

func (e *env) do(method, path, bearer string, body any) result {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, ok := body.([]byte)
		if !ok {
			b, _ = json.Marshal(body)
		}
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("User-Agent", e.ua)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.Header.Get(wire.HeaderRequestID) == "" {
		e.t.Errorf("%s %s: response has no %s", method, path, wire.HeaderRequestID)
	}
	r := result{status: resp.StatusCode, header: resp.Header, body: b, code: wire.ErrorCode(resp.Header.Get(wire.HeaderGatewayError))}
	if r.code != "" {
		var eb wire.ErrorBody
		if err := json.Unmarshal(b, &eb); err != nil || eb.Error.Code != r.code || r.code.Status() != r.status {
			e.t.Errorf("%s %s: malformed error envelope %d %q: %s", method, path, r.status, r.code, b)
		}
	}
	return r
}

func (e *env) decode(r result, v any) {
	e.t.Helper()
	if r.status != http.StatusOK {
		e.t.Fatalf("status %d (%s): %s", r.status, r.code, r.body)
	}
	if err := json.Unmarshal(r.body, v); err != nil {
		e.t.Fatal(err)
	}
}

func signInRequest(code string) wire.GoogleAuthRequest {
	return wire.GoogleAuthRequest{Code: code, CodeVerifier: "verifier-123", RedirectURI: "http://127.0.0.1:53682/callback", Nonce: testNonce, DeviceName: "test laptop"}
}

// signIn signs sub/email in and returns the grant.
func (e *env) signIn(sub, email string) wire.SessionGrant {
	e.t.Helper()
	code := "code-" + sub + "-" + randomSuffix()
	e.idp.expect(code, sub, email, nil)
	var g wire.SessionGrant
	e.decode(e.do("POST", wire.RouteAuthGoogle, "", signInRequest(code)), &g)
	return g
}

func (e *env) refresh(token string) result {
	return e.do("POST", wire.RouteAuthRefresh, "", wire.RefreshRequest{RefreshToken: token})
}

func (e *env) wantCode(r result, code wire.ErrorCode) {
	e.t.Helper()
	if r.code != code {
		e.t.Fatalf("got %d %q (%s), want %q", r.status, r.code, r.body, code)
	}
}

var codeCounter atomic.Int64

func randomSuffix() string { return strconv.FormatInt(codeCounter.Add(1), 10) }
