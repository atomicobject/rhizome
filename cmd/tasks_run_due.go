package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/spf13/cobra"
)

func newTasksRunDueCmd(open func(*cobra.Command) (*tasks.Service, error), asJSON *bool) *cobra.Command {
	var host bool
	cmd := &cobra.Command{
		Use: "run-due", Short: "Run every task that is due, once per repository", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			run := svc.RunDue
			if host {
				run = svc.RunDueHost
			}
			summary, err := run(cmd.Context())
			if err != nil {
				return err
			}
			return printTasks(cmd, *asJSON, summary, func(w io.Writer) { renderRunSummary(w, summary) })
		},
	}
	cmd.Flags().BoolVar(&host, "host", false, "run as this machine's task host: nothing runs unless `rzm tasks machine on` is set for the repository")
	return cmd
}

func newTasksPauseCmd(open func(*cobra.Command) (*tasks.Service, error)) *cobra.Command {
	var until, span string
	cmd := &cobra.Command{
		Use: "pause", Short: "Stop all background task work for you here, until resumed or for a while", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if until != "" && span != "" {
				return errors.New("give --until or --for, not both")
			}
			var end time.Time
			switch {
			case until != "":
				t, err := time.Parse(time.RFC3339, until)
				if err != nil {
					return fmt.Errorf("--until must be an RFC 3339 time like 2026-11-23T09:00:00-05:00: %w", err)
				}
				end = t
			case span != "":
				d, err := tasks.ParseDuration(span)
				if err != nil {
					return fmt.Errorf("--for: %w", err)
				}
				end = time.Now().Add(d)
			}
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			if end.IsZero() {
				return svc.SetPaused(cmd.Context(), true)
			}
			if err := svc.PauseUntil(cmd.Context(), end); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Paused until %s.\n", end.Local().Format("Mon Jan 2 15:04"))
			return nil
		},
	}
	cmd.Flags().StringVar(&until, "until", "", "resume on its own at this RFC 3339 time")
	cmd.Flags().StringVar(&span, "for", "", "resume on its own after this long (1h, 1d)")
	return cmd
}

// startTasksRunDue is how a runtime asks for due tasks: a detached
// `rzm tasks run-due --host` in the vault, so the work outlives the
// runtime. Its output goes to task state's run-due.log.
func startTasksRunDue(vaultPath string) (*appruntime.Detached, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{"tasks", "run-due", "--host"}
	log, err := tasks.OpenRunDueLog(context.Background(), vaultPath)
	if err != nil {
		// ponytail: an unwritable log must not stop scheduled work.
		return appruntime.StartDetachedTo(nil, exe, vaultPath, args...)
	}
	defer log.Close()
	fmt.Fprintf(log, "\n[%s] rzm %s\n", time.Now().Format(time.RFC3339), "tasks run-due --host")
	return appruntime.StartDetachedTo(log, exe, vaultPath, args...)
}
