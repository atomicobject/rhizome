// Package identity assembles the gateway's identity side: the store, the
// runtime configuration, sign-in and sessions, request authentication, and
// the admin API.
package identity

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/admin"
	"github.com/atomicobject/rhizome/services/team-api/internal/auth"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

type Identity struct {
	auth    *auth.Service
	admin   *admin.Handler
	runtime *auth.Runtime
}

// New wires the identity side onto db, a handle from store.Open. usage and
// rec may be nil; now defaults to time.Now.
func New(cfg config.Config, db *sql.DB, usage core.UsageReader, rec *diagnostics.Recorder, now func() time.Time) (*Identity, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	st := store.NewSQLite(db)
	rt, err := auth.NewRuntime(context.Background(), st, cfg, now)
	if err != nil {
		return nil, err
	}
	return &Identity{
		auth:    auth.New(cfg, st, rt, usage, rec, now),
		admin:   admin.New(cfg, st, rt, usage, rec, now),
		runtime: rt,
	}, nil
}

// Routes registers meta, auth, session, and admin routes on mux.
func (i *Identity) Routes(mux *http.ServeMux) {
	handle := func(pattern string, h http.Handler) { mux.Handle(pattern, auth.WithRequestID(h)) }
	handle("GET "+wire.RouteMeta, http.HandlerFunc(i.auth.Meta))
	handle("POST "+wire.RouteAuthGoogle, http.HandlerFunc(i.auth.SignIn))
	handle("POST "+wire.RouteAuthRefresh, http.HandlerFunc(i.auth.Refresh))
	handle("POST "+wire.RouteAuthLogout, http.HandlerFunc(i.auth.Logout))
	handle("GET "+wire.RouteSession, i.auth.Middleware(http.HandlerFunc(i.auth.SessionInfo)))
	i.admin.Routes(mux, func(h http.Handler) http.Handler { return auth.WithRequestID(i.auth.Middleware(h)) })
}

// Middleware authenticates a request's bearer access token and puts a
// core.Principal in its context. The traffic side wraps provider routes in it.
func (i *Identity) Middleware(next http.Handler) http.Handler { return i.auth.Middleware(next) }

// Runtime is the cached runtime configuration the traffic side reads.
func (i *Identity) Runtime() core.RuntimeSource { return i.runtime }
