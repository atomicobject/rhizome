package cmd

import (
	"fmt"
	"io"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/spf13/cobra"
)

// newTasksStopCmd stops the run holding the repository's run lock
// (SPEC-0104 US7), the way Quit Rhizome Completely does.
func newTasksStopCmd(open func(*cobra.Command) (*tasks.Service, error), asJSON *bool) *cobra.Command {
	var wait string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the task run in progress and record it as stopped",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := tasks.ParseDuration(wait)
			if err != nil {
				return fmt.Errorf("--wait: %w", err)
			}
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			stopped, err := svc.Stop(cmd.Context(), d)
			if err != nil {
				return err
			}
			return printTasks(cmd, *asJSON, map[string]bool{"stopped": stopped}, func(w io.Writer) {
				if stopped {
					fmt.Fprintln(w, "Stopped the task run.")
					return
				}
				fmt.Fprintln(w, "No task run is in progress.")
			})
		},
	}
	cmd.Flags().StringVar(&wait, "wait", tasks.DefaultStopWait.String(), "how long the run may take to exit before its processes are killed")
	return cmd
}
