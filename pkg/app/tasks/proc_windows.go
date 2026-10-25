//go:build windows

package tasks

import "os/exec"

func configureProcess(*exec.Cmd) {}

// killTree kills the direct child; scheduled tasks are not supported on
// Windows yet (SPEC-0126 Delivery Phases).
func killTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
