package teamaccess

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// check routes each origin to the right guard: remote origins pass, the
// default development origin is started on demand, a fixture's explicit
// origin may use fake providers, and any other loopback origin must be live.
func TestCheckRoutesOrigins(t *testing.T) {
	errEnsure, errLive := errors.New("ensure"), errors.New("require live")
	env := map[string]string{}
	a := &access{development: true, getenv: func(k string) string { return env[k] },
		ensure:      func(context.Context, string) error { return errEnsure },
		requireLive: func(context.Context, string) error { return errLive }}
	ctx := context.Background()

	if err := a.check(ctx, "https://team.example.test"); err != nil {
		t.Fatalf("remote origin: %v", err)
	}
	if err := a.check(ctx, session.DevelopmentOrigin); !errors.Is(err, errEnsure) {
		t.Fatalf("development origin: %v, want ensure", err)
	}
	if err := a.check(ctx, "http://127.0.0.1:8788"); !errors.Is(err, errLive) {
		t.Fatalf("selector loopback origin: %v, want require live", err)
	}
	env[session.OriginEnv] = "http://127.0.0.1:8788/"
	if err := a.check(ctx, "http://127.0.0.1:8788"); err != nil {
		t.Fatalf("explicit fixture origin (trailing slash): %v", err)
	}
	a.development = false
	if err := a.check(ctx, session.DevelopmentOrigin); !errors.Is(err, errLive) {
		t.Fatalf("release build, loopback origin: %v, want require live", err)
	}
}

func TestRetainedProviderRecoversAfterGatewayCheckFailure(t *testing.T) {
	var calls atomic.Int32
	var checks int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(srv.Close)
	account, err := session.Open(session.Options{Origin: srv.URL, Store: &session.MemoryStore{}, LockPath: filepath.Join(t.TempDir(), "lock")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = account.Publish(context.Background(), wire.SessionGrant{SessionID: "s1", Email: "synthetic@example.test",
		AccessToken: "a", RefreshToken: "r",
		AccessExpiresAt: time.Now().Add(10 * time.Minute), SessionExpiresAt: time.Now().Add(time.Hour)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := true
	a := &access{account: account, at: time.Now(), state: session.Status{SignedIn: true},
		requireLive: func(context.Context, string) error {
			checks++
			if unavailable {
				return errors.New("temporary gateway outage")
			}
			return nil
		}}
	provider, ok, err := a.load()
	if err != nil || !ok {
		t.Fatalf("load provider: signed in %v, error %v", ok, err)
	}
	transport := provider.Transport()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	if _, err := transport.RoundTrip(req); err == nil {
		t.Fatal("an unavailable gateway must fail the request")
	}
	if calls.Load() != 0 {
		t.Fatal("the failed guard allowed a provider call")
	}
	unavailable = false
	// The same provider and transport must recover, without a new lookup.
	req, _ = http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("the retained provider did not recover: %v", err)
	}
	resp.Body.Close()
	if calls.Load() != 1 || checks != 2 {
		t.Fatalf("provider calls %d, gateway checks %d", calls.Load(), checks)
	}
}

// The development gateway exits after an idle hour while a vault keeps its
// provider, so the transport makes sure it is up before every request.
func TestEnsureTransportStartsTheGatewayBeforeEachRequest(t *testing.T) {
	var ensured int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	tr := ensureTransport{inner: http.DefaultTransport, origin: session.DevelopmentOrigin,
		ensure: func(context.Context, string) error { ensured++; return nil }}
	for range 2 {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if ensured != 2 {
		t.Fatalf("ensure ran %d times, want once per request", ensured)
	}
	failing := ensureTransport{inner: http.DefaultTransport, origin: session.DevelopmentOrigin,
		ensure: func(context.Context, string) error { return errors.New("cannot start") }}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	if _, err := failing.RoundTrip(req); err == nil {
		t.Fatal("a gateway that cannot start must fail the request")
	}
}
