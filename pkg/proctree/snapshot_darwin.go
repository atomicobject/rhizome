package proctree

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Read the kernel table directly: spawning ps can consume the shutdown budget.
func snapshot() (map[int]proc, bool) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, false
	}
	procs := make(map[int]proc, len(processes))
	for _, p := range processes {
		pid := int(p.Proc.P_pid)
		procs[pid] = proc{
			ppid:  int(p.Eproc.Ppid),
			sid:   sessionOf(pid),
			start: fmt.Sprintf("%d.%06d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec),
		}
	}
	return procs, true
}
