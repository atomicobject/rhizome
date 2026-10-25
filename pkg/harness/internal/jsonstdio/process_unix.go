//go:build !windows

package jsonstdio

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Harness CLIs spawn shells, tools, and MCP servers; a process group lets
// Close terminate all of them, as hook handlers and agent code execution do.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup also works after the leader has been reaped, while any
// descendant still holds the group.
func killProcessGroup(process *os.Process) error {
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
