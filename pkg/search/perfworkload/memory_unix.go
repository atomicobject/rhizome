//go:build darwin || linux

package perfworkload

import (
	"runtime"

	"golang.org/x/sys/unix"
)

func processPeakRSSBytes() uint64 {
	var usage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &usage); err != nil || usage.Maxrss < 0 {
		return 0
	}
	value := uint64(usage.Maxrss)
	if runtime.GOOS == "linux" {
		value *= 1024
	}
	return value
}
