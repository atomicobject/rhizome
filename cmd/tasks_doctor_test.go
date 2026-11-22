package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// Doctor prints its report and exits 1 when a check fails, here because the
// repository is untrusted and has no primary ref.
func TestTasksDoctorExitsOneWhenACheckFails(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	out, err := exec.Command("git", "-C", dir, "init", "--quiet", "-b", "main").CombinedOutput()
	require.NoError(t, err, string(out))
	repo, err := tasks.OpenRepo(context.Background(), dir)
	require.NoError(t, err)
	svc, err := tasks.New(tasks.Options{Repo: repo, Trusted: func(string) (bool, error) { return false, nil }})
	require.NoError(t, err)

	asJSON := true
	cmd := newTasksDoctorCmd(func(*cobra.Command) (*tasks.Service, error) { return svc, nil }, &asJSON)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true // newTasksCmd sets it on every subcommand
	cmd.SetArgs(nil)
	runErr := cmd.ExecuteContext(context.Background())
	var exit silentExitError
	require.True(t, errors.As(runErr, &exit), "got %v", runErr)
	require.Equal(t, 1, exit.ExitCode())

	var report tasks.DoctorReport
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &report))
	require.False(t, report.OK)
	require.Equal(t, tasks.CheckTrust, report.Checks[0].Check)
	require.False(t, report.Checks[0].OK)
	require.Equal(t, tasks.CheckPrimaryRef, report.Checks[1].Check)
	require.False(t, report.Checks[1].OK)
}
