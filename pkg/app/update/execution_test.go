package update

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunPreparesEveryArtifactBeforeMutation(t *testing.T) {
	repo, global, _ := updateExecutionFixture(t, "v0.50.0")
	events := []string{}
	deps := executionDependencies()
	deps.prepare = func(_ context.Context, _ *http.Client, _ string, _ Manifest, version string) (preparedArtifact, error) {
		events = append(events, "prepare "+version)
		return preparedArtifact{}, errors.New("broken artifact")
	}
	deps.replace = func(_, target string) error {
		events = append(events, "replace "+target)
		return nil
	}

	result, err := Run(context.Background(), Options{
		ManifestURL:    "https://releases.invalid/latest.json",
		Latest:         true,
		ExecutablePath: global,
		WorkDir:        repo,
		HTTPClient:     manifestClient("v0.51.0"),
		deps:           &deps,
	})

	require.ErrorContains(t, err, "prepare")
	require.Empty(t, result.Targets)
	require.Equal(t, []string{"prepare v0.51.0"}, events)
}

func TestRunReportsGlobalSuccessWhenRepoReplacementFails(t *testing.T) {
	repo, global, repoTarget := updateExecutionFixture(t, "v0.50.0")
	deps := executionDependencies()
	deps.replace = func(_, target string) error {
		if target == repoTarget {
			return errors.New("repo destination is read-only")
		}
		return nil
	}

	result, err := Run(context.Background(), Options{
		ManifestURL:    "https://releases.invalid/latest.json",
		Latest:         true,
		ExecutablePath: global,
		WorkDir:        repo,
		HTTPClient:     manifestClient("v0.51.0"),
		deps:           &deps,
	})

	require.ErrorContains(t, err, "repo")
	require.ErrorContains(t, err, repoTarget)
	require.Equal(t, []TargetResult{
		{Role: UpdateTargetGlobal, Path: global, Version: "v0.51.0", BinaryUpdated: true},
		{Role: UpdateTargetRepo, Path: repoTarget, Version: "v0.51.0"},
	}, result.Targets)
	require.Equal(t, []string{"Updated global " + global + " to Rhizome v0.51.0"}, result.Summaries())
}

func TestRunStopsBeforeRepoWhenGlobalReplacementFails(t *testing.T) {
	repo, global, repoTarget := updateExecutionFixture(t, "v0.50.0")
	replaced := []string{}
	deps := executionDependencies()
	deps.replace = func(_, target string) error {
		replaced = append(replaced, target)
		if target == global {
			return errors.New("global destination is read-only")
		}
		return nil
	}

	result, err := Run(context.Background(), Options{
		ManifestURL: "https://releases.invalid/latest.json", Latest: true,
		ExecutablePath: global, WorkDir: repo, HTTPClient: manifestClient("v0.51.0"), deps: &deps,
	})

	require.ErrorContains(t, err, "global")
	require.ErrorContains(t, err, global)
	require.Equal(t, []string{global}, replaced)
	require.NotContains(t, replaced, repoTarget)
	require.Equal(t, []TargetResult{{Role: UpdateTargetGlobal, Path: global, Version: "v0.51.0"}}, result.Targets)
}

func TestRunRejectsRepoOnlyModesBeforeManifestFetch(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{name: "pinned", opts: Options{Pinned: true}},
		{name: "set version", opts: Options{SetVersion: "v0.49.0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requested := false
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				requested = true
				return nil, errors.New("unexpected request")
			})}
			tc.opts.ExecutablePath = filepath.Join(t.TempDir(), executableName())
			tc.opts.WorkDir = t.TempDir()
			tc.opts.HTTPClient = client

			result, err := Run(context.Background(), tc.opts)

			require.Error(t, err)
			require.Empty(t, result.Targets)
			require.False(t, requested)
		})
	}
}

func TestRunRepoOnlyModesLeaveGlobalExecutableUntouched(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{name: "pinned", opts: Options{Pinned: true}},
		{name: "set version", opts: Options{SetVersion: "v0.49.0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, global, repoTarget := updateExecutionFixture(t, "v0.50.0")
			replaced := []string{}
			deps := executionDependencies()
			deps.replace = func(_, target string) error {
				replaced = append(replaced, target)
				return nil
			}
			tc.opts.ManifestURL = "https://releases.invalid/latest.json"
			tc.opts.ExecutablePath = global
			tc.opts.WorkDir = repo
			tc.opts.HTTPClient = manifestClient("v0.51.0")
			tc.opts.deps = &deps

			_, err := Run(context.Background(), tc.opts)

			require.NoError(t, err)
			require.Equal(t, []string{repoTarget}, replaced)
			require.NotContains(t, replaced, global)
		})
	}
}

