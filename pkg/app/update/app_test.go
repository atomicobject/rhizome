package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunAppUpdateThroughSymlink(t *testing.T) {
	for _, latest := range []bool{false, true} {
		for _, pinned := range []bool{false, true} {
			for _, scenario := range []string{"current", "newer", "timeout", "open failure", "dev", "feed failure"} {
				t.Run(fmt.Sprintf("latest=%t/pinned=%t/%s", latest, pinned, scenario), func(t *testing.T) {
					bundle, link := appUpdateFixture(t, scenario == "dev")
					repo, _, repoTarget := updateExecutionFixture(t, "v1.0.0")
					if !pinned {
						repo = t.TempDir()
					}
					appDeps := defaultAppUpdateDependencies()
					appDeps.feedURL = "https://app.invalid/latest.json"
					appDeps.timeout = 2 * time.Second
					launchCalls, waitCalls := 0, 0
					appDeps.launch = func(_ context.Context, name string, args ...string) error {
						launchCalls++
						require.Equal(t, "open", name)
						require.Equal(t, []string{"-n", "-a", bundle.Root, "--args", "--install-update"}, args)
						if scenario == "open failure" {
							return errors.New("no GUI session")
						}
						return nil
					}
					appDeps.wait = func(context.Context, time.Duration) error {
						waitCalls++
						if scenario == "newer" {
							writeAppFixturePlist(t, bundle.Root, "2.0.0", false)
						}
						return nil
					}
					requests := []string{}
					client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						requests = append(requests, req.URL.Host)
						if req.URL.Host == "app.invalid" {
							if scenario == "feed failure" {
								return nil, errors.New("feed offline")
							}
							version := "2.0.0"
							if scenario == "current" {
								version = "1.0.0"
							}
							return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":"` + version + `"}`))}, nil
						}
						return manifestClient("v3.0.0").Transport.RoundTrip(req)
					})}
					deps := executionDependencies()
					replaced := []string{}
					deps.replace = func(_, path string) error { replaced = append(replaced, path); return nil }
					marker, pin := false, false
					deps.writeMarker = func(path, version string) error {
						require.Equal(t, repoTarget, path)
						require.Equal(t, "v3.0.0", version)
						marker = true
						return nil
					}
					deps.saveConfig = func(_ string, cfg obsidian.LocalConfig) error {
						require.Equal(t, "v3.0.0", cfg.Rhizome.Version)
						pin = true
						return nil
					}
					var output strings.Builder
					result, err := Run(context.Background(), Options{Latest: latest, Yes: true, ExecutablePath: link, WorkDir: repo, ManifestURL: "https://cli.invalid/latest", HTTPClient: client, Stdout: &output, deps: &deps, appDeps: &appDeps})
					failed := scenario != "current" && scenario != "newer"
					if failed {
						require.ErrorContains(t, err, "global target")
						require.ErrorContains(t, err, desktopDownloadURL)
					} else {
						require.NoError(t, err)
					}
					require.Equal(t, UpdateTargetGlobal, result.Targets[0].Role)
					require.Equal(t, bundle.Root, result.Targets[0].Path)
					require.Equal(t, scenario == "newer", result.Targets[0].BinaryUpdated)
					if scenario == "newer" {
						require.Equal(t, "v2.0.0", result.Targets[0].Version)
						require.Contains(t, result.Summaries()[0], "v2.0.0")
					}
					if scenario == "current" {
						require.Contains(t, output.String(), "up to date")
					}
					if scenario == "dev" {
						require.ErrorContains(t, err, "make desktop-install-dev")
						require.Zero(t, launchCalls)
						require.NotContains(t, requests, "app.invalid")
					}
					if scenario == "current" || scenario == "feed failure" {
						require.Zero(t, launchCalls)
					}
					if scenario == "timeout" {
						require.ErrorContains(t, err, "timed out")
						require.Equal(t, 2, waitCalls)
					}
					if scenario == "open failure" {
						require.ErrorContains(t, err, "no GUI session")
					}
					if pinned {
						require.Equal(t, []string{repoTarget}, replaced)
						require.True(t, marker)
						require.True(t, pin)
						require.Len(t, result.Targets, 2)
						require.True(t, result.Targets[1].BinaryUpdated)
					} else {
						require.Empty(t, replaced)
						require.Len(t, result.Targets, 1)
						require.NotContains(t, requests, "cli.invalid")
					}
					assertAppLinkIntact(t, link, bundle.ExecutablePath)
				})
			}
		}
	}
}

