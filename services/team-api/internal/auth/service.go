// Package auth signs people in with OIDC, issues and rotates gateway
// sessions, and authenticates requests. It verifies the development issuer
// and Google with the same code; only the configuration differs.
package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

const subsystem = "teamapi.auth"

type Service struct {
	cfg     config.Config
	store   store.Store
	runtime *Runtime
	usage   core.UsageReader
	rec     *diagnostics.Recorder
	now     func() time.Time
	client  *http.Client
	limiter *limiter
	cache   sessionCache

	mu       sync.Mutex
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

// New builds the service. usage and rec may be nil.
func New(cfg config.Config, st store.Store, rt *Runtime, usage core.UsageReader, rec *diagnostics.Recorder, now func() time.Time) *Service {
	// Identity-provider calls ignore proxy environment variables, like the
	// provider upstreams, and refuse redirects.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{
		Transport:     transport,
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Service{
		cfg: cfg, store: st, runtime: rt, usage: usage, rec: rec, now: now, client: client,
		limiter: newLimiter(60, 1, now),
		cache:   sessionCache{entries: map[string]cachedSession{}},
	}
}

// oidcProvider runs discovery on first use and keeps the result. A failure
// is retried on the next call, so the gateway can start before its issuer.
func (s *Service) oidcProvider() (*oidc.Provider, *oidc.IDTokenVerifier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider != nil {
		return s.provider, s.verifier, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(context.Background(), s.client), s.cfg.Issuer)
	if err != nil {
		return nil, nil, err
	}
	s.provider = p
	s.verifier = p.Verifier(&oidc.Config{ClientID: s.cfg.ClientID, Now: s.now, SupportedSigningAlgs: []string{oidc.RS256}})
	return s.provider, s.verifier, nil
}

func (s *Service) Meta(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.oidcProvider()
	if err != nil {
		WriteError(w, wire.CodeUpstreamDown, "identity provider discovery failed")
		return
	}
	WriteJSON(w, wire.Meta{
		APIVersion:            wire.APIVersion,
		ClientID:              s.cfg.ClientID,
		AuthorizationEndpoint: p.Endpoint().AuthURL,
		Scopes:                s.cfg.Scopes,
		HostedDomainHint:      s.cfg.HDHint,
		MinClientVersion:      s.cfg.MinClientVersion,
	})
}

type idClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	HD            string `json:"hd"`
	AZP           string `json:"azp"`
}

// SignIn handles POST /v1/auth/google.
func (s *Service) SignIn(w http.ResponseWriter, r *http.Request) { s.limiter.wrap(s.signIn)(w, r) }

