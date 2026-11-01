package devidp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	testClient   = "dev-client"
	testRedirect = "http://127.0.0.1:53682/callback"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time                       { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration)                  { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }
func newClock() *clock                                { return &clock{t: time.Now()} }
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

type harness struct {
	t     *testing.T
	idp   *IdP
	srv   *httptest.Server
	clock *clock
	http  *http.Client
	oauth *oauth2.Config
	ctx   context.Context
}

func start(t *testing.T) *harness {
	t.Helper()
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	c := newClock()
	idp, err := New(Config{Issuer: srv.URL + "/dev-idp", ClientID: testClient, Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = idp.Handler()
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, idp.Issuer())
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	return &harness{
		t: t, idp: idp, srv: srv, clock: c, ctx: ctx,
		http: &http.Client{CheckRedirect: noRedirect},
		oauth: &oauth2.Config{
			ClientID: testClient, Endpoint: provider.Endpoint(), RedirectURL: testRedirect,
			Scopes: []string{oidc.ScopeOpenID, "email"},
		},
	}
}

var csrfPattern = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// authorize loads the sign-in page and returns its CSRF token.
func (h *harness) authorize(verifier string) string {
	h.t.Helper()
	resp, err := h.http.Get(h.oauth.AuthCodeURL("state-1", oauth2.S256ChallengeOption(verifier), oidc.Nonce("nonce-1")))
	if err != nil {
		h.t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("authorize page: %d %s", resp.StatusCode, body)
	}
	page := string(body)
	for _, want := range []string{"<title>Rhizome development sign-in</title>", "not Google", `value="member"`, `value="wrongdomain"`} {
		if !strings.Contains(page, want) {
			h.t.Fatalf("authorize page lacks %q", want)
		}
	}
	m := csrfPattern.FindStringSubmatch(page)
	if m == nil {
		h.t.Fatal("no CSRF token on page")
	}
	return m[1]
}

func (h *harness) submit(form url.Values) *http.Response {
	h.t.Helper()
	resp, err := h.http.PostForm(h.idp.Issuer()+"/authorize", form)
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

// code runs the browser part of the flow for identity and returns the code.
func (h *harness) code(identity, verifier string) string {
	h.t.Helper()
	resp := h.submit(url.Values{"csrf": {h.authorize(verifier)}, "identity": {identity}})
	if resp.StatusCode != http.StatusSeeOther {
		h.t.Fatalf("submit: %d", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || !strings.HasPrefix(loc.String(), testRedirect+"?") || loc.Query().Get("state") != "state-1" {
		h.t.Fatalf("redirect = %q", resp.Header.Get("Location"))
	}
	return loc.Query().Get("code")
}

func oauthErrorCode(t *testing.T, err error) string {
	t.Helper()
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		t.Fatalf("expected an OAuth error, got %v", err)
	}
	return re.ErrorCode
}

func TestFullFlow(t *testing.T) {
	h := start(t)
	verifier := oauth2.GenerateVerifier()
	tok, err := h.oauth.Exchange(h.ctx, h.code("member", verifier), oauth2.VerifierOption(verifier))
	if err != nil {
		t.Fatal(err)
	}
	if tok.TokenType != "Bearer" || tok.AccessToken == "" || tok.Expiry.IsZero() {
		t.Fatalf("token = %+v", tok)
	}
	raw, _ := tok.Extra("id_token").(string)
	provider, _ := oidc.NewProvider(h.ctx, h.idp.Issuer())
	idt, err := provider.Verifier(&oidc.Config{ClientID: testClient}).Verify(h.ctx, raw)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		HD            string `json:"hd"`
	}
	if err := idt.Claims(&claims); err != nil {
		t.Fatal(err)
	}
	if idt.Subject != "110000000000000000001" || idt.Nonce != "nonce-1" || claims.Email != "dev-member@atomicobject.com" || !claims.EmailVerified || claims.HD != "atomicobject.com" {
		t.Fatalf("claims = %+v %+v", idt, claims)
	}
	if got := idt.Expiry.Sub(idt.IssuedAt); got != 5*time.Minute {
		t.Fatalf("lifetime = %v", got)
	}
	// Audience is enforced.
	if _, err := provider.Verifier(&oidc.Config{ClientID: "other"}).Verify(h.ctx, raw); err == nil {
		t.Fatal("verifier accepted a token for another audience")
	}
}

func TestOutsiderHasNoHostedDomain(t *testing.T) {
	h := start(t)
	verifier := oauth2.GenerateVerifier()
	tok, err := h.oauth.Exchange(h.ctx, h.code("outsider", verifier), oauth2.VerifierOption(verifier))
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := oidc.NewProvider(h.ctx, h.idp.Issuer())
	idt, err := provider.Verifier(&oidc.Config{ClientID: testClient}).Verify(h.ctx, tok.Extra("id_token").(string))
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	_ = idt.Claims(&claims)
	if _, ok := claims["hd"]; ok {
		t.Fatalf("outsider token has hd: %v", claims)
	}
}

func TestCodeReplayFails(t *testing.T) {
	h := start(t)
	verifier := oauth2.GenerateVerifier()
	code := h.code("member", verifier)
	if _, err := h.oauth.Exchange(h.ctx, code, oauth2.VerifierOption(verifier)); err != nil {
		t.Fatal(err)
	}
	_, err := h.oauth.Exchange(h.ctx, code, oauth2.VerifierOption(verifier))
	if got := oauthErrorCode(t, err); got != "invalid_grant" {
		t.Fatalf("replay error = %q", got)
	}
}

func TestWrongVerifierFails(t *testing.T) {
	h := start(t)
	code := h.code("member", oauth2.GenerateVerifier())
	_, err := h.oauth.Exchange(h.ctx, code, oauth2.VerifierOption(oauth2.GenerateVerifier()))
	if got := oauthErrorCode(t, err); got != "invalid_grant" {
		t.Fatalf("error = %q", got)
	}
}

func TestMismatchedRedirectFails(t *testing.T) {
	h := start(t)
	verifier := oauth2.GenerateVerifier()
	code := h.code("member", verifier)
	other := *h.oauth
	other.RedirectURL = "http://127.0.0.1:53682/other"
	_, err := other.Exchange(h.ctx, code, oauth2.VerifierOption(verifier))
	if got := oauthErrorCode(t, err); got != "invalid_grant" {
		t.Fatalf("error = %q", got)
	}
}

func TestExpiredCodeFails(t *testing.T) {
	h := start(t)
	verifier := oauth2.GenerateVerifier()
	code := h.code("member", verifier)
	h.clock.Add(61 * time.Second)
	_, err := h.oauth.Exchange(h.ctx, code, oauth2.VerifierOption(verifier))
	if got := oauthErrorCode(t, err); got != "invalid_grant" {
		t.Fatalf("error = %q", got)
	}
}

func TestAuthorizeRejectsBadRequests(t *testing.T) {
	h := start(t)
	verifier := oauth2.GenerateVerifier()
	good, _ := url.Parse(h.oauth.AuthCodeURL("s", oauth2.S256ChallengeOption(verifier), oidc.Nonce("n")))
	cases := map[string]func(url.Values){
		"non-loopback redirect": func(v url.Values) { v.Set("redirect_uri", "http://evil.example:8080/cb") },
		"https redirect":        func(v url.Values) { v.Set("redirect_uri", "https://127.0.0.1:8080/cb") },
		"redirect without port": func(v url.Values) { v.Set("redirect_uri", "http://127.0.0.1/cb") },
		"wrong client":          func(v url.Values) { v.Set("client_id", "other") },
		"plain PKCE":            func(v url.Values) { v.Set("code_challenge_method", "plain") },
		"no PKCE":               func(v url.Values) { v.Del("code_challenge") },
		"no nonce":              func(v url.Values) { v.Del("nonce") },
		"no state":              func(v url.Values) { v.Del("state") },
		"no openid scope":       func(v url.Values) { v.Set("scope", "email") },
		"token response type":   func(v url.Values) { v.Set("response_type", "token") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			u := *good
			q := u.Query()
			mutate(q)
			u.RawQuery = q.Encode()
			resp, err := h.http.Get(u.String())
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body struct{ Error string }
			_ = json.NewDecoder(resp.Body).Decode(&body)
			if resp.StatusCode != http.StatusBadRequest || body.Error == "" {
				t.Fatalf("status %d, error %q", resp.StatusCode, body.Error)
			}
		})
	}
}

func TestSubmitRequiresCSRFToken(t *testing.T) {
	h := start(t)
	token := h.authorize(oauth2.GenerateVerifier())
	for name, form := range map[string]url.Values{
		"missing": {"identity": {"member"}},
		"forged":  {"identity": {"member"}, "csrf": {"forged"}},
	} {
		if resp := h.submit(form); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s token: status %d", name, resp.StatusCode)
		}
	}
	// A real token works once.
	if resp := h.submit(url.Values{"csrf": {token}, "identity": {"admin"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("valid token: status %d", resp.StatusCode)
	}
	if resp := h.submit(url.Values{"csrf": {token}, "identity": {"admin"}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("reused token: status %d", resp.StatusCode)
	}
}

func TestTokenErrorsUseOAuthJSON(t *testing.T) {
	h := start(t)
	resp, err := http.PostForm(h.idp.Issuer()+"/token", url.Values{"grant_type": {"password"}, "client_id": {testClient}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body["error"] != "unsupported_grant_type" || body["error_description"] == "" {
		t.Fatalf("status %d body %v err %v", resp.StatusCode, body, err)
	}
}

func TestNewRefusesNonLoopbackIssuer(t *testing.T) {
	for _, issuer := range []string{
		"https://127.0.0.1:8080", "http://accounts.google.com", "http://10.0.0.5:8080",
		"http://localhost.evil.example", "http://user@127.0.0.1:1", "",
	} {
		if _, err := New(Config{Issuer: issuer, ClientID: testClient}); err == nil {
			t.Errorf("issuer %q accepted", issuer)
		}
	}
	for _, issuer := range []string{"http://127.0.0.1:8080", "http://localhost:9/idp/", "http://[::1]:7"} {
		if _, err := New(Config{Issuer: issuer, ClientID: testClient}); err != nil {
			t.Errorf("issuer %q refused: %v", issuer, err)
		}
	}
}

func TestMintIDTokenRoundTrips(t *testing.T) {
	h := start(t)
	exp := time.Now().Add(time.Hour).Unix()
	raw, err := h.idp.MintIDToken(map[string]any{
		"iss": h.idp.Issuer(), "aud": testClient, "sub": "hostile", "exp": exp, "iat": time.Now().Unix(),
		"email": "x@example.com", "custom": []any{"a", 1.5},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := oidc.NewProvider(h.ctx, h.idp.Issuer())
	idt, err := provider.Verifier(&oidc.Config{ClientID: testClient}).Verify(h.ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	_ = idt.Claims(&claims)
	if idt.Subject != "hostile" || claims["email"] != "x@example.com" || claims["custom"].([]any)[1] != 1.5 {
		t.Fatalf("claims = %v", claims)
	}
	// Hostile variants the gateway must reject are still signed validly.
	wrongIss, _ := h.idp.MintIDToken(map[string]any{"iss": "https://accounts.google.com", "aud": testClient, "sub": "s", "exp": exp})
	if _, err := provider.Verifier(&oidc.Config{ClientID: testClient}).Verify(h.ctx, wrongIss); err == nil {
		t.Fatal("verifier accepted a wrong issuer")
	}
	if len(h.idp.Identities()) != 5 {
		t.Fatalf("identities = %v", h.idp.Identities())
	}
}
