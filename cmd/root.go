package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "rzm",
	Short:   "rhizome CLI to open, search, move, create, delete and update notes",
	Version: version.Version,
}

func init() {
	// Custom help function that shows banner only for root command
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == rootCmd {
			fmt.Fprint(cmd.OutOrStdout(), renderBanner())
		}
		defaultHelp(cmd, args)
	})

	rootCmd.PersistentFlags().BoolVar(&noPager, "no-pager", false, "disable paging (when stdout is a TTY)")
	rootCmd.AddCommand(mcpCmd)

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return maybeStartPager(cmd)
	}
}

func Execute() int {
	defer func() { _ = closePager() }()

	decision := maybeDelegateToRepoBinary(os.Args[1:])
	if shouldPrintRepoDelegateWarning(os.Args[1:], decision.Warning) {
		fmt.Fprintf(os.Stderr, "rzm: warning: %s\n", decision.Warning)
	}
	switch {
	case decision.Err != nil:
		fmt.Fprintf(os.Stderr, "Whoops. There was an error while preparing repo Rhizome binary: %v\n", decision.Err)
		return 1
	case decision.Bootstrap:
		fmt.Fprintf(os.Stderr, "rzm: installing Rhizome %s for this repo (%s)…\n", decision.PinnedVersion, decision.Reason)
		if err := bootstrapRepoBinary(decision); err != nil {
			fmt.Fprintf(os.Stderr, "rzm: installing Rhizome %s failed: %v\nRun `rzm update --pinned` to retry the install.\n", decision.PinnedVersion, err)
			return 1
		}
		return executeRepoBinary(decision.Target, os.Args[1:])
	case decision.Delegate:
		return executeRepoBinary(decision.Target, os.Args[1:])
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		cancel()
		<-sigCh
		os.Exit(130)
	}()

	previousPreRun := rootCmd.PersistentPreRunE
	diagnosticsScope := &commandDiagnostics{}
	restoreArguments := observeDiagnosticArguments(rootCmd, diagnosticsScope)
	defer restoreArguments()
	rootCmd.PersistentPreRunE = func(command *cobra.Command, args []string) error {
		diagnosticsScope.start(command)
		if previousPreRun != nil {
			return previousPreRun(command, args)
		}
		return nil
	}
	defer func() { rootCmd.PersistentPreRunE = previousPreRun }()
	defer func() {
		if panicked := recover(); panicked != nil {
			diagnosticsScope.finish(nil, errors.New("diagnostic boundary panicked"))
			panic(panicked)
		}
	}()
	command, err := rootCmd.ExecuteContextC(ctx)
	diagnosticsScope.finish(command, err)
	if err != nil {
		if coded, ok := err.(interface{ ExitCode() int }); ok {
			return coded.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "Whoops. There was an error while executing your CLI '%s'\n", err)
		return 1
	}
	return 0
}

func shouldPrintRepoDelegateWarning(args []string, warning string) bool {
	return warning != "" && firstCommandArg(args) != "agent"
}
