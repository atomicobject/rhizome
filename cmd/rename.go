package cmd

import (
	"errors"
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *noteInvocationOptions) makeRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <source> <target>",
		Short: "Rename a file (note or attachment) and update backlinks, preserving history in git vaults",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault := repoconfig.Vault{Env: commandEnvFromContext(cmd.Context()).Config, Name: commandOptions.vaultName}
			vaultPath, err := vault.Path()
			if err != nil {
				return err
			}

			indexer, err := newNoteMetadataIndexer()
			if err != nil {
				return err
			}
			params := actions.RenameParams{
				Context:         cmd.Context(),
				NoteMetadata:    indexer,
				Source:          args[0],
				Target:          args[1],
				Overwrite:       commandOptions.renameOverwrite,
				UpdateBacklinks: !commandOptions.renameNoBacklinks,
			}
			definition, err := vault.Definition()
			if err != nil {
				return err
			}
			params.PostApplyRefresher = validationproduct.ValidationProjectionPostApplyRefresher{
				VaultPath: vaultPath, VaultDef: definition, NoteMetadata: indexer, NoteReader: &obsidian.Note{},
			}

			// Load code ref config
			if localCfg, err := repoconfig.LoadLocalConfig(vaultPath); err == nil {
				if inc, exc := repoconfig.NormalizeCodeRefPatterns(*localCfg); len(inc) > 0 {
					params.CodeRefConfig = coderefs.NewConfig(true, inc, exc)
				}
			}

			result, actionErr := actions.RenameNote(&vault, params)
			_, outputErr := fmt.Fprint(cmd.OutOrStdout(), renderRenameResultText(result))
			return errors.Join(actionErr, outputErr)
		},
	}
}

func renderRenameResultText(result actions.RenameResult) string {
	var out strings.Builder
	if result.RenamedPath != "" {
		fmt.Fprintf(&out, "Renamed to %s; link updates: %d; git history preserved: %t\n", result.RenamedPath, result.LinkUpdates, result.GitHistoryPreserved)
	}
	if result.CodeRefUpdates > 0 {
		fmt.Fprintf(&out, "Code references updated: %d in %d files\n", result.CodeRefUpdates, result.CodeFilesUpdated)
	}
	if len(result.Skipped) > 0 {
		fmt.Fprintf(&out, "Skipped: %s\n", strings.Join(result.Skipped, ", "))
	}
	for _, pointer := range result.HeadingPointers {
		fmt.Fprintf(&out, "Heading reference left for review: %s -> %s#%s; run %s\n", pointer.SourceNote, pointer.TargetNote, pointer.Fragment, pointer.Command)
	}
	out.WriteString(renderNamespaceMutationText(result.Mutation))
	return out.String()
}

func (commandOptions *noteInvocationOptions) registerRenameCommands(renameCmd, noteCmd *cobra.Command) {
	renameCmd.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name (not required if default is set)")
	renameCmd.Flags().BoolVar(&commandOptions.renameOverwrite, "overwrite", false, "overwrite target if it exists")
	renameCmd.Flags().BoolVar(&commandOptions.renameNoBacklinks, "no-backlinks", false, "skip rewriting backlinks to the renamed file")
	noteCmd.AddCommand(renameCmd)
}
