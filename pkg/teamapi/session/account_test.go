package session

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/stretchr/testify/require"
)

func openAccount(t *testing.T, origin string, store Store, lockPath string) *Account {
	t.Helper()
	a, err := Open(Options{Origin: origin, Store: store, LockPath: lockPath, UserAgent: "rzm/test"})
	require.NoError(t, err)
	return a
}

func TestCanonicalOrigin(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://Team.Example.com", "https://team.example.com"},
		{"https://team.example.com/", "https://team.example.com"},
		{"https://team.example.com:443", "https://team.example.com"},
		{"https://team.example.com:8443", "https://team.example.com:8443"},
		{"http://127.0.0.1:8787", "http://127.0.0.1:8787"},
		{"http://localhost:8787/", "http://localhost:8787"},
		{"http://[::1]:8787", "http://[::1]:8787"},
		{"http://127.0.0.1:80", "http://127.0.0.1"},
		{"http://team.example.com", ""},
		{"http://127.0.0.2:8787", ""},
		{"ftp://team.example.com", ""},
		{"https://team.example.com/v1", ""},
		{"https://team.example.com?x=1", ""},
		{"https://team.example.com/?", ""},
		{"https://team.example.com#frag", ""},
		{"https://user@team.example.com", ""},
		{"https://user:pw@team.example.com", ""},
		{"team.example.com", ""},
		{"https://", ""},
		{"", ""},
	} {
		got, err := CanonicalOrigin(tc.in)
		if tc.want == "" {
			require.Error(t, err, tc.in)
			continue
		}
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.want, got, tc.in)
	}
}

func TestNewerRecordIsNeverOverwritten(t *testing.T) {
	g := newFakeGateway(t)
	store := &MemoryStore{}
	newer := []byte(`{"v":2,"origin":"` + g.origin() + `","future":"synthetic"}`)
	require.NoError(t, store.Save(g.origin(), newer))
	a := openAccount(t, g.origin(), store, filepath.Join(t.TempDir(), "lock"))

	_, err := a.Status()
	require.ErrorIs(t, err, ErrNewerRecord)
	_, err = a.Logout(context.Background())
	require.ErrorIs(t, err, ErrNewerRecord)
	_, err = a.Publish(context.Background(), wire.SessionGrant{SessionID: "s2", AccessToken: "a", RefreshToken: "r"}, 0)
	require.ErrorIs(t, err, ErrNewerRecord)
	_, err = a.HTTPClient().Get(g.origin() + wire.RouteSession)
	require.ErrorIs(t, err, ErrNewerRecord)

	stored, err := store.Load(g.origin())
	require.NoError(t, err)
	require.Equal(t, newer, stored)
}

func TestRefreshWhenNearExpiry(t *testing.T) {
	g := newFakeGateway(t)
	store := &MemoryStore{}
	saveRecord(t, store, g.record(time.Now().Add(30*time.Second)))
	a := openAccount(t, g.origin(), store, filepath.Join(t.TempDir(), "lock"))

	for range 2 {
		resp, err := a.HTTPClient().Get(g.origin() + wire.RouteSession)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}
	rotations, reused, _ := g.state()
	require.Equal(t, 1, rotations)
	require.Zero(t, reused)
	rec, ok := loadRecord(t, store, g.origin())
	require.True(t, ok)
	require.Equal(t, "rzt_a_s1.a1", rec.AccessToken)
	require.Equal(t, uint64(1), rec.Generation, "refresh keeps the sign-in generation")
}

