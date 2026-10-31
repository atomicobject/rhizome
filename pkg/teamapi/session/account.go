package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// refreshMargin is how long an access token must remain valid to be used
// without refreshing first.
const refreshMargin = 60 * time.Second

// logoutTimeout bounds remote revocation during logout. A variable for tests.
var logoutTimeout = 10 * time.Second

// Options configure an Account. Production callers pass DefaultStore and
// DefaultLockPath, which derive from the real OS user; tests inject both.
type Options struct {
	Origin   string
	Store    Store
	LockPath string
	// BaseTransport carries every request. Nil means DefaultBaseTransport.
	BaseTransport http.RoundTripper
	// UserAgent is sent on every request, normally rzm/<version>.
	UserAgent string
	// Now replaces the clock for expiry checks in tests.
	Now func() time.Time
	// SelectorPath, normally DefaultSelectorPath, is pointed at this origin
	// when a sign-in publishes and cleared at sign-out. Empty skips it.
	SelectorPath string
}

// Account is one OS user's session with one gateway origin. It is safe for
// concurrent use; separate processes coordinate through the lock file.
type Account struct {
	origin    string
	store     Store
	lockPath  string
	selector  string
	base      http.RoundTripper
	userAgent string
	now       func() time.Time

	mu     sync.Mutex
	cached *Record
}

// Status describes a session without its tokens.
type Status struct {
	SignedIn         bool
	Email            string
	Origin           string
	SessionExpiresAt time.Time
	Limits           wire.Limits
}

// Open validates the origin and prepares an account. It reads nothing yet.
func Open(opts Options) (*Account, error) {
	origin, err := CanonicalOrigin(opts.Origin)
	if err != nil {
		return nil, err
	}
	if opts.Store == nil || opts.LockPath == "" {
		return nil, errors.New("team session: a store and a lock path are required")
	}
	a := &Account{origin: origin, store: opts.Store, lockPath: opts.LockPath, selector: opts.SelectorPath, base: opts.BaseTransport, userAgent: opts.UserAgent, now: opts.Now}
	if a.base == nil {
		a.base = DefaultBaseTransport()
	}
	if a.now == nil {
		a.now = time.Now
	}
	return a, nil
}

// DefaultBaseTransport is http.DefaultTransport without proxy support.
func DefaultBaseTransport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	// Proxy settings come from the environment, and main loads repository
	// dotenv files into it, so a repository could route session tokens
	// through its own proxy. ponytail: no proxy at all; add one from trusted
	// user configuration (captured before dotenv loading) if a network needs it.
	t.Proxy = nil
	return t
}

// NewClient is an HTTP client over base (nil means DefaultBaseTransport)
// that refuses redirects.
func NewClient(base http.RoundTripper) *http.Client {
	if base == nil {
		base = DefaultBaseTransport()
	}
	return &http.Client{Transport: base, CheckRedirect: refuseRedirect}
}

func refuseRedirect(*http.Request, []*http.Request) error { return ErrRedirect }

// Origin is the canonical gateway origin.
func (a *Account) Origin() string { return a.origin }

// Status reports the stored session. An expired or missing session is
// SignedIn false with a nil error.
func (a *Account) Status() (Status, error) {
	rec, err := a.load()
	if errors.Is(err, ErrNotFound) || errors.Is(err, errMalformed) {
		return Status{Origin: a.origin}, nil
	}
	if err != nil {
		return Status{Origin: a.origin}, err
	}
	return a.statusOf(rec), nil
}

func (a *Account) statusOf(rec Record) Status {
	return Status{
		SignedIn: a.now().Before(rec.SessionExpiresAt), Email: rec.Email, Origin: a.origin,
		SessionExpiresAt: rec.SessionExpiresAt, Limits: rec.Limits,
	}
}

// Generation identifies the current sign-in, 0 when there is none. Login
// captures it before opening the browser and passes it to Publish.
func (a *Account) Generation() (uint64, error) {
	rec, err := a.load()
	if errors.Is(err, ErrNotFound) || errors.Is(err, errMalformed) {
		return 0, nil
	}
	return rec.Generation, err
}

