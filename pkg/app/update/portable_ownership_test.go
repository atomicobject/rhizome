package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestStandaloneInvocationProtectsRegisteredAppTargets(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "installed", "Rhizome.exe")
	require.NoError(t, os.MkdirAll(filepath.Dir(app), 0o700))
	require.NoError(t, os.WriteFile(app, []byte("installed GUI"), 0o700))
	standalone := filepath.Join(dir, "standalone-rzm")
	require.NoError(t, os.WriteFile(standalone, []byte("standalone"), 0o700))
	original := registeredAppPaths
	registeredAppPaths = func() ([]string, error) { return []string{app}, nil }
	t.Cleanup(func() { registeredAppPaths = original })
	cfg := &repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.0.0", BinaryPath: app}}
	invocation, err := ResolveInvocation(standalone, dir, cfg)
	require.NoError(t, err)
	require.Nil(t, invocation.Bundle)
	require.True(t, invocation.RepoTargetAppOwned)
	if runtime.GOOS == "windows" {
		registeredAppPaths = func() ([]string, error) { return []string{`\\?\` + app}, nil }
		invocation, err = ResolveInvocation(standalone, dir, cfg)
		require.NoError(t, err)
		require.True(t, invocation.RepoTargetAppOwned, "Rust's canonical app path must protect the same installed file")
	}
}

func TestRepositoryPlannerProtectsSharedEntryWithoutFollowingLocalSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux CLI entry is a symlink")
	}
	dir := t.TempDir()
	standalone := filepath.Join(dir, "standalone-rzm")
	image := filepath.Join(dir, "immutable-image", "rzm")
	shared := filepath.Join(dir, "account-bin", "rzm")
	local := filepath.Join(dir, "repo", "rzm")
	for _, path := range []string{standalone, image} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("executable"), 0o700))
	}
	for _, path := range []string{shared, local} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.Symlink(image, path))
	}
	original := registeredAppPaths
	registeredAppPaths = func() ([]string, error) { return []string{shared}, nil }
	t.Cleanup(func() { registeredAppPaths = original })
	for _, scenario := range []struct {
		name    string
		target  string
		appOwns bool
	}{
		{name: "shared app entry", target: shared, appOwns: true},
		{name: "repository-local symlink", target: local},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := &repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.0.0", BinaryPath: scenario.target}}
			invocation, err := ResolveInvocation(standalone, dir, cfg)
			require.NoError(t, err)
			require.Equal(t, scenario.appOwns, invocation.RepoTargetAppOwned)
			plan, err := BuildUpdatePlan(invocation, UpdatePlanOptions{Mode: UpdateModePinned})
			if scenario.appOwns {
				require.ErrorContains(t, err, "owned by the app")
			} else {
				require.NoError(t, err)
				require.Equal(t, []UpdateTarget{{Role: UpdateTargetRepo, LogicalPath: local, Version: "v1.0.0", WriteMarker: true}}, plan.Targets)
			}
		})
	}
	// If a foreign CLI later owns that path, the registration no longer lists
	// the entry, and the planner must allow it as an ordinary destination.
	require.NoError(t, os.Remove(shared))
	require.NoError(t, os.WriteFile(shared, []byte("foreign CLI"), 0o700))
	registeredAppPaths = func() ([]string, error) { return nil, nil }
	cfg := &repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.0.0", BinaryPath: shared}}
	invocation, err := ResolveInvocation(standalone, dir, cfg)
	require.NoError(t, err)
	require.False(t, invocation.RepoTargetAppOwned)
	plan, err := BuildUpdatePlan(invocation, UpdatePlanOptions{Mode: UpdateModePinned})
	require.NoError(t, err)
	require.Equal(t, shared, plan.Targets[0].LogicalPath)
}
