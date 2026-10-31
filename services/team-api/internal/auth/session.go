package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

// AuthCacheTTL bounds how long a revoked session or disabled person can
// still authenticate.
const AuthCacheTTL = 30 * time.Second

// maxRotationAttempts bounds compare-and-swap retries when concurrent
// refreshes of one session race.
const maxRotationAttempts = 64

// Refresh handles POST /v1/auth/refresh.
func (s *Service) Refresh(w http.ResponseWriter, r *http.Request) { s.limiter.wrap(s.refresh)(w, r) }

func (s *Service) refresh(w http.ResponseWriter, r *http.Request) {
	start := s.now()
	ctx := r.Context()
	if !versionAllowed(clientVersion(r), s.cfg.MinClientVersion) {
		s.event(ctx, "auth.refresh", string(wire.CodeUpgradeRequired), start)
		WriteError(w, wire.CodeUpgradeRequired, "this rzm is older than "+s.cfg.MinClientVersion+"; upgrade it")
		return
	}
	var req wire.RefreshRequest
	if !DecodeJSON(w, r, &req) {
		s.event(ctx, "auth.refresh", string(wire.CodeBadRequest), start)
		return
	}
	grant, kind, code, sessionID := s.rotate(ctx, req.RefreshToken)
	if code != "" {
		s.event(ctx, "auth.refresh", string(code), start, slog.String("session_id", sessionID))
		WriteError(w, code, refreshMessages[code])
		return
	}
	s.event(ctx, "auth.refresh", kind, start, slog.String("session_id", sessionID))
	WriteJSON(w, grant)
}

var refreshMessages = map[wire.ErrorCode]string{
	wire.CodeSessionRevoked: "the session is not valid; sign in again",
	wire.CodeSessionExpired: "the session has expired; sign in again",
	wire.CodeRefreshReused:  "a replaced refresh token was reused, so the session was revoked; sign in again",
	wire.CodeNotEligible:    "this account is not eligible for the team API",
	wire.CodeInternal:       "the session store is unavailable",
}

