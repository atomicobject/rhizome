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
)
