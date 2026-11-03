package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/stretchr/testify/require"
)

func TestTaskCodeModeUsesExistingIndexWithoutStartingRuntime(t *testing.T) {
	binary := os.Getenv(fixtureBinaryEnv)
	if binary == "" {
		t.Skipf("set %s to a compiled rzm to run the runtime fixture", fixtureBinaryEnv)
	}
	_, err := exec.LookPath("node")
	require.NoError(t, err)
	t.Setenv("RZM_RUNTIME_AUTOSTART", "1")
	t.Setenv("RZM_SKIP_REPO_DELEGATE", "1")
	vault := setupAgentTestVault(t, map[string]string{
		"note.md":                  "# Fixture\n\nExisting source.\n",
		".gitignore":               ".rhizome/db.sqlite*\n.rhizome/task-state/\n.rhizome/runtime*\n.rhizome/diagnostics/\n",
		".rhizome/tasks/probe.yml": "name: Probe\nevery: 1d\ncheck:\n  script: probe.mjs\n",
		"probe.mjs": `const result = await rzm.files({inputs:["note.md"],includeContent:true});
if (!result.ok) throw new Error(result.stderr || "read failed");
return {needed:false,summary:"existing index read without runtime"};`,
	})
	for _, args := range [][]string{{"init", "--quiet", "-b", "main"}, {"add", "."}, {"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgSign=false", "commit", "--quiet", "-m", "fixture"}} {
		output, err := exec.Command("git", append([]string{"-C", vault.path}, args...)...).CombinedOutput()
		require.NoError(t, err, string(output))
	}
	repo, err := tasks.OpenRepo(context.Background(), vault.path)
	require.NoError(t, err)
	svc, err := tasks.New(tasks.Options{Repo: repo, Checkout: vault.path, Rzm: binary, Trusted: func(string) (bool, error) { return true, nil }})
	require.NoError(t, err)
	require.NoFileExists(t, appruntime.ManifestPath(vault.path))
	outcome, err := svc.RunDraft(context.Background(), filepath.Join(vault.path, ".rhizome/tasks/probe.yml"))
	require.NoError(t, err)
	require.Equal(t, tasks.OutcomeNotNeeded, outcome.Outcome, outcome.Reason)
	require.Equal(t, "existing index read without runtime", outcome.Reason)
	require.NoFileExists(t, appruntime.ManifestPath(vault.path))
	require.NoFileExists(t, appruntime.SpawnLockPath(vault.path))
	require.NoFileExists(t, appruntime.LogPath(vault.path))
}
