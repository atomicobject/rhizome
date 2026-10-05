//go:build darwin

package indexlock

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// runtimeIdentity is the short hostname plus this boot's start time. A lock
// left behind by a process that ran before the last reboot is therefore
// foreign-by-boot and reclaimable, while a DNS suffix change is not.
func runtimeIdentity() (string, error) {
	host := shortHostname()
	if host == "" || host == "unknown" {
		return "", fmt.Errorf("hostname unavailable")
	}
	boot, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return "", fmt.Errorf("read kern.boottime: %w", err)
	}
	return fmt.Sprintf("%s:boot-%d", host, boot.Sec), nil
}
