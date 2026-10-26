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

// Configure makes cmd a session leader, which also gives it its own process
// group. The child has no controlling terminal, which suits children
// driven over pipes.
func Configure(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

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

func killPID(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }

func killGroup(process *os.Process) error {
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	// macOS answers EPERM for a group whose only member is an unreaped
	// zombie; the group is ours, so that means it is gone.
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM) {
		return os.ErrProcessDone
	}
	return err
}
