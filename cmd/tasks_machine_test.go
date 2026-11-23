package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

// `run-due --host` runs nothing until the person turns tasks on for the
// repository on this machine; `hosts` then lists it for Rhizome Desktop.
func TestTasksMachineHostsAndHostRunDue(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".rhizome", "tasks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".rhizome", "tasks", "a.yml"), []byte("name: A\nevery: 1h\nprompt: p\n"), 0o644))
	for _, args := range [][]string{{"init", "--quiet", "-b", "main"}, {"add", "."}, {"-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "--quiet", "-m", "initial"}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	writeConfig := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(writeConfig, nil, 0o600))
	ctx := contextWithCommandEnv(context.Background(), commandEnv{
		Getwd:  func() (string, error) { return dir, nil },
		Config: vaultconfig.Env{CLIConfigFile: writeConfig},
	})
	// Through the invocation wrapper, so the fixture's Env and the
	// execution context both reach the command (cmd/AGENTS.md).
	rzm := func(args ...string) string {
		t.Helper()
		stdout, stderr, err := runRootCLIWithRootSilence(t, ctx, append([]string{"tasks"}, args...), true)
		require.NoError(t, err, stderr)
		return stdout + stderr
	}

	require.Contains(t, rzm("run-due", "--host"), "Skipped: tasks do not run on this machine")
	require.JSONEq(t, "[]", rzm("hosts", "--json"))

	require.Contains(t, rzm("machine", "on"), "run on this machine")
	var hosts []tasks.Host
	require.NoError(t, json.Unmarshal([]byte(rzm("hosts", "--json")), &hosts))
	require.Equal(t, []tasks.Host{{CommonDir: filepath.Join(dir, ".git"), Checkout: dir, RunHere: true}}, hosts)

	// The temporary repository is not trusted: the host pass says so.
	require.Contains(t, rzm("run-due", "--host"), "rzm trust")

	rzm("pause", "--for", "1h")
	var report tasks.Report
	require.NoError(t, json.Unmarshal([]byte(rzm("status", "--json")), &report))
	require.True(t, report.Paused)
	require.False(t, report.PausedUntil.IsZero())
	require.True(t, report.RunHere)

	rzm("machine", "off")
	require.JSONEq(t, "[]", rzm("hosts", "--json"))
}
