package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// The child test process has a synthetic host HOME and cwd B. Its command
// factory has A, and its execution context deliberately has B. No test mutates
// the parent process's working directory, environment or standard streams.
func TestAgentCodeManagedOperationsKeepFactoryEnvironment(t *testing.T) {
	if base := os.Getenv("RZM_MANAGED_ENV_FIXTURE"); base != "" {
		runManagedEnvironmentFixture(t, base, os.Getenv("RZM_MANAGED_ENV_OVERRIDES") == "1")
		return
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required")
	}
	base := t.TempDir()
	for _, name := range []string{"A", "B"} {
		for _, overrides := range []bool{false, true} {
			env := managedFixtureEnv(base, name, overrides)
			require.NoError(t, os.MkdirAll(env.WorkingDir, 0700))
			require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{DefaultVaultName: "managed-" + name}))
			registry, err := env.ObsidianFile()
			require.NoError(t, err)
			require.NoError(t, os.MkdirAll(filepath.Dir(registry), 0700))
			vault := filepath.Join(base, name, "managed-"+name)
			require.NoError(t, os.MkdirAll(filepath.Join(vault, ".rhizome"), 0700))
			data, err := json.Marshal(map[string]any{"vaults": map[string]any{"managed-" + name: map[string]string{"path": vault}}})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(registry, data, 0600))
			require.NoError(t, os.WriteFile(filepath.Join(vault, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(vault, ".rhizome", "ignore"), []byte(name+"-only.txt\n"), 0600))
			_, err = identity.WriteCurrentUser(vault, "Person "+name)
			require.NoError(t, err)
		}
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, overrides := range []bool{false, true} {
		t.Run(map[bool]string{false: "home and config directory", true: "explicit config files"}[overrides], func(t *testing.T) {
			command := exec.Command(executable, "-test.run=^TestAgentCodeManagedOperationsKeepFactoryEnvironment$", "-test.count=1")
			command.Dir = filepath.Join(base, "B", "working")
			command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(base, "B", "home"), "XDG_CONFIG_HOME=" + filepath.Join(base, "B", "config"), "RZM_MANAGED_ENV_FIXTURE=" + base}
			if overrides {
				command.Env = append(command.Env, "RZM_MANAGED_ENV_OVERRIDES=1")
			}
			output, err := command.CombinedOutput()
			require.NoError(t, err, "isolated managed operation: %s", output)
		})
	}
}

func managedFixtureEnv(base, name string, overrides bool) vaultconfig.Env {
	env := vaultconfig.Env{HomeDir: filepath.Join(base, name, "home"), ConfigDir: filepath.Join(base, name, "config"), WorkingDir: filepath.Join(base, name, "working")}
	if overrides {
		env.CLIConfigFile = filepath.Join(base, name, "preferences", "custom.yml")
		env.ObsidianConfigFile = filepath.Join(base, name, "registry", "custom.json")
	}
	return env
}

func runManagedEnvironmentFixture(t *testing.T, base string, overrides bool) {
	factory, caller := managedFixtureEnv(base, "A", overrides), managedFixtureEnv(base, "B", overrides)
	var factoryLookups, callerLookups atomic.Int32
	factory.LookupEnv = func(key string) (string, bool) {
		if key == "TYPESAFE_API_KEY" {
			factoryLookups.Add(1)
			return "synthetic\ninvalid", true
		}
		return "", false
	}
	caller.LookupEnv = func(string) (string, bool) { callerLookups.Add(1); return "", false }
	vaultA, vaultB := filepath.Join(base, "A", "managed-A"), filepath.Join(base, "B", "managed-B")
	for _, readWrite := range []bool{false, true} {
		app := newRootCommand(commandEnv{Config: factory})
		var stdout, stderr bytes.Buffer
		app.SetOut(&stdout)
		app.SetErr(&stderr)
		app.SilenceUsage, app.SilenceErrors = true, true
		code := `const user = await rzm.currentUser({action:"show"});
const paths = await rzm.checkPaths({paths:[{path:"A-only.txt"},{path:"B-only.txt"}]});
const evaluation = await rzm.evaluate({state:"synthetic fixture",questions:{ok:{type:"noul"}}});
const changed = await rzm.currentUser({action:"set",personTitleOrRef:"Person A updated"});
return {user, paths, changed, usedFactoryLookup: !evaluation.ok && evaluation.stderr.includes("valid API key is required"), hostHome: process.env.HOME, cwd:process.cwd()};`
		args := []string{"agent", "code", "execute", "--code", code}
		if readWrite {
			args = append(args, "--read-write")
		}
		app.SetArgs(args)
		err := app.ExecuteContext(vaultconfig.WithEnv(context.Background(), caller))
		require.NoError(t, err, stderr.String())
		var result agentcode.ExecuteResult
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
		require.True(t, result.OK, "%+v", result.Error)
		var value struct {
			User struct {
				OK      bool                `json:"ok"`
				Payload identity.Resolution `json:"payload"`
			} `json:"user"`
			Paths struct {
				OK      bool `json:"ok"`
				Payload struct {
					Paths []struct {
						Status string `json:"status"`
					} `json:"paths"`
				} `json:"payload"`
			} `json:"paths"`
			Changed struct {
				OK bool `json:"ok"`
			} `json:"changed"`
			UsedFactoryLookup bool   `json:"usedFactoryLookup"`
			HostHome          string `json:"hostHome"`
			CWD               string `json:"cwd"`
		}
		require.NoError(t, json.Unmarshal(result.Result, &value))
		require.True(t, value.User.OK)
		require.Equal(t, "Person A", value.User.Payload.Ref)
		require.True(t, value.Paths.OK)
		require.Len(t, value.Paths.Payload.Paths, 2)
		require.Equal(t, "ignored", value.Paths.Payload.Paths[0].Status)
		require.Equal(t, "allowed", value.Paths.Payload.Paths[1].Status)
		require.True(t, value.UsedFactoryLookup, "evaluation must resolve the synthetic factory-only lookup without sending a request")
		require.Equal(t, readWrite, value.Changed.OK)
		require.Equal(t, filepath.Join(base, "B", "home"), value.HostHome)
		canonical, err := filepath.EvalSymlinks(vaultA)
		require.NoError(t, err)
		require.Equal(t, canonical, value.CWD)
		current, _, err := identity.ReadCurrentUser(vaultA)
		require.NoError(t, err)
		expected := "Person A"
		if readWrite {
			expected = "Person A updated"
		}
		require.Equal(t, expected, current.Ref)
		current, _, err = identity.ReadCurrentUser(vaultB)
		require.NoError(t, err)
		require.Equal(t, "Person B", current.Ref)
		_, err = identity.WriteCurrentUser(vaultA, "Person A")
		require.NoError(t, err)
	}
	require.Greater(t, factoryLookups.Load(), int32(0))
	require.Zero(t, callerLookups.Load(), "caller context B cannot replace factory A")
}
