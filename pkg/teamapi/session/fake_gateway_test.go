package session

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// fakeGateway implements the auth routes and one protected echo route with
// strict refresh rotation. Tokens are synthetic.
type fakeGateway struct {
	t   testing.TB
	srv *httptest.Server

	mu          sync.Mutex
	n           int
	access      string
	old         map[string]bool // rotated-out access tokens
	refresh     string
	revoked     bool
	rotations   int
	reused      int
	logouts     []string      // access tokens presented to logout
	requests    []string      // bodies seen by the echo route
	refreshGate chan struct{} // when set, refresh waits for it
}

func newFakeGateway(t testing.TB) *fakeGateway {
	g := &fakeGateway{t: t, access: "rzt_a_s1.a0", refresh: "rzt_r_s1.r0", old: map[string]bool{}}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGateway) origin() string { return g.srv.URL }

// record is a stored record for the gateway's current tokens.
func (g *fakeGateway) record(accessExpiry time.Time) Record {
	g.mu.Lock()
	defer g.mu.Unlock()
	return Record{V: 1, Origin: g.srv.URL, Generation: 1, SessionID: "s1", Email: "person@example.test",
		AccessToken: g.access, AccessExpiresAt: accessExpiry, RefreshToken: g.refresh,
		SessionExpiresAt: time.Now().Add(30 * 24 * time.Hour), Limits: wire.Limits{VoyageConcurrency: 4, TypeSafeConcurrency: 4}}
}

func gatewayFail(w http.ResponseWriter, code wire.ErrorCode) {
	w.Header().Set(wire.HeaderGatewayError, string(code))
	w.Header().Set("Content-Type", "application/json")
	if code == wire.CodeRateLimited {
		w.Header().Set("Retry-After", "7")
	}
	w.WriteHeader(code.Status())
	_ = json.NewEncoder(w).Encode(wire.ErrorBody{Error: wire.ErrorDetail{Code: code, Message: "synthetic " + string(code)}})
}

func (g *fakeGateway) serve(w http.ResponseWriter, r *http.Request) {
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch r.URL.Path {
	case wire.RouteAuthRefresh:
		var body wire.RefreshRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		g.mu.Lock()
		gate := g.refreshGate
		g.mu.Unlock()
		if gate != nil {
			<-gate
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.revoked {
			gatewayFail(w, wire.CodeSessionRevoked)
			return
		}
		if body.RefreshToken != g.refresh {
			g.reused++
			gatewayFail(w, wire.CodeRefreshReused)
			return
		}
		g.n++
		g.rotations++
		g.old[g.access] = true
		g.access, g.refresh = fmt.Sprintf("rzt_a_s1.a%d", g.n), fmt.Sprintf("rzt_r_s1.r%d", g.n)
		_ = json.NewEncoder(w).Encode(wire.SessionGrant{SessionID: "s1", Email: "person@example.test",
			AccessToken: g.access, AccessExpiresAt: time.Now().Add(10 * time.Minute), RefreshToken: g.refresh,
			SessionExpiresAt: time.Now().Add(30 * 24 * time.Hour)})
	case wire.RouteAuthLogout:
		g.mu.Lock()
		defer g.mu.Unlock()
		g.logouts = append(g.logouts, bearer)
		if bearer != g.access {
			gatewayFail(w, wire.CodeSessionRevoked)
			return
		}
		g.revoked = true
		w.WriteHeader(http.StatusNoContent)
	case "/rate-limited":
		gatewayFail(w, wire.CodeRateLimited)
	case "/redirect":
		http.Redirect(w, r, "/v1/session", http.StatusFound)
	default:
		body, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		defer g.mu.Unlock()
		switch {
		case g.revoked:
			gatewayFail(w, wire.CodeSessionRevoked)
		case g.old[bearer]:
			gatewayFail(w, wire.CodeAccessRefreshRequired)
		case bearer != g.access:
			gatewayFail(w, wire.CodeSessionRevoked)
		default:
			g.requests = append(g.requests, string(body))
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}
}

func (g *fakeGateway) state() (rotations, reused int, revoked bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rotations, g.reused, g.revoked
}

// fileStore keeps records as files so separate processes share them.
type fileStore struct{ dir string }

// QueryEscape also escapes ":", which Windows does not allow in file names.
func (f fileStore) path(origin string) string { return filepath.Join(f.dir, url.QueryEscape(origin)) }

func (f fileStore) Load(origin string) ([]byte, error) {
	data, err := os.ReadFile(f.path(origin))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return data, err
}

func (f fileStore) Save(origin string, record []byte) error {
	tmp := f.path(origin) + ".tmp"
	if err := os.WriteFile(tmp, record, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path(origin))
}

func (f fileStore) Delete(origin string) error {
	if err := os.Remove(f.path(origin)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func saveRecord(t testing.TB, store Store, rec Record) {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(rec.Origin, data); err != nil {
		t.Fatal(err)
	}
}

func loadRecord(t testing.TB, store Store, origin string) (Record, bool) {
	t.Helper()
	data, err := store.Load(origin)
	if err == ErrNotFound {
		return Record{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	return rec, true
}
