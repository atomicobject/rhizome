package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestAgentCodeInputFileUsesFactoryWorkingDirectory(t *testing.T) {
	if base := os.Getenv("RZM_MANAGED_INPUT_FIXTURE"); base != "" {
		runManagedInputFixture(t, base)
		return
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required")
	}
	base := t.TempDir()
	factory := managedFixtureEnv(base, "A", false)
	host := managedFixtureEnv(base, "B", false)
	vault := filepath.Join(base, "selected-vault")
	for _, file := range []struct{ path, content string }{
		{filepath.Join(factory.WorkingDir, "payload.json"), `{"source":"factory A"}`},
		{filepath.Join(host.WorkingDir, "payload.json"), `{"source":"host B"}`},
		{filepath.Join(vault, "payload.json"), `{"source":"selected vault"}`},
		{filepath.Join(base, "A", "external.json"), `{"source":"outside vault"}`},
		{filepath.Join(vault, ".rhizome", "config.yml"), "notes: {}\n"},
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(file.path), 0700))
		require.NoError(t, os.WriteFile(file.path, []byte(file.content), 0600))
	}
	require.NoError(t, repoconfig.SaveCliConfig(factory, repoconfig.CliConfig{
		DefaultVaultName: "selected-vault",
		Vaults:           map[string]obsidian.VaultDefinition{"selected-vault": {Name: "selected-vault", Path: vault}},
	}))
	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.Command(executable, "-test.run=^TestAgentCodeInputFileUsesFactoryWorkingDirectory$", "-test.count=1")
	command.Dir = host.WorkingDir
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + host.HomeDir, "RZM_MANAGED_INPUT_FIXTURE=" + base}
	output, err := command.CombinedOutput()
	require.NoError(t, err, "isolated input-file command: %s", output)
}

func runManagedInputFixture(t *testing.T, base string) {
	factory := managedFixtureEnv(base, "A", false)
	caller := managedFixtureEnv(base, "B", false)
	vault, err := filepath.EvalSymlinks(filepath.Join(base, "selected-vault"))
	require.NoError(t, err)
	for _, test := range []struct{ name, inputFile, expected string }{
		{"relative file", "payload.json", `{"source":"factory A"}`},
		{"relative file outside vault", filepath.Join("..", "external.json"), `{"source":"outside vault"}`},
		{"absolute file outside vault", filepath.Join(base, "A", "external.json"), `{"source":"outside vault"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newRootCommand(commandEnv{Config: factory})
			var stdout, stderr bytes.Buffer
			app.SetOut(&stdout)
			app.SetErr(&stderr)
			app.SilenceUsage, app.SilenceErrors = true, true
			app.SetArgs([]string{"agent", "code", "execute", "--code", `return {input, nodeWorkingDir:process.cwd()};`, "--input-file", test.inputFile})
			err := app.ExecuteContext(vaultconfig.WithEnv(context.Background(), caller))
			require.NoError(t, err, stderr.String())
			var result agentcode.ExecuteResult
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
			require.True(t, result.OK, "%+v", result.Error)
			var value struct {
				Input          json.RawMessage `json:"input"`
				NodeWorkingDir string          `json:"nodeWorkingDir"`
			}
			require.NoError(t, json.Unmarshal(result.Result, &value))
			require.JSONEq(t, test.expected, string(value.Input))
			require.Equal(t, vault, value.NodeWorkingDir)
		})
	}
}
