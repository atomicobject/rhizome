//go:build !darwin && !linux

package session

// DefaultStore reports ErrStoreUnavailable until a native secure-store adapter
// passes checks on this platform.
func DefaultStore() (Store, error) { return nil, ErrStoreUnavailable }
