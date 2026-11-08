package repoexec

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func writeExecutable(t *testing.T, path string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755))
	return path
}

func sourceCheckout(t *testing.T, config string) string {
	t.Helper()
	root := paths.ResolveSymlinks(t.TempDir()).String()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte(config), 0o644))
	return root
}

func TestSelectRunsTheDevAppInASourceCheckoutWithoutItsOwnBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit fixture is Unix-only")
	}
	devApp := writeExecutable(t, filepath.Join(t.TempDir(), "rzm"))
	t.Setenv(DevAppEnv, devApp)
	release := filepath.Join(t.TempDir(), "release-rzm")

	root := sourceCheckout(t, "{}\n")
	plan, err := Select(root, release)
	require.NoError(t, err)
	require.Equal(t, "development", plan.Authority)
	require.Equal(t, devApp, plan.Target)
	require.Empty(t, plan.TrustRoot(), "the checkout did not choose the Dev app, so it needs no trust")
	for _, args := range [][]string{{"search", "x"}, {"update"}, {"init"}} {
		_, required, err := TrustRequirement(args, root, release)
		require.NoError(t, err)
		require.False(t, required, args)
	}
	require.Equal(t, devApp, DevAppHandoff([]string{"search", "x"}, root, release))
	require.Empty(t, DevAppHandoff([]string{"update"}, root, release), "update never replaces the Dev app's rzm")
	require.Empty(t, DevAppHandoff([]string{"search", "x"}, root, devApp), "the Dev app does not hand off to itself")

	decision := DecisionFor([]string{"search", "x"}, root, release)
	require.NoError(t, decision.Err)
	require.True(t, decision.Delegate)
	require.Equal(t, devApp, decision.Target)
	require.False(t, DecisionFor([]string{"update"}, root, release).Delegate)

	build := writeExecutable(t, filepath.Join(root, "bin", runtime.GOOS, ExecutableName()))
	plan, err = Select(root, release)
	require.NoError(t, err)
	require.Equal(t, build, plan.Target, "the checkout's own build outranks the Dev app")
	require.False(t, plan.DevApp)
	require.Equal(t, root, plan.TrustRoot(), "the checkout's own build still needs trust")

	plain := paths.ResolveSymlinks(t.TempDir()).String()
	require.NoError(t, os.MkdirAll(filepath.Join(plain, ".rhizome"), 0o755))
	plan, err = Select(plain, release)
	require.NoError(t, err)
	require.Equal(t, "global", plan.Authority, "other repositories never select the Dev app")

	t.Setenv(DevAppEnv, "")
	plan, err = Select(sourceCheckout(t, "rhizome:\n  version: v1.2.3\n"), release)
	require.NoError(t, err)
	require.Equal(t, "managed", plan.Authority, "an empty override turns discovery off")
}

func TestPinnedSourceCheckoutKeepsItsTrustWhenTheDevAppRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit fixture is Unix-only")
	}
	devApp := writeExecutable(t, filepath.Join(t.TempDir(), "rzm"))
	t.Setenv(DevAppEnv, devApp)
	release := filepath.Join(t.TempDir(), "release-rzm")
	root := sourceCheckout(t, "rhizome:\n  version: v1.2.3\n")

	plan, err := Select(root, release)
	require.NoError(t, err)
	require.Equal(t, devApp, plan.Target, "the Dev app outranks a pin in a source checkout")
	require.Equal(t, "v1.2.3", plan.PinnedVersion)
	require.Empty(t, plan.TrustRoot(), "running the app-owned Dev build needs no trust")
	// On the command line, init and update act on the pin whichever binary
	// runs them, including the Dev app's own rzm, so dispatch keeps trust.
	for _, exe := range []string{release, devApp} {
		for _, args := range [][]string{{"init"}, {"update", "--pinned"}, {"search", "x"}} {
			checkout, required, err := TrustRequirement(args, root, exe)
			require.NoError(t, err)
			require.True(t, required, "%s %v", exe, args)
			require.Equal(t, root, checkout)
		}
	}
	decision := DecisionFor([]string{"search", "x"}, root, release)
	require.True(t, decision.Delegate, "after trust, commands run on the Dev app")
	require.Equal(t, devApp, decision.Target)
	require.False(t, decision.Bootstrap)
}

func TestDevAppDiscoveryFindsTheLinuxInstallBesideItsDesktopExecutable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux install layout")
	}
	home := t.TempDir()
	env := vaultconfig.Env{HomeDir: home}
	dir := filepath.Join(home, ".local", "share", "rhizome-dev", "bin")
	rzm := writeExecutable(t, filepath.Join(dir, "rzm"))
	_, ok := DevAppExecutable(env)
	require.False(t, ok, "an rzm without the desktop executable beside it is not the Dev app")

	writeExecutable(t, filepath.Join(dir, "rhizome-desktop"))
	found, ok := DevAppExecutable(env)
	require.True(t, ok)
	require.Equal(t, rzm, found)
}

func TestIsSourceRootRequiresTheCheckoutRoot(t *testing.T) {
	root := sourceCheckout(t, "{}\n")
	require.True(t, IsSourceRoot(root))
	sub := filepath.Join(root, "pkg")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.False(t, IsSourceRoot(sub))
	require.False(t, IsSourceRoot(t.TempDir()))
}