func TestAccessRefreshRequiredReplaysOnce(t *testing.T) {
	g := newFakeGateway(t)
	store := &MemoryStore{}
	saveRecord(t, store, g.record(time.Now().Add(5*time.Minute)))
	a := openAccount(t, g.origin(), store, filepath.Join(t.TempDir(), "lock"))
	post := func(body io.Reader) (*http.Response, error) {
		return a.HTTPClient().Post(g.origin()+wire.RouteVoyageEmbed, "application/json", body)
	}
	resp, err := post(strings.NewReader(`{"n":1}`))
	require.NoError(t, err)
	resp.Body.Close()

	// Another process rotates the tokens; this account still holds a0.
	other := openAccount(t, g.origin(), store, a.lockPath)
	_, err = other.refresh(context.Background(), "rzt_a_s1.a0")
	require.NoError(t, err)

	resp, err = post(strings.NewReader(`{"n":2}`))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	rotations, _, _ := g.state()
	require.Equal(t, 1, rotations, "the replay reuses the other process's rotation")
	require.Equal(t, []string{`{"n":1}`, `{"n":2}`}, g.requests)

	// Without GetBody the request cannot be replayed.
	_, err = openAccount(t, g.origin(), store, a.lockPath).refresh(context.Background(), "rzt_a_s1.a1")
	require.NoError(t, err)
	_, err = post(io.NopCloser(strings.NewReader(`{"n":3}`)))
	var gerr *wire.GatewayError
	require.ErrorAs(t, err, &gerr)
	require.Equal(t, wire.CodeAccessRefreshRequired, gerr.Code)
	_, ok := loadRecord(t, store, g.origin())
	require.True(t, ok, "a rejected older access token never clears the shared session")
}

func TestSignInEndingCodesClearOnlyTheSameGeneration(t *testing.T) {
	g := newFakeGateway(t)
	store := &MemoryStore{}
	saveRecord(t, store, g.record(time.Now().Add(5*time.Minute)))
	a := openAccount(t, g.origin(), store, filepath.Join(t.TempDir(), "lock"))
	resp, err := a.HTTPClient().Get(g.origin() + wire.RouteSession)
	require.NoError(t, err)
	resp.Body.Close()

	// A newer sign-in replaced the record while this account cached gen 1.
	newer := g.record(time.Now().Add(5 * time.Minute))
	newer.Generation, newer.AccessToken = 2, "rzt_a_s2.x"
	saveRecord(t, store, newer)
	g.mu.Lock()
	g.revoked = true
	g.mu.Unlock()
	_, err = a.HTTPClient().Get(g.origin() + wire.RouteSession)
	require.ErrorIs(t, err, wire.ErrSignInRequired)
	rec, ok := loadRecord(t, store, g.origin())
	require.True(t, ok)
	require.Equal(t, uint64(2), rec.Generation)

	// The same generation is cleared.
	b := openAccount(t, g.origin(), store, a.lockPath)
	_, err = b.HTTPClient().Get(g.origin() + wire.RouteSession)
	var gerr *wire.GatewayError
	require.ErrorAs(t, err, &gerr)
	require.Equal(t, wire.CodeSessionRevoked, gerr.Code)
	_, ok = loadRecord(t, store, g.origin())
	require.False(t, ok)
}

func TestLogoutRacingLateRefreshCannotResurrect(t *testing.T) {
	g := newFakeGateway(t)
	store := &MemoryStore{}
	saveRecord(t, store, g.record(time.Now().Add(30*time.Second)))
	lock := filepath.Join(t.TempDir(), "lock")
	selector := filepath.Join(t.TempDir(), "team-session.json")
	require.NoError(t, writeSelector(selector, g.origin()))
	refresher := openAccount(t, g.origin(), store, lock)
	stale := openAccount(t, g.origin(), store, lock)
	staleRec, err := stale.load() // read before logout, as another process would
	require.NoError(t, err)
	logout, err := Open(Options{Origin: g.origin(), Store: store, LockPath: lock, SelectorPath: selector})
	require.NoError(t, err)

	gate := make(chan struct{})
	g.mu.Lock()
	g.refreshGate = gate
	g.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		resp, err := refresher.HTTPClient().Get(g.origin() + wire.RouteSession)
		if err == nil {
			resp.Body.Close()
		}
	}()
	time.Sleep(100 * time.Millisecond) // the refresher holds the lock
	var revoked bool
	go func() {
		defer wg.Done()
		revoked, err = logout.Logout(context.Background())
	}()
	time.Sleep(100 * time.Millisecond)
	close(gate)
	wg.Wait()
	require.NoError(t, err)
	require.True(t, revoked)
	_, ok := loadRecord(t, store, g.origin())
	require.False(t, ok)
	require.Equal(t, "rzt_a_s1.a1", g.logouts[len(g.logouts)-1], "logout revoked the rotated session")
	_, found, err := ReadSelector(selector)
	require.NoError(t, err)
	require.False(t, found)

	// A process that read the record before logout cannot write it back.
	g.mu.Lock()
	g.refreshGate = nil
	g.mu.Unlock()
	stale.setCache(&staleRec)
	_, err = stale.refresh(context.Background(), staleRec.AccessToken)
	require.ErrorIs(t, err, wire.ErrSignInRequired)
	_, ok = loadRecord(t, store, g.origin())
	require.False(t, ok)

	// Nor can a login that began before logout.
	_, err = logout.Publish(context.Background(), wire.SessionGrant{SessionID: "s9", AccessToken: "a", RefreshToken: "r"}, 1)
	require.ErrorIs(t, err, ErrSuperseded)
	_, ok = loadRecord(t, store, g.origin())
	require.False(t, ok)
	_, found, _ = ReadSelector(selector)
	require.False(t, found)
}

