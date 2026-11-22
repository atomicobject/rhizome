package cmd

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/spf13/cobra"
)

func newTasksMachineCmd(open func(*cobra.Command) (*tasks.Service, error)) *cobra.Command {
	var checkout string
	cmd := &cobra.Command{
		Use:   "machine on|off",
		Short: "Choose whether this repository's scheduled tasks run on this machine",
		Long: "Scheduled tasks run only on machines where you turn them on, through Rhizome Desktop's menu bar host. " +
			"The setting is yours and this machine's, kept in your user configuration, never in a task file.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			on := args[0] == "on"
			if !on && args[0] != "off" {
				return fmt.Errorf("say on or off, not %q", args[0])
			}
			if checkout != "" && !on {
				return errors.New("--checkout goes with on")
			}
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			if checkout != "" {
				if !filepath.IsAbs(checkout) {
					cwd, err := commandEnvFromContext(cmd.Context()).Getwd()
					if err != nil {
						return err
					}
					checkout = filepath.Join(cwd, checkout)
				}
				if err := svc.SetHostCheckout(cmd.Context(), checkout); err != nil {
					return err
				}
			}
			if err := svc.SetRunHere(cmd.Context(), on); err != nil {
				return err
			}
			if on {
				fmt.Fprintln(cmd.OutOrStdout(), "Scheduled tasks for this repository run on this machine while Rhizome Desktop is running.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Scheduled tasks for this repository no longer run on this machine.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&checkout, "checkout", "", "checkout of this repository that tasks run from (default: the main checkout)")
	return cmd
}

func newTasksHostsCmd(asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "hosts",
		Short: "List the repositories whose scheduled tasks run on this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			hosts, err := tasks.Hosts(commandEnvFromContext(cmd.Context()).Config)
			if err != nil {
				return err
			}
			return printTasks(cmd, *asJSON, hosts, func(w io.Writer) {
				if len(hosts) == 0 {
					fmt.Fprintln(w, "No repository runs its scheduled tasks on this machine. Run `rzm tasks machine on` in one.")
				}
				for _, h := range hosts {
					fmt.Fprintf(w, "%s (%s)\n", h.Checkout, h.CommonDir)
				}
			})
		},
	}
}
