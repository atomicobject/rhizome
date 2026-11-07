package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestStandaloneInvocationProtectsRegisteredAppShortFilename(t *testing.T) {
	dir := t.TempDir()
	app, alias := shortFilenameAppFixture(t, dir)
	if alias == "" {
		// RUNNER_TEMP can use a different volume from per-user installations.
		cache, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
		require.NoError(t, err)
		fallback, err := os.MkdirTemp(cache, "rzm-update-short-filename-")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, os.RemoveAll(fallback)) })
		dir = fallback
		app, alias = shortFilenameAppFixture(t, dir)
	}
	if alias == "" {
		t.Skip("neither the normal temp nor native user-cache filesystem provides an 8.3 filename")
	}
	standalone := filepath.Join(dir, "standalone-rzm.exe")
	require.NoError(t, os.WriteFile(standalone, []byte("standalone CLI"), 0o700))
	require.False(t, appbundle.SamePath(alias, app))
	appInfo, err := os.Stat(app)
	require.NoError(t, err)
	aliasInfo, err := os.Stat(alias)
	require.NoError(t, err)
	require.True(t, os.SameFile(appInfo, aliasInfo))
	original := registeredAppPaths
	registeredAppPaths = func() ([]string, error) { return []string{app}, nil }
	t.Cleanup(func() { registeredAppPaths = original })
	cfg := &repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.0.0", BinaryPath: alias}}
	invocation, err := ResolveInvocation(standalone, dir, cfg)
	require.NoError(t, err)
	require.Nil(t, invocation.Bundle)
	require.True(t, invocation.RepoTargetAppOwned, "the registered GUI must retain ownership through its actual 8.3 filename")
	_, err = BuildUpdatePlan(invocation, UpdatePlanOptions{Mode: UpdateModePinned})
	require.ErrorContains(t, err, "owned by the app")
	appBytes, err := os.ReadFile(app)
	require.NoError(t, err)
	require.Equal(t, "installed GUI", string(appBytes))
}

func shortFilenameAppFixture(t *testing.T, dir string) (app, alias string) {
	t.Helper()
	app = filepath.Join(dir, "installed", "Rhizome Desktop Application.exe")
	require.NoError(t, os.MkdirAll(filepath.Dir(app), 0o700))
	require.NoError(t, os.WriteFile(app, []byte("installed GUI"), 0o700))
	name, err := windows.UTF16PtrFromString(app)
	require.NoError(t, err)
	buffer := make([]uint16, 32768)
	n, err := windows.GetShortPathName(name, &buffer[0], uint32(len(buffer)))
	require.NoError(t, err)
	require.Positive(t, n)
	require.Less(t, n, uint32(len(buffer)))
	shortName := filepath.Base(windows.UTF16ToString(buffer[:n]))
	if strings.EqualFold(shortName, filepath.Base(app)) {
		return app, ""
	}
	// Keep the long parent spelling so only the final filename differs.
	return app, filepath.Join(filepath.Dir(app), shortName)
}
