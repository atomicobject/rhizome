package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func testService(t *testing.T) *Service {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "desktop"))
	require.NoError(t, err)
	s.home = t.TempDir()
	s.trust = repositorytrust.Store{Dir: filepath.Join(t.TempDir(), "trust")}
	s.ensure = func(context.Context, appruntime.EnsureOptions) (appruntime.EnsureResult, error) {
		t.Fatal("unexpected runtime start")
		return appruntime.EnsureResult{}, nil
	}
	s.ensurePinned = func(context.Context, appupdate.EnsureOptions) error { t.Fatal("unexpected download"); return nil }
	s.install = func(context.Context, appupdate.Options) (appupdate.Result, error) {
		t.Fatal("unexpected global installation")
		return appupdate.Result{}, nil
	}
	return s
}
func config(t *testing.T, cfg obsidian.LocalRhizomeConfig) string {
	t.Helper()
	folder := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(folder, obsidian.LocalConfig{Rhizome: cfg}))
	canonical, err := repositorytrust.CanonicalCheckout(folder)
	require.NoError(t, err)
	return canonical
}
func script(t *testing.T, path, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("executable shell fixture requires Unix")
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	return path
}
func workingScript(t *testing.T, path string) string {
	return script(t, path, "case \"$1\" in --version) echo 'rzm version v1.2.3';; serve) echo 'serve --headless';; *) exit 2;; esac")
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var p *Problem
	require.ErrorAs(t, err, &p)
	require.Equal(t, want, p.Code)
}

func TestInspectIsInertAndTrustPrecedesAllExecution(t *testing.T) {
	for _, authority := range []string{"development", "managed", "external", "global"} {
		t.Run(authority, func(t *testing.T) {
			s := testService(t)
			cfg := obsidian.LocalRhizomeConfig{}
			switch authority {
			case "development":
				cfg.DevBinaryDir = "dev"
			case "managed":
				cfg.Version = "v1.2.3"
			case "external":
				cfg.BinaryManager = "external"
			}
			folder := config(t, cfg)
			sentinel := filepath.Join(folder, "executed")
			target := filepath.Join(folder, "dev", runtime.GOOS, repoexec.ExecutableName())
			if authority == "managed" {
				target = filepath.Join(folder, ".rhizome", "bin", repoexec.PlatformDir(), repoexec.ExecutableName())
			}
			script(t, target, "touch '"+sentinel+"'; echo 'rzm version v1.2.3'")
			info, err := s.Inspect(folder)
			require.NoError(t, err)
			require.Equal(t, authority, info.Authority)
			require.True(t, info.Configured)
			if authority == "development" {
				require.Equal(t, target, info.Executable)
			} else {
				require.Empty(t, info.Executable)
			}
			require.NoFileExists(t, sentinel)
			require.NoDirExists(t, s.trust.Dir)
			require.NoDirExists(t, s.stateDir)
			if authority == "development" || authority == "managed" {
				_, err = s.Open(context.Background(), Request{Folder: folder})
				code(t, err, "trust_required")
				require.NoFileExists(t, sentinel)
			}
		})
	}
}

func TestFolderIdentityAndCanonicalTrust(t *testing.T) {
	s := testService(t)
	folder := config(t, obsidian.LocalRhizomeConfig{Version: "v1.2.3"})
	nested := filepath.Join(folder, "nested")
	require.NoError(t, os.Mkdir(nested, 0o755))
	root, err := s.Inspect(folder)
	require.NoError(t, err)
	child, err := s.Inspect(nested)
	require.NoError(t, err)
	require.Equal(t, root.ID, child.ID)
	require.Equal(t, folder, child.Path)
	_, err = s.Trust(nested)
	code(t, err, "invalid_request")
	trusted, err := s.Trust(folder)
	require.NoError(t, err)
	require.True(t, trusted.Trusted)
	require.False(t, trusted.TrustRequired)
	_, err = s.Inspect("relative")
	code(t, err, "invalid_request")
	_, err = s.Inspect(filepath.Join(folder, "gone"))
	code(t, err, "folder_missing")
}

