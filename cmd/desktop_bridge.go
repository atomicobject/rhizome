package cmd

import (
	"encoding/json"
	"os"
	"os/signal"

	"github.com/atomicobject/rhizome/pkg/app/desktop"
	"github.com/spf13/cobra"
)

func newDesktopBridgeCmd() *cobra.Command {
	var stateDir string
	command := &cobra.Command{
		Use:           "desktop-bridge",
		Hidden:        true,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := desktop.New(stateDir, commandEnvFromContext(cmd.Context()).Config)
			if err != nil {
				if writeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(desktop.Response{Protocol: desktop.Protocol, Error: &desktop.Problem{Code: "invalid_request", Message: err.Error()}}); writeErr != nil {
					return writeErr
				}
				return silentExitError{code: 1}
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer cancel()
			if err := desktop.Serve(ctx, service, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
				return silentExitError{code: 1}
			}
			return nil
		},
	}
	command.Flags().StringVar(&stateDir, "state-dir", "", "absolute desktop application data directory")
	return command
}