// Publish stores a new sign-in, but only if the account is still at the
// generation the login started from, so a sign-in or sign-out that finished
// meanwhile is never undone.
func (a *Account) Publish(ctx context.Context, grant wire.SessionGrant, expected uint64) (Status, error) {
	unlock, err := lockFile(ctx, a.lockPath)
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	current, err := a.Generation()
	if err != nil {
		return Status{}, err
	}
	if current != expected {
		return Status{}, ErrSuperseded
	}
	// ponytail: a sign-out returns the generation to 0, so a login that began
	// signed out can still publish after another process signs in and out
	// again. Keep a tombstone generation if that ABA case starts to matter.
	rec, err := recordFromGrant(a.origin, current+1, grant)
	if err != nil {
		return Status{}, err
	}
	if err := a.save(rec); err != nil {
		return Status{}, err
	}
	if a.selector != "" {
		if err := writeSelector(a.selector, a.origin); err != nil {
			return Status{}, errors.Join(err, a.delete())
		}
	}
	return a.statusOf(rec), nil
}

// Logout revokes the session on the gateway and deletes the local record,
// serialized with refresh so a refresh cannot recreate it. remoteRevoked is
// false when the gateway could not be reached; the record is deleted anyway.
func (a *Account) Logout(ctx context.Context) (remoteRevoked bool, err error) {
	unlock, err := lockFile(ctx, a.lockPath)
	if err != nil {
		return false, err
	}
	defer unlock()
	rec, err := a.load()
	switch {
	case errors.Is(err, ErrNotFound):
		return true, a.clearSelector()
	case errors.Is(err, errMalformed):
		return false, errors.Join(a.delete(), a.clearSelector())
	case err != nil:
		return false, err
	}
	// Remote revocation is bounded so an unresponsive gateway cannot hold the
	// session lock or keep the local record; local cleanup always happens.
	revokeCtx, cancel := context.WithTimeout(ctx, logoutTimeout)
	remoteRevoked = a.revoke(revokeCtx, rec)
	cancel()
	return remoteRevoked, errors.Join(a.delete(), a.clearSelector())
}

func (a *Account) clearSelector() error {
	if a.selector == "" {
		return nil
	}
	return clearSelector(a.selector, a.origin)
}

func (a *Account) revoke(ctx context.Context, rec Record) bool {
	if !a.fresh(rec) {
		rotated, err := a.rotate(ctx, rec)
		if err != nil {
			return errors.Is(err, wire.ErrSignInRequired)
		}
		rec = rotated
	}
	err := a.post(ctx, wire.RouteAuthLogout, rec.AccessToken, nil, nil)
	return err == nil || errors.Is(err, wire.ErrSignInRequired)
}

// current returns a record whose access token is usable, refreshing when it
// expires within refreshMargin. The common path takes no lock.
func (a *Account) current(ctx context.Context) (Record, error) {
	a.mu.Lock()
	cached := a.cached
	a.mu.Unlock()
	if cached != nil && a.fresh(*cached) {
		return *cached, nil
	}
	rec, err := a.load()
	if err != nil {
		return Record{}, signInError(err)
	}
	if a.fresh(rec) {
		a.setCache(&rec)
		return rec, nil
	}
	return a.refresh(ctx, "")
}

