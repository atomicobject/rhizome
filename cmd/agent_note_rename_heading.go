package cmd

import (
	"encoding/json"
	"fmt"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/spf13/cobra"
)

func (commandOptions *vaultCommandOptions) newAgentNoteRenameHeadingCmd() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "note-rename-heading <path> <old-heading> <new-heading>",
		Short: "Plan a heading rename and inbound-link rewrite as JSON",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if apply {
				writeAgentError(fmt.Errorf(`{"code":"apply_requires_read_write","message":"agent note-rename-heading is read-only; use write-capable MCP or rzm note rename-heading --apply"}`))
				return silentExitError{code: 1}
			}
			result, err := commandOptions.executeRenameHeading(cmd.Context(), actions.RenameHeadingParams{
				Path:       args[0],
				OldHeading: args[1],
				NewHeading: args[2],
			})
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "refused on the read-only agent CLI")
	return cmd
}
