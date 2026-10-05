package indexlock

import (
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var linuxBootIdentity = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}:pid:\[[0-9]+\]$`)

// lockFromPreviousBoot proves that this host rebooted after acquiring the
// lock. A dead PID is still required: estimated Windows boot times can drift,
// and uncertainty must never permit takeover from a live writer.
func lockFromPreviousBoot(owner LockData, currentIdentity string) bool {
	host := shortHostname()
	if owner.Host != getHostname() || owner.PID <= 0 {
		return false
	}
	prefix := host + ":boot-"
	previous, previousOK := strings.CutPrefix(owner.Runtime, prefix)
	current, currentOK := strings.CutPrefix(currentIdentity, prefix)
	if !previousOK || !currentOK || previous == current {
		return false
	}
	var boot time.Time
	switch runtime.GOOS {
	case "darwin", "windows":
		oldSeconds, oldErr := strconv.ParseInt(previous, 10, 64)
		seconds, err := strconv.ParseInt(current, 10, 64)
		if oldErr != nil || err != nil || oldSeconds <= 0 || oldSeconds >= seconds || strconv.FormatInt(oldSeconds, 10) != previous {
			return false
		}
		boot = time.Unix(seconds, 0)
	case "linux":
		if !linuxBootIdentity.MatchString(previous) || !linuxBootIdentity.MatchString(current) {
			return false
		}
		oldID, _, _ := strings.Cut(previous, ":pid:")
		currentID, _, _ := strings.Cut(current, ":pid:")
		if oldID == currentID {
			return false // A different PID namespace on this boot is foreign.
		}
		data, err := os.ReadFile("/proc/stat")
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "btime" {
				seconds, err := strconv.ParseInt(fields[1], 10, 64)
				if err != nil || seconds <= 0 {
					return false
				}
				boot = time.Unix(seconds, 0)
				break
			}
		}
	default:
		return false
	}
	started, err := time.Parse(time.RFC3339Nano, owner.Started)
	return err == nil && !boot.IsZero() && started.Before(boot) && !pidExists(owner.PID)
}
