package provideraccess

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func setPreferences(t *testing.T, env vaultconfig.Env, prefs map[string]string, saved map[string]string) {
	t.Helper()
	cfg := struct {
		ProviderAccess map[string]string `json:"provider_access"`
		Env            map[string]string `json:"env"`
	}{prefs, saved}
	content, err := json.Marshal(cfg)
	require.NoError(t, err)
	dir, path, err := env.CliPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(path, content, 0600))
}

func TestExplicitPreferencesAndProvenance(t *testing.T) {
	const origin = "https://team.example.test"
	saved := map[string]string{"VOYAGE_API_KEY": "pa-saved", "TYPESAFE_API_KEY": "ts-saved"}
	for _, tc := range []struct {
		name, preference   string
		vars               map[string]string
		load               Loader
		want               Source
		provenance, reason string
	}{
		{"server beats all keys", "server", saved, signedIn(origin, http.DefaultTransport), SourceTeam, "", ""},
		{"server signed out never uses keys or bundle", "server", saved, signedOut, SourceNone, "", "server_signed_out"},
		{"server locked never uses keys or bundle", "server", saved, func() (Managed, bool, error) { return nil, false, errors.New("synthetic-secret") }, SourceNone, "", "session_unavailable"},
		{"key uses environment", "key", saved, signedIn(origin, http.DefaultTransport), SourceDirect, "environment", ""},
		{"key uses saved", "key", nil, signedIn(origin, http.DefaultTransport), SourceDirect, "saved", ""},
		{"invalid preference fails closed", "synthetic-secret", saved, signedIn(origin, http.DefaultTransport), SourceNone, "", "invalid_preference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testEnv(t, tc.vars)
			setPreferences(t, env, map[string]string{"voyage": tc.preference, "typesafe": tc.preference}, saved)
			f := Factory{Load: tc.load, DevBuild: true, bundle: bundleWith("pa-bundle"), recorder: &runtimeRecorder{}}
			for _, report := range f.Reports(env) {
				require.Equal(t, tc.want, report.Source)
				require.Equal(t, tc.provenance, report.Key)
				require.Equal(t, tc.reason, report.Reason)
				require.True(t, report.Saved)
				encoded, marshalErr := json.Marshal(report)
				require.NoError(t, marshalErr)
				require.NotContains(t, string(encoded), "synthetic-secret")
			}
			_, cfg, err := f.NewEmbeddings(env, embeddings.Config{Provider: "voyage"}, "pa-flag")
			_, jevErr := f.NewTypeSafe(env)
			if tc.want == SourceNone {
				require.Error(t, err)
				require.Error(t, jevErr)
				require.NotContains(t, err.Error(), "synthetic-secret")
				return
			}
			require.NoError(t, err)
			require.NoError(t, jevErr)
			if tc.want == SourceTeam {
				require.NotNil(t, cfg.Managed)
				require.Empty(t, cfg.APIKey)
			} else {
				require.Nil(t, cfg.Managed)
				require.Equal(t, "pa-flag", cfg.APIKey)
			}
			runtime := f.RuntimeReports(env)
			require.Equal(t, tc.want, runtime[0].Active.Source)
			require.Equal(t, tc.want, runtime[1].Active.Source)
			if tc.want == SourceDirect {
				require.Equal(t, "flag", runtime[0].Active.Key)
			}
		})
	}
}

func TestExplicitKeyDoesNotUseTeamOrBundle(t *testing.T) {
	env := testEnv(t, nil)
	setPreferences(t, env, map[string]string{"voyage": "key", "typesafe": "key"}, nil)
	f := Factory{Load: signedIn("http://127.0.0.1:8787", http.DefaultTransport), DevBuild: true, bundle: bundleWith("bundle")}
	for _, report := range f.Reports(env) {
		require.Equal(t, SourceNone, report.Source)
		require.Equal(t, "key_missing", report.Reason)
	}
	_, _, err := f.NewEmbeddings(env, embeddings.Config{Provider: "voyage"}, "")
	require.Error(t, err)
	_, err = f.NewTypeSafe(env)
	require.Error(t, err)
}

func TestRuntimeReportsDistinguishActiveFromNext(t *testing.T) {
	env := testEnv(t, nil)
	f := Factory{Load: signedIn("https://team.example.test", http.DefaultTransport), recorder: &runtimeRecorder{}}
	setPreferences(t, env, map[string]string{"voyage": "server", "typesafe": "server"}, nil)
	before := f.RuntimeReports(env)
	require.Nil(t, before[0].Active)
	require.Nil(t, before[1].Active)
	_, _, err := f.NewEmbeddings(env, embeddings.Config{Provider: "voyage"}, "")
	require.NoError(t, err)
	setPreferences(t, env, map[string]string{"voyage": "key", "typesafe": "key"}, map[string]string{"VOYAGE_API_KEY": "pa-saved", "TYPESAFE_API_KEY": "ts-saved"})
	changed := f.RuntimeReports(env)
	require.Equal(t, SourceTeam, changed[0].Active.Source)
	require.Equal(t, SourceDirect, changed[0].Next.Source)
	require.True(t, changed[0].RestartRequired)
	require.Nil(t, changed[1].Active)
	_, err = f.NewTypeSafe(env)
	require.NoError(t, err)
	require.Equal(t, SourceDirect, f.RuntimeReports(env)[1].Active.Source)
	setPreferences(t, env, map[string]string{"voyage": "server", "typesafe": "server"}, nil)
	_, err = f.NewTypeSafe(env)
	require.NoError(t, err)
	final := f.RuntimeReports(env)
	require.Equal(t, SourceTeam, final[1].Active.Source)
	require.False(t, final[1].RestartRequired)
	require.False(t, final[0].RestartRequired)
	// Reports serialize metadata only, never any key value.
	encoded, err := json.Marshal(changed)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "pa-saved")
	require.NotContains(t, string(encoded), "ts-saved")
}

