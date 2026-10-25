//go:build windows

package jsonstdio

import (
	"os"
	"os/exec"
)

func configureProcessGroup(*exec.Cmd) {}

func processTree(*os.Process) []int { return nil }

// ponytail: kills only the direct child on Windows; use taskkill /T as
// pkg/app/cli/hook does if Windows scheduled tasks leave descendants.
func killProcessTree(process *os.Process, _ []int) error { return process.Kill() }
