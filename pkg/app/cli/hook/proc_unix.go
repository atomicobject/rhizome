//go:build !windows

package hook

import (
	"os/exec"
	"syscall"
)

// Handlers run in their own process group so a timeout kills everything
// they started, as agent code execution does.
func configureProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
