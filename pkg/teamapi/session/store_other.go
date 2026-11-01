//go:build !darwin

package session

// DefaultStore reports ErrStoreUnavailable: managed sign-in is macOS-only
// until Secret Service and Credential Manager adapters pass native checks.
func DefaultStore() (Store, error) { return nil, ErrStoreUnavailable }
