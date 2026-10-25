package repoexec

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestDecisionForExecutableAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, config, marker, reason string
		args                         []string
		installed, source, goRun     bool
		currentTarget                bool
		delegate, bootstrap, fail    bool
	}{
		{name: "unconfigured repository keeps invoking executable"},
		{name: "pin without installed binary bootstraps", config: "version: v1.2.3", bootstrap: true, reason: BootstrapReasonMissing},
		{name: "matching install delegates without probing", config: "version: v1.2.3", installed: true, marker: "v1.2.3", delegate: true},
		{name: "wrong version bootstraps exact pin", config: "version: v1.2.3", installed: true, marker: "v1.2.2", bootstrap: true, reason: BootstrapReasonMismatch},
		{name: "selected executable does not delegate to itself", config: "version: v1.2.3", installed: true, marker: "v1.2.3", currentTarget: true},
		{name: "binary path without pin has no executable authority", config: "binaryPath: selected/rzm"},
		{name: "external ownership keeps invoking executable", config: "binaryManager: external"},
		{name: "external ownership with managed pin fails closed", config: "version: v1.2.3\n  binaryManager: external", fail: true},
		{name: "unknown owner fails closed", config: "binaryManager: unknown", fail: true},
		{name: "configured development overrides pin", config: "version: v1.2.3\n  devBinaryDir: dev", installed: true, delegate: true},
		{name: "missing configured development binary fails closed", config: "devBinaryDir: dev", fail: true},
		{name: "source checkout uses development binary", config: "version: v1.2.3", source: true, installed: true, delegate: true},
		{name: "go run keeps source executable", config: "version: v1.2.3", source: true, goRun: true, installed: true},
		{name: "trust command never bootstraps", config: "version: v1.2.3", args: []string{"trust"}},
		{name: "untrust command never bootstraps", config: "version: v1.2.3", args: []string{"untrust"}},
		{name: "credentials never bootstrap", config: "version: v1.2.3", args: []string{"credentials", "import"}},
		{name: "credentials ignore configured development binary", config: "version: v1.2.3\n  devBinaryDir: dev", installed: true, args: []string{"credentials", "import"}},
		{name: "desktop dispatch never bootstraps", config: "version: v1.2.3", args: []string{"desktop"}},
		{name: "update owns its installation", config: "version: v1.2.3", args: []string{"update", "--pinned"}},
		{name: "external ownership migration uses invoking binary", config: "version: v1.2.3", args: []string{"init", "--binary-manager", "external"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := paths.ResolveSymlinks(t.TempDir()).String()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
			if tc.config != "" {
				require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("rhizome:\n  "+tc.config+"\n"), 0644))
			}
			if tc.source {
				require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0644))
			}
			target := filepath.Join(root, ".rhizome", "bin", PlatformDir(), ExecutableName())
			if tc.source {
				target = filepath.Join(root, "bin", runtime.GOOS, ExecutableName())
			} else if tc.config == "version: v1.2.3\n  devBinaryDir: dev" || tc.config == "devBinaryDir: dev" {
				target = filepath.Join(root, "dev", runtime.GOOS, ExecutableName())
			}
			if tc.installed {
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
				require.NoError(t, os.WriteFile(target, []byte("must not execute this fixture\n"), 0755))
				if tc.marker != "" {
					require.NoError(t, appupdate.WriteVersionMarker(target, tc.marker))
				}
			}
			executable := filepath.Join(t.TempDir(), ExecutableName())
			if tc.currentTarget {
				executable = target
			} else if tc.goRun {
				executable = filepath.Join(root, "go-build123", "b001", "exe", ExecutableName())
			}
			args := tc.args
			if args == nil {
				args = []string{"search", "fixture"}
			}
			decision := DecisionFor(args, root, executable)
			require.Equal(t, tc.fail, decision.Err != nil)
			require.Equal(t, tc.delegate, decision.Delegate)
			require.Equal(t, tc.bootstrap, decision.Bootstrap)
			require.Equal(t, tc.reason, decision.Reason)
			if tc.delegate || tc.bootstrap {
				require.Equal(t, target, decision.Target)
			}
			if tc.bootstrap {
				require.Equal(t, "v1.2.3", decision.PinnedVersion)
				require.Equal(t, root, decision.ConfigDir)
				require.NotNil(t, decision.Config)
			}
		})
	}
}

func TestBootstrapAcceptsInstallCompletedAfterSelection(t *testing.T) {
	root := paths.ResolveSymlinks(t.TempDir()).String()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	configPath := filepath.Join(root, ".rhizome", "config.yml")
	const config = "rhizome:\n  version: v1.2.3\n"
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0644))
	decision := DecisionFor([]string{"search", "fixture"}, root, filepath.Join(t.TempDir(), ExecutableName()))
	require.True(t, decision.Bootstrap)
	require.NoError(t, os.MkdirAll(filepath.Dir(decision.Target), 0755))
	const installed = "installed concurrently\n"
	require.NoError(t, os.WriteFile(decision.Target, []byte(installed), 0755))
	require.NoError(t, appupdate.WriteVersionMarker(decision.Target, "v1.2.3"))
	for range 2 {
		require.NoError(t, Bootstrap(context.Background(), decision, io.Discard))
	}
	content, err := os.ReadFile(decision.Target)
	require.NoError(t, err)
	require.Equal(t, installed, string(content))
	content, err = os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, config, string(content))
}
