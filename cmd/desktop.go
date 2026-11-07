package cmd

import (
	"github.com/atomicobject/rhizome/pkg/app/desktop"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/spf13/cobra"
)

// IsDesktopInvocation keeps app discovery independent of repository dotenv.
func IsDesktopInvocation(args []string) bool {
	command := firstCommandArg(args)
	return command == "desktop" || command == "desktop-bridge"
}

func (commandOptions *vaultCommandOptions) makeDesktopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "desktop [path]",
		Short: "Open a folder in the Rhizome desktop app",
		Long: `Open the Rhizome folder containing path (default: the current directory) in
the Rhizome desktop app. The app adds the repository to its library if needed
and switches its most recently focused window to the worktree containing path.

A running app is preferred on macOS. On Windows and Linux the registered
installation is used. Set RZM_DESKTOP_APP to an installed app path to choose one.`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			env := commandEnvFromContext(cmd.Context())
			cwd, err := env.Getwd()
			if err != nil {
				return err
			}
			target, err := desktop.LaunchTarget(cwd, path)
			if err != nil {
				return err
			}
			override, _ := env.Config.Lookup("RZM_DESKTOP_APP")
			if override != "" {
				override = paths.AbsFromInput(cwd, override).String()
			}
			app, err := desktop.FindApp(override, desktop.AppCandidates(env.Config.HomeDir), desktop.AppRunning)
			if err != nil {
				return err
			}
			return desktop.Launch(cmd.Context(), app, target)
		},
	}
}

func (commandOptions *vaultCommandOptions) registerDesktopCommands(rootCmd, desktopCmd *cobra.Command) {
	rootCmd.AddCommand(desktopCmd)
}
