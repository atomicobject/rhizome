package devidp

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const maxFormBytes = 16 << 10

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Rhizome development sign-in</title>
<style>
body { font: 15px/1.5 system-ui, sans-serif; max-width: 32rem; margin: 3rem auto; padding: 0 1rem; }
.note { background: #fff4d6; border: 1px solid #e6c56b; padding: .75rem 1rem; border-radius: 6px; }
button { display: block; width: 100%; text-align: left; margin: .5rem 0; padding: .6rem .9rem; font: inherit; cursor: pointer; }
small { color: #555; }
</style>
</head>
<body>
<h1>Rhizome development sign-in</h1>
<p class="note">This is the local development identity provider, not Google. Every identity here is synthetic.</p>
<form method="post" action="{{.Action}}">
<input type="hidden" name="csrf" value="{{.CSRF}}">
{{range .Identities}}<button type="submit" name="identity" value="{{.Name}}"><strong>{{.Name}}</strong><br><small>{{.Email}}{{if .HostedDomain}} · hd {{.HostedDomain}}{{end}}{{if not .EmailVerified}} · email not verified{{end}}</small></button>
{{end}}</form>
</body>
</html>
`))

// validRedirectURI accepts only http://127.0.0.1:<port>/... or
// http://localhost:<port>/..., the loopback redirects a desktop client uses.
func validRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" || u.Port() == "" {
		return false
	}
	return u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"
}

func (p *IdP) authorizePage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case q.Get("client_id") != p.clientID:
		oauthError(w, http.StatusBadRequest, "unauthorized_client", "unknown client_id")
		return
	case !validRedirectURI(q.Get("redirect_uri")):
		oauthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri must be http://127.0.0.1:<port>/ or http://localhost:<port>/")
		return
	case q.Get("response_type") != "code":
		oauthError(w, http.StatusBadRequest, "unsupported_response_type", "response_type must be code")
		return
	case !slices.Contains(strings.Fields(q.Get("scope")), "openid"):
		oauthError(w, http.StatusBadRequest, "invalid_scope", "scope must include openid")
		return
	case q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) < 43 || len(q.Get("code_challenge")) > 128:
		oauthError(w, http.StatusBadRequest, "invalid_request", "a PKCE code_challenge with code_challenge_method=S256 is required")
		return
	case q.Get("state") == "" || q.Get("nonce") == "":
		oauthError(w, http.StatusBadRequest, "invalid_request", "state and nonce are required")
		return
	}
	csrf := randomToken(32)
	now := p.now()
	p.mu.Lock()
	sweep(p.pending, now)
	p.pending[csrf] = authRequest{
		redirectURI: q.Get("redirect_uri"), challenge: q.Get("code_challenge"),
		state: q.Get("state"), nonce: q.Get("nonce"), expires: now.Add(pendingTTL),
	}
	p.mu.Unlock()

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	_ = pageTemplate.Execute(w, map[string]any{
		"Action": p.basePath + "/authorize", "CSRF": csrf, "Identities": p.identities,
	})
}

func (p *IdP) authorizeSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "malformed form")
		return
	}
	csrf := r.PostForm.Get("csrf")
	now := p.now()
	p.mu.Lock()
	// The token is single-use: a second submit of the same page fails.
	req, ok := p.pending[csrf]
	delete(p.pending, csrf)
	p.mu.Unlock()
	if csrf == "" || !ok || now.After(req.expires) {
		oauthError(w, http.StatusForbidden, "access_denied", "missing, unknown, or expired CSRF token; reload the sign-in page")
		return
	}
	var identity *Identity
	for i := range p.identities {
		if p.identities[i].Name == r.PostForm.Get("identity") {
			identity = &p.identities[i]
		}
	}
	if identity == nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "unknown identity")
		return
	}
	code := randomToken(32)
	p.mu.Lock()
	sweep(p.codes, now)
	grant := codeGrant{authRequest: req, identity: *identity}
	grant.expires = now.Add(codeTTL)
	p.codes[code] = grant
	p.mu.Unlock()

	target, _ := url.Parse(req.redirectURI) // validated at render time
	v := target.Query()
	v.Set("code", code)
	v.Set("state", req.state)
	target.RawQuery = v.Encode()
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func (p *IdP) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "malformed form")
		return
	}
	f := r.PostForm
	// Accept client_secret_basic, client_secret_post, or a public client. The
	// development client has no secret to check.
	clientID := f.Get("client_id")
	if user, _, ok := r.BasicAuth(); ok {
		clientID, _ = url.QueryUnescape(user)
	}
	if f.Get("grant_type") != "authorization_code" {
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code")
		return
	}
	if clientID != p.clientID {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		return
	}
	code := f.Get("code")
	now := p.now()
	p.mu.Lock()
	// Consume the code before any other check so every attempt burns it.
	grant, ok := p.codes[code]
	delete(p.codes, code)
	p.mu.Unlock()
	switch {
	case code == "" || !ok:
		oauthError(w, http.StatusBadRequest, "invalid_grant", "unknown or already used code")
		return
	case now.After(grant.expires):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code expired")
		return
	case f.Get("redirect_uri") != grant.redirectURI:
		oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
		return
	case !pkceMatches(f.Get("code_verifier"), grant.challenge):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}
	idToken, err := p.idToken(grant)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "could not sign ID token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": "devidp_" + randomToken(32),
		"token_type":   "Bearer",
		"expires_in":   int(idTokenTTL.Seconds()),
		"id_token":     idToken,
	})
}

func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

type expiring interface{ expiry() time.Time }

func (a authRequest) expiry() time.Time { return a.expires }

// sweep drops expired entries. Callers hold p.mu.
// ponytail: O(n) per insert, fine for a single-developer issuer.
func sweep[T expiring](m map[string]T, now time.Time) {
	for k, v := range m {
		if now.After(v.expiry()) {
			delete(m, k)
		}
	}
}