func TestRunBundledDefaultRetainsPinPrompt(t *testing.T) {
	bundle, link := appUpdateFixture(t, false)
	repo, _, _ := updateExecutionFixture(t, "v1.0.0")
	appDeps := defaultAppUpdateDependencies()
	appDeps.feedURL = "https://app.invalid/latest.json"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "app.invalid" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":"1.0.0"}`))}, nil
		}
		return manifestClient("v3.0.0").Transport.RoundTrip(req)
	})}
	deps := executionDependencies()
	deps.replace = func(string, string) error { t.Fatal("declined repo pin must not replace a binary"); return nil }
	var output strings.Builder
	result, err := Run(context.Background(), Options{ExecutablePath: link, WorkDir: repo, ManifestURL: "https://cli.invalid/latest", Stdin: strings.NewReader("no\n"), Stdout: &output, HTTPClient: client, deps: &deps, appDeps: &appDeps})
	require.NoError(t, err)
	require.Len(t, result.Targets, 1)
	require.Contains(t, output.String(), "v1.0.0")
	require.Contains(t, output.String(), "v3.0.0")
	assertAppLinkIntact(t, link, bundle.ExecutablePath)
}

func TestRunBundledRepoOnlyModesDoNotHandOff(t *testing.T) {
	for _, setVersion := range []bool{false, true} {
		t.Run(fmt.Sprint(setVersion), func(t *testing.T) {
			bundle, link := appUpdateFixture(t, false)
			repo, _, repoTarget := updateExecutionFixture(t, "v1.0.0")
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("repo-only mode must not fetch feed or latest release")
				return nil, nil
			})}
			deps := executionDependencies()
			replaced := []string{}
			deps.replace = func(_, path string) error { replaced = append(replaced, path); return nil }
			opts := Options{Pinned: !setVersion, ExecutablePath: link, WorkDir: repo, HTTPClient: client, deps: &deps}
			if setVersion {
				opts.SetVersion = "v2.0.0"
			}
			result, err := Run(context.Background(), opts)
			require.NoError(t, err)
			require.Len(t, result.Targets, 1)
			require.Equal(t, []string{repoTarget}, replaced)
			assertAppLinkIntact(t, link, bundle.ExecutablePath)
		})
	}
}

func TestBuildUpdatePlanNeverReplacesBundledGlobal(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		for _, mode := range []UpdateMode{UpdateModeDefault, UpdateModeLatest} {
			invocation := Invocation{ExecutablePath: "/usr/local/bin/rzm", Bundle: &appbundle.Info{Root: "/Applications/Rhizome.app"}}
			if pinned {
				invocation.RepoPinned = true
				invocation.RepoPinVersion = "v1.0.0"
				invocation.RepoTargetPath = "/repo/.rhizome/bin/rzm"
			}
			plan, err := BuildUpdatePlan(invocation, UpdatePlanOptions{Mode: mode, LatestVersion: "v2.0.0", AdvancePin: true})
			require.NoError(t, err)
			for _, target := range plan.Targets {
				require.Equal(t, UpdateTargetRepo, target.Role)
			}
		}
	}
}

func TestAppOutcomeSurvivesRepoManifestFailure(t *testing.T) {
	bundle, link := appUpdateFixture(t, false)
	repo, _, _ := updateExecutionFixture(t, "v1.0.0")
	appDeps := defaultAppUpdateDependencies()
	appDeps.feedURL = "https://app.invalid/latest.json"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "app.invalid" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":"1.0.0"}`))}, nil
		}
		return nil, errors.New("repo release unavailable")
	})}
	result, err := Run(context.Background(), Options{Latest: true, ExecutablePath: link, WorkDir: repo, HTTPClient: client, ManifestURL: "https://cli.invalid/latest", appDeps: &appDeps})
	require.ErrorContains(t, err, "repo release unavailable")
	require.Len(t, result.Targets, 1)
	require.Equal(t, bundle.Root, result.Targets[0].Path)
}

func TestAppUpdateFeedEnvironmentOverride(t *testing.T) {
	t.Setenv("RZM_DESKTOP_UPDATE_FEED", "https://app.invalid/custom.json")
	require.Equal(t, "https://app.invalid/custom.json", defaultAppUpdateDependencies().feedURL)
}

func appUpdateFixture(t *testing.T, dev bool) (appbundle.Info, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Rhizome.app")
	executable := filepath.Join(root, "Contents", "MacOS", "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(executable), 0o755))
	require.NoError(t, os.WriteFile(executable, []byte("bundled executable"), 0o755))
	writeAppFixturePlist(t, root, "1.0.0", dev)
	link := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.Symlink(executable, link))
	bundle, err := appbundle.Resolve(link)
	require.NoError(t, err)
	require.NotNil(t, bundle)
	return *bundle, link
}

func writeAppFixturePlist(t *testing.T, root, version string, dev bool) {
	t.Helper()
	identifier := "com.atomicobject.rhizome"
	if dev {
		identifier += ".dev"
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "Contents", "Info.plist"), []byte(`<plist><dict><key>CFBundleIdentifier</key><string>`+identifier+`</string><key>CFBundleShortVersionString</key><string>`+version+`</string></dict></plist>`), 0o644))
}

func assertAppLinkIntact(t *testing.T, link, executable string) {
	t.Helper()
	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
	resolved, err := filepath.EvalSymlinks(link)
	require.NoError(t, err)
	require.Equal(t, executable, resolved)
	content, err := os.ReadFile(link)
	require.NoError(t, err)
	require.Equal(t, "bundled executable", string(content))
}

