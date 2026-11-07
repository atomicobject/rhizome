//go:build windows

// Package hiddencommand configures noninteractive commands without changing their lifetime.
package hiddencommand

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Configure prevents a console-subsystem child from opening a console window.
func Configure(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
