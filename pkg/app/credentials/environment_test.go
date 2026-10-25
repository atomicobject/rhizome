package credentials

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func sourceFixture() repoconfig.CredentialSource {
	return repoconfig.CredentialSource{Account: "example.1password.com", Environment: strings.Repeat("a", 26), Keys: []string{"VOYAGE_API_KEY", "TYPESAFE_API_KEY"}}
}

func TestCaptureValidatesAllKeysBeforeWriting(t *testing.T) {
	for _, keys := range [][]string{nil, {"PATH"}, {"VOYAGE_API_KEY", "PATH"}, {"VOYAGE_API_KEY", "VOYAGE_API_KEY"}} {
		var output bytes.Buffer
		handled, code := Capture(append([]string{"credentials-capture"}, keys...), func(key string) (string, bool) {
			return map[string]string{"VOYAGE_API_KEY": "synthetic-voyage"}[key], true
		}, &output)
		require.True(t, handled)
		require.Equal(t, 1, code)
		require.Empty(t, output.String())
	}
	var output bytes.Buffer
	handled, code := Capture([]string{"credentials-capture", "VOYAGE_API_KEY"}, func(string) (string, bool) { return "synthetic-voyage", true }, &output)
	require.True(t, handled)
	require.Zero(t, code)
	require.JSONEq(t, `{"VOYAGE_API_KEY":"synthetic-voyage"}`, output.String())
}

func TestEnvironmentRejectsUnsupportedKeysAndCLIWithoutSaving(t *testing.T) {
	for _, version := range []string{"2.40.9", "2.41.0-beta.01", "garbage"} {
		t.Run(version, func(t *testing.T) {
			reader := OnePasswordCLI{Binary: "synthetic-op", Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
				require.Equal(t, []string{"--version"}, args)
				return []byte(version), nil
			}}
			_, err := reader.ReadEnvironment(context.Background(), sourceFixture())
			require.Error(t, err)
		})
	}
	reader := OnePasswordCLI{Run: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("must validate before op")
		return nil, nil
	}}
	source := sourceFixture()
	source.Keys = []string{"VOYAGE_API_KEY=op://vault/item/field"}
	_, err := reader.ReadEnvironment(context.Background(), source)
	require.Error(t, err)
}

func TestEnvironmentImportPreservesConfigAndRemembersSourceAtomically(t *testing.T) {
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(t.TempDir(), "config.yml")
	original := repoconfig.CliConfig{DefaultVaultName: "existing", Env: map[string]string{"OPENAI_API_KEY": "synthetic-existing"}, CredentialSkips: map[string]bool{"VOYAGE_API_KEY": true, "CEREBRAS_API_KEY": true}}
	require.NoError(t, repoconfig.SaveCliConfig(env, original))
	ctx := vaultconfig.WithEnv(context.Background(), env)
	source := sourceFixture()
	reader := OnePasswordCLI{Binary: "synthetic-op", Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "--version" {
			return []byte("2.41.1"), nil
		}
		require.Equal(t, []string{"run", "--account", source.Account, "--environment", source.Environment, "--no-masking", "--"}, args[:7])
		require.Equal(t, []string{"credentials-capture", "VOYAGE_API_KEY", "TYPESAFE_API_KEY"}, args[8:])
		return []byte(`{"VOYAGE_API_KEY":"synthetic-voyage","TYPESAFE_API_KEY":"synthetic-typesafe","GITHUB_TOKEN":"synthetic-unrequested"}`), nil
	}}
	n, err := importEnvironment(ctx, reader, source)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	cfg, err := repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Equal(t, &source, cfg.CredentialSource)
	require.Equal(t, "existing", cfg.DefaultVaultName)
	require.Equal(t, "synthetic-existing", cfg.Env["OPENAI_API_KEY"])
	require.NotContains(t, cfg.Env, "GITHUB_TOKEN")
	require.False(t, cfg.CredentialSkips["VOYAGE_API_KEY"])
	require.True(t, cfg.CredentialSkips["CEREBRAS_API_KEY"])
	if runtime.GOOS != "windows" {
		stat, err := os.Stat(env.CLIConfigFile)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), stat.Mode().Perm())
	}
	before, err := os.ReadFile(env.CLIConfigFile)
	require.NoError(t, err)
	for _, response := range []string{`{"VOYAGE_API_KEY":"synthetic-new"}`, `synthetic-secret`, strings.Repeat("x", maxCaptureBytes+1)} {
		reader.Run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "--version" {
				return []byte("2.41.0"), nil
			}
			return []byte(response), nil
		}
		changed := source
		changed.Account = "changed.1password.com"
		_, err = importEnvironment(ctx, reader, changed)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "synthetic-secret")
		after, readErr := os.ReadFile(env.CLIConfigFile)
		require.NoError(t, readErr)
		require.Equal(t, before, after)
	}
}

