package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/shellquote"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/stretchr/testify/require"
)

func TestUpdateCommandPrintsCompletedTargetsBeforeReturningError(t *testing.T) {
	app := newRootCommand(commandEnv{Config: testkit.Env(t)})
	wantErr := errors.New("write repo version marker")
	originalRunUpdate := runUpdate
	runUpdate = func(context.Context, appupdate.Options) (appupdate.Result, error) {
		return appupdate.Result{Targets: []appupdate.TargetResult{
			{Role: appupdate.UpdateTargetGlobal, Path: "/usr/local/bin/rzm", Version: "v0.51.0", BinaryUpdated: true},
			{Role: appupdate.UpdateTargetRepo, Path: "/workspace/.rhizome/bin/darwin-arm64/rzm", Version: "v0.51.0"},
		}}, wantErr
	}
	t.Cleanup(func() { runUpdate = originalRunUpdate })
	stdout, _, err := runCommandInvocation(t, app, context.Background(), []string{"update"}, true)
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, "Updated global /usr/local/bin/rzm to Rhizome v0.51.0\n", stdout)
}

func TestUpdateCommandRefreshesCompletedRepositoryUpdates(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		repo, yes              bool
		updateError, initError bool
	}{
		{name: "pin advanced", repo: true},
		{name: "pin advanced with yes", repo: true, yes: true},
		{name: "global changed"},
		{name: "update failure after global change", updateError: true},
		{name: "update failure after pin advanced", repo: true, updateError: true},
		{name: "init failure keeps pin", repo: true, initError: true},
		{name: "update and init failure", repo: true, updateError: true, initError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := testkit.Env(t)
			root := filepath.Join(t.TempDir(), "repo's files")
			require.NoError(t, os.MkdirAll(root, 0o755))
			root, err := filepath.EvalSymlinks(root)
			require.NoError(t, err)
			cwd := filepath.Join(root, "nested")
			require.NoError(t, os.MkdirAll(cwd, 0o755))
			env.WorkingDir = cwd
			cfg := repoconfig.LocalConfig{}
			if tt.repo {
				cfg.Rhizome.Version = "v1.0.0"
			}
			require.NoError(t, repoconfig.SaveLocalConfig(root, cfg))
			previousRun, previousInit, previousVersion := runUpdate, runUpdateInit, version.Version
			t.Cleanup(func() { runUpdate, runUpdateInit, version.Version = previousRun, previousInit, previousVersion })
			version.Version = "1.0.0"
			global := appupdate.TargetResult{Role: appupdate.UpdateTargetGlobal, Path: filepath.Join(t.TempDir(), "rzm"), Version: "v1.2.0", BinaryUpdated: true}
			target := global
			if tt.repo {
				target.Role, target.Path, target.PinWritten = appupdate.UpdateTargetRepo, filepath.Join(root, "custom", "rzm"), true
			}
			updateFailure, initFailure := errors.New("update failed"), errors.New("init failed")
			runUpdate = func(_ context.Context, opts appupdate.Options) (appupdate.Result, error) {
				require.Equal(t, cwd, opts.WorkDir)
				require.Equal(t, tt.yes, opts.Yes)
				if tt.repo {
					cfg.Rhizome.Version = target.Version
					require.NoError(t, repoconfig.SaveLocalConfig(root, cfg))
				}
				result := appupdate.Result{Targets: []appupdate.TargetResult{target}}
				if tt.repo {
					global.BinaryUpdated = !tt.updateError
					result.Targets = append([]appupdate.TargetResult{global}, result.Targets...)
				}
				if tt.updateError {
					return result, updateFailure
				}
				return result, nil
			}
			var stdout, stderr bytes.Buffer
			stdin := strings.NewReader("interactive input")
			calls := 0
			runUpdateInit = func(child *exec.Cmd) error {
				calls++
				require.Equal(t, []string{target.Path, "init", "--path", root}, child.Args)
				require.Equal(t, root, child.Dir)
				require.Contains(t, child.Env, "RZM_REPO_DELEGATED=1")
				require.Contains(t, child.Env, "RZM_SKIP_REPO_DELEGATE=1")
				require.Same(t, &stdout, child.Stdout)
				require.Same(t, &stderr, child.Stderr)
				if tt.yes {
					require.Nil(t, child.Stdin)
				} else {
					require.Same(t, stdin, child.Stdin)
				}
				if tt.initError {
					return initFailure
				}
				return nil
			}
			app := newRootCommand(commandEnv{Config: env})
			app.SetIn(stdin)
			app.SetOut(&stdout)
			app.SetErr(&stderr)
			app.SilenceUsage, app.SilenceErrors = true, true
			args := []string{"update"}
			if tt.yes {
				args = append(args, "--yes")
			}
			app.SetArgs(args)
			err = app.ExecuteContext(context.Background())
			if tt.updateError {
				require.ErrorIs(t, err, updateFailure)
			}
			if tt.initError {
				require.ErrorIs(t, err, initFailure)
				require.ErrorContains(t, err, "Retry with: cd "+shellquote.Quote(root)+" && rzm init")
			}
			if !tt.updateError && !tt.initError {
				require.NoError(t, err)
			}
			if tt.repo {
				_, saved, loadErr := repoconfig.FindLocalConfig(root)
				require.NoError(t, loadErr)
				require.Equal(t, target.Version, saved.Rhizome.Version)
			}
			require.Equal(t, 1, calls)
		})
	}
}

func TestUpdateInitRunsTheSelectedChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("child executable is a Unix shell fixture")
	}
	env := testkit.Env(t)
	root, err := filepath.EvalSymlinks(env.WorkingDir)
	require.NoError(t, err)
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{}))
	executable := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.WriteFile(executable, []byte(`#!/bin/sh
[ "$RZM_REPO_DELEGATED" = 1 ] && [ "$RZM_SKIP_REPO_DELEGATE" = 1 ] || exit 7
[ "$1" = init ] && [ "$2" = --path ] && [ "$3" = "$PWD" ] || exit 8
cat
echo 'child stderr' >&2
`), 0o755))
	previous := runUpdate
	t.Cleanup(func() { runUpdate = previous })
	runUpdate = func(ctx context.Context, _ appupdate.Options) (appupdate.Result, error) {
		return appupdate.Result{Targets: []appupdate.TargetResult{{Role: appupdate.UpdateTargetGlobal, Path: executable, Version: "v99.0.0", BinaryUpdated: true}}}, nil
	}
	for _, scenario := range []string{"interactive", "yes", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			app := newRootCommand(commandEnv{Config: env})
			app.SetIn(strings.NewReader("child input\n"))
			args := []string{"update"}
			if scenario == "yes" {
				args = append(args, "--yes")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			stdout, stderr, err := runCommandInvocation(t, app, ctx, args, true)
			if scenario == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorContains(t, err, "Retry with: cd "+shellquote.Quote(root)+" && rzm init")
			} else {
				require.NoError(t, err)
				require.Equal(t, "child stderr\n", stderr)
			}
			if scenario == "interactive" {
				require.Contains(t, stdout, "child input\n")
			} else {
				require.NotContains(t, stdout, "child input")
			}
		})
	}
}