func TestRunExactRepoModesDoNotFetchLatestRelease(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{name: "pinned", opts: Options{Pinned: true}},
		{name: "set version", opts: Options{SetVersion: "v0.49.0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, global, _ := updateExecutionFixture(t, "v0.50.0")
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests++
				return nil, errors.New("unexpected latest request")
			})}
			deps := executionDependencies()
			deps.prepare = func(_ context.Context, _ *http.Client, _ string, latest Manifest, version string) (preparedArtifact, error) {
				require.Empty(t, latest.Version)
				require.NotEmpty(t, version)
				return preparedArtifact{path: "prepared-" + version}, nil
			}
			tc.opts.ManifestURL = "https://releases.invalid/latest"
			tc.opts.ExecutablePath = global
			tc.opts.WorkDir = repo
			tc.opts.HTTPClient = client
			tc.opts.deps = &deps

			_, err := Run(context.Background(), tc.opts)

			require.NoError(t, err)
			require.Zero(t, requests)
		})
	}
}

func TestRunDirectRepoDefaultDeclineDoesNotMutate(t *testing.T) {
	repo, _, repoTarget := updateExecutionFixture(t, "v0.50.0")
	require.NoError(t, os.MkdirAll(filepath.Dir(repoTarget), 0o755))
	require.NoError(t, os.WriteFile(repoTarget, []byte("current"), 0o755))
	replaced := []string{}
	deps := executionDependencies()
	deps.replace = func(_, target string) error {
		replaced = append(replaced, target)
		return nil
	}

	result, err := Run(context.Background(), Options{
		ManifestURL: "https://releases.invalid/latest.json", ExecutablePath: repoTarget, WorkDir: repo,
		Stdin: strings.NewReader("no\n"), HTTPClient: manifestClient("v0.51.0"), deps: &deps,
	})

	require.NoError(t, err)
	require.Empty(t, result.Targets)
	require.Empty(t, replaced)
}

func TestRunCommitsRepoMetadataAfterBinaryInOrder(t *testing.T) {
	repo, global, repoTarget := updateExecutionFixture(t, "v0.50.0")
	events := []string{}
	deps := executionDependencies()
	deps.replace = func(_, target string) error { events = append(events, "binary "+target); return nil }
	deps.writeMarker = func(target, _ string) error { events = append(events, "marker "+target); return nil }
	deps.saveConfig = func(dir string, _ obsidian.LocalConfig) error { events = append(events, "pin "+dir); return nil }

	result, err := Run(context.Background(), Options{
		ManifestURL:    "https://releases.invalid/latest.json",
		SetVersion:     "v0.49.0",
		ExecutablePath: global,
		WorkDir:        repo,
		HTTPClient:     manifestClient("v0.51.0"),
		deps:           &deps,
	})

	require.NoError(t, err)
	require.Equal(t, []string{"binary " + repoTarget, "marker " + repoTarget, "pin " + filepath.ToSlash(repo)}, events)
	require.Equal(t, []TargetResult{{
		Role: UpdateTargetRepo, Path: repoTarget, Version: "v0.49.0",
		BinaryUpdated: true, MarkerWritten: true, PinWritten: true,
	}}, result.Targets)
}

func TestRunMetadataFailuresReturnTruthfulPartialResult(t *testing.T) {
	tests := []struct {
		name       string
		failMarker bool
		want       TargetResult
		wantEvents []string
	}{
		{name: "marker failure leaves pin unwritten", failMarker: true, want: TargetResult{BinaryUpdated: true}, wantEvents: []string{"binary", "marker"}},
		{name: "pin failure retains binary and marker completion", want: TargetResult{BinaryUpdated: true, MarkerWritten: true}, wantEvents: []string{"binary", "marker", "pin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, global, repoTarget := updateExecutionFixture(t, "v0.50.0")
			events := []string{}
			deps := executionDependencies()
			deps.replace = func(_, _ string) error { events = append(events, "binary"); return nil }
			deps.writeMarker = func(_, _ string) error {
				events = append(events, "marker")
				if tt.failMarker {
					return errors.New("marker denied")
				}
				return nil
			}
			deps.saveConfig = func(_ string, _ obsidian.LocalConfig) error {
				events = append(events, "pin")
				return errors.New("pin denied")
			}

			result, err := Run(context.Background(), Options{
				ManifestURL: "https://releases.invalid/latest.json", Latest: true,
				ExecutablePath: global, WorkDir: repo, HTTPClient: manifestClient("v0.51.0"), deps: &deps,
			})

			require.Error(t, err)
			require.Len(t, result.Targets, 2)
			got := result.Targets[1]
			tt.want.Role, tt.want.Path, tt.want.Version = UpdateTargetRepo, repoTarget, "v0.51.0"
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantEvents, events[1:])
		})
	}
}

