package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/stretchr/testify/require"
)

// fakeAuthGateway serves meta, a fake identity provider, the Google exchange,
// logout, and two admin routes. Tokens are synthetic.
func fakeAuthGateway(t *testing.T) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var patches []string
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+wire.RouteMeta, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(wire.Meta{APIVersion: 1, ClientID: "synthetic", AuthorizationEndpoint: srv.URL + "/authorize", Scopes: "openid email"})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		http.Redirect(w, r, q.Get("redirect_uri")+"/?code=c&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("POST "+wire.RouteAuthGoogle, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(wire.SessionGrant{SessionID: "s1", Email: "person@example.test",
			AccessToken: wire.AccessTokenPrefix + "s1.synthetic", AccessExpiresAt: time.Now().Add(10 * time.Minute), RefreshToken: wire.RefreshTokenPrefix + "s1.synthetic",
			SessionExpiresAt: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)})
	})
	mux.HandleFunc("POST "+wire.RouteAuthLogout, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET "+wire.RouteAdminPeople, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+wire.AccessTokenPrefix+"s1.synthetic" {
			w.Header().Set(wire.HeaderGatewayError, string(wire.CodeNotAdmin))
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(wire.ErrorBody{Error: wire.ErrorDetail{Code: wire.CodeNotAdmin, Message: "synthetic"}})
			return
		}
		_ = json.NewEncoder(w).Encode([]wire.AdminPerson{{Sub: "sub-1", Email: "person@example.test", Status: wire.PersonActive}})
	})
	mux.HandleFunc("PATCH "+wire.RouteAdminConfig, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		patches = append(patches, string(body))
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(wire.AdminConfig{PaidCallsEnabled: false})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &patches
}

func testAuthDeps(t *testing.T) authDeps {
	store := &session.MemoryStore{}
	dir := t.TempDir()
	return authDeps{
		version: "v9.9.9", getenv: func(string) string { return "" },
		store:        func() (session.Store, error) { return store, nil },
		lockPath:     func() (string, error) { return filepath.Join(dir, "team-session.lock"), nil },
		selectorPath: func() (string, error) { return filepath.Join(dir, "team-session.json"), nil },
		opener: func(u string) error {
			go func() {
				if resp, err := http.Get(u); err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
	}
}

func runAuth(t *testing.T, deps authDeps, args ...string) (string, error) {
	t.Helper()
	command := newAuthCommand(deps)
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(io.Discard)
	command.SetArgs(args)
	err := command.ExecuteContext(context.Background())
	require.NotContains(t, out.String(), "rzt_", "output never contains a token")
	return out.String(), err
}

func jsonLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var value map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &value), line)
		lines = append(lines, value)
	}
	return lines
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestAuthJSONContract(t *testing.T) {
	gateway, patches := fakeAuthGateway(t)
	deps := testAuthDeps(t)

	out, err := runAuth(t, deps, "login", "--json", "--server", gateway.URL, "--device-name", "test")
	require.NoError(t, err)
	lines := jsonLines(t, out)
	require.Len(t, lines, 2)
	require.Equal(t, []string{"event", "url"}, keys(lines[0]))
	require.Equal(t, "authorize_url", lines[0]["event"])
	require.Contains(t, lines[0]["url"], gateway.URL+"/authorize?")
	require.Equal(t, map[string]any{"event": "signed_in", "email": "person@example.test", "origin": gateway.URL,
		"session_expires_at": "2030-01-02T03:04:05Z"}, lines[1])

	// Without --server, every command finds the origin through the selector.
	out, err = runAuth(t, deps, "status", "--json")
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"signed_in": true, "email": "person@example.test", "origin": gateway.URL,
		"session_expires_at": "2030-01-02T03:04:05Z"}}, jsonLines(t, out))

	out, err = runAuth(t, deps, "admin", "people", "--json")
	require.NoError(t, err)
	require.JSONEq(t, `[{"sub":"sub-1","email":"person@example.test","hd":"","status":"active","admin":false,"created_at":"0001-01-01T00:00:00Z"}]`, out)

	_, err = runAuth(t, deps, "admin", "config", "--paid-calls", "off", "--eligibility", "allowlist", "--allow", "a@example.test,b@example.test", "--json")
	require.NoError(t, err)
	require.JSONEq(t, `{"paid_calls_enabled":false,"eligibility_mode":"allowlist","allowlist":["a@example.test","b@example.test"]}`, (*patches)[0])

	out, err = runAuth(t, deps, "logout", "--json")
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"signed_out": true, "remote_revoked": true}}, jsonLines(t, out))

	out, err = runAuth(t, deps, "status", "--json", "--server", gateway.URL)
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"signed_in": false, "origin": gateway.URL}}, jsonLines(t, out))

	// Signed out, admin commands report a typed error.
	out, err = runAuth(t, deps, "admin", "people", "--json", "--server", gateway.URL)
	require.Error(t, err)
	require.Equal(t, "sign_in_required", jsonLines(t, out)[0]["error"].(map[string]any)["code"])
}

func TestAuthLoginJSONReportsErrors(t *testing.T) {
	deps := testAuthDeps(t)
	out, err := runAuth(t, deps, "login", "--json", "--server", "http://team.example.test")
	var coded interface{ ExitCode() int }
	require.ErrorAs(t, err, &coded)
	require.Equal(t, 1, coded.ExitCode())
	lines := jsonLines(t, out)
	require.Len(t, lines, 1)
	require.Equal(t, []string{"code", "event", "message"}, keys(lines[0]))
	require.Equal(t, "error", lines[0]["event"])

	// A release build with no selector and no compiled-in origin needs --server.
	out, err = runAuth(t, deps, "status", "--json")
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"signed_in": false, "origin": ""}}, jsonLines(t, out))
	_, err = runAuth(t, deps, "login")
	require.ErrorContains(t, err, "--server")
}

func TestAuthInvocationBypassesDotEnv(t *testing.T) {
	require.True(t, IsAuthInvocation([]string{"auth", "status"}))
	require.True(t, IsAuthInvocation([]string{"--vault", "x", "auth"}))
	require.False(t, IsAuthInvocation([]string{"search", "auth"}))
}
