package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func setupNamespaceCommandVault(t *testing.T, files map[string]string) agentTestVault {
	t.Helper()
	vault := setupAgentTestVault(t, files)
	t.Chdir(vault.path)
	preferences := t.TempDir()
	originalPreferences, originalHome := obsidian.CliConfigPath, config.UserHomeDirectory
	obsidian.CliConfigPath = func() (string, string, error) {
		return preferences, filepath.Join(preferences, "config.yml"), nil
	}
	config.UserHomeDirectory = func() (string, error) { return preferences, nil }
	t.Cleanup(func() {
		obsidian.CliConfigPath, config.UserHomeDirectory = originalPreferences, originalHome
		renameCmd.SetOut(nil)
		renameCmd.SetErr(nil)
		moveCmd.SetOut(nil)
		moveCmd.SetErr(nil)
		resetCommandContexts(rootCmd)
	})
	return vault
}

func TestNoteNamespaceCommandsPublishAndReportRequiredLinks(t *testing.T) {
	for _, command := range []string{"rename", "move"} {
		t.Run(command, func(t *testing.T) {
			vault := setupNamespaceCommandVault(t, map[string]string{
				"Old.md": "Link [[Old]]", "Ref.md": "Ref [[Old|Alias]]", "refs.go": "package fixture\n// [[Old]]\n",
				".rhizome/config.yml": "notes: {}\ncode:\n  scan: ['*.go']\n",
			})
			stdout, stderr, err := runRootCLI(t, context.Background(), []string{"note", command, "Old", "New", "--vault", vault.name})
			require.NoError(t, err, stderr)
			require.Empty(t, stderr)
			require.Contains(t, stdout, "link updates: 2")
			require.Contains(t, stdout, "Code references updated: 1 in 1 files")
			require.NotContains(t, stdout, "pending")
			require.NoFileExists(t, filepath.Join(vault.path, "Old.md"))
			for path, want := range map[string]string{"New.md": "Link [[New]]", "Ref.md": "Ref [[New|Alias]]", "refs.go": "package fixture\n// [[New]]\n"} {
				got, readErr := os.ReadFile(filepath.Join(vault.path, path))
				require.NoError(t, readErr)
				require.Equal(t, want, string(got))
			}
		})
	}
}

func TestNoteNamespaceCommandsDoNotReportRejectedPublication(t *testing.T) {
	for _, command := range []string{"rename", "move"} {
		t.Run(command, func(t *testing.T) {
			vault := setupNamespaceCommandVault(t, map[string]string{"Old.md": "# Old\n", "New.md": "# Occupied\n"})
			stdout, _, err := runRootCLI(t, context.Background(), []string{"note", command, "Old.md", "New.md", "--vault", vault.name})
			require.Error(t, err)
			require.NotContains(t, stdout, "Renamed to")
			require.NotContains(t, stdout, "Moved Old.md")
			for path, want := range map[string]string{"Old.md": "# Old\n", "New.md": "# Occupied\n"} {
				got, readErr := os.ReadFile(filepath.Join(vault.path, path))
				require.NoError(t, readErr)
				require.Equal(t, want, string(got))
			}
		})
	}
}

