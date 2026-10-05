package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticsProtocolProcess(t *testing.T) {
	if os.Getenv("RZM_DIAGNOSTIC_PROTOCOL_CHILD") != "1" {
		return
	}
	var args []string
	require.NoError(t, json.Unmarshal([]byte(os.Getenv("RZM_DIAGNOSTIC_PROTOCOL_ARGS")), &args))
	vaultName = os.Getenv("RZM_DIAGNOSTIC_PROTOCOL_VAULT")
	// Global metadata has a selected vault available, but must never resolve it
	// merely to create diagnostic artifacts.
	obsidian.ObsidianConfigFile = func() (string, error) {
		return filepath.Join(os.Getenv("HOME"), "obsidian.json"), nil
	}
	os.Args = append([]string{"rzm"}, args...)
	os.Exit(Execute())
}

func runDiagnosticProcess(t *testing.T, vault string, args ...string) (string, string, error) {
	t.Helper()
	encoded, err := json.Marshal(args)
	require.NoError(t, err)
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "obsidian.json"), []byte(`{"vaults":{}}`), 0600))
	child := exec.Command(os.Args[0], "-test.run=^TestDiagnosticsProtocolProcess$")
	child.Dir = vault
	child.Env = append(os.Environ(),
		"RZM_DIAGNOSTIC_PROTOCOL_CHILD=1", "RZM_DIAGNOSTIC_PROTOCOL_ARGS="+string(encoded),
		"RZM_DIAGNOSTIC_PROTOCOL_VAULT="+vault, "RZM_SKIP_REPO_DELEGATE=1", "HOME="+home,
		"ATOMIC_RHIZOME_KEY=", "OPENAI_API_KEY=", "VOYAGE_API_KEY=",
	)
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	err = child.Run()
	return stdout.String(), stderr.String(), err
}

func TestDiagnosticAgentFailureKeepsExactlyOneJSONError(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	stdout, stderr, err := runDiagnosticProcess(t, vault.path, "agent", "file-context", "missing.md", "--vault", vault.path)
	require.Error(t, err)
	require.Empty(t, stdout)
	decoder := json.NewDecoder(bytes.NewBufferString(stderr))
	var result map[string]any
	require.NoError(t, decoder.Decode(&result), stderr)
	require.NotEmpty(t, result["error"])
	require.ErrorIs(t, decoder.Decode(new(any)), io.EOF, stderr)
	reports, readErr := diagnostics.ReadReports(vault.path, diagnostics.Filter{Kind: "command"})
	require.NoError(t, readErr)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, "error", reports.Reports[0].Status)
	events, readErr := diagnostics.ReadEvents(vault.path, diagnostics.Filter{})
	require.NoError(t, readErr)
	failures := map[string]bool{}
	for _, event := range events.Events {
		if event.Attributes["status"] == "error" {
			failures[event.Attributes["kind"].(string)] = true
			require.Equal(t, "ERROR", event.Level)
		}
	}
	require.True(t, failures["tool"])
	require.True(t, failures["command"])
}

func TestDiagnosticHumanCommandFailureKeepsExistingTerminalError(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	_, stderr, err := runDiagnosticProcess(t, vault.path, "note", "print", "missing-note", "--vault", vault.path)
	require.Error(t, err)
	require.Contains(t, stderr, "Whoops. There was an error")
	require.NotContains(t, stderr, "ERROR [command]")
	reports, readErr := diagnostics.ReadReports(vault.path, diagnostics.Filter{Kind: "command"})
	require.NoError(t, readErr)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, "error", reports.Reports[0].Status)
}

func TestDiagnosticStaticCommandsCreateNoVaultArtifacts(t *testing.T) {
	for _, args := range [][]string{
		{"help"}, {"--help"}, {"--version"}, {"completion", "zsh"},
		{"__complete", "agent", ""}, {"__completeNoDesc", "agent", ""},
		{"vault", "list"}, {"agent", "surface"}, {"agent", "code", "describe", "--operation", "files"},
	} {
		t.Run(args[0]+" "+args[len(args)-1], func(t *testing.T) {
			vault := setupAgentTestVault(t, nil)
			before := diagnosticTree(t, vault.path)
			_, stderr, err := runDiagnosticProcess(t, vault.path, args...)
			require.NoError(t, err, stderr)
			require.Equal(t, before, diagnosticTree(t, vault.path))
		})
	}
}
