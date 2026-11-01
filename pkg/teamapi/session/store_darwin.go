//go:build darwin

package session

// DefaultStore is the user's login keychain.
func DefaultStore() (Store, error) { return Keychain{}, nil }