func TestOfflineLogoutDeletesAndReportsIncompleteRevocation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	origin := "http://" + listener.Addr().String()
	listener.Close()
	store := &MemoryStore{}
	rec := newFakeGateway(t).record(time.Now().Add(5 * time.Minute))
	rec.Origin = origin
	saveRecord(t, store, rec)
	revoked, err := openAccount(t, origin, store, filepath.Join(t.TempDir(), "lock")).Logout(context.Background())
	require.NoError(t, err)
	require.False(t, revoked)
	_, ok := loadRecord(t, store, origin)
	require.False(t, ok)
}

func TestPublishWritesSelectorAndStatusHidesTokens(t *testing.T) {
	g := newFakeGateway(t)
	store := &MemoryStore{}
	selector := filepath.Join(t.TempDir(), "team-session.json")
	other := "https://other.example.test"
	require.NoError(t, writeSelector(selector, other))
	a, err := Open(Options{Origin: g.origin(), Store: store, LockPath: filepath.Join(t.TempDir(), "lock"), SelectorPath: selector})
	require.NoError(t, err)
	gen, err := a.Generation()
	require.NoError(t, err)
	status, err := a.Publish(context.Background(), wire.SessionGrant{SessionID: "s1", Email: "person@example.test",
		AccessToken: wire.AccessTokenPrefix + "s1.synthetic", AccessExpiresAt: time.Now().Add(time.Minute), RefreshToken: wire.RefreshTokenPrefix + "s1.synthetic",
		SessionExpiresAt: time.Now().Add(time.Hour)}, gen)
	require.NoError(t, err)
	require.True(t, status.SignedIn)
	encoded, _ := json.Marshal(status)
	require.NotContains(t, string(encoded), "synthetic")
	origin, found, err := ReadSelector(selector)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, g.origin(), origin)

	// Logging out of a different origin leaves the selector alone.
	b, err := Open(Options{Origin: other, Store: store, LockPath: a.lockPath, SelectorPath: selector})
	require.NoError(t, err)
	_, err = b.Logout(context.Background())
	require.NoError(t, err)
	_, found, _ = ReadSelector(selector)
	require.True(t, found)
}

func TestResolveOrigin(t *testing.T) {
	store := &MemoryStore{}
	signedIn := "http://127.0.0.1:8788"
	require.NoError(t, store.Save(signedIn, []byte("{}")))
	selector := filepath.Join(t.TempDir(), "team-session.json")
	env := func(value string) func(string) string {
		return func(name string) string {
			if name == OriginEnv {
				return value
			}
			return ""
		}
	}
	resolve := func(opts ResolveOptions) string {
		t.Helper()
		opts.Store, opts.SelectorPath = store, selector
		origin, err := ResolveOrigin(opts)
		require.NoError(t, err)
		return origin
	}

	require.Equal(t, "https://explicit.example.test", resolve(ResolveOptions{Server: "https://explicit.example.test/", Version: "dev", Getenv: env(signedIn)}))
	require.Equal(t, signedIn, resolve(ResolveOptions{Version: "dev", Getenv: env(signedIn)}))
	_, err := ResolveOrigin(ResolveOptions{Version: "dev", Getenv: env("http://127.0.0.1:9999"), Store: store, SelectorPath: selector})
	require.ErrorIs(t, err, ErrNotFound, "the variable cannot select an origin with no stored session")
	require.Equal(t, DevelopmentOrigin, resolve(ResolveOptions{Version: "dev"}))
	require.Equal(t, "", resolve(ResolveOptions{Version: "v1.2.3", Getenv: env(signedIn)}), "release builds ignore the variable")

	require.NoError(t, writeSelector(selector, "https://selected.example.test"))
	require.Equal(t, "https://selected.example.test", resolve(ResolveOptions{Version: "v1.2.3", Getenv: env(signedIn)}))
	require.Equal(t, "https://selected.example.test", resolve(ResolveOptions{Version: "dev"}))
}

