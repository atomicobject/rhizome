//go:build linux

package indexlock

import (
	"fmt"
	"os"
	"strings"
)

// runtimeIdentity is the short hostname plus the kernel boot id and PID
// namespace. Boot id already fences a reboot; the short hostname keeps the
// identity readable and survives a DNS suffix change.
func runtimeIdentity() (string, error) {
	host := shortHostname()
	if host == "" || host == "unknown" {
		return "", fmt.Errorf("hostname unavailable")
	}
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	pidNamespace, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		return "", err
	}
	boot := strings.TrimSpace(string(bootID))
	if boot == "" || pidNamespace == "" {
		return "", fmt.Errorf("empty boot or PID namespace identity")
	}
	return host + ":boot-" + boot + ":" + pidNamespace, nil
}
