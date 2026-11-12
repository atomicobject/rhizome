package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/shellquote"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/spf13/cobra"
)

var runUpdate = appupdate.Run
var runUpdateInit = (*exec.Cmd).Run

func (commandOptions *updateInvocationOptions) makeUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update the Rhizome binary",
		Long: `Update Rhizome installations from the public GitHub Releases.

When entered through a global rzm inside a pinned repo, plain update refreshes
the global binary and asks before advancing the repo pin. Use --latest to update
both to latest. --pinned and --set-version are repo-only and never change the
global installation. A repo launcher can update only its repo installation.

When the repository's Rhizome version changes, update runs the new executable's
init to refresh generated agent files. With --yes, edited files are kept.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			cwd, err := commandEnvFromContext(ctx).Getwd()
			if err != nil {
				return err
			}
			executable, err := os.Executable()
			if err != nil {
				return err
			}
			selection, err := repoexec.SelectWithEnv(commandEnvFromContext(ctx).Config, cwd, executable)
			if err != nil {
				return err
			}
			result, err := runUpdate(ctx, appupdate.Options{
				WorkDir:     cwd,
				ManifestURL: commandOptions.updateManifestURL,
				Pinned:      commandOptions.updatePinned,
				Latest:      commandOptions.updateLatest,
				SetVersion:  commandOptions.updateSetVersion,
				Yes:         commandOptions.updateYes,
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
			})
			for _, summary := range result.Summaries() {
				fmt.Fprintln(cmd.OutOrStdout(), summary)
			}
			if err != nil {
				return err
			}
			return refreshUpdatedRepository(cmd, selection, result, commandOptions.updateYes)
		},
	}
}

func refreshUpdatedRepository(cmd *cobra.Command, selection repoexec.Plan, result appupdate.Result, yes bool) error {
	if selection.Config == nil || (selection.Authority != "managed" && selection.Authority != "global") {
		return nil
	}
	for _, target := range result.Targets {
		previousVersion := version.Version
		if selection.Authority == "managed" {
			if target.Role != appupdate.UpdateTargetRepo || !target.PinWritten {
				continue
			}
			previousVersion = selection.PinnedVersion
		} else if target.Role != appupdate.UpdateTargetGlobal {
			continue
		}
		if !target.BinaryUpdated || appupdate.NormalizeVersion(target.Version) == appupdate.NormalizeVersion(previousVersion) {
			continue
		}
		retryError := func(err error) error {
			return fmt.Errorf("Rhizome was updated, but refreshing repository files failed: %w\nRetry with: cd %s && rzm init", err, shellquote.Quote(selection.ConfigDir))
		}
		executable := target.Path
		if target.Role == appupdate.UpdateTargetGlobal {
			bundle, err := appbundle.Resolve(selection.Target)
			if err != nil {
				return retryError(err)
			}
			if bundle != nil {
				executable = bundle.ExecutablePath
				if bundle.Portable {
					current, err := appbundle.Read(bundle.Root)
					if err != nil {
						return retryError(err)
					}
					executable = current.ExecutablePath
				}
			}
		}
		child := exec.CommandContext(cmd.Context(), executable, "init", "--path", selection.ConfigDir)
		child.Dir = selection.ConfigDir
		child.Env = append(os.Environ(), repoDelegatedEnv+"=1", repoSkipDelegateEnv+"=1")
		child.Stdout, child.Stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
		if !yes {
			child.Stdin = cmd.InOrStdin()
		}
		if err := runUpdateInit(child); err != nil {
			return retryError(err)
		}
		return nil
	}
	return nil
}

func (commandOptions *updateInvocationOptions) registerUpdateCommands(updateCmd, rootCmd *cobra.Command) {
	updateCmd.Flags().BoolVar(&commandOptions.updatePinned, "pinned", false, "update only the repo binary to its current rhizome.version")
	updateCmd.Flags().BoolVar(&commandOptions.updateLatest, "latest", false, "update global and repo targets to latest when entered globally")
	updateCmd.Flags().StringVar(&commandOptions.updateSetVersion, "set-version", "", "update only the repo binary and pin to a specific version")
	updateCmd.Flags().StringVar(&commandOptions.updateManifestURL, "manifest-url", os.Getenv("RZM_UPDATE_MANIFEST_URL"), "latest GitHub Release API URL")
	updateCmd.Flags().BoolVar(&commandOptions.updateYes, "yes", false, "answer yes to update prompts")
	rootCmd.AddCommand(updateCmd)
}
