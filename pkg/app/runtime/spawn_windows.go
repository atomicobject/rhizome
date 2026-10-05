//go:build windows

package runtime

import (
	"os/exec"
	"syscall"
)

// Windows creation flags for a fully detached background process. syscall
// exports CREATE_NEW_PROCESS_GROUP only, so the other two are spelled out.
const (
	detachedProcess = 0x00000008
	createNoWindow  = 0x08000000
)

// detachProcess gives the child no console, no window, and its own process
// group so a Ctrl-C in the client's console never reaches the runtime.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow,
	}
}
