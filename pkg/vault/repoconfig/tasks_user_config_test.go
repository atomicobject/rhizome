package repoconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestTasksUserConfigRoundTrip(t *testing.T) {
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(t.TempDir(), "config.yml")
	initial := "# mine\ndefault_vault_name: notes\ntasks:\n  timezone: America/Detroit\n  notify: {command: [say, done]}\n"
	require.NoError(t, os.WriteFile(env.CLIConfigFile, []byte(initial), 0o600))

	const common = "/repos/a/.git"
	require.NoError(t, repoconfig.UpdateTasksRepository(env, common, func(r *repoconfig.TasksRepositoryConfig) error {
		r.RunHere, r.Checkout, r.DailyTokenCap = true, "/repos/a", 5000
		r.Tasks = map[string]repoconfig.TaskAgentConfig{"dream": {Harness: "codex", Model: "gpt-6.1", Effort: "high"}}
		return nil
	}))
	got, err := repoconfig.LoadTasksUserConfig(env)
	require.NoError(t, err)
	require.Equal(t, "America/Detroit", got.Timezone)
	require.Equal(t, []string{"say", "done"}, got.Notify.Command)
	require.Equal(t, repoconfig.TasksRepositoryConfig{RunHere: true, Checkout: "/repos/a", DailyTokenCap: 5000, Tasks: map[string]repoconfig.TaskAgentConfig{"dream": {Harness: "codex", Model: "gpt-6.1", Effort: "high"}}}, got.Repositories[common])
	data, err := os.ReadFile(env.CLIConfigFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "# mine\ndefault_vault_name: notes\n")
	require.Contains(t, string(data), "runHere: true")

	// Turning everything off removes the entry, and an empty section goes too.
	require.NoError(t, os.WriteFile(env.CLIConfigFile, []byte("tasks:\n  repositories:\n    /repos/a/.git: {runHere: true}\n"), 0o600))
	require.NoError(t, repoconfig.UpdateTasksRepository(env, common, func(r *repoconfig.TasksRepositoryConfig) error {
		r.RunHere = false
		return nil
	}))
	data, err = os.ReadFile(env.CLIConfigFile)
	require.NoError(t, err)
	require.NotContains(t, string(data), "tasks")
}
