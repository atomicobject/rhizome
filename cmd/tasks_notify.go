package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/spf13/cobra"
)

// newTasksNotifyTestCmd sends a sample notification through the person's
// tasks.notify.command (SPEC-0127 US14), so they can check it reaches them.
func newTasksNotifyTestCmd(open func(*cobra.Command) (*tasks.Service, error), asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "notify-test",
		Short: "Send a sample notification through your tasks.notify.command",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			n, command, out, err := svc.NotifyTest(cmd.Context())
			if command == nil {
				return err
			}
			result := struct {
				Command      []string           `json:"command"`
				Notification tasks.Notification `json:"notification"`
				Output       string             `json:"output,omitempty"`
				Error        string             `json:"error,omitempty"`
			}{Command: command, Notification: n, Output: string(out)}
			if err != nil {
				result.Error = err.Error()
			}
			if printErr := printTasks(cmd, *asJSON, result, func(w io.Writer) { renderNotifyTest(w, command, out, err) }); printErr != nil {
				return printErr
			}
			if err != nil {
				cmd.SilenceErrors = true // the output already says what failed
				return silentExitError{code: 1}
			}
			return nil
		},
	}
}

func renderNotifyTest(w io.Writer, command []string, out []byte, err error) {
	if err != nil {
		fmt.Fprintf(w, "The notify command %q failed: %v\n", command, err)
	} else {
		fmt.Fprintf(w, "Sent a test notification through %q.\n", command)
	}
	if text := strings.TrimSpace(string(out)); text != "" {
		fmt.Fprintf(w, "Its output:\n%s\n", text)
	}
}

// newTasksDeliverAlertsCmd sends queued task alerts. Task passes start it
// detached once their work is recorded, so alert delivery never holds them.
func newTasksDeliverAlertsCmd(registry harnessRegistry) *cobra.Command {
	return &cobra.Command{
		Use: "deliver-alerts", Short: "Send queued task alerts through your tasks.notify.command", Args: cobra.NoArgs, Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := openTasksService(cmd.Context(), cmd, registry)
			if err != nil {
				return err
			}
			svc.DeliverAlerts(cmd.Context())
			return nil
		},
	}
}

// startDeliverAlerts starts a detached `rzm tasks deliver-alerts` in dir.
func startDeliverAlerts(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	_, err = appruntime.StartDetachedTo(nil, exe, dir, "tasks", "deliver-alerts")
	return err
}
