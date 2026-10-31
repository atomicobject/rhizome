package teamaccess

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/session"
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