func TestCodeModeNoteMoveRetainsPublishedAndRejectedOutcomes(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "published", true: "rejected"}[occupied], func(t *testing.T) {
			files := map[string]string{"Old.md": "[[Old]]\n", "Ref.md": "[[Old]]\n"}
			if occupied {
				files["New.md"] = "occupied\n"
			}
			vault := setupNamespaceCommandVault(t, files)
			outcome := runNamespaceCodeMove(t, vault, map[string]any{"source": "Old.md", "target": "New.md"})
			require.Equal(t, !occupied, outcome.OK, outcome.Stderr)
			body, err := json.Marshal(outcome.Payload)
			require.NoError(t, err)
			var summary actions.MoveSummary
			require.NoError(t, json.Unmarshal(body, &summary))
			diagnostic := outcome.Diagnostic.(map[string]any)["mutation"].(map[string]any)
			current := diagnostic["current"].(map[string]any)
			if occupied {
				require.NotZero(t, outcome.ExitCode)
				require.Equal(t, validate.NamespaceNotStarted, summary.Mutation.Current.Decision)
				require.Equal(t, "not_started", current["decision"])
				require.Empty(t, summary.Results)
				require.NotContains(t, outcome.Stdout, "Moved Old.md")
				require.Contains(t, outcome.Stderr, "target")
				require.FileExists(t, filepath.Join(vault.path, "Old.md"))
			} else {
				require.Zero(t, outcome.ExitCode)
				require.Equal(t, validate.NamespaceCommitted, summary.Mutation.Current.Decision)
				require.False(t, summary.Mutation.Current.RecoveryPending)
				require.NotEmpty(t, summary.Mutation.Current.TransactionID)
				require.Equal(t, "committed", current["decision"])
				require.Equal(t, summary.Mutation.Current.TransactionID, current["transactionId"])
				require.Equal(t, 2, summary.TotalLinkUpdates)
				require.Contains(t, outcome.Stdout, "Moved Old.md -> New.md")
				require.Empty(t, outcome.Stderr)
				require.NoFileExists(t, filepath.Join(vault.path, "Old.md"))
				for _, name := range []string{"New.md", "Ref.md"} {
					got, readErr := os.ReadFile(filepath.Join(vault.path, name))
					require.NoError(t, readErr)
					require.Equal(t, "[[New]]\n", string(got))
				}
			}
		})
	}
}

func TestNoteNamespaceCommandsRetainCommittedProjectionFailureAndRecovery(t *testing.T) {
	for _, command := range []string{"rename", "move", "code move"} {
		t.Run(command, func(t *testing.T) {
			vault := setupNamespaceCommandVault(t, map[string]string{
				"Old.md": "[[Old]]\n", "Ref.md": "[[Old]]\n", "refs.go": "package fixture\n// [[Old]]\n",
				".rhizome/config.yml": "notes: {}\ncode:\n  scan: ['*.go']\n",
			})
			index := filepath.Join(vault.path, ".rhizome", "db.sqlite")
			backup := filepath.Join(t.TempDir(), "db.sqlite")
			require.NoError(t, os.Rename(index, backup))
			require.NoError(t, os.Mkdir(index, 0o755))
			call := func(source, target string, recovered bool) {
				t.Helper()
				if command == "code move" {
					outcome := runNamespaceCodeMove(t, vault, map[string]any{"source": source, "target": target})
					require.False(t, outcome.OK)
					require.NotZero(t, outcome.ExitCode)
					require.NotEmpty(t, outcome.Stderr)
					body, err := json.Marshal(outcome.Payload)
					require.NoError(t, err)
					var summary actions.MoveSummary
					require.NoError(t, json.Unmarshal(body, &summary))
					status := outcome.Diagnostic.(map[string]any)["mutation"].(map[string]any)
					current := status["current"].(map[string]any)
					if recovered {
						require.Equal(t, validate.NamespaceNotStarted, summary.Mutation.Current.Decision)
						require.Equal(t, "not_started", current["decision"])
						require.Empty(t, summary.Results)
						require.Len(t, summary.Mutation.Recovered, 1)
						require.Equal(t, validate.NamespaceCommitted, summary.Mutation.Recovered[0].Decision)
						require.False(t, summary.Mutation.Recovered[0].RecoveryPending)
					} else {
						require.Equal(t, validate.NamespaceCommitted, summary.Mutation.Current.Decision)
						require.Equal(t, "committed", current["decision"])
						require.Equal(t, true, current["recoveryPending"])
						require.NotEmpty(t, current["receiptPath"])
						require.True(t, summary.Mutation.Current.RecoveryPending)
						require.Equal(t, 2, summary.TotalLinkUpdates)
						require.Contains(t, outcome.Stdout, "Do not repeat this move")
					}
					return
				}
				stdout, _, err := runRootCLI(t, context.Background(), []string{"note", command, source, target, "--vault", vault.name})
				require.Error(t, err)
				if recovered {
					require.Contains(t, stdout, "Recovered move transaction")
					require.Contains(t, stdout, "committed")
					require.Contains(t, stdout, "current move did not start")
					require.NotContains(t, stdout, "pending")
				} else {
					require.Contains(t, stdout, "link updates: 2")
					require.Contains(t, stdout, "committed; recovery is pending")
					require.Contains(t, stdout, "Do not repeat this move")
					require.Contains(t, stdout, "Receipt path:")
				}
			}
			call("Old.md", "New.md", false)
			require.NoFileExists(t, filepath.Join(vault.path, "Old.md"))
			for _, name := range []string{"New.md", "Ref.md"} {
				got, err := os.ReadFile(filepath.Join(vault.path, name))
				require.NoError(t, err)
				require.Equal(t, "[[New]]\n", string(got))
			}
			code, err := os.ReadFile(filepath.Join(vault.path, "refs.go"))
			require.NoError(t, err)
			require.Equal(t, "package fixture\n// [[Old]]\n", string(code))
			require.NoError(t, os.Rename(index, filepath.Join(t.TempDir(), "blocked-index")))
			require.NoError(t, os.Rename(backup, index))
			call("New.md", "Next.md", true)
			require.FileExists(t, filepath.Join(vault.path, "New.md"))
			require.NoFileExists(t, filepath.Join(vault.path, "Next.md"))
			got, err := os.ReadFile(filepath.Join(vault.path, "Ref.md"))
			require.NoError(t, err)
			require.Equal(t, "[[New]]\n", string(got))
			code, err = os.ReadFile(filepath.Join(vault.path, "refs.go"))
			require.NoError(t, err)
			require.Equal(t, "package fixture\n// [[Old]]\n", string(code))
		})
	}
}

