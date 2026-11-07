//go:build windows

package hook

import (
	"context"
	"os/exec"
	"strconv"
	"time"

	"github.com/atomicobject/rhizome/pkg/hiddencommand"
)

func configureProcessTree(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	kill := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	hiddencommand.Configure(kill)
	if err := kill.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