// rotate issues a new token pair for raw. kind is "rotated" when raw was the
// current refresh token and "recovered" when it was the predecessor within
// its grace deadline. Recovery keeps the original predecessor and deadline,
// so repeated recoveries never extend the grace period.
func (s *Service) rotate(ctx context.Context, raw string) (grant wire.SessionGrant, kind string, code wire.ErrorCode, id string) {
	id, ok := parseToken(raw, wire.RefreshTokenPrefix)
	if !ok {
		return grant, "", wire.CodeSessionRevoked, ""
	}
	presented := hashToken(raw)
	sc := s.cfg.Sessions
	for range maxRotationAttempts {
		now := s.now()
		x, err := s.store.Session(ctx, id, now)
		if err != nil {
			return grant, "", sessionCode(err), id
		}
		var rot store.Rotation
		switch {
		case hashEqual(presented, x.RefreshHash):
			kind = "rotated"
			rot.PrevRefreshHash, rot.PrevRefreshDeadline = x.RefreshHash, now.Add(time.Duration(sc.RefreshGrace))
		case hashEqual(presented, x.PrevRefreshHash) && now.Before(x.PrevRefreshDeadline):
			kind = "recovered"
			rot.PrevRefreshHash, rot.PrevRefreshDeadline = x.PrevRefreshHash, x.PrevRefreshDeadline
		case hashEqual(presented, x.PrevRefreshHash):
			if err := s.store.RevokeSession(ctx, id, string(wire.CodeRefreshReused), now, nil); err != nil {
				return grant, "", wire.CodeInternal, id
			}
			s.cache.forget(id)
			return grant, "", wire.CodeRefreshReused, id
		default:
			// An unknown token names the session but proves nothing, so it
			// must not be able to revoke it.
			return grant, "", wire.CodeSessionRevoked, id
		}
		// Eligibility is checked only for a token that proved itself, so
		// naming a session ID reveals nothing about its person.
		person, err := s.store.Person(ctx, x.Sub)
		if err != nil {
			return grant, "", sessionCode(err), id
		}
		rt, err := s.runtime.Runtime(ctx)
		if err != nil {
			return grant, "", wire.CodeInternal, id
		}
		if !eligible(person, rt) {
			return grant, "", wire.CodeNotEligible, id
		}
		access, accessHash := newToken(wire.AccessTokenPrefix, id)
		refresh, refreshHash := newToken(wire.RefreshTokenPrefix, id)
		rot.AccessHash, rot.RefreshHash = accessHash, refreshHash
		rot.AccessExpiresAt = earliest(now.Add(time.Duration(sc.AccessTTL)), x.AbsoluteExpiresAt)
		rot.IdleExpiresAt = earliest(now.Add(time.Duration(sc.IdleTTL)), x.AbsoluteExpiresAt)
		rot.LastUsedAt = now
		err = s.store.RotateSession(ctx, id, x.Generation, rot)
		if errors.Is(err, store.ErrStale) {
			continue // another refresh won; decide again from its result
		}
		if err != nil {
			return grant, "", wire.CodeInternal, id
		}
		s.cache.forget(id)
		x.AccessExpiresAt = rot.AccessExpiresAt
		return s.grant(person, x, access, refresh), kind, "", id
	}
	return grant, "", wire.CodeInternal, id
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func sessionCode(err error) wire.ErrorCode {
	switch {
	case errors.Is(err, store.ErrExpired):
		return wire.CodeSessionExpired
	case errors.Is(err, store.ErrRevoked), errors.Is(err, store.ErrNotFound):
		return wire.CodeSessionRevoked
	default:
		return wire.CodeInternal
	}
}

// Logout handles POST /v1/auth/logout. It needs the session's current access
// token, which may have expired; an already ended session also returns 204.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	start := s.now()
	ctx := r.Context()
	raw := bearer(r)
	id, ok := parseToken(raw, wire.AccessTokenPrefix)
	if !ok {
		s.event(ctx, "auth.logout", string(wire.CodeSessionRevoked), start)
		WriteError(w, wire.CodeSessionRevoked, "a session access token is required")
		return
	}
	x, err := s.store.Session(ctx, id, start)
	switch code := sessionCode(err); {
	case err == nil:
	case code == wire.CodeInternal:
		s.event(ctx, "auth.logout", string(code), start, slog.String("session_id", id))
		WriteError(w, code, "the session store is unavailable")
		return
	default:
		s.event(ctx, "auth.logout", "already_ended", start, slog.String("session_id", id))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !hashEqual(hashToken(raw), x.AccessHash) {
		s.event(ctx, "auth.logout", string(wire.CodeAccessRefreshRequired), start, slog.String("session_id", id))
		WriteError(w, wire.CodeAccessRefreshRequired, "the access token was replaced; reload the session and retry")
		return
	}
	if err := s.store.RevokeSession(ctx, id, "logout", start, nil); err != nil {
		s.event(ctx, "auth.logout", string(wire.CodeInternal), start, slog.String("session_id", id))
		WriteError(w, wire.CodeInternal, "the session store is unavailable")
		return
	}
	s.cache.forget(id)
	s.event(ctx, "auth.logout", "ok", start, slog.String("sub", x.Sub), slog.String("session_id", id))
	ensureRequestID(w)
	w.WriteHeader(http.StatusNoContent)
}

// SessionInfo handles GET /v1/session behind Middleware.
func (s *Service) SessionInfo(w http.ResponseWriter, r *http.Request) {
	p, _ := core.PrincipalFrom(r.Context())
	now := s.now()
	x, err := s.store.Session(r.Context(), p.SessionID, now)
	if err != nil {
		WriteError(w, sessionCode(err), "the session is not valid")
		return
	}
	var usage wire.DailyUsage
	if s.usage != nil {
		_, people, err := s.usage.Usage(r.Context(), now.UTC().Format(time.DateOnly))
		if err != nil {
			WriteError(w, wire.CodeInternal, "usage is unavailable")
			return
		}
		usage = people[p.Sub]
	}
	WriteJSON(w, wire.SessionInfo{
		SessionID: x.ID, Email: p.Email, DeviceName: x.DeviceName, SessionExpiresAt: x.AbsoluteExpiresAt,
		Limits: s.cfg.Concurrency, Today: wire.UsageWithLimits{Usage: usage, Limits: p.Limits},
	})
}

// Middleware authenticates "Authorization: Bearer <access token>" and puts a
// core.Principal in the request context. Session and person reads are cached
// for at most AuthCacheTTL.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ensureRequestID(w)
		p, code := s.authenticate(r)
		if code != "" {
			WriteError(w, code, authMessages[code])
			return
		}
		next.ServeHTTP(w, r.WithContext(core.WithPrincipal(r.Context(), p)))
	})
}

