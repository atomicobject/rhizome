//go:build windows

package credentials

import (
	"context"
	"os/exec"
	"strconv"
	"time"

	"github.com/atomicobject/rhizome/pkg/hiddencommand"
)

func configureImportProcess(command *exec.Cmd) {
	command.Cancel = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		kill := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
		hiddencommand.Configure(kill)
		if err := kill.Run(); err != nil {
			return command.Process.Kill()
		}
		return nil
	}
}
