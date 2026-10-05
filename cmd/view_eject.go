package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newViewEjectCmd(agent bool, opts *viewOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eject <id>",
		Short: "Copy a bundled view's folder into .rhizome/views to customize it",
		Long: "Copy the folder of a view that ships with rzm into .rhizome/views/<folder>/. " +
			"The copy then replaces the bundled views it shares ids with. Existing files are never overwritten.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, cleanup, err := buildViewServiceForCommand(cmd, opts)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			result, err := service.Eject(cmd.Context(), args[0])
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			if useViewJSON(agent, opts.jsonOutput) {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			for _, written := range result.Written {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", written)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s now resolves to %s\n", result.ID, result.Folder)
			return err
		},
	}
	addViewJSONFlag(cmd, agent, opts)
	return cmd
}
