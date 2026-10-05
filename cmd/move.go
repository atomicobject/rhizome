package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	"github.com/spf13/cobra"
)

var shouldOpen bool
var moveOverwrite bool
var moveUpdateBacklinks bool = true
var moveToFolder string

var moveCmd = &cobra.Command{
	Use:     "move <source> <target> | move --to-folder <folder> <sources...>",
	Aliases: []string{"m"},
	Short:   "Move or rename files (notes/attachments) within the vault; backlinks updated by default",
	Args: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(moveToFolder) != "" {
			if len(args) < 1 {
				return fmt.Errorf("at least one source is required when using --to-folder")
			}
			return nil
		}
		if len(args) != 2 {
			return fmt.Errorf("move requires source and target (or use --to-folder)")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var moves []actions.MoveRequest
		if strings.TrimSpace(moveToFolder) != "" {
			for _, src := range args {
				base := filepath.Base(src)
				moves = append(moves, actions.MoveRequest{
					Source: src,
					Target: filepath.Join(moveToFolder, base),
				})
			}
		} else {
			moves = append(moves, actions.MoveRequest{Source: args[0], Target: args[1]})
		}

		params := actions.MoveParams{
			Moves:           moves,
			Overwrite:       moveOverwrite,
			UpdateBacklinks: moveUpdateBacklinks,
			ShouldOpen:      shouldOpen,
		}

		summary, actionErr := executeMoveNotes(cmd.Context(), params)
		_, outputErr := fmt.Fprint(cmd.OutOrStdout(), renderMoveSummaryText(summary))
		return errors.Join(actionErr, outputErr)
	},
}

func executeMoveNotes(ctx context.Context, params actions.MoveParams) (actions.MoveSummary, error) {
	params.Context = ctx
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return actions.MoveSummary{}, err
	}
	if localCfg, loadErr := obsidian.LoadLocalConfig(vaultDef.BasePath()); loadErr == nil {
		if include, exclude := obsidian.NormalizeCodeRefPatterns(*localCfg); len(include) > 0 {
			params.CodeRefConfig = coderefs.NewConfig(true, include, exclude)
		}
	}
	params.NoteMetadata, err = newNoteMetadataIndexer()
	if err != nil {
		return actions.MoveSummary{}, err
	}
	params.PostApplyRefresher = indexing.ValidationProjectionPostApplyRefresher{
		VaultPath: vaultDef.BasePath(), VaultDef: vaultDef, NoteMetadata: params.NoteMetadata, NoteReader: &obsidian.Note{},
	}
	return actions.MoveNotes(&fixedVaultDefinition{def: vaultDef}, &obsidian.Uri{}, params)
}

func renderMoveSummaryText(summary actions.MoveSummary) string {
	var out strings.Builder
	for _, res := range summary.Results {
		fmt.Fprintf(&out, "Moved %s -> %s (git history preserved: %t, link updates: %d)\n", res.Source, res.Target, res.GitHistoryPreserved, res.LinkUpdates)
	}
	if summary.TotalCodeRefUpdates > 0 {
		fmt.Fprintf(&out, "Code references updated: %d in %d files\n", summary.TotalCodeRefUpdates, summary.CodeFilesUpdated)
	}
	if len(summary.Skipped) > 0 {
		fmt.Fprintf(&out, "Skipped: %s\n", strings.Join(summary.Skipped, ", "))
	}
	if summary.TotalLinkUpdates > 0 && len(summary.Results) > 1 {
		fmt.Fprintf(&out, "Total link updates: %d\n", summary.TotalLinkUpdates)
	}
	for _, pointer := range summary.HeadingPointers {
		fmt.Fprintf(&out, "Heading reference left for review: %s -> %s#%s; run %s\n", pointer.SourceNote, pointer.TargetNote, pointer.Fragment, pointer.Command)
	}
	out.WriteString(renderNamespaceMutationText(summary.Mutation))
	return out.String()
}

func init() {
	moveCmd.Flags().BoolVarP(&shouldOpen, "open", "o", false, "open new note")
	moveCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	moveCmd.Flags().BoolVar(&moveOverwrite, "overwrite", false, "overwrite target if it exists")
	moveCmd.Flags().BoolVar(&moveUpdateBacklinks, "update-backlinks", true, "rewrite backlinks/embeds to point to the moved file (default: true; set --update-backlinks=false to skip)")
	moveCmd.Flags().StringVar(&moveToFolder, "to-folder", "", "move one or more files into the specified folder (preserves filenames)")
	noteCmd.AddCommand(moveCmd)
}
