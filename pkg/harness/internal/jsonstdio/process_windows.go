//go:build windows

package jsonstdio

import (
	"os"
	"os/exec"
)

func configureProcessGroup(*exec.Cmd) {}

// ponytail: kills only the direct child on Windows; use taskkill /T as
// pkg/app/cli/hook does if Windows scheduled tasks leave descendants.
func killProcessGroup(process *os.Process) error { return process.Kill() }
