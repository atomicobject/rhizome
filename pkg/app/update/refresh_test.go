package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/stretchr/testify/require"
)

func TestUpdateRepositoryRefreshExecutable(t *testing.T) {
	previous := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = previous })
	for _, tt := range []struct {
		name, authority, pin, targetVersion string
		configured, repo, pinWritten        bool
		unchanged, wantRefresh              bool
	}{
		{name: "pin advanced", authority: "managed", configured: true, pin: "v1.0.0", repo: true, pinWritten: true, wantRefresh: true},
		{name: "configured unpinned", authority: "global", configured: true, wantRefresh: true},
		{name: "configured latest", authority: "global", configured: true, pin: "latest", wantRefresh: true},
		{name: "already current global", authority: "global", configured: true, targetVersion: "v1.0.0"},
		{name: "already current pin", authority: "managed", configured: true, pin: "1.2.0", repo: true, pinWritten: true},
		{name: "pin not advanced", authority: "managed", configured: true, pin: "v1.0.0"},
		{name: "pinned repair", authority: "managed", configured: true, pin: "v1.0.0", repo: true},
		{name: "no global binary change", authority: "global", configured: true, unchanged: true},
		{name: "no repo binary change", authority: "managed", configured: true, pin: "v1.0.0", repo: true, pinWritten: true, unchanged: true},
		{name: "outside configured folder", authority: "global"},
		{name: "development authority", authority: "development", configured: true},
		{name: "external authority", authority: "external", configured: true},
		{name: "repo target under global authority", authority: "global", configured: true, repo: true, pinWritten: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *repoconfig.LocalConfig
			if tt.configured {
				cfg = &repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: tt.pin}}
			}
			target := TargetResult{Role: UpdateTargetGlobal, Path: filepath.Join(t.TempDir(), "rzm"), Version: "v1.2.0", BinaryUpdated: !tt.unchanged}
			if tt.targetVersion != "" {
				target.Version = tt.targetVersion
			}
			result := Result{}
			if tt.repo {
				if tt.authority == "managed" {
					result.Targets = append(result.Targets, target)
				}
				target.Role, target.Path, target.PinWritten = UpdateTargetRepo, filepath.Join(t.TempDir(), "custom", "rzm"), tt.pinWritten
			}
			result.Targets = append(result.Targets, target)
			executable, err := result.RepositoryRefreshExecutable(cfg, tt.authority, "")
			require.NoError(t, err)
			if tt.wantRefresh {
				require.Equal(t, target.Path, executable)
			} else {
				require.Empty(t, executable)
			}
		})
	}
}

func TestUpdateRepositoryRefreshUsesTheUpdatedAppExecutable(t *testing.T) {
	bundle, link := appUpdateFixture(t, false)
	result := Result{Targets: []TargetResult{{Role: UpdateTargetGlobal, Path: bundle.Root, Version: "v99.0.0", BinaryUpdated: true}}}
	cfg := &repoconfig.LocalConfig{}
	executable, err := result.RepositoryRefreshExecutable(cfg, "global", link)
	require.NoError(t, err)
	require.Equal(t, bundle.ExecutablePath, executable)
	require.NoError(t, os.WriteFile(filepath.Join(bundle.Root, "Contents", "Info.plist"), []byte("invalid plist"), 0o644))
	executable, err = result.RepositoryRefreshExecutable(cfg, "global", link)
	require.Error(t, err)
	require.Empty(t, executable)
}
