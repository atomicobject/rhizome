// Package session keeps the team API session for an OS user: the stored
// record, the platform secret store, and refresh coordination.
package session

import "errors"

var (
	// ErrNotFound means no session is stored for the origin.
	ErrNotFound = errors.New("team session: not signed in")
	// ErrStoreUnavailable means the platform secret store is missing, locked,
	// or refused access. There is no plaintext fallback.
	ErrStoreUnavailable = errors.New("team session: secure credential store unavailable")
	// ErrNewerRecord means the stored record has a version this client does
	// not understand. It is never overwritten or deleted.
	ErrNewerRecord = errors.New("team session: a newer Rhizome manages this session; update Rhizome")
	// ErrSuperseded means the account changed (sign-in, sign-out) while a
	// login waited in the browser, so its grant was not published.
	ErrSuperseded = errors.New("team session: the account changed during sign-in; sign in again")
	// ErrForeignOrigin means a request was addressed somewhere other than the
	// session's origin. Session tokens are never sent there.
	ErrForeignOrigin = errors.New("team session: request is not addressed to the session origin")
	// ErrRedirect means the gateway answered with a redirect, which session
	// requests never follow.
	ErrRedirect = errors.New("team session: gateway redirects are refused")
	// ErrLockTimeout means another process held the session lock too long.
	ErrLockTimeout = errors.New("team session: timed out waiting for the session lock")
)
