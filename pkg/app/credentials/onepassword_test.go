package credentials

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type fakeOnePasswordReader struct {
	values map[string]string
	errs   map[string]error
	calls  []string
}

func (r *fakeOnePasswordReader) Read(_ context.Context, account, reference string) (string, error) {
	r.calls = append(r.calls, account+":"+reference)
	if err := r.errs[reference]; err != nil {
		return "", err
	}
	return r.values[reference], nil
}

func TestImportRejectsMappingsBeforeReadingOnePassword(t *testing.T) {
	reader := &fakeOnePasswordReader{}
	_, err := Import(context.Background(), reader, "example.1password.com", []string{"UNSUPPORTED_KEY=op://Private/item/credential"}, func(bool) (obsidian.CliConfig, error) {
		return obsidian.CliConfig{}, nil
	}, func(obsidian.CliConfig) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported credential key")
	require.Empty(t, reader.calls)
}

func TestImportRejectsInvalidReferencesBeforeReadingOnePassword(t *testing.T) {
	reader := &fakeOnePasswordReader{}
	_, err := Import(context.Background(), reader, "example.1password.com", []string{"VOYAGE_API_KEY=not-a-reference"}, func(bool) (obsidian.CliConfig, error) {
		return obsidian.CliConfig{}, nil
	}, func(obsidian.CliConfig) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid 1Password reference")
	require.Empty(t, reader.calls)
}

func TestImportAllowsQuotedOnePasswordReferencesWithSpaces(t *testing.T) {
	reader := &fakeOnePasswordReader{values: map[string]string{
		"op://Private/ssh key/private key?ssh-format=openssh": "synthetic-value",
	}}
	count, err := Import(context.Background(), reader, "example.1password.com", []string{
		"VOYAGE_API_KEY=op://Private/ssh key/private key?ssh-format=openssh",
	}, func(bool) (obsidian.CliConfig, error) {
		return obsidian.CliConfig{}, nil
	}, func(obsidian.CliConfig) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, []string{"example.1password.com:op://Private/ssh key/private key?ssh-format=openssh"}, reader.calls)
}

func TestImportReadsAllCredentialsBeforeSaving(t *testing.T) {
	reader := &fakeOnePasswordReader{
		values: map[string]string{"op://Private/voyage/credential": "synthetic-voyage"},
		errs:   map[string]error{"op://Private/typesafe/credential": errors.New("synthetic failure")},
	}
	saved := false
	loaded := false
	_, err := Import(context.Background(), reader, "example.1password.com", []string{
		"VOYAGE_API_KEY=op://Private/voyage/credential",
		"TYPESAFE_API_KEY=op://Private/typesafe/credential",
	}, func(bool) (obsidian.CliConfig, error) {
		loaded = true
		return obsidian.CliConfig{}, nil
	}, func(obsidian.CliConfig) error {
		saved = true
		return nil
	})
	require.Error(t, err)
	require.Len(t, reader.calls, 2)
	require.False(t, loaded)
	require.False(t, saved)
}

func TestImportPersistsMappingsPreservesConfigAndClearsSkips(t *testing.T) {
	original := obsidian.CliConfigPath
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	obsidian.CliConfigPath = func() (string, string, error) { return dir, path, nil }
	t.Cleanup(func() { obsidian.CliConfigPath = original })
	require.NoError(t, obsidian.SaveCliConfig(obsidian.CliConfig{
		DefaultVaultName: "existing",
		Env:              map[string]string{"OPENAI_API_KEY": "synthetic-openai"},
		CredentialSkips:  map[string]bool{"VOYAGE_API_KEY": true, "CEREBRAS_API_KEY": true},
	}))

	reader := &fakeOnePasswordReader{values: map[string]string{
		"op://Private/voyage/credential":   "synthetic-voyage",
		"op://Private/typesafe/credential": "synthetic-typesafe",
	}}
	count, err := Import(context.Background(), reader, "example.1password.com", []string{
		"VOYAGE_API_KEY=op://Private/voyage/credential",
		"TYPESAFE_API_KEY=op://Private/typesafe/credential",
	}, obsidian.LoadCliConfig, obsidian.SaveCliConfig)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	cfg, err := obsidian.LoadCliConfig(false)
	require.NoError(t, err)
	require.Equal(t, "existing", cfg.DefaultVaultName)
	require.Equal(t, "synthetic-openai", cfg.Env["OPENAI_API_KEY"])
	require.Equal(t, "synthetic-voyage", cfg.Env["VOYAGE_API_KEY"])
	require.Equal(t, "synthetic-typesafe", cfg.Env["TYPESAFE_API_KEY"])
	require.False(t, cfg.CredentialSkips["VOYAGE_API_KEY"])
	require.True(t, cfg.CredentialSkips["CEREBRAS_API_KEY"])
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestOnePasswordCLIUsesOrdinaryReadReference(t *testing.T) {
	var gotName string
	var gotArgs []string
	reader := OnePasswordCLI{Binary: "op-test", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return []byte("synthetic-value\n"), nil
	}}
	value, err := reader.Read(context.Background(), "example.1password.com", "op://Private/item/credential")
	require.NoError(t, err)
	require.Equal(t, "synthetic-value", value)
	require.Equal(t, "op-test", gotName)
	require.Equal(t, []string{"read", "op://Private/item/credential", "--account", "example.1password.com"}, gotArgs)
}

func TestOnePasswordCLIDoesNotExposeCommandOutputOnFailure(t *testing.T) {
	reader := OnePasswordCLI{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("synthetic-secret"), errors.New("synthetic command failure")
	}}
	_, err := reader.Read(context.Background(), "example.1password.com", "op://Private/item/credential")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "synthetic-secret")
}
