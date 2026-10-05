package cmd

import (
	"errors"
	"os"
	"runtime"

	"github.com/atomicobject/rhizome/pkg/app/desktop"
	"github.com/spf13/cobra"
)

var desktopCmd = &cobra.Command{
	Use:   "desktop [path]",
	Short: "Open a folder in the Rhizome desktop app",
	Long: `Open the Rhizome folder containing path (default: the current directory) in
the Rhizome desktop app. The app adds the repository to its library if needed
and switches its most recently focused window to the worktree containing path.

A running app is preferred, then Rhizome, then Rhizome Dev, from /Applications
or ~/Applications. Set RZM_DESKTOP_APP to an app bundle path to choose one.`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS != "darwin" {
			return errors.New("rzm desktop is only available on macOS")
		}
		path := "."
		if len(args) == 1 {
			path = args[0]
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		target, err := desktop.LaunchTarget(cwd, path)
		if err != nil {
			return err
		}
		home, _ := os.UserHomeDir()
		app, err := desktop.FindApp(os.Getenv("RZM_DESKTOP_APP"), desktop.AppCandidates(home), desktop.AppRunning)
		if err != nil {
			return err
		}
		return desktop.Launch(cmd.Context(), app, target)
	},
}

func init() {
	rootCmd.AddCommand(desktopCmd)
}