var authMessages = map[wire.ErrorCode]string{
	wire.CodeUpgradeRequired:       "this rzm is older than the gateway's minimum version; upgrade it",
	wire.CodeAccessRefreshRequired: "the access token was replaced or expired; reload the session and retry",
	wire.CodeSessionRevoked:        "the session is not valid; sign in again",
	wire.CodeSessionExpired:        "the session has expired; sign in again",
	wire.CodeNotEligible:           "this account is not eligible for the team API",
	wire.CodeInternal:              "the session store is unavailable",
}

func (s *Service) authenticate(r *http.Request) (core.Principal, wire.ErrorCode) {
	if !versionAllowed(clientVersion(r), s.cfg.MinClientVersion) {
		return core.Principal{}, wire.CodeUpgradeRequired
	}
	raw := bearer(r)
	id, ok := parseToken(raw, wire.AccessTokenPrefix)
	if !ok {
		return core.Principal{}, wire.CodeSessionRevoked
	}
	ctx := r.Context()
	now := s.now()
	presented := hashToken(raw)
	e, err := s.cachedSession(ctx, id, now, false)
	if err == nil && !hashEqual(presented, e.session.AccessHash) {
		// The cache may predate a rotation in this process or another one.
		e, err = s.cachedSession(ctx, id, now, true)
	}
	if err != nil {
		return core.Principal{}, sessionCode(err)
	}
	x := e.session
	if !now.Before(x.AbsoluteExpiresAt) || !now.Before(x.IdleExpiresAt) {
		return core.Principal{}, wire.CodeSessionExpired
	}
	if !hashEqual(presented, x.AccessHash) || !now.Before(x.AccessExpiresAt) {
		return core.Principal{}, wire.CodeAccessRefreshRequired
	}
	rt, err := s.runtime.Runtime(ctx)
	if err != nil {
		return core.Principal{}, wire.CodeInternal
	}
	if !eligible(e.person, rt) {
		return core.Principal{}, wire.CodeNotEligible
	}
	limits := rt.PersonLimits
	if e.person.Limits != nil {
		limits = *e.person.Limits
	}
	return core.Principal{
		Sub: e.person.Sub, Email: e.person.Email, SessionID: x.ID, Limits: limits,
		Admin: e.person.Status == wire.PersonActive && slices.Contains(s.cfg.Admins, e.person.Email),
	}, ""
}

type cachedSession struct {
	session store.Session
	person  store.Person
	at      time.Time
}

// sessionCache holds successful session and person reads. Failures are never
// cached, so an ended session is reported from the store.
type sessionCache struct {
	mu      sync.Mutex
	entries map[string]cachedSession
}

func (c *sessionCache) forget(id string) {
	c.mu.Lock()
	delete(c.entries, id)
	c.mu.Unlock()
}

func (s *Service) cachedSession(ctx context.Context, id string, now time.Time, fresh bool) (cachedSession, error) {
	c := &s.cache
	c.mu.Lock()
	e, ok := c.entries[id]
	c.mu.Unlock()
	if ok && !fresh && now.Sub(e.at) < AuthCacheTTL && !now.Before(e.at) {
		return e, nil
	}
	x, err := s.store.Session(ctx, id, now)
	if err != nil {
		c.forget(id)
		return cachedSession{}, err
	}
	p, err := s.store.Person(ctx, x.Sub)
	if err != nil {
		c.forget(id)
		return cachedSession{}, err
	}
	e = cachedSession{session: x, person: p, at: now}
	c.mu.Lock()
	if len(c.entries) > 10000 {
		for k, old := range c.entries {
			if now.Sub(old.at) >= AuthCacheTTL {
				delete(c.entries, k)
			}
		}
	}
	c.entries[id] = e
	c.mu.Unlock()
	return e, nil
}
