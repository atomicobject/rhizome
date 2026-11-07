package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestResolveInvocationRequiresAppEntryLookupToConfirmIndependence(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	app := filepath.Join(dir, "Rhizome Desktop.exe")
	target := filepath.Join(dir, "repository-rzm")
	standalone := filepath.Join(dir, "standalone-rzm")
	for _, path := range []string{app, target, standalone} {
		require.NoError(t, os.WriteFile(path, []byte("executable"), 0o700))
	}
	original, originalCanonical := registeredAppPaths, canonicalAppEntryPath
	registeredAppPaths = func() ([]string, error) { return []string{app}, nil }
	t.Cleanup(func() { registeredAppPaths, canonicalAppEntryPath = original, originalCanonical })
	for _, failedPath := range []string{target, app} {
		for _, failure := range []error{os.ErrPermission, errors.New("native lookup IO failure"), os.ErrNotExist} {
			t.Run(filepath.Base(failedPath)+"/"+failure.Error(), func(t *testing.T) {
				canonicalAppEntryPath = func(path string) (paths.AbsPath, error) {
					if path == failedPath {
						return "", failure
					}
					return originalCanonical(path)
				}
				cfg := &repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.0.0", BinaryPath: target}}
				invocation, err := ResolveInvocation(standalone, dir, cfg)
				if errors.Is(failure, os.ErrNotExist) {
					require.NoError(t, err)
					require.False(t, invocation.RepoTargetAppOwned)
				} else {
					require.ErrorIs(t, err, failure)
				}
			})
		}
	}
}

func TestRepositoryPlannerPreservesIndependentAppEntryLinks(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "installed", "Rhizome Desktop.exe")
	standalone := filepath.Join(dir, "standalone-rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(app), 0o700))
	require.NoError(t, os.WriteFile(app, []byte("installed GUI"), 0o700))
	require.NoError(t, os.WriteFile(standalone, []byte("standalone CLI"), 0o700))
	original := registeredAppPaths
	registeredAppPaths = func() ([]string, error) { return []string{app}, nil }
	t.Cleanup(func() { registeredAppPaths = original })
	for _, scenario := range []struct {
		name string
		link func(string, string) error
	}{
		{name: "hardlink", link: os.Link},
		{name: "symlink", link: os.Symlink},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "repository-rzm")
			if err := scenario.link(app, target); err != nil {
				t.Skipf("%s unavailable: %v", scenario.name, err)
			}
			appInfo, err := os.Stat(app)
			require.NoError(t, err)
			targetInfo, err := os.Stat(target)
			require.NoError(t, err)
			require.True(t, os.SameFile(appInfo, targetInfo), "control must refer to the same existing file")
			cfg := &repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.0.0", BinaryPath: target}}
			invocation, err := ResolveInvocation(standalone, dir, cfg)
			require.NoError(t, err)
			require.False(t, invocation.RepoTargetAppOwned)
			plan, err := BuildUpdatePlan(invocation, UpdatePlanOptions{Mode: UpdateModePinned})
			require.NoError(t, err)
			require.Equal(t, []UpdateTarget{{Role: UpdateTargetRepo, LogicalPath: target, Version: "v1.0.0", WriteMarker: true}}, plan.Targets)
			replacement := filepath.Join(t.TempDir(), "replacement-rzm")
			require.NoError(t, os.WriteFile(replacement, []byte("downloaded CLI"), 0o700))
			require.NoError(t, replaceFile(replacement, plan.Targets[0].LogicalPath))
			appBytes, err := os.ReadFile(app)
			require.NoError(t, err)
			require.Equal(t, "installed GUI", string(appBytes))
			targetBytes, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "downloaded CLI", string(targetBytes))
		})
	}
}
