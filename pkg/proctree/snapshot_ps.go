//go:build !darwin && !windows

package proctree

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// snapshot lists every process with its parent, session, and start time. A
// stalled ps gives up after a second rather than holding up a shutdown.
func snapshot() (map[int]proc, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=,lstart=").Output()
	if err != nil {
		return nil, false
	}
	procs := map[int]proc{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		sid := sessionOf(pid)
		procs[pid] = proc{ppid: ppid, sid: sid, start: strings.Join(fields[2:], " ")}
	}
	return procs, true
}
