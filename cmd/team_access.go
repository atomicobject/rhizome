package cmd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	"github.com/atomicobject/rhizome/pkg/app/teamdev"
	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/pkg/vault/version"
)

// installProviderAccess gives every provider construction in this process the
// team session: the person's stored session for the resolved origin. In a
// development build a signed-in loopback session wins over direct keys, and
// the default development origin's gateway is started on demand.
func installProviderAccess() {
	if testing.Testing() {
		// Tests never read the person's real session or reach their gateway.
		return
	}
	provideraccess.Install(provideraccess.Factory{
		Load:     loadTeamSession,
		Status:   teamSessionStatus,
		DevBuild: version.Development(),
	})
}

type teamManaged struct {
	*session.Account
	limits wire.Limits
}

func (m teamManaged) Limits() wire.Limits { return m.limits }

// teamAccountTTL bounds how long one process reuses its account lookup.
// Providers are built per search and per evaluation, and each lookup reads
// the keychain through a /usr/bin/security subprocess; a sign-in or sign-out
// elsewhere is noticed within this window, and a revoked session already
// fails its next request.
const teamAccountTTL = 30 * time.Second

// teamBaseTransport is shared by every account lookup in the process, so
// rebuilding the account each TTL keeps its pooled connections.
var teamBaseTransport = session.DefaultBaseTransport()

var teamAccountCache struct {
	sync.Mutex
	at      time.Time
	account *session.Account
	status  session.Status
	err     error
	// byOrigin keeps one Account per origin for the life of the process. An
	// Account may hold refreshed tokens it could not yet save to the keychain;
	// replacing it would fall back to the stored, rotated-away refresh token.
	byOrigin map[string]*session.Account
}

func openTeamAccount() (*session.Account, session.Status, error) {
	c := &teamAccountCache
	c.Lock()
	defer c.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < teamAccountTTL {
		return c.account, c.status, c.err
	}
	c.account, c.status, c.err = lookupTeamAccount()
	c.at = time.Now()
	return c.account, c.status, c.err
}

// lookupTeamAccount runs with teamAccountCache held.
func lookupTeamAccount() (*session.Account, session.Status, error) {
	store, err := session.DefaultStore()
	if err != nil {
		// No supported secret store on this platform: never signed in.
		return nil, session.Status{}, nil
	}
	selector, err := session.DefaultSelectorPath()
	if err != nil {
		return nil, session.Status{}, err
	}
	origin, err := session.ResolveOrigin(session.ResolveOptions{Development: version.Development(), Getenv: os.Getenv, Store: store, SelectorPath: selector})
	if err != nil || origin == "" {
		return nil, session.Status{}, err
	}
	account := teamAccountCache.byOrigin[origin]
	if account == nil {
		lock, err := session.DefaultLockPath()
		if err != nil {
			return nil, session.Status{}, err
		}
		if account, err = session.Open(session.Options{Origin: origin, Store: store, LockPath: lock, SelectorPath: selector,
			BaseTransport: teamBaseTransport, UserAgent: "rzm/" + version.Version}); err != nil {
			return nil, session.Status{}, err
		}
		if teamAccountCache.byOrigin == nil {
			teamAccountCache.byOrigin = map[string]*session.Account{}
		}
		teamAccountCache.byOrigin[origin] = account
	}
	status, err := account.Status()
	if errors.Is(err, session.ErrNotFound) {
		return account, session.Status{}, nil
	}
	return account, status, err
}

func loadTeamSession() (provideraccess.Managed, bool, error) {
	account, status, err := openTeamAccount()
	if err != nil || !status.SignedIn {
		return nil, false, err
	}
	m := teamManaged{Account: account, limits: status.Limits}
	origin := account.Origin()
	var check error
	switch {
	case !teamdev.Loopback(origin):
	case version.Development() && origin == session.DevelopmentOrigin:
		check = teamdev.Ensure(context.Background(), origin)
	case version.Development() && explicitOrigin() == origin:
		// A fixture run chose this origin explicitly; fake upstreams are allowed.
	default:
		// A loopback origin reached through the selector must not route a real
		// vault's work to fake providers.
		check = teamdev.RequireLive(context.Background(), origin)
	}
	if check != nil {
		// Fail every call loudly rather than quietly billing a direct key.
		return failingManaged{teamManaged: m, err: check}, true, nil
	}
	return m, true, nil
}

// explicitOrigin is RHIZOME_TEAM_API_ORIGIN in canonical form, so a trailing
// slash still names the same origin.
func explicitOrigin() string {
	origin, err := session.CanonicalOrigin(os.Getenv(session.OriginEnv))
	if err != nil {
		return ""
	}
	return origin
}

func teamSessionStatus() provideraccess.TeamStatus {
	_, status, err := openTeamAccount()
	if err != nil || !status.SignedIn {
		return provideraccess.TeamStatus{}
	}
	expires := status.SessionExpiresAt
	return provideraccess.TeamStatus{SignedIn: true, Email: status.Email, Origin: status.Origin, ExpiresAt: &expires}
}

// failingManaged reports why the development gateway could not start on every
// request instead of falling back to another billing source.
type failingManaged struct {
	teamManaged
	err error
}

func (f failingManaged) Transport() http.RoundTripper { return errorTransport{f.err} }

type errorTransport struct{ err error }

func (t errorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }
