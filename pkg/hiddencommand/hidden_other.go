//go:build !windows

// Package hiddencommand configures noninteractive commands without changing their lifetime.
package hiddencommand

import "os/exec"

// Configure leaves command behavior unchanged outside Windows.
func Configure(*exec.Cmd) {}
