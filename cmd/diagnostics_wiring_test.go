package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticFailedCommandRestoresLoggerAndOwnTerminalIdentity(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	vaultName = vault.name
	writer, flags := log.Writer(), log.Flags()
	var stdout, stderr bytes.Buffer
	command := &cobra.Command{Use: "test-failure", Run: func(*cobra.Command, []string) {}}
	command.SetContext(context.Background())
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	scope := &commandDiagnostics{}
	scope.start(command)
	require.NotNil(t, scope.recorder)
	child := diagnostics.NewOperation("index", "manual")
	command.SetContext(diagnostics.WithOperation(command.Context(), child))
	log.Print("live: ready query=PRIVATE_QUERY")
	scope.finish(command, errors.New("permission denied PRIVATE_PROVIDER_BODY"))
	require.False(t, logging.StandardInstalled())
	require.Equal(t, writer, log.Writer())
	require.Equal(t, flags, log.Flags())
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String(), "command failures already have terminal output owned by Cobra")
	reports, err := diagnostics.ReadReports(vault.path, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, "error", reports.Reports[0].Status)
	events, err := diagnostics.ReadEvents(vault.path, diagnostics.Filter{})
	require.NoError(t, err)
	require.Equal(t, scope.operation.ID, events.Events[len(events.Events)-1].OperationID)
}

func TestDiagnosticLegacyPrintErrorReturnsForCleanup(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	vaultName = vault.name
	var stderr bytes.Buffer
	printCmd.SetContext(context.Background())
	originalError := printCmd.ErrOrStderr()
	printCmd.SetErr(&stderr)
	t.Cleanup(func() { printCmd.SetErr(originalError) })
	scope := &commandDiagnostics{}
	scope.start(printCmd)
	err := printCmd.RunE(printCmd, []string{"missing-note"})
	require.Error(t, err, "legacy print must return its actionable error instead of exiting the process")
	scope.finish(printCmd, err)
	require.False(t, logging.StandardInstalled())
	reports, readErr := diagnostics.ReadReports(vault.path, diagnostics.Filter{})
	require.NoError(t, readErr)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, "error", reports.Reports[0].Status)
}

func TestDiagnosticCommandStorageFailureKeepsWarningsAndOutcome(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	vaultName = vault.name
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "diagnostics"), []byte("synthetic obstruction"), 0600))
	var stderr bytes.Buffer
	command := &cobra.Command{Use: "test-storage-failure", Run: func(*cobra.Command, []string) {}}
	command.SetContext(context.Background())
	command.SetErr(&stderr)
	scope := &commandDiagnostics{}
	scope.start(command)
	require.NotNil(t, scope.recorder, "advisory recorder must preserve console warnings when storage fails")
	log.Print("Warning: permission denied PRIVATE_BODY")
	scope.finish(command, fmt.Errorf("wrapped: %w", context.Canceled))
	require.Contains(t, stderr.String(), "WARN")
	require.Contains(t, stderr.String(), "permission_denied")
	require.NotContains(t, stderr.String(), "PRIVATE")
	require.False(t, logging.StandardInstalled())
}

func TestDiagnosticAgentMetadataIsInert(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	vaultName = vault.name
	agent := &cobra.Command{Use: "agent"}
	code := &cobra.Command{Use: "code"}
	agent.AddCommand(code)
	surface := &cobra.Command{Use: "surface"}
	agent.AddCommand(surface)
	commands := []*cobra.Command{surface}
	for _, name := range []string{"surface", "describe", "generate"} {
		command := &cobra.Command{Use: name}
		code.AddCommand(command)
		commands = append(commands, command)
	}
	for _, command := range commands {
		scope := &commandDiagnostics{}
		scope.start(command)
		scope.finish(command, nil)
		require.Nil(t, scope.recorder)
	}
	require.NoDirExists(t, filepath.Join(vault.path, ".rhizome", "diagnostics"))
}

func TestDiagnosticSilentFailurePersistsAndFinalizesOnce(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	vaultName = vault.name
	var stderr bytes.Buffer
	command := &cobra.Command{Use: "execute", Run: func(*cobra.Command, []string) {}}
	command.SetContext(context.Background())
	command.SetErr(&stderr)
	scope := &commandDiagnostics{}
	scope.start(command)
	finalizer := diagnosticCommandFinalizer(command.Context())
	require.NotNil(t, finalizer)
	finalizer(fmt.Errorf("wrapped: %w", silentExitError{code: 1}))
	require.Empty(t, stderr.String(), "JSON error envelopes must stay quiet")
	scope.finish(command, errors.New("later finalization must be ignored"))
	require.Empty(t, stderr.String())
	require.False(t, logging.StandardInstalled())
	reports, err := diagnostics.ReadReports(vault.path, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, "error", reports.Reports[0].Status)
	events, err := diagnostics.ReadEvents(vault.path, diagnostics.Filter{})
	require.NoError(t, err)
	require.Equal(t, "ERROR", events.Events[len(events.Events)-1].Level)
}

func TestDiagnosticServeAlreadyOwnedIsSkipped(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	vaultName = vault.name
	var stderr bytes.Buffer
	previousError := serveCmd.ErrOrStderr()
	serveCmd.SetErr(&stderr)
	serveCmd.SetContext(context.Background())
	t.Cleanup(func() { serveCmd.SetErr(previousError) })
	scope := &commandDiagnostics{}
	scope.start(serveCmd)
	scope.finish(serveCmd, silentExitError{code: appruntime.ExitCodeAlreadyRunning})
	require.Empty(t, stderr.String())
	reports, err := diagnostics.ReadReports(vault.path, diagnostics.Filter{Kind: "command"})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, "skipped", reports.Reports[0].Status)
	require.Equal(t, "runtime_already_owned", reports.Reports[0].ReasonCode)
}

func TestDiagnosticDesktopHandoffDoesNotTouchSelectedVault(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	vaultName = vault.name
	command := &cobra.Command{Use: "desktop", Run: func(*cobra.Command, []string) {}}
	command.SetContext(context.Background())
	before := diagnosticTree(t, vault.path)
	scope := &commandDiagnostics{}
	scope.start(command)
	scope.finish(command, nil)
	require.Nil(t, scope.recorder)
	require.Equal(t, before, diagnosticTree(t, vault.path))
}
