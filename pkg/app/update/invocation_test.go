package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestResolveInvocationResolvesPinnedRepoTarget(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	cases := []struct {
		name string
		cfg  repoconfig.LocalRhizomeConfig
		want string
	}{
		{
			name: "default location",
			cfg:  repoconfig.LocalRhizomeConfig{Version: " 1.2.3 "},
			want: filepath.Join(repo, ".rhizome", "bin", CurrentPlatformDir(), executableName()),
		},
		{
			name: "relative binary directory",
			cfg:  repoconfig.LocalRhizomeConfig{Version: "v1.2.3", BinaryDir: "tools/rhizome"},
			want: filepath.Join(repo, "tools", "rhizome", CurrentPlatformDir(), executableName()),
		},
		{
			name: "relative binary path",
			cfg:  repoconfig.LocalRhizomeConfig{Version: "v1.2.3", BinaryPath: "tools/rzm-custom"},
			want: filepath.Join(repo, "tools", "rzm-custom"),
		},
		{
			name: "binary path takes precedence over binary directory",
			cfg:  repoconfig.LocalRhizomeConfig{Version: "v1.2.3", BinaryPath: "legacy/rzm", BinaryDir: ".rhizome/custom-bin"},
			want: filepath.Join(repo, "legacy", "rzm"),
		},
		{
			name: "development directory does not choose pinned update target",
			cfg:  repoconfig.LocalRhizomeConfig{Version: "v1.2.3", DevBinaryDir: "bin"},
			want: filepath.Join(repo, ".rhizome", "bin", CurrentPlatformDir(), executableName()),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			invocation, err := ResolveInvocation(filepath.Join(t.TempDir(), "rzm"), repo, &repoconfig.LocalConfig{Rhizome: tc.cfg})

			require.NoError(t, err)
			require.True(t, invocation.RepoPinned)
			require.Equal(t, "v1.2.3", invocation.RepoPinVersion)
			require.Equal(t, tc.want, invocation.RepoTargetPath)
			require.False(t, invocation.RepoScoped)
		})
	}
}

func TestResolveInvocationUsesCurrentExecutableByDefault(t *testing.T) {
	t.Parallel()

	invocation, err := ResolveInvocation("", "", nil)

	require.NoError(t, err)
	require.NotEmpty(t, invocation.ExecutablePath)
	require.True(t, filepath.IsAbs(invocation.ExecutablePath))
}

func TestResolveInvocationIgnoresLocationWithoutPin(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	for _, tc := range []struct {
		name string
		cfg  repoconfig.LocalRhizomeConfig
	}{
		{"both locations", repoconfig.LocalRhizomeConfig{BinaryPath: "tools/rzm-custom", BinaryDir: "tools/rhizome"}},
		{"binary path", repoconfig.LocalRhizomeConfig{BinaryPath: "tools/rzm-custom"}},
		{"binary directory", repoconfig.LocalRhizomeConfig{BinaryDir: "tools/rhizome"}},
		{"development directory", repoconfig.LocalRhizomeConfig{DevBinaryDir: "bin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), "rzm")
			invocation, err := ResolveInvocation(executable, repo, &repoconfig.LocalConfig{Rhizome: tc.cfg})
			require.NoError(t, err)
			require.Equal(t, executable, invocation.ExecutablePath)
			require.False(t, invocation.RepoPinned)
			require.Empty(t, invocation.RepoPinVersion)
			require.Empty(t, invocation.RepoTargetPath)
			require.False(t, invocation.RepoScoped)
		})
	}
}

func TestResolveInvocationTrustsExternallyManagedExecutable(t *testing.T) {
	t.Parallel()

	executable := filepath.Join(t.TempDir(), "rzm")
	invocation, err := ResolveInvocation(executable, "", &repoconfig.LocalConfig{
		Rhizome: repoconfig.LocalRhizomeConfig{BinaryManager: repoconfig.BinaryManagerExternal},
	})

	require.NoError(t, err)
	require.Equal(t, executable, invocation.ExecutablePath)
	require.False(t, invocation.RepoPinned)
	require.Empty(t, invocation.RepoPinVersion)
	require.Empty(t, invocation.RepoTargetPath)
	require.False(t, invocation.RepoScoped)
}

func TestResolveInvocationRejectsInvalidBinaryOwnership(t *testing.T) {
	t.Parallel()

	_, err := ResolveInvocation(filepath.Join(t.TempDir(), "rzm"), t.TempDir(), &repoconfig.LocalConfig{
		Rhizome: repoconfig.LocalRhizomeConfig{
			BinaryManager: repoconfig.BinaryManagerExternal,
			Version:       "v1.2.3",
		},
	})

	require.ErrorContains(t, err, "rhizome.version")
}

func TestResolveInvocationRequiresConfigDirectoryForPin(t *testing.T) {
	t.Parallel()

	_, err := ResolveInvocation(filepath.Join(t.TempDir(), "rzm"), "", &repoconfig.LocalConfig{
		Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.2.3"},
	})

	require.ErrorContains(t, err, "config directory is required")
}

func TestResolveInvocationClassifiesMissingTargetByNormalizedPath(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	target := filepath.Join(repo, "tools", "rzm")
	invocation, err := ResolveInvocation(filepath.Join(repo, "tools", ".", "rzm"), repo, &repoconfig.LocalConfig{
		Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.2.3", BinaryPath: target},
	})

	require.NoError(t, err)
	require.Equal(t, target, invocation.ExecutablePath)
	require.Equal(t, target, invocation.RepoTargetPath)
	require.True(t, invocation.RepoScoped)
}

func TestResolveInvocationClassifiesFilesystemAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink and hardlink executable fixtures are Unix-oriented")
	}
	t.Parallel()

	repo := t.TempDir()
	target := filepath.Join(repo, "repo-rzm")
	require.NoError(t, os.WriteFile(target, []byte("binary"), 0o755))

	t.Run("symlink", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "rzm-link")
		if err := os.Symlink(target, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		invocation, err := ResolveInvocation(alias, repo, &repoconfig.LocalConfig{
			Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.2.3", BinaryPath: target},
		})

		require.NoError(t, err)
		require.Equal(t, alias, invocation.ExecutablePath)
		require.Equal(t, target, invocation.RepoTargetPath)
		require.True(t, invocation.RepoScoped)
	})

	t.Run("repo target symlink keeps logical destination", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "repo-rzm-link")
		if err := os.Symlink(target, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		invocation, err := ResolveInvocation(target, repo, &repoconfig.LocalConfig{
			Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.2.3", BinaryPath: alias},
		})

		require.NoError(t, err)
		require.Equal(t, target, invocation.ExecutablePath)
		require.Equal(t, alias, invocation.RepoTargetPath)
		require.True(t, invocation.RepoScoped)
	})

	t.Run("hardlink", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "rzm-hardlink")
		if err := os.Link(target, alias); err != nil {
			t.Skipf("hardlink unavailable: %v", err)
		}

		invocation, err := ResolveInvocation(alias, repo, &repoconfig.LocalConfig{
			Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.2.3", BinaryPath: target},
		})

		require.NoError(t, err)
		require.Equal(t, alias, invocation.ExecutablePath)
		require.Equal(t, target, invocation.RepoTargetPath)
		require.True(t, invocation.RepoScoped)
	})
}
