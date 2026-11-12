package cmd

import "os"

import "github.com/atomicobject/rhizome/internal/testkit"

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/shellquote"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/stretchr/testify/require"
)

func TestUpdateCommandPrintsCompletedTargetsBeforeReturningError(t *testing.T) {
	rootCmd := newRootCommand(commandEnv{Getwd: os.Getwd, Config: testkit.Env(t)})
	updateCmd, _, _ := rootCmd.Find([]string{"update"})

	wantErr := errors.New("write repo version marker")
	originalRunUpdate := runUpdate
	runUpdate = func(context.Context, appupdate.Options) (appupdate.Result, error) {
		return appupdate.Result{Targets: []appupdate.TargetResult{
			{
				Role:          appupdate.UpdateTargetGlobal,
				Path:          "/usr/local/bin/rzm",
				Version:       "v0.51.0",
				BinaryUpdated: true,
			},
			{
				Role:    appupdate.UpdateTargetRepo,
				Path:    "/workspace/.rhizome/bin/darwin-arm64/rzm",
				Version: "v0.51.0",
			},
		}}, wantErr
	}
	t.Cleanup(func() { runUpdate = originalRunUpdate })

	var stdout bytes.Buffer
	updateCmd.SetOut(&stdout)
	t.Cleanup(func() { updateCmd.SetOut(nil) })

	err := updateCmd.RunE(updateCmd, nil)

	require.ErrorIs(t, err, wantErr)
	require.Equal(t, "Updated global /usr/local/bin/rzm to Rhizome v0.51.0\n", stdout.String())
}

func TestUpdateCommandRefreshesOnlyTheRepositoryVersionThatChanged(t *testing.T) {
	for _, tt := range []struct {
		name, pin, dev, targetVersion string
		configured, repo, pinWritten  bool
		unchanged, yes                bool
		wantInit, updateError         bool
		initError                     bool
	}{
		{name: "pin advanced", configured: true, pin: "v1.0.0", repo: true, pinWritten: true, wantInit: true},
		{name: "pin advanced with yes", configured: true, pin: "v1.0.0", repo: true, pinWritten: true, yes: true, wantInit: true},
		{name: "configured unpinned", configured: true, wantInit: true},
		{name: "configured latest", configured: true, pin: "latest", wantInit: true},
		{name: "already current global", configured: true, targetVersion: "v1.0.0"},
		{name: "already current pin", configured: true, pin: "v1.2.0", repo: true, pinWritten: true},
		{name: "pin not advanced", configured: true, pin: "v1.0.0"},
		{name: "pinned repair", configured: true, pin: "v1.0.0", repo: true},
		{name: "no binary change", configured: true, unchanged: true},
		{name: "outside configured folder"},
		{name: "development authority", configured: true, dev: "build"},
		{name: "update failure", configured: true, updateError: true},
		{name: "init failure keeps pin", configured: true, pin: "v1.0.0", repo: true, pinWritten: true, wantInit: true, initError: true},
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
			cfg := repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: tt.pin, DevBinaryDir: tt.dev}}
			if tt.configured {
				require.NoError(t, repoconfig.SaveLocalConfig(root, cfg))
			}
			previousRun, previousInit, previousVersion := runUpdate, runUpdateInit, version.Version
			t.Cleanup(func() { runUpdate, runUpdateInit, version.Version = previousRun, previousInit, previousVersion })
			version.Version = "1.0.0"
			global := appupdate.TargetResult{Role: appupdate.UpdateTargetGlobal, Path: filepath.Join(t.TempDir(), "rzm"), Version: "v1.2.0", BinaryUpdated: !tt.unchanged}
			if tt.targetVersion != "" {
				global.Version = tt.targetVersion
			}
			target := global
			if tt.repo {
				target.Role, target.Path, target.PinWritten = appupdate.UpdateTargetRepo, filepath.Join(root, "custom", "rzm"), tt.pinWritten
			}
			updateFailure, initFailure := errors.New("update failed"), errors.New("init failed")
			runUpdate = func(_ context.Context, opts appupdate.Options) (appupdate.Result, error) {
				require.Equal(t, cwd, opts.WorkDir)
				require.Equal(t, tt.yes, opts.Yes)
				if tt.repo && tt.pinWritten {
					cfg.Rhizome.Version = target.Version
					require.NoError(t, repoconfig.SaveLocalConfig(root, cfg))
				}
				result := appupdate.Result{Targets: []appupdate.TargetResult{target}}
				if tt.repo {
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
				require.True(t, tt.wantInit)
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
			command, _, err := app.Find([]string{"update"})
			require.NoError(t, err)
			command.SetContext(contextWithCommandEnv(context.Background(), commandEnv{Config: env}))
			command.SetIn(stdin)
			command.SetOut(&stdout)
			command.SetErr(&stderr)
			if tt.yes {
				require.NoError(t, command.ParseFlags([]string{"--yes"}))
			}
			err = command.RunE(command, nil)
			switch {
			case tt.updateError:
				require.ErrorIs(t, err, updateFailure)
			case tt.initError:
				require.ErrorIs(t, err, initFailure)
				require.ErrorContains(t, err, "Retry with: cd "+shellquote.Quote(root)+" && rzm init")
				_, saved, loadErr := repoconfig.FindLocalConfig(root)
				require.NoError(t, loadErr)
				require.Equal(t, target.Version, saved.Rhizome.Version)
			default:
				require.NoError(t, err)
			}
			if tt.wantInit {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
			}
		})
	}
}

func TestUpdateInitRunsTheSelectedChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("child executable is a Unix shell fixture")
	}
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	executable := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.WriteFile(executable, []byte(`#!/bin/sh
[ "$RZM_REPO_DELEGATED" = 1 ] && [ "$RZM_SKIP_REPO_DELEGATE" = 1 ] || exit 7
[ "$1" = init ] && [ "$2" = --path ] && [ "$3" = "$PWD" ] || exit 8
cat
echo 'child stderr' >&2
`), 0o755))
	command := (&updateInvocationOptions{}).makeUpdateCmd()
	command.SetContext(context.Background())
	command.SetIn(strings.NewReader("child input\n"))
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	selection := repoexec.Plan{Authority: "global", Target: executable, ConfigDir: root, Config: &repoconfig.LocalConfig{}}
	result := appupdate.Result{Targets: []appupdate.TargetResult{{Role: appupdate.UpdateTargetGlobal, Path: executable, Version: "v99.0.0", BinaryUpdated: true}}}
	require.NoError(t, refreshUpdatedRepository(command, selection, result, false))
	require.Equal(t, "child input\n", stdout.String())
	require.Equal(t, "child stderr\n", stderr.String())
	stdout.Reset()
	require.NoError(t, refreshUpdatedRepository(command, selection, result, true))
	require.Empty(t, stdout.String(), "--yes gives the child no terminal or input")
}

func TestUpdateInitUsesTheUpdatedAppExecutable(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(t.TempDir(), "Rhizome.app")
	executable := filepath.Join(app, "Contents", "MacOS", "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(executable), 0o755))
	require.NoError(t, os.WriteFile(executable, nil, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.atomicobject.rhizome</string><key>CFBundleShortVersionString</key><string>99.0.0</string></dict></plist>`), 0o644))
	previous := runUpdateInit
	t.Cleanup(func() { runUpdateInit = previous })
	called := false
	runUpdateInit = func(child *exec.Cmd) error {
		called = true
		require.True(t, repoexec.SameExecutablePath(executable, child.Path))
		require.Equal(t, []string{child.Path, "init", "--path", root}, child.Args)
		return nil
	}
	command := (&updateInvocationOptions{}).makeUpdateCmd()
	command.SetContext(context.Background())
	selection := repoexec.Plan{Authority: "global", Target: executable, ConfigDir: root, Config: &repoconfig.LocalConfig{}}
	result := appupdate.Result{Targets: []appupdate.TargetResult{{Role: appupdate.UpdateTargetGlobal, Path: app, Version: "v99.0.0", BinaryUpdated: true}}}
	require.NoError(t, refreshUpdatedRepository(command, selection, result, false))
	require.True(t, called)
}
