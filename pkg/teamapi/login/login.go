// Package login signs a person in to the team API gateway: Google OAuth
// through the system browser and a loopback redirect with PKCE, state, and
// nonce, then the gateway's code exchange and publication to the session
// store.
package login

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// Options configure one sign-in. Account supplies the origin and the store
// the grant is published to.
type Options struct {
	Account *session.Account
	// Opener opens the authorization URL in a browser. It is not called when
	// NoBrowser is set, and its failure is not fatal: the URL is also emitted.
	Opener    func(url string) error
	NoBrowser bool
	// Port is the loopback port; 0 picks a free one.
	Port       int
	DeviceName string
	// HTTPClient calls the gateway. Nil means session.NewClient(nil).
	HTTPClient *http.Client
	UserAgent  string
	// Out receives progress events.
	Out func(Event)
}

// Event is progress reported to Out.
type Event struct {
	Name string // "authorize_url"
	URL  string
}

// ErrDenied means the identity provider returned an error to the callback.
var ErrDenied = errors.New("sign-in was not completed in the browser")

// Run signs in and returns the published session status. It returns when the
// sign-in completes, fails, or ctx ends; the listener is closed either way,
// so a late browser callback reaches nothing.
func Run(ctx context.Context, opts Options) (session.Status, error) {
	if opts.Account == nil {
		return session.Status{}, errors.New("login: an account is required")
	}
	origin := opts.Account.Origin()
	client := opts.HTTPClient
	if client == nil {
		client = session.NewClient(nil)
	}
	call := func(method, route string, body, out any) error {
		var data []byte
		if body != nil {
			var err error
			if data, err = json.Marshal(body); err != nil {
				return err
			}
		}
		return session.Call(ctx, client, method, origin+route, "", opts.UserAgent, data, out)
	}

	var meta wire.Meta
	if err := call(http.MethodGet, wire.RouteMeta, nil, &meta); err != nil {
		return session.Status{}, err
	}
	if err := checkMeta(meta); err != nil {
		return session.Status{}, err
	}
	// Captured before the browser opens: publication requires the account to
	// be unchanged when the callback arrives.
	generation, err := opts.Account.Generation()
	if err != nil {
		return session.Status{}, err
	}

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.Port)))
	if err != nil {
		return session.Status{}, fmt.Errorf("login: listen on loopback: %w", err)
	}
	redirectURI := "http://" + listener.Addr().String()
	verifier, state, nonce := randomToken(), randomToken(), randomToken()
	challenge := sha256.Sum256([]byte(verifier))
	authorizeURL, err := authorizationURL(meta, redirectURI, base64.RawURLEncoding.EncodeToString(challenge[:]), state, nonce)
	if err != nil {
		listener.Close()
		return session.Status{}, err
	}

	codes := make(chan callbackResult, 1)
	server := &http.Server{Handler: callbackHandler(state, codes), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	if opts.Out != nil {
		opts.Out(Event{Name: "authorize_url", URL: authorizeURL})
	}
	if !opts.NoBrowser && opts.Opener != nil {
		_ = opts.Opener(authorizeURL)
	}

	var result callbackResult
	select {
	case <-ctx.Done():
		return session.Status{}, ctx.Err()
	case result = <-codes:
	}
	// Let the success page reach the browser, then stop listening.
	shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = server.Shutdown(shutdown)
	cancel()
	if result.err != nil {
		return session.Status{}, result.err
	}

	var grant wire.SessionGrant
	err = call(http.MethodPost, wire.RouteAuthGoogle, wire.GoogleAuthRequest{
		Code: result.code, CodeVerifier: verifier, RedirectURI: redirectURI, Nonce: nonce, DeviceName: opts.DeviceName,
	}, &grant)
	if err != nil {
		return session.Status{}, err
	}
	status, err := opts.Account.Publish(ctx, grant, generation)
	if err != nil {
		// The gateway session exists but nothing local holds it; revoke it.
		if grant.AccessToken != "" {
			revoke := session.Call(context.WithoutCancel(ctx), client, http.MethodPost, origin+wire.RouteAuthLogout, grant.AccessToken, opts.UserAgent, nil, nil)
			if revoke != nil {
				err = fmt.Errorf("%w (revoking the unsaved gateway session also failed: %v)", err, revoke)
			}
		}
		return session.Status{}, err
	}
	return status, nil
}

func checkMeta(meta wire.Meta) error {
	if meta.APIVersion != wire.APIVersion || meta.ClientID == "" || meta.AuthorizationEndpoint == "" {
		return fmt.Errorf("login: gateway metadata is incomplete or for API version %d", meta.APIVersion)
	}
	u, err := url.Parse(meta.AuthorizationEndpoint)
	if err != nil {
		return fmt.Errorf("login: authorization endpoint: %w", err)
	}
	loopback := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return fmt.Errorf("login: authorization endpoint must use HTTPS")
	}
	return nil
}

func authorizationURL(meta wire.Meta, redirectURI, challenge, state, nonce string) (string, error) {
	u, err := url.Parse(meta.AuthorizationEndpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", meta.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", meta.Scopes)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("nonce", nonce)
	if meta.HostedDomainHint != "" {
		q.Set("hd", meta.HostedDomainHint)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

type callbackResult struct {
	code string
	err  error
}

// callbackHandler accepts exactly one request carrying the expected state.
// Anything else gets 400 and the wait continues.
func callbackHandler(state string, results chan<- callbackResult) http.Handler {
	var once sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/" || q.Get("state") != state {
			http.Error(w, "This is not the sign-in Rhizome is waiting for.", http.StatusBadRequest)
			return
		}
		result := callbackResult{code: q.Get("code")}
		if e := q.Get("error"); e != "" {
			result = callbackResult{err: fmt.Errorf("%w: %s", ErrDenied, e)}
		} else if result.code == "" {
			http.Error(w, "The sign-in response has no authorization code.", http.StatusBadRequest)
			return
		}
		accepted := false
		once.Do(func() { accepted = true; results <- result })
		if !accepted {
			http.Error(w, "Sign-in already finished.", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := "Signed in. You can close this tab and return to Rhizome."
		if result.err != nil {
			page = "Sign-in did not finish. Return to Rhizome to try again."
		}
		fmt.Fprintf(w, "<!doctype html><title>Rhizome</title><p>%s</p>", page)
	})
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