func TestOpenPassesExactSelectedBinaryAndBuildToRuntime(t *testing.T) {
	for _, authority := range []string{"development", "managed", "external", "global"} {
		t.Run(authority, func(t *testing.T) {
			s := testService(t)
			cfg := obsidian.LocalRhizomeConfig{}
			switch authority {
			case "development":
				cfg.DevBinaryDir = "dev"
			case "managed":
				cfg.Version = "v1.2.3"
			case "external":
				cfg.BinaryManager = "external"
			}
			folder := config(t, cfg)
			req := Request{Folder: folder}
			target := filepath.Join(folder, "chosen-rzm")
			switch authority {
			case "development":
				target = filepath.Join(folder, "dev", runtime.GOOS, repoexec.ExecutableName())
			case "managed":
				target = filepath.Join(folder, ".rhizome", "bin", repoexec.PlatformDir(), repoexec.ExecutableName())
			case "external":
				req.Executable = target
			case "global":
				req.GlobalExecutable = target
			}
			workingScript(t, target)
			if authority == "managed" || authority == "development" {
				_, err := s.Trust(folder)
				require.NoError(t, err)
			}
			prepared := false
			s.ensurePinned = func(_ context.Context, opts appupdate.EnsureOptions) error {
				require.Equal(t, folder, opts.CfgDir)
				require.Equal(t, "v1.2.3", opts.Config.Rhizome.Version)
				prepared = true
				return nil
			}
			s.ensure = func(_ context.Context, opts appruntime.EnsureOptions) (appruntime.EnsureResult, error) {
				require.Equal(t, target, opts.Executable)
				require.Equal(t, folder, opts.VaultPath)
				if authority == "external" {
					require.Empty(t, opts.BuildID)
				} else {
					require.Equal(t, appruntime.BuildID("v1.2.3", target), opts.BuildID)
				}
				require.True(t, opts.Autostart)
				require.True(t, opts.Wait)
				if authority == "managed" {
					require.True(t, prepared)
				}
				return appruntime.EnsureResult{Spawned: true, Client: &appruntime.Client{Manifest: appruntime.InstanceManifest{HTTPURL: "http://127.0.0.1:4567", ControlToken: "synthetic-control-secret"}}, Health: appruntime.Health{VaultPath: folder, Version: "v1.2.3", BuildID: opts.BuildID, PID: 123, Mode: appruntime.ModeHeadless}}, nil
			}
			result, err := s.Open(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, "http://127.0.0.1:4567", result.URL)
			require.True(t, result.Spawned)
			data, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(data), "synthetic-control-secret")
		})
	}
}

func TestOpenRejectsUnsupportedBuildBeforeSpawn(t *testing.T) {
	s := testService(t)
	folder := config(t, obsidian.LocalRhizomeConfig{})
	target := script(t, filepath.Join(t.TempDir(), "rzm"), "echo 'rzm version v0.1.0'")
	_, err := s.Open(context.Background(), Request{Folder: folder, GlobalExecutable: target})
	code(t, err, "runtime_error")
	require.Contains(t, err.Error(), "serve --headless")
}

func TestExternalAttachesVerifiedRuntimeWithoutExecutable(t *testing.T) {
	s := testService(t)
	folder := config(t, obsidian.LocalRhizomeConfig{BinaryManager: "external"})
	health := appruntime.Health{VaultPath: folder, PID: 321, RunID: "fixture-run", Mode: appruntime.ModeAttached, Version: "v1.2.3"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, appruntime.HealthPath, r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		require.NoError(t, json.NewEncoder(w).Encode(health))
	}))
	defer server.Close()
	require.NoError(t, appruntime.WriteManifest(folder, appruntime.InstanceManifest{VaultPath: folder, HTTPURL: server.URL, PID: health.PID, RunID: health.RunID, ControlToken: "synthetic-secret"}))
	result, err := s.Open(context.Background(), Request{Folder: folder})
	require.NoError(t, err)
	require.Equal(t, server.URL, result.URL)
	require.False(t, result.Spawned)
	health.VaultPath = t.TempDir()
	_, err = s.Open(context.Background(), Request{Folder: folder})
	code(t, err, "runtime_error")
}

func TestOpenRejectsRemoteAndForeignManifestsBeforeProbe(t *testing.T) {
	s := testService(t)
	folder := config(t, obsidian.LocalRhizomeConfig{BinaryManager: "external"})
	for _, m := range []appruntime.InstanceManifest{{VaultPath: folder, HTTPURL: "http://example.invalid:1234"}, {VaultPath: t.TempDir(), HTTPURL: "http://127.0.0.1:1234"}} {
		require.NoError(t, appruntime.WriteManifest(folder, m))
		_, err := s.Open(context.Background(), Request{Folder: folder})
		code(t, err, "runtime_error")
	}
}

func TestRuntimeOriginRejectsUnexpectedAddresses(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:1234", "http://localhost:1234", "http://127.0.0.1", "http://u:p@127.0.0.1:1", "http://127.0.0.1:1234/path", "http://127.0.0.1:1234?q=1", "http://127.0.0.1:1234#fragment"} {
		_, err := runtimeOrigin(raw)
		require.Error(t, err, raw)
	}
	for _, raw := range []string{"http://127.0.0.1:1234", "http://[::1]:1234"} {
		got, err := runtimeOrigin(raw)
		require.NoError(t, err)
		require.Equal(t, raw, got)
	}
}