func TestRuntimeWithoutVoyageNeedsRestartAfterBecomingConfigured(t *testing.T) {
	env := testEnv(t, nil)
	f := Factory{recorder: &runtimeRecorder{}}
	setPreferences(t, env, map[string]string{"voyage": "key"}, nil)
	_, _, err := f.NewEmbeddings(env, embeddings.Config{Provider: "voyage"}, "")
	require.Error(t, err)
	require.False(t, f.RuntimeReports(env)[0].RestartRequired)
	setPreferences(t, env, map[string]string{"voyage": "key"}, map[string]string{"VOYAGE_API_KEY": "pa-synthetic-new"})
	report := f.RuntimeReports(env)[0]
	require.Nil(t, report.Active)
	require.Equal(t, SourceDirect, report.Next.Source)
	require.True(t, report.RestartRequired)
}

func TestReportsPreserveDirectKeyPrecedence(t *testing.T) {
	env := testEnv(t, map[string]string{"VOYAGE_API_KEY": "pa-env", "TYPESAFE_API_KEY": "ts-env"})
	setPreferences(t, env, nil, map[string]string{"RHIZOME_VOYAGE_API_KEY": "pa-saved", "TYPESAFE_API_KEY": "ts-saved"})
	f := Factory{Load: signedIn("https://team.example.test", http.DefaultTransport)}
	reports := f.Reports(env)
	require.Equal(t, "VOYAGE_API_KEY", reports[0].EnvName)
	for _, report := range reports {
		require.Equal(t, SourceDirect, report.Source)
		require.Equal(t, "environment", report.Key)
		require.Empty(t, report.Preference)
	}
	// Malformed config cannot silently erase an explicit routing preference.
	_, path, err := env.CliPath()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Clean(path), []byte("provider_access: [broken"), 0600))
	for _, report := range f.Reports(env) {
		require.Equal(t, SourceNone, report.Source)
		require.Equal(t, "configuration_unavailable", report.Reason)
	}
}

func TestRuntimeRecorderIsProcessLocalAndConcurrent(t *testing.T) {
	env := testEnv(t, map[string]string{"TYPESAFE_API_KEY": "ts-synthetic"})
	f := Factory{recorder: &runtimeRecorder{}}
	another := Factory{recorder: &runtimeRecorder{}}
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			client, err := f.NewTypeSafe(env)
			if err != nil || client == nil {
				t.Errorf("client construction failed: %v", err)
			}
			_ = f.RuntimeReports(env)
		}()
	}
	group.Wait()
	require.NotNil(t, f.RuntimeReports(env)[1].Active)
	require.Nil(t, another.RuntimeReports(env)[1].Active)
}

func TestNoConfigPathPreservesEnvironmentAndFlagAccess(t *testing.T) {
	env := vaultconfig.Env{LookupEnv: func(name string) (string, bool) {
		if name == "VOYAGE_API_KEY" || name == "TYPESAFE_API_KEY" {
			return "synthetic-env-key", true
		}
		return "", false
	}}
	f := Factory{bundle: bundleWith("")}
	for _, report := range f.Reports(env) {
		require.Equal(t, SourceDirect, report.Source)
		require.Equal(t, "environment", report.Key)
	}
	_, cfg, err := f.NewEmbeddings(vaultconfig.Env{}, embeddings.Config{Provider: "voyage"}, "pa-flag")
	require.NoError(t, err)
	require.Equal(t, "pa-flag", cfg.APIKey)
}

func TestRuntimeNextPreservesExplicitKeyPresenceWithoutStoringIt(t *testing.T) {
	env := testEnv(t, map[string]string{"VOYAGE_API_KEY": "pa-env"})
	f := Factory{Load: signedIn("https://team.example.test", http.DefaultTransport), recorder: &runtimeRecorder{}}
	// An explicit key is retained by the runtime's constructor inputs, even
	// while an explicit server choice currently supersedes it.
	setPreferences(t, env, map[string]string{"voyage": "server"}, nil)
	_, _, err := f.NewEmbeddings(env, embeddings.Config{Provider: "voyage"}, "pa-flag")
	require.NoError(t, err)
	setPreferences(t, env, map[string]string{"voyage": "key"}, nil)
	report := f.RuntimeReports(env)[0]
	require.Equal(t, SourceTeam, report.Active.Source)
	require.Equal(t, "flag", report.Next.Key)
	require.True(t, report.RestartRequired)
	encoded, err := json.Marshal(f.recorder.active)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "pa-flag")
}
