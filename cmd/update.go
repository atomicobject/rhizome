package cmd

import (
	"context"
	"fmt"
	"os"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/spf13/cobra"
)

var (
	updatePinned      bool
	updateLatest      bool
	updateSetVersion  string
	updateManifestURL string
	updateYes         bool
	runUpdate         = appupdate.Run
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the Rhizome binary",
	Long: `Update Rhizome installations from the public GitHub Releases.

When entered through a global rzm inside a pinned repo, plain update refreshes
the global binary and asks before advancing the repo pin. Use --latest to update
both to latest. --pinned and --set-version are repo-only and never change the
global installation. A repo launcher can update only its repo installation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		result, err := runUpdate(ctx, appupdate.Options{
			ManifestURL: updateManifestURL,
			Pinned:      updatePinned,
			Latest:      updateLatest,
			SetVersion:  updateSetVersion,
			Yes:         updateYes,
			Stdin:       cmd.InOrStdin(),
			Stdout:      cmd.OutOrStdout(),
		})
		for _, summary := range result.Summaries() {
			fmt.Fprintln(cmd.OutOrStdout(), summary)
		}
		return err
	},
}

func init() {
	updateCmd.Flags().BoolVar(&updatePinned, "pinned", false, "update only the repo binary to its current rhizome.version")
	updateCmd.Flags().BoolVar(&updateLatest, "latest", false, "update global and repo targets to latest when entered globally")
	updateCmd.Flags().StringVar(&updateSetVersion, "set-version", "", "update only the repo binary and pin to a specific version")
	updateCmd.Flags().StringVar(&updateManifestURL, "manifest-url", os.Getenv("RZM_UPDATE_MANIFEST_URL"), "latest GitHub Release API URL")
	updateCmd.Flags().BoolVar(&updateYes, "yes", false, "answer yes to update prompts")
	rootCmd.AddCommand(updateCmd)
}
