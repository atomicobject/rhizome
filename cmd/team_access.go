package cmd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

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

func openTeamAccount() (*session.Account, session.Status, error) {
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
	lock, err := session.DefaultLockPath()
	if err != nil {
		return nil, session.Status{}, err
	}
	account, err := session.Open(session.Options{Origin: origin, Store: store, LockPath: lock, SelectorPath: selector,
		BaseTransport: session.DefaultBaseTransport(), UserAgent: "rzm/" + version.Version})
	if err != nil {
		return nil, session.Status{}, err
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
	case version.Development() && os.Getenv(session.OriginEnv) == origin:
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
