package teamdev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

func gateway(t *testing.T, upstreams string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case wire.RouteHealth:
		case wire.RouteMeta:
			_ = json.NewEncoder(w).Encode(wire.Meta{APIVersion: wire.APIVersion, Upstreams: upstreams})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEnsureAcceptsRunningLiveGateway(t *testing.T) {
	if err := Ensure(context.Background(), gateway(t, "").URL); err != nil {
		t.Fatalf("Ensure(live) = %v", err)
	}
}

func TestEnsureRefusesFakeUpstreams(t *testing.T) {
	err := Ensure(context.Background(), gateway(t, "fake").URL)
	if err == nil || !strings.Contains(err.Error(), "fake providers") {
		t.Fatalf("Ensure(fake) = %v, want refusal", err)
	}
}

func TestEnsureLeavesRemoteOriginsAlone(t *testing.T) {
	if err := Ensure(context.Background(), "https://team-api.invalid"); err != nil {
		t.Fatalf("Ensure(remote) = %v", err)
	}
}
