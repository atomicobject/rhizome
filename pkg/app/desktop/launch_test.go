package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindDesktopAppPrefersARunningAppThenInstallOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	release := filepath.Join(dir, "Rhizome.app")
	dev := filepath.Join(dir, "Rhizome Dev.app")
	missing := filepath.Join(dir, "Missing.app")
	require.NoError(t, os.Mkdir(release, 0o755))
	require.NoError(t, os.Mkdir(dev, 0o755))
	candidates := []string{missing, release, dev}

	app, err := FindApp("", candidates, func(string) bool { return false })
	require.NoError(t, err)
	require.Equal(t, release, app)

	app, err = FindApp("", candidates, func(app string) bool { return app == dev })
	require.NoError(t, err)
	require.Equal(t, dev, app)

	app, err = FindApp(dev, []string{release}, func(string) bool { return false })
	require.NoError(t, err)
	require.Equal(t, dev, app, "RZM_DESKTOP_APP wins")
}

func TestFindDesktopAppExplainsAMissingInstall(t *testing.T) {
	t.Parallel()

	_, err := FindApp("", []string{filepath.Join(t.TempDir(), "Rhizome.app")}, func(string) bool { return true })
	require.ErrorContains(t, err, "not installed")
	_, err = FindApp(filepath.Join(t.TempDir(), "Nope.app"), nil, func(string) bool { return false })
	require.ErrorContains(t, err, "RZM_DESKTOP_APP")
}

func TestDesktopTargetAcceptsRhizomeFoldersAndGitWorkingTrees(t *testing.T) {
	t.Parallel()

	plain := t.TempDir()
	_, err := LaunchTarget(plain, ".")
	require.ErrorContains(t, err, "not inside a Rhizome folder or Git repository")

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere\n"), 0o644))
	worktree, err := LaunchTarget(root, ".")
	require.NoError(t, err, "a Git worktree root may keep its vault in a subfolder or need setup")
	want, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(want), worktree)

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("{}\n"), 0o644))
	nested := filepath.Join(root, "notes", "deep")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	got, err := LaunchTarget(root, nested)
	require.NoError(t, err)
	want, err = filepath.EvalSymlinks(nested)
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(want), got)
}