func TestProtocolRejectsMalformedAndOversizedRequests(t *testing.T) {
	s := testService(t)
	for _, input := range []string{`{}`, `{"protocol":2,"operation":"inspect"}`, `{"protocol":1,"operation":"unknown"}`, `{"protocol":1,"operation":"inspect","surprise":true}`, `{} {}`, strings.Repeat("x", MaxRequestBytes+1)} {
		var output bytes.Buffer
		require.Error(t, Serve(context.Background(), s, strings.NewReader(input), &output))
		var response Response
		require.NoError(t, json.Unmarshal(output.Bytes(), &response))
		require.Equal(t, Protocol, response.Protocol)
		require.Equal(t, "invalid_request", response.Error.Code)
	}
}

func TestRuntimeFailureAndCancellationRemainErrors(t *testing.T) {
	s := testService(t)
	folder := config(t, obsidian.LocalRhizomeConfig{})
	target := workingScript(t, filepath.Join(t.TempDir(), "rzm"))
	s.ensure = func(ctx context.Context, _ appruntime.EnsureOptions) (appruntime.EnsureResult, error) {
		return appruntime.EnsureResult{}, context.Canceled
	}
	_, err := s.Open(context.Background(), Request{Folder: folder, GlobalExecutable: target})
	code(t, err, "runtime_error")
	require.ErrorContains(t, err, "canceled")
	s.ensure = func(context.Context, appruntime.EnsureOptions) (appruntime.EnsureResult, error) {
		return appruntime.EnsureResult{}, appruntime.ErrAttachedMismatch
	}
	_, err = s.Open(context.Background(), Request{Folder: folder, GlobalExecutable: target})
	code(t, err, "runtime_error")
}

func TestInitializeRequiresExplicitUnconfiguredFolder(t *testing.T) {
	s := testService(t)
	folder := t.TempDir()
	target := script(t, filepath.Join(t.TempDir(), "rzm"), `[ "$1" = init ] && [ "$2" = --path ] && [ "$3" = "$PWD" ] || exit 3
mkdir -p "$3/.rhizome"
printf 'rhizome:\n  version: v1.2.3\n' > "$3/.rhizome/config.yml"`)
	result, err := s.Initialize(context.Background(), Request{Folder: folder, GlobalExecutable: target})
	require.NoError(t, err)
	require.True(t, result.Configured)
	_, err = s.Initialize(context.Background(), Request{Folder: folder, GlobalExecutable: target})
	code(t, err, "invalid_request")
}

func TestGlobalStatusUsesNeutralCwdAndProtectsExternalInstallations(t *testing.T) {
	s := testService(t)
	target := script(t, s.managedPath(), `[ ! -f .rhizome/config.yml ] || exit 4
echo 'rzm version v1.2.3'`)
	status, err := s.GlobalStatus(context.Background(), target)
	require.NoError(t, err)
	require.True(t, status.CanUpdate)
	require.True(t, status.Installed)
	external := workingScript(t, filepath.Join(t.TempDir(), "rzm"))
	status, err = s.GlobalStatus(context.Background(), external)
	require.NoError(t, err)
	require.False(t, status.CanUpdate)
	s.home = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Dir(s.managedPath()), 0o755))
	require.NoError(t, os.Symlink(external, s.managedPath()))
	status, err = s.GlobalStatus(context.Background(), "")
	require.NoError(t, err)
	require.False(t, status.CanUpdate)
	_, err = s.InstallGlobal(context.Background())
	code(t, err, "install_error")
}

func TestGlobalInstallUsesOnlyOwnedTargetAndReportsPartialCompletion(t *testing.T) {
	s := testService(t)
	s.install = func(_ context.Context, opts appupdate.Options) (appupdate.Result, error) {
		require.Equal(t, s.managedPath(), opts.ExecutablePath)
		require.True(t, opts.Latest)
		require.True(t, opts.Yes)
		cwd, err := os.Getwd()
		require.NoError(t, err)
		require.NotEqual(t, opts.WorkDir, cwd)
		_, _, err = obsidian.FindLocalConfigForDelegation(opts.WorkDir)
		require.ErrorIs(t, err, obsidian.ErrNoLocalConfig)
		workingScript(t, opts.ExecutablePath)
		return appupdate.Result{Targets: []appupdate.TargetResult{{Path: opts.ExecutablePath, Version: "v1.2.3", BinaryUpdated: true}}}, errors.New("synthetic later failure")
	}
	result, err := s.InstallGlobal(context.Background())
	code(t, err, "install_error")
	require.True(t, result.Installed)
	require.Equal(t, "v1.2.3", result.Version)
	require.Contains(t, err.Error(), "later installation step failed")
}
