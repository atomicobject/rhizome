package cmd

import (
	"context"
	"errors"
	"fmt"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"os/signal"
	"syscall"

	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/spf13/cobra"
)

// commandInvocation owns one execution. Construct another invocation to run again.
type commandInvocation struct {
	*cobra.Command
	env      commandEnv
	pager    invocationPager
	executed bool
}

func newRootCommand(env commandEnv) *commandInvocation {
	app := &commandInvocation{Command: &cobra.Command{
		Use: "rzm", Short: "rhizome CLI to open, search, move, create, delete and update notes", Version: version.Version,
	}, env: commandEnvFromContext(contextWithCommandEnv(context.Background(), env))}
	rootCmd := app.Command
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == rootCmd {
			fmt.Fprint(cmd.OutOrStdout(), renderBanner())
		}
		defaultHelp(cmd, args)
	})
	rootCmd.PersistentFlags().BoolVar(&app.pager.disabled, "no-pager", false, "disable paging (when stdout is a TTY)")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error { return app.pager.maybeStart(cmd) }
	registerAgentFamily(rootCmd)
	registerCodeFamily(rootCmd)
	registerGraphFamily(rootCmd)
	registerNoteFamily(rootCmd)
	registerIndexFamily(rootCmd)
	registerVaultFamily(rootCmd)
	registerInitFamily(rootCmd)
	registerListFamily(rootCmd)
	registerPromptFamily(rootCmd)
	registerOntologyFamily(rootCmd)
	registerSearchFamily(rootCmd)
	registerServeFamily(rootCmd)
	registerStopFamily(rootCmd)
	registerUpdateFamily(rootCmd)
	registerClaudeFamily(rootCmd)
	registerCodexFamily(rootCmd)
	registerConfigFamily(rootCmd)
	registerCredentialsFamily(rootCmd)
	registerDesktopFamily(rootCmd)
	registerChangelogFamily(rootCmd)
	registerHarnessFamily(rootCmd)
	registerMcpFamily(rootCmd)
	registerDiagnosticsFamily(rootCmd)
	registerRepositoryFamily(rootCmd)
	registerValidationFamily(rootCmd)
	registerQueryRecipeFamily(rootCmd)
	registerViewFamily(rootCmd)
	registerHookFamily(rootCmd)
	return app
}

// Factory Env supplies paths and configuration; execution supplies cancellation.
func (app *commandInvocation) ExecuteContextC(ctx context.Context) (*cobra.Command, error) {
	if app.executed {
		return nil, errors.New("command invocation already executed; construct a fresh command")
	}
	app.executed = true
	defer app.pager.close()
	return app.Command.ExecuteContextC(contextWithCommandEnv(ctx, app.env))
}
func (app *commandInvocation) ExecuteContext(ctx context.Context) error {
	_, err := app.ExecuteContextC(ctx)
	return err
}
func (app *commandInvocation) Execute() error { return app.ExecuteContext(context.Background()) }
func (app *commandInvocation) ExecuteC() (*cobra.Command, error) {
	return app.ExecuteContextC(context.Background())
}

func Execute() int {
	if IsHookInvocation(os.Args[1:]) {
		// Hooks select a repository binary their own way: no prompt, probe,
		// or install, and never a nonzero exit.
		return runHook(os.Args[2:])
	}
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
	ctx = vaultconfig.WithEnv(ctx, vaultconfig.FromContext(ctx))
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

	app := newRootCommand(commandEnvFromContext(ctx))
	rootCmd := app.Command
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
	command, err := app.ExecuteContextC(ctx)
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