func TestTransportRefusesForeignOriginAndRedirect(t *testing.T) {
	g := newFakeGateway(t)
	var foreignHits int
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignHits++ }))
	defer foreign.Close()
	store := &MemoryStore{}
	saveRecord(t, store, g.record(time.Now().Add(5*time.Minute)))
	a := openAccount(t, g.origin(), store, filepath.Join(t.TempDir(), "lock"))

	_, err := a.HTTPClient().Get(foreign.URL + wire.RouteSession)
	require.ErrorIs(t, err, ErrForeignOrigin)
	require.Zero(t, foreignHits)

	// Refused even through a client that follows redirects.
	_, err = (&http.Client{Transport: a.Transport()}).Get(g.origin() + "/redirect")
	require.ErrorIs(t, err, ErrRedirect)

	_, err = a.HTTPClient().Get(g.origin() + "/rate-limited")
	var gerr *wire.GatewayError
	require.ErrorAs(t, err, &gerr)
	require.Equal(t, wire.CodeRateLimited, gerr.Code)
	require.Equal(t, http.StatusTooManyRequests, gerr.Status)
	require.Equal(t, 7*time.Second, gerr.RetryAfter)
}

func TestTransportIgnoresProxyEnvironment(t *testing.T) {
	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer proxy.Close()
	var mu sync.Mutex
	hits := 0
	go func() {
		for {
			conn, err := proxy.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			hits++
			mu.Unlock()
			conn.Close()
		}
	}()
	t.Setenv("HTTPS_PROXY", "http://"+proxy.Addr().String())
	t.Setenv("HTTP_PROXY", "http://"+proxy.Addr().String())
	require.Nil(t, DefaultBaseTransport().(*http.Transport).Proxy)

	origin := "https://gateway.invalid"
	store := &MemoryStore{}
	rec := newFakeGateway(t).record(time.Now().Add(5 * time.Minute))
	rec.Origin = origin
	saveRecord(t, store, rec)
	a := openAccount(t, origin, store, filepath.Join(t.TempDir(), "lock"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, origin+wire.RouteSession, nil)
	_, err = a.HTTPClient().Do(req)
	require.Error(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Zero(t, hits, "the session transport never dials an environment proxy")
}

func TestRefreshDiagnosticsNeverContainTokens(t *testing.T) {
	g := newFakeGateway(t)
	store := &MemoryStore{}
	saveRecord(t, store, g.record(time.Now().Add(30*time.Second)))
	a := openAccount(t, g.origin(), store, filepath.Join(t.TempDir(), "lock"))
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: io.Discard})
	require.NoError(t, err)
	ctx := diagnostics.WithRecorder(context.Background(), recorder)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, g.origin()+"/rate-limited", nil)
	_, err = a.HTTPClient().Do(req)
	require.Error(t, err)
	require.NoError(t, recorder.Close())

	result, err := diagnostics.ReadEvents(root, diagnostics.Filter{Subsystem: "teamapi", Level: slog.LevelDebug, Since: time.Now().Add(-time.Hour)})
	require.NoError(t, err)
	var names []string
	for _, e := range result.Events {
		names = append(names, e.Name)
	}
	require.Contains(t, names, "session.refresh")
	require.Contains(t, names, "gateway.error")
	scanned := 0
	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		scanned++
		require.False(t, bytes.Contains(data, []byte("rzt_")), path)
		return err
	}))
	require.NotZero(t, scanned)
}
