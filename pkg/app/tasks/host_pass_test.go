package tasks

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// remoteClone makes a bare remote, a seed clone that pushed the task files,
// and a host clone whose primary ref is origin/main.
func remoteClone(t *testing.T, files map[string]string) (seed, host string) {
	t.Helper()
	isolateGit(t)
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	run(t, root, "init", "--quiet", "--bare", "-b", "main", remote)
	seed = filepath.Join(root, "seed")
	run(t, root, "init", "--quiet", "-b", "main", seed)
	for name, content := range files {
		writeFile(t, filepath.Join(seed, TasksDir, name), content)
	}
	commitFile(t, seed, "README.md", "hello\n", "add tasks")
	run(t, seed, "remote", "add", "origin", remote)
	run(t, seed, "push", "--quiet", "origin", "main")
	host = filepath.Join(root, "host")
	run(t, root, "clone", "--quiet", remote, host)
	return seed, host
}

// A host pass reads "Run tasks on this machine" again before each task, so
// turning it off during a pass runs nothing more.
func TestHostPassStopsWhenRunHereTurnsOff(t *testing.T) {
	dir := newRepo(t, map[string]string{
		"a.yml": "name: A\nevery: 1h\nprompt: p\n",
		"b.yml": "name: B\nevery: 1h\nprompt: p\n",
	})
	repo, err := OpenRepo(context.Background(), dir)
	require.NoError(t, err)
	var svc *Service
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		return AgentResult{Message: "nothing to do"}, svc.SetRunHere(context.Background(), false)
	}}
	svc, err = New(Options{
		Repo:   repo,
		Config: repoconfig.LocalTasksConfig{PrimaryRef: "main", DefaultMode: "auto"},
		Agent:  agent,
		Env:    vaultconfig.Env{CLIConfigFile: filepath.Join(t.TempDir(), "config.yml")},
	})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, svc.SetRunHere(ctx, true))

	summary, err := svc.RunDueHost(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, agent.calls(), "the second task ran after runHere was turned off")
	require.Len(t, summary.Outcomes, 1)
	require.Equal(t, notRunHere, summary.Skipped)
}

// Only a pass settles items that wait for their work to reach the primary
// ref, so such items make a pass due every 15 minutes; it fetches, closes
// merged items, and runs nothing for the tasks still blocked.
func TestItemsWaitingForTheirWorkMakeAPassDue(t *testing.T) {
	seed, host := remoteClone(t, map[string]string{
		"a.yml": "name: A\nevery: 1d\nprompt: p\n",
		"b.yml": "name: B\nevery: 1d\nprompt: p\n",
	})
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := hostService(t, host, "", &now, true)
	ctx := context.Background()
	require.NoError(t, svc.SetRunHere(ctx, true))
	base := run(t, host, "rev-parse", "origin/main")
	var itemA, itemB string
	setState(t, svc, func(st *State) {
		st.Seen = map[string]time.Time{"a": now.Add(-3 * time.Hour), "b": now.Add(-3 * time.Hour)}
		for _, task := range []string{"a", "b"} {
			st.Record(RunRecord{Task: task, StartedAt: now.Add(-2 * time.Hour), At: now.Add(-2 * time.Hour), Outcome: OutcomeReady})
		}
		itemA = st.AddItem(Item{Task: "a", Kind: KindTask, State: StateReady, Commit: base}, now.Add(-2*time.Hour)).ID
		itemB = st.AddItem(Item{Task: "b", Kind: KindTask, State: StateStarted, Commit: base}, now.Add(-2*time.Hour)).ID
	})
	at, ok, err := svc.NextDueAt(ctx)
	require.NoError(t, err)
	require.True(t, ok, "open items that only a pass settles must make a pass due")
	require.False(t, at.After(now))

	// A's work merges on the remote; the pass must fetch to see it.
	commitFile(t, seed, "a.md", "a\n", "A's work\n\nRhizome-Task: a")
	run(t, seed, "push", "--quiet", "origin", "main")
	summary, err := svc.RunDueHost(ctx)
	require.NoError(t, err)
	require.Empty(t, summary.Outcomes, "a blocked task ran")
	st := mustState(t, svc)
	require.True(t, st.Fetch.At.Equal(now), "the settling pass did not fetch")
	a, err := st.Item(itemA)
	require.NoError(t, err)
	require.Equal(t, StateDone, a.State)
	b, err := st.Item(itemB)
	require.NoError(t, err)
	require.Equal(t, StateStarted, b.State)

	at, ok, err = svc.NextDueAt(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, at.Equal(now.Add(settleInterval)), "B still waits, so the next settling pass is due 15 minutes later, not %v", at)
}
