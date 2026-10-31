// Package teamaccess connects provider access to the person's team API
// session: it resolves the session origin, keeps one Account per origin for
// the process, and decides whether a loopback gateway may be used. In a
// development build a signed-in loopback session wins over direct keys, and
// the default development origin's gateway is started on demand.
package teamaccess

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	"github.com/atomicobject/rhizome/pkg/app/teamdev"
	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// Factory returns the provider-access factory for this process.
func Factory(development bool, version string) provideraccess.Factory {
	a := &access{development: development, version: version, getenv: os.Getenv,
		ensure: teamdev.Ensure, requireLive: teamdev.RequireLive}
	return provideraccess.Factory{Load: a.load, Status: a.status, DevBuild: development}
}

// accountTTL bounds how long one process reuses its account lookup.
// Providers are built per search and per evaluation, and each lookup reads
// the keychain through a /usr/bin/security subprocess; a sign-in or sign-out
// elsewhere is noticed within this window, and a revoked session already
// fails its next request.
const accountTTL = 30 * time.Second

// baseTransport is shared by every account in the process, so rebuilding the
// lookup each TTL keeps pooled connections.
var baseTransport = session.DefaultBaseTransport()

type access struct {
	development bool
	version     string
	getenv      func(string) string
	ensure      func(context.Context, string) error
	requireLive func(context.Context, string) error

	mu      sync.Mutex
	at      time.Time
	account *session.Account
	state   session.Status
	err     error
	// byOrigin keeps one Account per origin for the life of the process. An
	// Account may hold refreshed tokens it could not yet save to the keychain;
	// replacing it would fall back to the stored, rotated-away refresh token.
	byOrigin map[string]*session.Account
}

func (a *access) open() (*session.Account, session.Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.at.IsZero() && time.Since(a.at) < accountTTL {
		return a.account, a.state, a.err
	}
	a.account, a.state, a.err = a.lookup()
	a.at = time.Now()
	return a.account, a.state, a.err
}

// lookup runs with a.mu held.
func (a *access) lookup() (*session.Account, session.Status, error) {
	store, err := session.DefaultStore()
	if err != nil {
		// No supported secret store on this platform: never signed in.
		return nil, session.Status{}, nil
	}
	selector, err := session.DefaultSelectorPath()
	if err != nil {
		return nil, session.Status{}, err
	}
	origin, err := session.ResolveOrigin(session.ResolveOptions{Development: a.development, Getenv: a.getenv, Store: store, SelectorPath: selector})
	if err != nil || origin == "" {
		return nil, session.Status{}, err
	}
	account := a.byOrigin[origin]
	if account == nil {
		lock, err := session.DefaultLockPath()
		if err != nil {
			return nil, session.Status{}, err
		}
		if account, err = session.Open(session.Options{Origin: origin, Store: store, LockPath: lock, SelectorPath: selector,
			BaseTransport: baseTransport, UserAgent: "rzm/" + a.version}); err != nil {
			return nil, session.Status{}, err
		}
		if a.byOrigin == nil {
			a.byOrigin = map[string]*session.Account{}
		}
		a.byOrigin[origin] = account
	}
	status, err := account.Status()
	if errors.Is(err, session.ErrNotFound) {
		return account, session.Status{}, nil
	}
	return account, status, err
}

func (a *access) load() (provideraccess.Managed, bool, error) {
	account, status, err := a.open()
	if err != nil || !status.SignedIn {
		return nil, false, err
	}
	m := managed{Account: account, limits: status.Limits}
	if err := a.check(context.Background(), account.Origin()); err != nil {
		// Fail every call loudly rather than quietly billing a direct key.
		return failing{managed: m, err: err}, true, nil
	}
	return m, true, nil
}

// check decides whether the gateway at origin may serve provider calls.
func (a *access) check(ctx context.Context, origin string) error {
	switch {
	case !teamdev.Loopback(origin):
		return nil
	case a.development && origin == session.DevelopmentOrigin:
		return a.ensure(ctx, origin)
	case a.development && a.explicitOrigin() == origin:
		// A fixture run chose this origin explicitly; fake upstreams are allowed.
		return nil
	default:
		// A loopback origin reached through the selector must not route a real
		// vault's work to fake providers.
		return a.requireLive(ctx, origin)
	}
}

// explicitOrigin is RHIZOME_TEAM_API_ORIGIN in canonical form, so a trailing
// slash still names the same origin.
func (a *access) explicitOrigin() string {
	origin, err := session.CanonicalOrigin(a.getenv(session.OriginEnv))
	if err != nil {
		return ""
	}
	return origin
}

func (a *access) status() provideraccess.TeamStatus {
	_, status, err := a.open()
	if err != nil || !status.SignedIn {
		return provideraccess.TeamStatus{}
	}
	expires := status.SessionExpiresAt
	return provideraccess.TeamStatus{SignedIn: true, Email: status.Email, Origin: status.Origin, ExpiresAt: &expires}
}

type managed struct {
	*session.Account
	limits wire.Limits
}

func (m managed) Limits() wire.Limits { return m.limits }

// failing reports why the gateway cannot be used on every request instead of
// falling back to another billing source.
type failing struct {
	managed
	err error
}

func (f failing) Transport() http.RoundTripper { return errorTransport{f.err} }

type errorTransport struct{ err error }

func (t errorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }
