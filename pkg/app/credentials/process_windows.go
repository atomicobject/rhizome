//go:build windows

package credentials

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

func configureImportProcess(command *exec.Cmd) {
	command.Cancel = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run(); err != nil {
			return command.Process.Kill()
		}
		return nil
	}
}
