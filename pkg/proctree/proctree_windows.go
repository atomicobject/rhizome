//go:build windows

package proctree

import (
	"os"
	"os/exec"
)

const supported = false

// Configure does nothing on Windows.
func Configure(*exec.Cmd) {}

func snapshot() (map[int]proc, bool) { return nil, false }

func killPID(int) {}

// ponytail: kills only the direct child on Windows; use taskkill /T as
// pkg/app/cli/hook does if Windows needs tree cleanup.
func killGroup(process *os.Process) error { return process.Kill() }