func TestRunReportsPinWrittenWhenConfigSavePersistsPinBeforeFailing(t *testing.T) {
	repo, global, repoTarget := updateExecutionFixture(t, "v0.50.0")
	deps := executionDependencies()
	deps.saveConfig = func(dir string, cfg obsidian.LocalConfig) error {
		require.NoError(t, obsidian.SaveLocalConfig(dir, cfg))
		return errors.New("workflow metadata denied")
	}

	result, err := Run(context.Background(), Options{
		ManifestURL: "https://releases.invalid/latest.json", Latest: true,
		ExecutablePath: global, WorkDir: repo, HTTPClient: manifestClient("v0.51.0"), deps: &deps,
	})

	require.ErrorContains(t, err, "workflow metadata denied")
	require.Len(t, result.Targets, 2)
	require.Equal(t, TargetResult{
		Role: UpdateTargetRepo, Path: repoTarget, Version: "v0.51.0",
		BinaryUpdated: true, MarkerWritten: true, PinWritten: true,
	}, result.Targets[1])
}

func TestRunDefaultPromptControlsRepoMutationAndNamesTarget(t *testing.T) {
	tests := []struct {
		name        string
		answer      string
		wantTargets int
		wantRepo    bool
	}{
		{name: "accept", answer: "yes\n", wantTargets: 2, wantRepo: true},
		{name: "decline", answer: "no\n", wantTargets: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, global, repoTarget := updateExecutionFixture(t, "v0.50.0")
			deps := executionDependencies()
			var prompt strings.Builder
			result, err := Run(context.Background(), Options{
				ManifestURL: "https://releases.invalid/latest.json", ExecutablePath: global, WorkDir: repo,
				Stdin: strings.NewReader(tt.answer), Stdout: &prompt, HTTPClient: manifestClient("v0.51.0"), deps: &deps,
			})

			require.NoError(t, err)
			require.Len(t, result.Targets, tt.wantTargets)
			require.Contains(t, prompt.String(), "v0.50.0")
			require.Contains(t, prompt.String(), "v0.51.0")
			require.Contains(t, prompt.String(), repoTarget)
			if tt.wantRepo {
				require.Equal(t, UpdateTargetRepo, result.Targets[1].Role)
			}
		})
	}
}

func executionDependencies() runDependencies {
	return runDependencies{
		prepare: func(_ context.Context, _ *http.Client, _ string, _ Manifest, version string) (preparedArtifact, error) {
			return preparedArtifact{path: "prepared-" + version}, nil
		},
		replace:     func(_, _ string) error { return nil },
		writeMarker: func(_, _ string) error { return nil },
		saveConfig:  func(_ string, _ obsidian.LocalConfig) error { return nil },
	}
}

func updateExecutionFixture(t *testing.T, pin string) (repo, global, repoTarget string) {
	t.Helper()
	repo = t.TempDir()
	canonicalRepo, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	repo = canonicalRepo
	global = filepath.Join(t.TempDir(), executableName())
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{Version: pin}}))
	repoTarget = filepath.Join(repo, ".rhizome", "bin", CurrentPlatformDir(), executableName())
	return repo, global, repoTarget
}

func manifestClient(version string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := testSHA256 + "  " + ArtifactObjectName(runtime.GOOS, runtime.GOARCH) + "\n"
		if !strings.HasSuffix(req.URL.Path, "/checksums.txt") {
			body = `{"tag_name":"` + version + `","draft":false,"prerelease":false,"assets":[` +
				`{"name":"` + ArtifactObjectName(runtime.GOOS, runtime.GOARCH) + `","browser_download_url":"https://artifact.invalid/rzm.tar.gz"},` +
				`{"name":"checksums.txt","browser_download_url":"https://artifact.invalid/checksums.txt"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }
