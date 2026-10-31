package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/login"
	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// TestDevStack runs the whole local stack (gateway, development identity
// provider, fake providers) on one listener and drives it with the real
// client packages: loopback PKCE sign-in, a session transport, paid calls,
// idempotency, eligibility, and the admin API.
func TestDevStack(t *testing.T) {
	dir := t.TempDir()
	origin := startStack(t, dir)

	member := signIn(t, origin, dir, "member")
	embed := func(key string) (*http.Response, error) {
		req, _ := http.NewRequest(http.MethodPost, origin+wire.RouteVoyageEmbed, strings.NewReader(`{"model":"voyage-4-lite","input":["hello","world"],"input_type":"document"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(wire.HeaderIdempotencyKey, key)
		return member.HTTPClient().Do(req)
	}
	resp, err := embed("stack-test-key-1")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("embed: status %d, %v", resp.StatusCode, err)
	}
	resp.Body.Close()
	if len(out.Data) != 2 || len(out.Data[0].Embedding) != 1024 || out.Usage.TotalTokens == 0 {
		t.Fatalf("embed response: %d vectors, %d dims, %d tokens", len(out.Data), len(out.Data[0].Embedding), out.Usage.TotalTokens)
	}

	// The same key never dispatches twice.
	if _, err := embed("stack-test-key-1"); !gatewayCode(err, wire.CodeResultLost) {
		t.Fatalf("repeat key: %v, want %s", err, wire.CodeResultLost)
	}
	if n := fakeStat(t, origin, "/dev/upstream/voyage", "/v1/embeddings"); n != 1 {
		t.Fatalf("fake Voyage saw %d requests, want 1", n)
	}

	// Members are not admins; admins see the member's usage.
	if err := session.Call(context.Background(), member.HTTPClient(), http.MethodGet, origin+wire.RouteAdminUsage, "", "rzm/dev", nil, nil); !gatewayCode(err, wire.CodeNotAdmin) {
		t.Fatalf("member admin usage: %v, want %s", err, wire.CodeNotAdmin)
	}
	admin := signIn(t, origin, dir, "admin")
	var usage wire.AdminUsage
	if err := session.Call(context.Background(), admin.HTTPClient(), http.MethodGet, origin+wire.RouteAdminUsage, "", "rzm/dev", nil, &usage); err != nil {
		t.Fatalf("admin usage: %v", err)
	}
	if usage.Global.Usage.Requests != 1 {
		t.Fatalf("global requests = %d, want 1", usage.Global.Usage.Requests)
	}

	// Disabling the member stops their paid calls within the cache window.
	var people []wire.AdminPerson
	if err := session.Call(context.Background(), admin.HTTPClient(), http.MethodGet, origin+wire.RouteAdminPeople, "", "rzm/dev", nil, &people); err != nil {
		t.Fatalf("admin people: %v", err)
	}
	sub := ""
	for _, p := range people {
		if p.Email == "dev-member@atomicobject.com" {
			sub = p.Sub
		}
	}
	disabled := wire.PersonDisabled
	body, _ := json.Marshal(wire.AdminPersonUpdate{Status: &disabled})
	path := strings.Replace(wire.RouteAdminPerson, "{sub}", sub, 1)
	if err := session.Call(context.Background(), admin.HTTPClient(), http.MethodPatch, origin+path, "", "rzm/dev", body, nil); err != nil {
		t.Fatalf("disable member: %v", err)
	}
	revokeAll := strings.Replace(wire.RouteAdminRevokeAll, "{sub}", sub, 1)
	if err := session.Call(context.Background(), admin.HTTPClient(), http.MethodPost, origin+revokeAll, "", "rzm/dev", nil, nil); err != nil {
		t.Fatalf("revoke member: %v", err)
	}
	// Denial within the 30-second cache window is covered with a controlled
	// clock in internal/identity; here the change must show in the listing
	// and the audit log.
	people = nil
	if err := session.Call(context.Background(), admin.HTTPClient(), http.MethodGet, origin+wire.RouteAdminPeople, "", "rzm/dev", nil, &people); err != nil {
		t.Fatalf("admin people: %v", err)
	}
	for _, p := range people {
		if p.Sub == sub && p.Status != wire.PersonDisabled {
			t.Fatalf("member status = %s, want disabled", p.Status)
		}
	}
	var audit []wire.AuditEntry
	if err := session.Call(context.Background(), admin.HTTPClient(), http.MethodGet, origin+wire.RouteAdminAudit, "", "rzm/dev", nil, &audit); err != nil || len(audit) < 2 {
		t.Fatalf("audit: %d entries, %v", len(audit), err)
	}

	// Logging out ends the admin session at once.
	if remote, err := admin.Logout(context.Background()); err != nil || !remote {
		t.Fatalf("logout: remote=%v err=%v", remote, err)
	}
	if err := session.Call(context.Background(), admin.HTTPClient(), http.MethodGet, origin+wire.RouteAdminUsage, "", "rzm/dev", nil, nil); !errors.Is(err, wire.ErrSignInRequired) {
		t.Fatalf("admin call after logout: %v, want sign-in required", err)
	}

	// Ineligible identities cannot sign in.
	for _, who := range []string{"outsider", "unverified", "wrongdomain"} {
		if _, err := trySignIn(t, origin, dir, who); err == nil {
			t.Fatalf("%s signed in", who)
		}
	}

	// Server diagnostics land in the data directory, apart from any vault.
	events, _ := filepath.Glob(filepath.Join(dir, "data", ".rhizome", "diagnostics", "events", "*.jsonl"))
	if len(events) == 0 {
		t.Fatal("no server diagnostics events were written")
	}
}

func startStack(t *testing.T, dir string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- devWithReady(ctx, []string{"--listen", addr, "--data-dir", filepath.Join(dir, "data"), "--upstreams", "fake"}, ready)
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("stack exited: %v", err)
		}
	})
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("stack failed to start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("stack did not start")
	}
	return "http://" + addr
}

func signIn(t *testing.T, origin, dir, identity string) *session.Account {
	t.Helper()
	acct, err := trySignIn(t, origin, dir, identity)
	if err != nil {
		t.Fatalf("sign in as %s: %v", identity, err)
	}
	return acct
}

func trySignIn(t *testing.T, origin, dir, identity string) (*session.Account, error) {
	t.Helper()
	acct, err := session.Open(session.Options{
		Origin:       origin,
		Store:        &session.MemoryStore{},
		LockPath:     filepath.Join(dir, identity+".lock"),
		SelectorPath: filepath.Join(dir, identity+"-selector.json"),
		UserAgent:    "rzm/dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err = login.Run(ctx, login.Options{Account: acct, Opener: browser(identity), UserAgent: "rzm/dev", DeviceName: "stack-test"})
	return acct, err
}

var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

// browser stands in for the person: it loads the development sign-in page,
// picks identity, and follows the redirect to the loopback callback.
func browser(identity string) func(string) error {
	return func(authURL string) error {
		go func() {
			noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := noFollow.Get(authURL)
			if err != nil {
				return
			}
			page, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			m := csrfField.FindSubmatch(page)
			if m == nil {
				return
			}
			u, _ := url.Parse(authURL)
			resp, err = noFollow.PostForm(u.Scheme+"://"+u.Host+u.Path, url.Values{"csrf": {string(m[1])}, "identity": {identity}})
			if err != nil {
				return
			}
			resp.Body.Close()
			if loc := resp.Header.Get("Location"); loc != "" {
				if cb, err := http.Get(loc); err == nil {
					cb.Body.Close()
				}
			}
		}()
		return nil
	}
}

func fakeStat(t *testing.T, origin, mount, path string) int {
	t.Helper()
	resp, err := http.Get(origin + mount + "/_stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var stats map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	return stats[path]
}

func gatewayCode(err error, code wire.ErrorCode) bool {
	var ge *wire.GatewayError
	return errors.As(err, &ge) && ge.Code == code
}

// A second gateway on the same data directory stops before it opens the
// store, so its startup recovery can never mark the first gateway's
// in-flight receipts as unknown outcomes.
func TestSecondGatewayOnTheSameDataDirRefusesToStart(t *testing.T) {
	dir := t.TempDir()
	startStack(t, dir)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	err = devWithReady(context.Background(), []string{"--listen", addr, "--data-dir", filepath.Join(dir, "data"), "--upstreams", "fake"}, nil)
	if err == nil || !strings.Contains(err.Error(), "already using") {
		t.Fatalf("second gateway: %v, want refusal", err)
	}
}
