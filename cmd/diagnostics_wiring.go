package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	appserve "github.com/atomicobject/rhizome/pkg/app/cli/serve"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/spf13/cobra"
)

type commandDiagnostics struct {
	recorder        *diagnostics.Recorder
	restore         func()
	operation       diagnostics.Operation
	command         *cobra.Command
	previousContext context.Context
	finished        bool
}

type commandDiagnosticsFinalizerKey struct{}

func diagnosticCommandFinalizer(ctx context.Context) func(error) {
	finalizer, _ := ctx.Value(commandDiagnosticsFinalizerKey{}).(func(error))
	return finalizer
}

// Metadata discovery is deliberately inert, including diagnostics persistence.
// Explicit generate owns only the task artifacts requested by the caller.
func isInertAgentMetadata(command *cobra.Command) bool {
	parent := command.Parent()
	if parent == nil {
		return false
	}
	if parent.Name() == "agent" && command.Name() == "surface" {
		return true
	}
	if parent.Name() != "code" || parent.Parent() == nil || parent.Parent().Name() != "agent" {
		return false
	}
	switch command.Name() {
	case "surface", "describe", "generate":
		return true
	}
	return false
}

// Static discovery and global metadata must not resolve a default vault merely
// to open a diagnostic sink in it.
func isInertCommand(command *cobra.Command) bool {
	if command.Run == nil && command.RunE == nil {
		return true
	}
	for current := command; current != nil; current = current.Parent() {
		switch current.Name() {
		case "help", "version", "completion", "__complete", "__completeNoDesc", "desktop":
			return true
		}
	}
	if command.Parent() != nil && command.Parent().Name() == "vault" {
		switch command.Name() {
		case "list", "print-default":
			return true
		}
	}
	return false
}

func isDiagnosticsReader(command *cobra.Command) bool {
	for command != nil {
		if command.Name() == "diagnostics" {
			return true
		}
		command = command.Parent()
	}
	return false
}

// start happens after Cobra parsed flags. Failed argument validation can start
// the same boundary during finalization, without executing runtime work.
func (d *commandDiagnostics) start(command *cobra.Command) {
	if command == nil || d.finished || d.recorder != nil || isDiagnosticsReader(command) || isInertAgentMetadata(command) || isInertCommand(command) {
		return
	}
	for _, name := range []string{"help", "version"} {
		if flag := command.Flags().Lookup(name); flag != nil && flag.Value.String() == "true" {
			return
		}
	}
	def, err := vaultDefOrDefaultContext(command.Context())
	if err != nil {
		return
	}
	options, err := diagnostics.LoadOptions(def.BasePath())
	if err != nil {
		fmt.Fprintf(command.ErrOrStderr(), "WARN [diagnostics] configuration unavailable (%s); using bounded defaults\n", logging.ClassifyError(err))
	}
	options.Role = "cli"
	if command.Parent() == rootCmd && (command.Name() == "serve" || command.Name() == "start") {
		options.Role = "runtime"
	}
	options.Version = version.Version
	options.Stderr = command.ErrOrStderr()
	recorder, err := diagnostics.Open(def.BasePath(), options)
	if err != nil {
		fmt.Fprintf(command.ErrOrStderr(), "WARN [diagnostics] persistence unavailable (%s)\n", logging.ClassifyError(err))
	}
	if recorder == nil {
		return
	}
	d.command = command
	d.recorder = recorder
	d.operation = diagnostics.NewOperation("command", command.CommandPath())
	ctx := diagnostics.WithRecorder(command.Context(), recorder)
	ctx = diagnostics.WithOperation(ctx, d.operation)
	ctx = context.WithValue(ctx, commandDiagnosticsFinalizerKey{}, func(err error) { d.finish(command, err) })
	d.previousContext = command.Context()
	command.SetContext(ctx)
	d.restore = logging.InstallStandard(ctx)
	recorder.Event(ctx, slog.LevelInfo, "command", "command.started", "", slog.String("command", command.CommandPath()))
}

func (d *commandDiagnostics) finish(command *cobra.Command, err error) {
	if d.finished {
		return
	}
	if command == nil {
		command = d.command
	}
	d.start(command)
	if d.recorder == nil {
		return
	}
	d.finished = true
	status, reason := "success", ""
	if err != nil {
		status = "error"
		reason = logging.ClassifyError(err)
		if errors.Is(err, context.Canceled) {
			status = "canceled"
		}
	}
	var coded interface{ ExitCode() int }
	alreadyRunning := errors.Is(err, appserve.ErrAlreadyRunning)
	if command.Parent() == rootCmd && command.Name() == "serve" && errors.As(err, &coded) && coded.ExitCode() == appruntime.ExitCodeAlreadyRunning {
		alreadyRunning = true
	}
	if alreadyRunning {
		status, reason = "skipped", "runtime_already_owned"
	}
	// Cobra or the protocol owns error output. Keep the retained failure without
	// adding a second diagnostic error line to the existing terminal contract.
	logging.CompleteQuiet(diagnostics.WithOperation(command.Context(), d.operation), d.operation, status, reason, map[string]any{"command": command.CommandPath()})

	d.restore()
	_ = d.recorder.Close()
	command.SetContext(d.previousContext)
}

// Cobra runs Args before pre-run. Wrap those validators so a rejected invocation
// still has a started boundary and all command-scoped cleanup runs in Execute.
func observeDiagnosticArguments(root *cobra.Command, scope *commandDiagnostics) func() {
	previous := map[*cobra.Command]cobra.PositionalArgs{}
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		if command.Args != nil {
			original := command.Args
			previous[command] = original
			command.Args = func(cmd *cobra.Command, args []string) error { scope.start(cmd); return original(cmd, args) }
		}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(root)
	return func() {
		for command, args := range previous {
			command.Args = args
		}
	}
}
