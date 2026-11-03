package desktop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestProviderSettingsPreserveConfigAndReplaceEverySavedAlias(t *testing.T) {
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(t.TempDir(), "config.yml")
	source := &repoconfig.CredentialSource{Account: "example.1password.com", Environment: strings.Repeat("a", 26), Keys: []string{"VOYAGE_API_KEY"}}
	require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{
		DefaultVaultName: "existing", CredentialSource: source,
		Env: map[string]string{"RHIZOME_VOYAGE_API_KEY": "synthetic-stale", "VOYAGE_API_KEY": "synthetic-old", "TYPESAFE_API_KEY": "synthetic-jev", "OTHER_SETTING": "preserved"},
	}))
	service := &Service{env: env, stateDir: t.TempDir()}
	apply := func(operation, provider, preference, key string) {
		t.Helper()
		response := service.Handle(context.Background(), Request{Protocol: Protocol, Operation: operation, Provider: provider, Preference: preference, Key: key})
		require.Nil(t, response.Error)
		data, err := json.Marshal(response)
		require.NoError(t, err)
		for _, secret := range []string{"synthetic-stale", "synthetic-old", "synthetic-new", "synthetic-jev"} {
			require.NotContains(t, string(data), secret)
		}
	}
	apply("provider-preference-set", "voyage", "server", "")
	apply("provider-key-set", "voyage", "", "synthetic-new")
	cfg, err := repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Equal(t, "existing", cfg.DefaultVaultName)
	require.Equal(t, source, cfg.CredentialSource)
	require.Equal(t, map[string]string{"VOYAGE_API_KEY": "synthetic-new", "TYPESAFE_API_KEY": "synthetic-jev", "OTHER_SETTING": "preserved"}, cfg.Env)
	require.Equal(t, map[string]string{"voyage": "server"}, cfg.ProviderAccess)
	info, err := os.Stat(env.CLIConfigFile)
	require.NoError(t, err)
	if os.PathSeparator != '\\' {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	apply("provider-preference-set", "typesafe", "key", "")
	apply("provider-preference-set", "voyage", "", "")
	apply("provider-key-clear", "voyage", "", "")
	cfg, err = repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"typesafe": "key"}, cfg.ProviderAccess)
	require.Equal(t, map[string]string{"TYPESAFE_API_KEY": "synthetic-jev", "OTHER_SETTING": "preserved"}, cfg.Env)
	require.Equal(t, source, cfg.CredentialSource)
}

func TestProviderSettingsRejectInvalidRequestsWithoutSavingOrEchoingKeys(t *testing.T) {
	for _, request := range []Request{
		{Operation: "provider-key-set", Provider: "unknown", Key: "synthetic-secret"},
		{Operation: "provider-key-set", Provider: "voyage"},
		{Operation: "provider-key-set", Provider: "voyage", Key: "synthetic-secret\n"},
		{Operation: "provider-preference-set", Provider: "voyage", Preference: "other"},
		{Operation: "provider-key-clear", Provider: "openai"},
		{Operation: "provider-key-clear", Provider: "voyage", Key: "synthetic-secret"},
		{Operation: "credential-status", Key: "synthetic-secret"},
	} {
		t.Run(request.Operation+"/"+request.Provider+"/"+request.Preference, func(t *testing.T) {
			env := testkit.Env(t)
			env.CLIConfigFile = filepath.Join(t.TempDir(), "config.yml")
			service := &Service{env: env}
			request.Protocol = Protocol
			response := service.Handle(context.Background(), request)
			require.NotNil(t, response.Error)
			require.Equal(t, "invalid_request", response.Error.Code)
			data, err := json.Marshal(response)
			require.NoError(t, err)
			require.NotContains(t, string(data), "synthetic-secret")
			_, err = os.Stat(env.CLIConfigFile)
			require.True(t, os.IsNotExist(err))
		})
	}
}
