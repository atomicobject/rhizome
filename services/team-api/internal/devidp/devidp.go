// Package devidp is a real OpenID Connect issuer for local development and
// security tests. It stands in for Google so the gateway runs the same
// verification code path locally: discovery, JWKS, RS256 ID tokens, PKCE, and
// one-time authorization codes. It refuses any issuer that is not plain HTTP
// on a loopback host, so it cannot be mistaken for a deployed provider.
package devidp

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	codeTTL    = 60 * time.Second
	idTokenTTL = 5 * time.Minute
	pendingTTL = 10 * time.Minute
)

// Identity is one synthetic person offered on the sign-in page.
type Identity struct {
	// Name is the stable handle shown on the button and posted by the form.
	Name          string
	Sub           string
	Email         string
	EmailVerified bool
	// HostedDomain becomes the hd claim when non-empty.
	HostedDomain string
}

// DefaultIdentities are used when Config.Identities is empty. Their subs are
// stable across runs so local state keyed by sub survives restarts.
func DefaultIdentities() []Identity {
	return []Identity{
		{Name: "member", Sub: "110000000000000000001", Email: "dev-member@atomicobject.com", EmailVerified: true, HostedDomain: "atomicobject.com"},
		{Name: "admin", Sub: "110000000000000000002", Email: "dev-admin@atomicobject.com", EmailVerified: true, HostedDomain: "atomicobject.com"},
		{Name: "outsider", Sub: "110000000000000000003", Email: "outsider@example.com", EmailVerified: true},
		{Name: "unverified", Sub: "110000000000000000004", Email: "dev-unverified@atomicobject.com", EmailVerified: false, HostedDomain: "atomicobject.com"},
		{Name: "wrongdomain", Sub: "110000000000000000005", Email: "someone@example.org", EmailVerified: true, HostedDomain: "example.org"},
	}
}

type Config struct {
	// Issuer is the exact iss value and the URL the handler is served under,
	// for example http://127.0.0.1:8788/dev-idp.
	Issuer     string
	ClientID   string
	Identities []Identity
	// Now defaults to time.Now. Tests inject it to expire codes.
	Now func() time.Time
}

// IdP is safe for concurrent use.
type IdP struct {
	issuer     string
	basePath   string
	clientID   string
	identities []Identity
	now        func() time.Time
	key        *rsa.PrivateKey
	kid        string
	signer     jose.Signer

	mu      sync.Mutex
	pending map[string]authRequest // CSRF token -> authorize parameters
	codes   map[string]codeGrant
}

type authRequest struct {
	redirectURI, challenge, state, nonce string
	expires                              time.Time
}

type codeGrant struct {
	authRequest
	identity Identity
}

// New generates a fresh RSA-2048 signing key and validates the configuration.
func New(cfg Config) (*IdP, error) {
	issuer := strings.TrimRight(cfg.Issuer, "/")
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "http" || !loopbackHost(u.Hostname()) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("devidp: issuer must be a plain http URL on a loopback host, got %q", cfg.Issuer)
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("devidp: client ID is required")
	}
	identities := cfg.Identities
	if len(identities) == 0 {
		identities = DefaultIdentities()
	}
	seen := map[string]bool{}
	for _, id := range identities {
		if id.Name == "" || id.Sub == "" || id.Email == "" || seen[id.Name] {
			return nil, fmt.Errorf("devidp: identity %q needs a unique name, a sub, and an email", id.Name)
		}
		seen[id.Name] = true
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("devidp: generate key: %w", err)
	}
	kid := randomToken(12)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: kid}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return nil, fmt.Errorf("devidp: signer: %w", err)
	}
	return &IdP{
		issuer: issuer, basePath: u.Path, clientID: cfg.ClientID,
		identities: append([]Identity(nil), identities...), now: now,
		key: key, kid: kid, signer: signer,
		pending: map[string]authRequest{}, codes: map[string]codeGrant{},
	}, nil
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Issuer returns the normalized issuer URL (no trailing slash).
func (p *IdP) Issuer() string { return p.issuer }

// Identities returns a copy of the offered identities.
func (p *IdP) Identities() []Identity { return append([]Identity(nil), p.identities...) }

// Handler serves the issuer's endpoints under the issuer URL's path.
func (p *IdP) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+p.basePath+"/.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET "+p.basePath+"/jwks", p.jwks)
	mux.HandleFunc("GET "+p.basePath+"/authorize", p.authorizePage)
	mux.HandleFunc("POST "+p.basePath+"/authorize", p.authorizeSubmit)
	mux.HandleFunc("POST "+p.basePath+"/token", p.token)
	return mux
}

func (p *IdP) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                p.issuer,
		"authorization_endpoint":                p.issuer + "/authorize",
		"token_endpoint":                        p.issuer + "/token",
		"jwks_uri":                              p.issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic", "client_secret_post"},
		"claims_supported":                      []string{"iss", "aud", "sub", "email", "email_verified", "hd", "nonce", "iat", "exp"},
	})
}

func (p *IdP) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &p.key.PublicKey, KeyID: p.kid, Algorithm: string(jose.RS256), Use: "sig",
	}}})
}

// MintIDToken signs arbitrary claims with the IdP's key. It exists for
// hostile-token tests (wrong audience, expired, missing hd, and so on).
func (p *IdP) MintIDToken(claims map[string]any) (string, error) {
	return jwt.Signed(p.signer).Claims(claims).Serialize()
}

func (p *IdP) idToken(g codeGrant) (string, error) {
	now := p.now()
	claims := map[string]any{
		"iss":            p.issuer,
		"aud":            p.clientID,
		"azp":            p.clientID,
		"sub":            g.identity.Sub,
		"email":          g.identity.Email,
		"email_verified": g.identity.EmailVerified,
		"nonce":          g.nonce,
		"iat":            now.Unix(),
		"exp":            now.Add(idTokenTTL).Unix(),
	}
	if g.identity.HostedDomain != "" {
		claims["hd"] = g.identity.HostedDomain
	}
	return p.MintIDToken(claims)
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// oauthError writes the RFC 6749 error JSON.
func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}
