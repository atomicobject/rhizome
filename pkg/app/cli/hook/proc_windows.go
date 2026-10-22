//go:build windows

package hook

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

func configureProcessTree(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