func (s *Service) signIn(w http.ResponseWriter, r *http.Request) {
	start := s.now()
	ctx := r.Context()
	fail := func(code wire.ErrorCode, message string, attrs ...slog.Attr) {
		s.event(ctx, "auth.signin", string(code), start, attrs...)
		WriteError(w, code, message)
	}
	if !versionAllowed(clientVersion(r), s.cfg.MinClientVersion) {
		fail(wire.CodeUpgradeRequired, "this rzm is older than "+s.cfg.MinClientVersion+"; upgrade it")
		return
	}
	var req wire.GoogleAuthRequest
	if !DecodeJSON(w, r, &req) {
		s.event(ctx, "auth.signin", string(wire.CodeBadRequest), start)
		return
	}
	if req.Code == "" || req.CodeVerifier == "" || req.Nonce == "" || !loopbackRedirect(req.RedirectURI) {
		fail(wire.CodeBadRequest, "code, code_verifier, nonce, and a loopback redirect_uri are required")
		return
	}
	provider, verifier, err := s.oidcProvider()
	if err != nil {
		fail(wire.CodeUpstreamDown, "identity provider discovery failed")
		return
	}
	oc := oauth2.Config{
		ClientID: s.cfg.ClientID, ClientSecret: s.cfg.ClientSecret, Endpoint: provider.Endpoint(),
		RedirectURL: req.RedirectURI, Scopes: strings.Fields(s.cfg.Scopes),
	}
	tok, err := oc.Exchange(oidc.ClientContext(ctx, s.client), req.Code, oauth2.VerifierOption(req.CodeVerifier))
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		fail(wire.CodeBadRequest, "the identity provider rejected the authorization code", slog.String("reason", "code_rejected"))
		return
	} else if err != nil {
		fail(wire.CodeUpstreamDown, "the identity provider could not be reached")
		return
	}
	// The issuer's access and refresh tokens are discarded here; only the
	// verified ID token's claims are used.
	raw, _ := tok.Extra("id_token").(string)
	idt, err := verifier.Verify(ctx, raw)
	if err != nil {
		fail(wire.CodeBadRequest, "the ID token failed verification", slog.String("reason", "id_token_invalid"))
		return
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(req.Nonce)) != 1 {
		fail(wire.CodeBadRequest, "the ID token nonce does not match", slog.String("reason", "nonce_mismatch"))
		return
	}
	var claims idClaims
	if err := idt.Claims(&claims); err != nil || (claims.AZP != "" && claims.AZP != s.cfg.ClientID) {
		fail(wire.CodeBadRequest, "the ID token failed verification", slog.String("reason", "claims_invalid"))
		return
	}
	hd := strings.ToLower(claims.HD)
	if !claims.EmailVerified || claims.Email == "" {
		fail(wire.CodeNotEligible, "the account's email address is not verified", slog.String("sub", idt.Subject))
		return
	}
	if hd == "" || !slices.Contains(s.cfg.AcceptedDomains, hd) {
		fail(wire.CodeNotEligible, "this account is not in an accepted Workspace domain", slog.String("sub", idt.Subject))
		return
	}
	person, err := s.store.UpsertPerson(ctx, idt.Subject, strings.ToLower(claims.Email), hd, start)
	if err != nil {
		fail(wire.CodeInternal, "the session store is unavailable")
		return
	}
	rt, err := s.runtime.Runtime(ctx)
	if err != nil {
		fail(wire.CodeInternal, "the runtime configuration is unavailable")
		return
	}
	if !eligible(person, rt) {
		fail(wire.CodeNotEligible, "this account is not eligible for the team API", slog.String("sub", person.Sub))
		return
	}

	id := randomString(16)
	access, accessHash := newToken(wire.AccessTokenPrefix, id)
	refresh, refreshHash := newToken(wire.RefreshTokenPrefix, id)
	sc := s.cfg.Sessions
	abs := start.Add(time.Duration(sc.AbsoluteTTL))
	sess := store.Session{
		ID: id, Sub: person.Sub, DeviceName: truncate(req.DeviceName, 128), ClientVersion: truncate(clientVersion(r), 64),
		CreatedAt: start, LastUsedAt: start,
		IdleExpiresAt: start.Add(time.Duration(sc.IdleTTL)), AbsoluteExpiresAt: abs,
		AccessHash: accessHash, AccessExpiresAt: start.Add(time.Duration(sc.AccessTTL)),
		RefreshHash: refreshHash,
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		fail(wire.CodeInternal, "the session store is unavailable")
		return
	}
	s.event(ctx, "auth.signin", "ok", start, slog.String("sub", person.Sub), slog.String("session_id", id))
	WriteJSON(w, s.grant(person, sess, access, refresh))
}

func (s *Service) grant(p store.Person, x store.Session, access, refresh string) wire.SessionGrant {
	return wire.SessionGrant{
		SessionID: x.ID, Email: p.Email,
		AccessToken: access, AccessExpiresAt: x.AccessExpiresAt,
		RefreshToken: refresh, SessionExpiresAt: x.AbsoluteExpiresAt,
		Limits: s.cfg.Concurrency,
	}
}

// loopbackRedirect accepts only the desktop loopback redirect.
func loopbackRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	h := u.Hostname()
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// event records an outcome and its duration. Attributes carry sub and
// session IDs only: never tokens, codes, or email addresses.
func (s *Service) event(ctx context.Context, name, outcome string, start time.Time, attrs ...slog.Attr) {
	RecordEvent(ctx, s.rec, subsystem, name, outcome, s.now().Sub(start), attrs...)
}

// RecordEvent records a diagnostics event with an outcome code and duration.
// A nil recorder records nothing.
func RecordEvent(ctx context.Context, rec *diagnostics.Recorder, sub, name, outcome string, d time.Duration, attrs ...slog.Attr) {
	if rec == nil {
		return
	}
	level := slog.LevelInfo
	if outcome == string(wire.CodeInternal) {
		level = slog.LevelError
	}
	metrics, _ := json.Marshal(map[string]float64{"duration_ms": float64(d.Microseconds()) / 1000})
	attrs = append(attrs, slog.String("outcome", outcome))
	rec.EventMetrics(ctx, level, sub, name, fmt.Sprintf("%s %s", name, outcome), metrics, attrs...)
}
