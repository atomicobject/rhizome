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
