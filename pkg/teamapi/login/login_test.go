package login

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/stretchr/testify/require"
)

// fakeStack is the gateway's meta and Google exchange routes plus a fake
// identity provider whose authorize endpoint redirects straight back.
type fakeStack struct {
	srv *httptest.Server

	mu        sync.Mutex
	challenge string
	nonce     string
	redirect  string
	exchanges int
	logouts   int
}

func newFakeStack(t *testing.T) *fakeStack {
	f := &fakeStack{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+wire.RouteMeta, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(wire.Meta{APIVersion: 1, ClientID: "synthetic-client", AuthorizationEndpoint: f.srv.URL + "/authorize",
			Scopes: "openid email", HostedDomainHint: "example.test"})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != "synthetic-client" || q.Get("code_challenge_method") != "S256" || q.Get("hd") != "example.test" || q.Get("response_type") != "code" {
			http.Error(w, "bad authorize request", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.challenge, f.nonce, f.redirect = q.Get("code_challenge"), q.Get("nonce"), q.Get("redirect_uri")
		f.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"/?code=synthetic-code&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("POST "+wire.RouteAuthGoogle, func(w http.ResponseWriter, r *http.Request) {
		var req wire.GoogleAuthRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		sum := sha256.Sum256([]byte(req.CodeVerifier))
		f.mu.Lock()
		defer f.mu.Unlock()
		f.exchanges++
		if req.Code != "synthetic-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge || req.Nonce != f.nonce || req.RedirectURI != f.redirect || req.DeviceName != "test-device" {
			w.Header().Set(wire.HeaderGatewayError, string(wire.CodeBadRequest))
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(wire.ErrorBody{Error: wire.ErrorDetail{Code: wire.CodeBadRequest, Message: "exchange mismatch"}})
			return
		}
		_ = json.NewEncoder(w).Encode(wire.SessionGrant{SessionID: "s1", Email: "person@example.test",
			AccessToken: wire.AccessTokenPrefix + "s1.synthetic", AccessExpiresAt: time.Now().Add(10 * time.Minute),
			RefreshToken: wire.RefreshTokenPrefix + "s1.synthetic", SessionExpiresAt: time.Now().Add(30 * 24 * time.Hour)})
	})
	mux.HandleFunc("POST "+wire.RouteAuthLogout, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.logouts++
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func newAccount(t *testing.T, origin string) (*session.Account, *session.MemoryStore) {
	store := &session.MemoryStore{}
	a, err := session.Open(session.Options{Origin: origin, Store: store, LockPath: filepath.Join(t.TempDir(), "lock")})
	require.NoError(t, err)
	return a, store
}

// urls collects authorize_url events.
func urls() (func(Event), func() string) {
	ch := make(chan string, 1)
	return func(e Event) { ch <- e.URL }, func() string { return <-ch }
}

func TestFullLoopbackFlow(t *testing.T) {
	stack := newFakeStack(t)
	account, store := newAccount(t, stack.srv.URL)
	browser := func(u string) error {
		go func() {
			if resp, err := http.Get(u); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	status, err := Run(context.Background(), Options{Account: account, Opener: browser, DeviceName: "test-device", Out: func(Event) {}})
	require.NoError(t, err)
	require.True(t, status.SignedIn)
	require.Equal(t, "person@example.test", status.Email)
	_, err = store.Load(account.Origin())
	require.NoError(t, err)
}

func TestWrongStateIsRejectedAndWaitingContinues(t *testing.T) {
	stack := newFakeStack(t)
	account, _ := newAccount(t, stack.srv.URL)
	out, next := urls()
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), Options{Account: account, NoBrowser: true, DeviceName: "test-device", Out: out})
		done <- err
	}()
	authorize := next()
	parsed, _ := url.Parse(authorize)
	redirect := parsed.Query().Get("redirect_uri")

	resp, err := http.Get(redirect + "/?code=synthetic-code&state=wrong")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	select {
	case err := <-done:
		t.Fatalf("login ended after a wrong state: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	resp, err = http.Get(authorize) // the real callback
	require.NoError(t, err)
	resp.Body.Close()
	require.NoError(t, <-done)
}

func TestSecondCallbackIsIgnored(t *testing.T) {
	results := make(chan callbackResult, 1)
	handler := callbackHandler("expected", results)
	for i, want := range []int{http.StatusOK, http.StatusBadRequest} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?code=c"+string(rune('0'+i))+"&state=expected", nil))
		require.Equal(t, want, rec.Code)
	}
	require.Equal(t, "c0", (<-results).code)
	require.Empty(t, results)
}

func TestCancellationClosesTheListener(t *testing.T) {
	stack := newFakeStack(t)
	account, store := newAccount(t, stack.srv.URL)
	out, next := urls()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, Options{Account: account, NoBrowser: true, DeviceName: "test-device", Out: out})
		done <- err
	}()
	authorize := next()
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	// A late callback reaches nothing and creates no session.
	if resp, err := http.Get(authorize); err == nil {
		resp.Body.Close()
	}
	_, err := store.Load(account.Origin())
	require.ErrorIs(t, err, session.ErrNotFound)
	stack.mu.Lock()
	defer stack.mu.Unlock()
	require.Zero(t, stack.exchanges)
}

func TestLateLoginAfterLogoutRevokesItsGrant(t *testing.T) {
	stack := newFakeStack(t)
	account, store := newAccount(t, stack.srv.URL)
	// Signed in at generation 1 when the login starts.
	_, err := account.Publish(context.Background(), wire.SessionGrant{SessionID: "s0", AccessToken: "rzt_a_s0.x", RefreshToken: "rzt_r_s0.x",
		AccessExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(time.Hour)}, 0)
	require.NoError(t, err)
	out, next := urls()
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), Options{Account: account, NoBrowser: true, DeviceName: "test-device", Out: out})
		done <- err
	}()
	authorize := next()
	require.NoError(t, store.Delete(account.Origin())) // another process signed out
	resp, err := http.Get(authorize)
	require.NoError(t, err)
	resp.Body.Close()
	require.ErrorIs(t, <-done, session.ErrSuperseded)
	_, err = store.Load(account.Origin())
	require.ErrorIs(t, err, session.ErrNotFound)
	stack.mu.Lock()
	defer stack.mu.Unlock()
	require.Equal(t, 1, stack.logouts, "the unpublished gateway session is revoked")
}
