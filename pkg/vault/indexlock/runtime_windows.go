//go:build windows

package indexlock

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

// runtimeIdentity is the short hostname plus the boot time derived from
// GetTickCount64 (windows.DurationSinceBoot). Uptime and wall clock drift
// together, so every process on this boot computes the same instant; rounding
// to the minute absorbs the millisecond jitter between them.
func runtimeIdentity() (string, error) {
	host := shortHostname()
	if host == "" || host == "unknown" {
		return "", fmt.Errorf("hostname unavailable")
	}
	boot := time.Now().Add(-windows.DurationSinceBoot()).UTC().Round(time.Minute)
	return fmt.Sprintf("%s:boot-%d", host, boot.Unix()), nil
}
