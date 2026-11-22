package cmd

import (
	"fmt"
	"io"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/spf13/cobra"
)

// newTasksDoctorCmd reports what tasks need on this machine (SPEC-0127
// US11). It exits 1 when any check fails.
func newTasksDoctorCmd(open func(*cobra.Command) (*tasks.Service, error), asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [task]",
		Short: "Check what tasks need on this machine: commands, harness sign-in, MCP servers, trust, fetch, push, Obsidian Git",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			opts := tasks.DoctorOptions{}
			if len(args) == 1 {
				opts.Task = args[0]
			}
			machine, err := svc.MachineSettings()
			if err != nil {
				return err
			}
			opts.Push, opts.Checkout = machine.Repository.Push, machine.Repository.Checkout
			report, err := svc.Doctor(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if err := printTasks(cmd, *asJSON, report, func(w io.Writer) { renderDoctor(w, report) }); err != nil {
				return err
			}
			if !report.OK {
				cmd.SilenceErrors = true // the report already says what failed
				return silentExitError{code: 1}
			}
			return nil
		},
	}
}

func renderDoctor(w io.Writer, r tasks.DoctorReport) {
	fmt.Fprintln(w, "Repository")
	renderChecks(w, r.Checks)
	for _, t := range r.Tasks {
		fmt.Fprintf(w, "\n%s (%s)\n", t.Name, t.ID)
		if len(t.Checks) == 0 {
			fmt.Fprintln(w, "  nothing to check")
		}
		renderChecks(w, t.Checks)
	}
	if r.OK {
		fmt.Fprintln(w, "\nEverything checked is ready.")
	} else {
		fmt.Fprintln(w, "\nSome checks failed.")
	}
}

func renderChecks(w io.Writer, checks []tasks.DoctorCheck) {
	for _, c := range checks {
		mark := "ok  "
		if !c.OK {
			mark = "FAIL"
		}
		fmt.Fprintf(w, "  %s %s: %s\n", mark, c.Check, c.Detail)
	}
}
