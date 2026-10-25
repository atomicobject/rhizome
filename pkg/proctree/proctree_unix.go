//go:build !windows

package proctree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const supported = true

// Configure puts cmd in its own process group, so Kill can reach the whole
// group even after its leader exits.
func Configure(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// snapshot lists every process with its parent and start time. A stalled
// ps gives up after a second rather than holding up a shutdown.
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
		if err1 == nil && err2 == nil {
			procs[pid] = proc{ppid: ppid, start: strings.Join(fields[2:], " ")}
		}
	}
	return procs, true
}

func killPID(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }

func killGroup(process *os.Process) error {
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
