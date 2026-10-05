//go:build !linux && !darwin && !windows

package indexlock

import "fmt"

// runtimeIdentity has no boot-time source on this platform, so a lock left by
// a process from a previous boot stays foreign until an operator removes it.
func runtimeIdentity() (string, error) {
	host := shortHostname()
	if host == "" || host == "unknown" {
		return "", fmt.Errorf("hostname unavailable")
	}
	return host, nil
}
