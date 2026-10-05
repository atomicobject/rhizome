//go:build !windows

package runtime

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the child in its own session so a Ctrl-C in the client's
// terminal never reaches the runtime and the runtime outlives the client shell.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
