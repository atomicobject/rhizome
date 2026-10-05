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
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestCodeModeConfigSnapshotDetectsPublishedChange(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing config", true: "existing config"}[exists], func(t *testing.T) {
			vault := setupAgentTestVault(t, nil)
			t.Chdir(vault.path)
			// Match FindLocalConfig's canonical, slash-normalized definition root.
			root := paths.ResolveSymlinks(vault.path).String()
			// Keep named-vault resolution isolated, including the missing-config case.
			preferences := t.TempDir()
			originalPreferences := obsidian.CliConfigPath
			obsidian.CliConfigPath = func() (string, string, error) {
				return preferences, filepath.Join(preferences, "config.yml"), nil
			}
			t.Cleanup(func() { obsidian.CliConfigPath = originalPreferences })
			require.NoError(t, obsidian.SaveCliConfig(obsidian.CliConfig{Vaults: map[string]obsidian.VaultDefinition{
				vault.name: {Name: vault.name, Root: root, Includes: []string{"**/*.md"}},
			}}))
			path := filepath.Join(vault.path, ".rhizome", "config.yml")
			if exists {
				require.NoError(t, os.WriteFile(path, []byte("# original\n{}\n"), 0o644))
			} else {
				// The shared fixture normally creates config.yml. Preserve it elsewhere
				// so this connection really starts without a repository config.
				require.NoError(t, os.Rename(path, path+".fixture"))
				require.NoFileExists(t, path)
			}
			description, err := agentcode.Describe([]string{"surface"})
			require.NoError(t, err)
			input, requests := io.Pipe()
			responses, output := io.Pipe()
			command, _, err := rootCmd.Find([]string{"agent", "code", "serve"})
			require.NoError(t, err)
			command.SetIn(input)
			command.SetOut(output)
			t.Cleanup(func() {
				command.SetIn(nil)
				command.SetOut(nil)
				_ = input.Close()
				_ = requests.Close()
				_ = responses.Close()
				_ = output.Close()
				resetCommandContexts(rootCmd)
			})
			type reply struct {
				ID     int                   `json:"id"`
				Result agentcode.CallOutcome `json:"result"`
				Error  json.RawMessage       `json:"error"`
			}
			var first, second reply
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
					call := map[string]any{"operation": "surface", "input": map[string]any{}, "timeoutMs": 10000}
					if err := exchange(2, "call", call, &first); err != nil {
						return err
					}
					// Publish only after the first public response from this connection.
					// The typed definition stays the same; its raw config snapshot changes.
					if err := obsidian.WriteFileAtomicPreservingMode(path, []byte("# published\n{}\n"), 0o644); err != nil {
						return err
					}
					if err := exchange(3, "call", call, &second); err != nil {
						return err
					}
					return exchange(4, "shutdown", map[string]any{}, &shutdown)
				}()
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command.SetContext(ctx)
			_, stderr, err := runRootCLI(t, ctx, []string{"agent", "code", "serve", "--vault", vault.name})
			_ = output.Close()
			require.NoError(t, err, stderr)
			require.NoError(t, <-finished)
			require.Empty(t, stderr)
			require.Equal(t, 2, first.ID)
			require.Empty(t, first.Error)
			require.True(t, first.Result.OK, first.Result.Stderr)
			require.Equal(t, 3, second.ID)
			require.Empty(t, second.Error)
			require.False(t, second.Result.OK)
			require.Contains(t, second.Result.Stderr, "code_mode_configuration_changed")
			require.Contains(t, second.Result.Stderr, ".rhizome/config.yml")
		})
	}
}