func TestOnePasswordSubprocessStripsAmbientKeysBoundsOutputAndCancels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX subprocess fixture")
	}
	binary := filepath.Join(t.TempDir(), "op")
	require.NoError(t, os.WriteFile(binary, []byte(`#!/bin/sh
case "$1" in
 --version) printf '2.41.0';;
 run)
  test -z "${VOYAGE_API_KEY-}${RHIZOME_VOYAGE_API_KEY-}${TYPESAFE_API_KEY-}${OPENAI_API_KEY-}${ATOMIC_RHIZOME_KEY-}" || exit 2
  printf '{"VOYAGE_API_KEY":"synthetic-subprocess","TYPESAFE_API_KEY":"synthetic-typesafe"}'
 ;;
 stall) exec /bin/sleep 10;;
 huge) /usr/bin/head -c 100000 /dev/zero;;
 fail) printf 'synthetic-secret'; printf 'synthetic-secret' >&2; exit 1;;
esac
`), 0700))
	for _, key := range []string{"VOYAGE_API_KEY", "RHIZOME_VOYAGE_API_KEY", "TYPESAFE_API_KEY", "OPENAI_API_KEY", AtomicRhizomeKey} {
		t.Setenv(key, "synthetic-stale")
	}
	reader := OnePasswordCLI{Binary: binary}
	values, err := reader.ReadEnvironment(context.Background(), sourceFixture())
	require.NoError(t, err)
	require.Equal(t, "synthetic-subprocess", values["VOYAGE_API_KEY"])
	_, err = reader.run(context.Background(), "fail")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "synthetic-secret")
	_, err = reader.run(context.Background(), "huge")
	require.Error(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = reader.run(ctx, "stall")
	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestImportCancellationDoesNotSave(t *testing.T) {
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(t.TempDir(), "config.yml")
	ctx, cancel := context.WithCancel(vaultconfig.WithEnv(context.Background(), env))
	reader := OnePasswordCLI{Binary: "synthetic-op", Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "--version" {
			return []byte("2.41.0"), nil
		}
		cancel()
		return []byte(`{"VOYAGE_API_KEY":"synthetic-voyage","TYPESAFE_API_KEY":"synthetic-typesafe"}`), nil
	}}
	_, err := importEnvironment(ctx, reader, sourceFixture())
	require.Error(t, err)
	_, err = os.Stat(env.CLIConfigFile)
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestCaptureSubprocess(t *testing.T) {
	if os.Getenv("RZM_SYNTHETIC_CAPTURE_TEST") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				_, code := Capture(os.Args[i+1:], os.LookupEnv, os.Stdout)
				os.Exit(code)
			}
		}
		os.Exit(1)
	}
	for _, test := range []struct {
		keys     []string
		ok       bool
		expected string
	}{
		{[]string{"VOYAGE_API_KEY", "TYPESAFE_API_KEY"}, true, `{"VOYAGE_API_KEY":"synthetic-subprocess"}`},
		{[]string{"VOYAGE_API_KEY", "PATH"}, false, ""},
	} {
		command := exec.Command(os.Args[0], append([]string{"-test.run=^TestCaptureSubprocess$", "--", "credentials-capture"}, test.keys...)...)
		command.Env = []string{"RZM_SYNTHETIC_CAPTURE_TEST=1", "VOYAGE_API_KEY=synthetic-subprocess"}
		output, err := command.CombinedOutput()
		if test.ok {
			require.NoError(t, err)
			require.JSONEq(t, test.expected, string(output))
		} else {
			require.Error(t, err)
			require.Empty(t, output)
		}
	}
}
