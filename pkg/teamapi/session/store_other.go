//go:build !darwin && !linux && !windows

package session

// DefaultStore refuses platforms without a native protected store.
func DefaultStore() (Store, error) { return nil, ErrStoreUnavailable }
