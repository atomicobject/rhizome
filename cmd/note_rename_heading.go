package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/spf13/cobra"
)

func (commandOptions *noteInvocationOptions) makeRenameHeadingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename-heading <path> <old-heading> <new-heading>",
		Short: "Rename a heading and rewrite inbound heading-fragment references",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := commandOptions.executeRenameHeading(cmd.Context(), actions.RenameHeadingParams{
				Path:       args[0],
				OldHeading: args[1],
				NewHeading: args[2],
				Apply:      commandOptions.renameHeadingApply,
			})
			if err != nil {
				return err
			}
			if commandOptions.renameHeadingApply {
				return writeRenameHeadingApplyResult(cmd.OutOrStdout(), result)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		},
	}
}

func writeRenameHeadingApplyResult(out io.Writer, result actions.RenameHeadingResult) error {
	status := "Heading unchanged"
	if result.Applied {
		status = "Renamed heading"
	}
	if _, err := fmt.Fprintf(out, "%s in %s; rewrites: %d; skipped: %d\n", status, result.Path, result.Rewritten, len(result.Skipped)); err != nil {
		return err
	}
	if result.Message != "" {
		_, err := fmt.Fprintln(out, result.Message)
		return err
	}
	return nil
}

func (commandOptions *vaultCommandOptions) executeRenameHeading(ctx context.Context, params actions.RenameHeadingParams) (actions.RenameHeadingResult, error) {
	params.Context = ctx
	vaultDef, err := commandOptions.vaultDefOrDefaultContext(ctx)
	if err != nil {
		return actions.RenameHeadingResult{}, err
	}
	params.NoteMetadata, err = newNoteMetadataIndexer()
	if err != nil {
		return actions.RenameHeadingResult{}, err
	}
	return actions.RenameHeading(&fixedVaultDefinition{def: vaultDef}, params)
}

func (commandOptions *noteInvocationOptions) registerNoteRenameHeadingCommands(renameHeadingCmd, noteCmd *cobra.Command) {
	renameHeadingCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name (not required if default is set)")
	renameHeadingCmd.Flags().BoolVar(&commandOptions.renameHeadingApply, "apply", false, "apply the planned heading and inbound-link rewrites")
	noteCmd.AddCommand(renameHeadingCmd)
}