// refresh takes the lock, rereads the shared record, and rotates the tokens
// only if no other process already replaced them. rejected is the access
// token the gateway refused, or "" when the token is merely near expiry.
func (a *Account) refresh(ctx context.Context, rejected string) (rec Record, err error) {
	start := time.Now()
	var lockWait time.Duration
	outcome := "failed"
	defer func() {
		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelWarn
		}
		event(ctx, level, "session.refresh", slog.String("outcome", outcome),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()), slog.Int64("lock_wait_ms", lockWait.Milliseconds()))
	}()
	unlock, err := lockFile(ctx, a.lockPath)
	lockWait = time.Since(start)
	if err != nil {
		return Record{}, err
	}
	defer unlock()
	a.mu.Lock()
	unsaved := a.cached
	a.mu.Unlock()
	rec, err = a.load()
	if err != nil {
		outcome = "signed_out"
		return Record{}, signInError(err)
	}
	if unsaved != nil && unsaved.SessionID == rec.SessionID && unsaved.Generation == rec.Generation && unsaved.AccessExpiresAt.After(rec.AccessExpiresAt) {
		rec = *unsaved // an earlier rotation this process could not save
	}
	if rec.AccessToken != rejected && a.fresh(rec) {
		outcome = "already_fresh"
		a.setCache(&rec)
		return rec, nil
	}
	usable := func(cause error) (Record, error) {
		// A token that is near expiry but not rejected still works.
		if rec.AccessToken != rejected && a.now().Before(rec.AccessExpiresAt) {
			a.setCache(&rec)
			return rec, nil
		}
		return Record{}, cause
	}
	// Rotation retires the stored refresh token, and the gateway revokes the
	// session if another process reuses it after the grace period. Prove the
	// store accepts a write before rotating, so a failing keychain cannot
	// strand the other processes on the retired token.
	if err := a.save(rec); err != nil {
		outcome = "store_unwritable"
		return usable(fmt.Errorf("%w: %w", ErrStoreUnavailable, err))
	}
	rotated, err := a.rotate(ctx, rec)
	if errors.Is(err, wire.ErrSignInRequired) {
		outcome = "session_ended"
		return Record{}, errors.Join(err, a.delete())
	}
	if err != nil {
		return usable(err)
	}
	if err := a.save(rotated); err != nil {
		if err := a.save(rotated); err != nil {
			// Keep the rotation for this process; the next refresh here
			// writes it before rotating again.
			a.setCache(&rotated)
			event(ctx, slog.LevelWarn, "session.save_failed", slog.String("error", err.Error()))
		}
	}
	outcome = "refreshed"
	return rotated, nil
}

// rotate exchanges the refresh token. The caller holds the lock and saves.
func (a *Account) rotate(ctx context.Context, rec Record) (Record, error) {
	var grant wire.SessionGrant
	if err := a.post(ctx, wire.RouteAuthRefresh, "", wire.RefreshRequest{RefreshToken: rec.RefreshToken}, &grant); err != nil {
		return Record{}, err
	}
	return recordFromGrant(a.origin, rec.Generation, grant)
}

// clear forgets this process's cached record, then deletes the stored record
// only if it is still the session that was rejected. Generations restart
// after logout, so the session ID is compared too: a late rejection of an old
// session never deletes a newer sign-in.
func (a *Account) clear(ctx context.Context, generation uint64, sessionID string) {
	a.setCache(nil)
	unlock, err := lockFile(ctx, a.lockPath)
	if err != nil {
		return
	}
	defer unlock()
	if rec, err := a.load(); err == nil && rec.Generation == generation && rec.SessionID == sessionID {
		_ = a.delete()
	}
}

// post sends one JSON auth call outside Transport, which would recurse.
func (a *Account) post(ctx context.Context, route, token string, body, out any) error {
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return err
		}
	}
	return Call(ctx, NewClient(a.base), http.MethodPost, a.origin+route, token, a.userAgent, data, out)
}

func (a *Account) fresh(rec Record) bool {
	return rec.AccessExpiresAt.Sub(a.now()) > refreshMargin
}

func (a *Account) load() (Record, error) {
	data, err := a.store.Load(a.origin)
	if err != nil {
		return Record{}, err
	}
	return decodeRecord(data, a.origin)
}

func (a *Account) save(rec Record) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := a.store.Save(a.origin, data); err != nil {
		return err
	}
	a.setCache(&rec)
	return nil
}

func (a *Account) delete() error {
	a.setCache(nil)
	return a.store.Delete(a.origin)
}

func (a *Account) setCache(rec *Record) {
	a.mu.Lock()
	a.cached = rec
	a.mu.Unlock()
}

// signInError marks a missing session as needing sign-in and passes other
// store errors (unavailable, newer record) through.
func signInError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, errMalformed) {
		return fmt.Errorf("%w (%w)", wire.ErrSignInRequired, err)
	}
	return err
}

// event records client-side gateway diagnostics when the context carries a
// recorder. Callers never pass tokens.
func event(ctx context.Context, level slog.Level, name string, attrs ...slog.Attr) {
	diagnostics.FromContext(ctx).EventQuiet(ctx, level, "teamapi", name, "", attrs...)
}