func TestNoteNamespaceRecoveredCleanupRendersWithoutDeletedIdentity(t *testing.T) {
	result := validate.NamespaceMutationResult{
		Current:   validate.NamespaceOutcome{Decision: validate.NamespaceNotStarted},
		Recovered: []validate.NamespaceOutcome{{Decision: validate.NamespaceCommitted}},
	}
	require.Equal(t, "Recovered earlier move: committed.\nThe current move did not start; review the recovered result and retry the request.\n", renderNamespaceMutationText(result))
}

// Exercise the actual Cobra stdio connection and local production composition.
func runNamespaceCodeMove(t *testing.T, vault agentTestVault, input map[string]any) agentcode.CallOutcome {
	t.Helper()
	description, err := agentcode.Describe([]string{"note_move"})
	require.NoError(t, err)
	reader, requests := io.Pipe()
	responses, writer := io.Pipe()
	command, _, err := rootCmd.Find([]string{"agent", "code", "serve"})
	require.NoError(t, err)
	command.SetIn(reader)
	command.SetOut(writer)
	t.Cleanup(func() {
		command.SetIn(nil)
		command.SetOut(nil)
		_ = reader.Close()
		_ = requests.Close()
		_ = responses.Close()
		_ = writer.Close()
	})
	type reply struct {
		Result agentcode.CallOutcome `json:"result"`
		Error  json.RawMessage       `json:"error"`
	}
	var outcome reply
	finished := make(chan error, 1)
	go func() {
		defer requests.Close()
		defer responses.Close()
		encoder, decoder := json.NewEncoder(requests), json.NewDecoder(responses)
		exchange := func(id int, method string, params any, result *reply) error {
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
				return err
			}
			return decoder.Decode(result)
		}
		finished <- func() error {
			var initialized, shutdown reply
			if err := exchange(1, "initialize", map[string]any{"protocolVersion": agentcode.ProtocolVersion, "selected": description.Selected, "contractHash": description.ContractHash}, &initialized); err != nil {
				return err
			}
			if err := exchange(2, "call", map[string]any{"operation": "note_move", "input": input, "timeoutMs": 10000}, &outcome); err != nil {
				return err
			}
			return exchange(3, "shutdown", map[string]any{}, &shutdown)
		}()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command.SetContext(ctx)
	_, stderr, err := runRootCLI(t, ctx, []string{"agent", "code", "serve", "--vault", vault.name, "--read-write"})
	_ = writer.Close()
	require.NoError(t, err, stderr)
	require.NoError(t, <-finished)
	require.Empty(t, stderr)
	require.Empty(t, outcome.Error)
	return outcome.Result
}
