package desktop

import (
	"context"
	"path/filepath"
	"testing"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestServiceUsesInvocationHomeAndKeepsAccountTrustStore(t *testing.T) {
	previous := desktopExecutable
	desktopExecutable = func() (string, error) { return filepath.Join(t.TempDir(), "unbundled-rzm"), nil }
	t.Cleanup(func() { desktopExecutable = previous })
	accountTrust, err := repositorytrust.DefaultStore()
	require.NoError(t, err)
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			target := workingScript(t, filepath.Join(home, ".local", "bin", "rzm"))
			s, err := New(filepath.Join(t.TempDir(), "desktop"), vaultconfig.Env{HomeDir: home})
			require.NoError(t, err)
			require.Equal(t, accountTrust, s.trust)
			selected, err := s.globalExecutable("")
			require.NoError(t, err)
			require.Equal(t, target, selected)
		})
	}
}

func TestPinAdvanceCarriesServiceEnvironment(t *testing.T) {
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			s := testService(t)
			folder := config(t, repoconfig.LocalRhizomeConfig{Version: "v1.0.0"})
			home := t.TempDir()
			manifest := "https://" + name + ".invalid/releases"
			s.env = vaultconfig.Env{HomeDir: home, WorkingDir: folder, LookupEnv: func(key string) (string, bool) {
				return manifest, key == "RZM_UPDATE_MANIFEST_URL"
			}}
			s.available = func() string { return "v1.2.0" }
			called := false
			s.install = func(ctx context.Context, opts appupdate.Options) (appupdate.Result, error) {
				called = true
				require.Equal(t, home, vaultconfig.FromContext(ctx).HomeDir)
				require.Equal(t, folder, vaultconfig.FromContext(ctx).WorkingDir)
				require.Equal(t, manifest, opts.ManifestURL)
				require.Equal(t, folder, opts.WorkDir)
				return appupdate.Result{}, nil
			}
			_, err := s.Trust(folder)
			require.NoError(t, err)
			_, err = s.PinAdvance(context.Background(), folder)
			require.NoError(t, err)
			require.True(t, called)
		})
	}
}
