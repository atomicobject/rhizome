package cmd

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

// A run that fails before it starts, here because the temporary repository
// is not trusted, is saved on the item for the Tasks page.
func TestTasksRunRecordsALaunchError(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for _, args := range [][]string{{"init", "--quiet", "-b", "main"}, {"-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "--quiet", "--allow-empty", "-m", "initial"}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	repo, err := tasks.OpenRepo(context.Background(), dir)
	require.NoError(t, err)
	svc, err := tasks.New(tasks.Options{Repo: repo})
	require.NoError(t, err)
	var id string
	require.NoError(t, svc.Store().Update(context.Background(), func(st *tasks.State) error {
		id = st.AddItem(tasks.Item{Task: "look", Kind: tasks.KindTask, State: tasks.StateDue}, time.Now()).ID
		return nil
	}))

	cmd := newTasksCmd(func() map[harness.Kind]harness.Harness { return nil })
	cmd.SetArgs([]string{"run", id})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	runErr := cmd.ExecuteContext(contextWithCommandEnv(context.Background(), commandEnv{Getwd: func() (string, error) { return dir, nil }}))
	require.ErrorContains(t, runErr, "rzm trust")

	st, err := svc.Store().Load()
	require.NoError(t, err)
	it, err := st.Item(id)
	require.NoError(t, err)
	require.Equal(t, runErr.Error(), it.LaunchError)
}