func TestRunRefusesAppOwnedRepositoryTargets(t *testing.T) {
	for _, mode := range []UpdateMode{UpdateModePinned, UpdateModeSetVersion, UpdateModeLatest, UpdateModeDefault} {
		for _, targetKind := range []string{"bundled executable", "invocation link", "invocation parent alias", "parent link", "parent link missing directory", "separate repo link", "direct bundle standard link", "direct bundle separate repo link"} {
			t.Run(string(mode)+"/"+targetKind, func(t *testing.T) {
				bundle, link := appUpdateFixture(t, false)
				safeRepoLink := strings.HasSuffix(targetKind, "separate repo link")
				directBundle := strings.HasPrefix(targetKind, "direct bundle")
				if directBundle {
					original := appCommandLineLinkPath
					appCommandLineLinkPath = link
					t.Cleanup(func() { appCommandLineLinkPath = original })
				}
				repo := t.TempDir()
				target := bundle.ExecutablePath
				if targetKind == "invocation link" || targetKind == "direct bundle standard link" {
					target = link
				}

				if targetKind == "invocation parent alias" {
					parent := filepath.Join(repo, "global-directory")
					require.NoError(t, os.Symlink(filepath.Dir(link), parent))
					target = filepath.Join(parent, filepath.Base(link))
				}
				if strings.HasPrefix(targetKind, "parent link") {
					parent := filepath.Join(repo, "app-directory")
					require.NoError(t, os.Symlink(filepath.Dir(bundle.ExecutablePath), parent))
					target = filepath.Join(parent, "rzm")
					if targetKind == "parent link missing directory" {
						target = filepath.Join(parent, "new-directory", "rzm")
					}
				}
				if safeRepoLink {
					target = filepath.Join(repo, "repo-rzm")
					require.NoError(t, os.Symlink(bundle.ExecutablePath, target))
				}
				require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.0.0", BinaryPath: target}}))
				artifact := filepath.Join(t.TempDir(), "replacement-rzm")
				require.NoError(t, os.WriteFile(artifact, []byte("repo replacement"), 0o755))
				deps := defaultRunDependencies()
				deps.prepare = func(context.Context, *http.Client, string, Manifest, string) (preparedArtifact, error) {
					require.True(t, safeRepoLink, "unsafe target must fail before artifact preparation")
					return preparedArtifact{path: artifact}, nil
				}
				appDeps := defaultAppUpdateDependencies()
				appDeps.feedURL = "https://app.invalid/latest.json"
				appDeps.launch = func(context.Context, string, ...string) error { t.Fatal("current app must not launch"); return nil }
				client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Host == "app.invalid" {
						return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":"1.0.0"}`))}, nil
					}
					return manifestClient("v3.0.0").Transport.RoundTrip(req)
				})}
				opts := Options{ExecutablePath: link, WorkDir: repo, Yes: true, ManifestURL: "https://cli.invalid/latest", HTTPClient: client, deps: &deps, appDeps: &appDeps}
				if directBundle {
					opts.ExecutablePath = bundle.ExecutablePath
				}
				switch mode {
				case UpdateModePinned:
					opts.Pinned = true
				case UpdateModeSetVersion:
					opts.SetVersion = "v2.0.0"
				case UpdateModeLatest:
					opts.Latest = true
				}
				result, err := Run(context.Background(), opts)
				if safeRepoLink {
					require.NoError(t, err)
					info, err := os.Lstat(target)
					require.NoError(t, err)
					require.Zero(t, info.Mode()&os.ModeSymlink)
					content, err := os.ReadFile(target)
					require.NoError(t, err)
					require.Equal(t, "repo replacement", string(content))
					require.True(t, result.Targets[len(result.Targets)-1].BinaryUpdated)
				} else {
					require.ErrorContains(t, err, "owned by the app")
					require.ErrorContains(t, err, target)
					if targetKind == "parent link missing directory" {
						require.NoDirExists(t, filepath.Dir(target))
					} else {
						require.FileExists(t, target)
					}
					cfg, err := obsidian.LoadLocalConfig(repo)
					require.NoError(t, err)
					require.Equal(t, "v1.0.0", cfg.Rhizome.Version)
				}
				assertAppLinkIntact(t, link, bundle.ExecutablePath)
			})
		}
	}
}

func TestBuildUpdatePlanRefusesBundleRepositoryTargetFromStandaloneCLI(t *testing.T) {
	_, err := BuildUpdatePlan(Invocation{ExecutablePath: "/usr/local/bin/rzm", RepoPinned: true, RepoPinVersion: "v1.0.0", RepoTargetPath: "/Applications/Rhizome.app/Contents/MacOS/rzm"}, UpdatePlanOptions{Mode: UpdateModePinned})
	require.ErrorContains(t, err, "owned by the app")
}
