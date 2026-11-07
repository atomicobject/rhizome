//go:build windows

package agentcode

import (
	"context"
	"os/exec"
	"strconv"
	"time"

	"github.com/atomicobject/rhizome/pkg/hiddencommand"
)

func configureExecuteCommand(command *exec.Cmd) {}

func terminateExecuteProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	kill := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
	hiddencommand.Configure(kill)
	if err := kill.Run(); err != nil {
		_ = command.Process.Kill()
		return err
	}
	return nil
}
