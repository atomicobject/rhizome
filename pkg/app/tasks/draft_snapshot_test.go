package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDraftCaptureOmitsRunnerStateWithAndWithoutIgnoreRule(t *testing.T) {
	for _, ignore := range []string{"# no ignore rule\n", ".rhizome/task-state/\n"} {
		t.Run(ignore, func(t *testing.T) {
			dir := newRepo(t, map[string]string{"local.yml": "name: Local\nevery: 1d\nprompt: inspect\n"})
			commitFile(t, dir, ".gitignore", ignore, "ignore policy")
			agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
				_, err := os.Stat(filepath.Join(req.Dir, ".rhizome", "task-state"))
				require.ErrorIs(t, err, os.ErrNotExist)
				return AgentResult{}, nil
			}}
			svc := newService(t, dir, agent, "notify")
			outcome, err := svc.RunDraft(context.Background(), filepath.Join(dir, TasksDir, "local.yml"))
			require.NoError(t, err)
			require.Equal(t, OutcomeNoChanges, outcome.Outcome, outcome.Reason)
			require.Equal(t, 1, agent.calls())
		})
	}
}

func TestDraftCapturesAssumeUnchangedWithoutChangingSourceIndex(t *testing.T) {
	dir := newRepo(t, map[string]string{"local.yml": "name: Local\nevery: 1d\nprompt: inspect\n"})
	commitFile(t, dir, "helper.txt", "old contents", "helper")
	run(t, dir, "update-index", "--assume-unchanged", "helper.txt")
	writeFile(t, filepath.Join(dir, "helper.txt"), "current contents")
	index := run(t, dir, "rev-parse", "--path-format=absolute", "--git-path", "index")
	before, err := os.ReadFile(index)
	require.NoError(t, err)
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		content, err := os.ReadFile(filepath.Join(req.Dir, "helper.txt"))
		require.NoError(t, err)
		require.Equal(t, "current contents", string(content))
		return AgentResult{}, nil
	}}
	svc := newService(t, dir, agent, "notify")
	result, err := svc.RunDraft(context.Background(), filepath.Join(dir, TasksDir, "local.yml"))
	require.NoError(t, err)
	require.Equal(t, OutcomeNoChanges, result.Outcome, result.Reason)
	after, err := os.ReadFile(index)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
