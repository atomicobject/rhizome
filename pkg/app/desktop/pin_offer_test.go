package desktop

import (
	"context"
	"encoding/json"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/stretchr/testify/require"
)

func TestInspectPinOffers(t *testing.T) {
	for _, tt := range []struct {
		name, pinned, available, state, offered string
		rhizome                                 obsidian.LocalRhizomeConfig
		source                                  bool
	}{
		{name: "older pin", pinned: "v1.0.0", available: "v1.2.0", state: "advance"},
		{name: "release build without v", pinned: "v1.0.0", available: "1.2.0", state: "advance", offered: "v1.2.0"},
		{name: "equal", pinned: "v1.2.0", available: "v1.2.0"},
		{name: "newer pin", pinned: "v2.0.0", available: "v1.2.0", state: "app-older"},
		{name: "prerelease pin", pinned: "v1.2.0-rc.1", available: "v1.2.0", state: "advance"},
		{name: "unparseable pin", pinned: "dev", available: "v1.2.0"},
		{name: "unparseable app", pinned: "v1.0.0", available: "dev"},
		{name: "external manager", available: "v1.2.0", rhizome: obsidian.LocalRhizomeConfig{BinaryManager: "external"}},
		{name: "development override", pinned: "v1.0.0", available: "v1.2.0", rhizome: obsidian.LocalRhizomeConfig{DevBinaryDir: "dev"}},
		{name: "source checkout", pinned: "v1.0.0", available: "v1.2.0", source: true},
		{name: "no pin", available: "v1.2.0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testService(t)
			s.available = func() string { return tt.available }
			cfg := tt.rhizome
			cfg.Version = tt.pinned
			folder := config(t, cfg)
			if tt.source {
				require.NoError(t, os.WriteFile(filepath.Join(folder, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
			}
			info, err := s.Inspect(folder)
			require.NoError(t, err)
			encoded, err := json.Marshal(info)
			require.NoError(t, err)
			if tt.state == "" {
				require.Nil(t, info.PinOffer)
				require.NotContains(t, string(encoded), "pinOffer")
			} else {
				offered := tt.offered
				if offered == "" {
					offered = tt.available
				}
				require.Equal(t, &PinOffer{State: tt.state, Pinned: tt.pinned, Available: offered}, info.PinOffer)
			}
		})
	}
}

func TestPinAdvanceRejectsUnmanagedAndSourceCheckouts(t *testing.T) {
	for _, authority := range []string{"global", "external", "development", "source"} {
		t.Run(authority, func(t *testing.T) {
			s := testService(t)
			cfg := obsidian.LocalRhizomeConfig{}
			switch authority {
			case "external":
				cfg.BinaryManager = "external"
			case "development":
				cfg.DevBinaryDir = "build"
			case "source":
				cfg.Version = "v1.0.0"
			}
			folder := config(t, cfg)
			if authority == "source" {
				require.NoError(t, os.WriteFile(filepath.Join(folder, "go.mod"), []byte("module github.com/atomicobject/rhizome\n"), 0o644))
			}
			_, err := s.PinAdvance(context.Background(), folder)
			code(t, err, "pin_unsupported")
		})
	}
}

func TestAvailableVersionUsesBridgeBuildWhenBundled(t *testing.T) {
	s := testService(t)
	executable := workingScript(t, filepath.Join(t.TempDir(), "Rhizome.app", "Contents", "MacOS", "rzm"))
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.atomicobject.rhizome</string><key>CFBundleShortVersionString</key><string>9.0.0</string></dict></plist>`
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(executable)), "Info.plist"), []byte(plist), 0o644))
	previous := desktopExecutable
	desktopExecutable = func() (string, error) { return executable, nil }
	t.Cleanup(func() { desktopExecutable = previous })
	require.Equal(t, version.Version, s.availableVersion())
}

func TestAvailableVersionUsesSelectedGlobalBuildOutsideBundle(t *testing.T) {
	s := testService(t)
	workingScript(t, filepath.Join(s.home, ".local", "bin", "rzm"))
	previous := desktopExecutable
	desktopExecutable = os.Executable
	t.Cleanup(func() { desktopExecutable = previous })
	require.Equal(t, "v1.2.3", s.availableVersion())
}

func TestRepositoryResponseIncludesPerWorktreePinOffers(t *testing.T) {
	s := testService(t)
	s.available = func() string { return "v2.0.0" }
	main, feature := fixtureRepository(t)
	equal := filepath.Join(filepath.Dir(main), "project-equal")
	git(t, main, "worktree", "add", "-b", "equal", equal)
	for path, pin := range map[string]string{main: "v1.0.0", feature: "v3.0.0", equal: "v2.0.0"} {
		require.NoError(t, obsidian.SaveLocalConfig(path, obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{Version: pin}}))
	}

	response := s.Handle(context.Background(), Request{Protocol: Protocol, Operation: "repository", Folder: feature})
	require.Nil(t, response.Error)
	repository, ok := response.Result.(Repository)
	require.True(t, ok)
	require.Len(t, repository.Worktrees, 3)
	want := map[string]*PinOffer{
		main:    {State: "advance", Pinned: "v1.0.0", Available: "v2.0.0"},
		feature: {State: "app-older", Pinned: "v3.0.0", Available: "v2.0.0"},
		equal:   nil,
	}
	for _, worktree := range repository.Worktrees {
		require.Equal(t, want[worktree.Path], worktree.PinOffer, worktree.Path)
	}

	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	var document struct {
		Result struct {
			Worktrees []map[string]json.RawMessage `json:"worktrees"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(encoded, &document))
	expectedJSON := map[string]string{
		main:    `{"state":"advance","pinned":"v1.0.0","available":"v2.0.0"}`,
		feature: `{"state":"app-older","pinned":"v3.0.0","available":"v2.0.0"}`,
	}
	for _, worktree := range document.Result.Worktrees {
		var path string
		require.NoError(t, json.Unmarshal(worktree["path"], &path))
		if want[path] == nil {
			require.NotContains(t, worktree, "pinOffer", path)
			continue
		}
		require.JSONEq(t, expectedJSON[path], string(worktree["pinOffer"]), path)
	}
}

func TestPinAdvanceRefusesAStaleOfferThatWouldDowngrade(t *testing.T) {
	for _, pinned := range []string{"v2.0.0", "v1.2.0"} {
		t.Run(pinned, func(t *testing.T) {
			s := testService(t)
			s.available = func() string { return "v1.2.0" }
			s.install = func(context.Context, appupdate.Options) (appupdate.Result, error) {
				t.Fatal("pin-advance must not run the updater for a pin that is not older")
				return appupdate.Result{}, nil
			}
			folder := config(t, obsidian.LocalRhizomeConfig{Version: pinned})
			_, err := s.Trust(folder)
			require.NoError(t, err)
			_, err = s.PinAdvance(context.Background(), folder)
			code(t, err, "pin_unsupported")
		})
	}
}

func TestPinAdvanceInstallsTheOfferedVersion(t *testing.T) {
	s := testService(t)
	// Outside a bundle the offer comes from the global executable, which may
	// differ from the version this bridge was built as.
	s.available = func() string { return "v1.2.0" }
	installed := ""
	s.install = func(_ context.Context, opts appupdate.Options) (appupdate.Result, error) {
		installed = opts.SetVersion
		return appupdate.Result{}, nil
	}
	folder := config(t, obsidian.LocalRhizomeConfig{Version: "v1.0.0"})
	_, err := s.Trust(folder)
	require.NoError(t, err)
	_, err = s.PinAdvance(context.Background(), folder)
	require.NoError(t, err)
	require.Equal(t, "v1.2.0", installed)
}
