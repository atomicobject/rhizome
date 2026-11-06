//go:build !windows

package proctree

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
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
