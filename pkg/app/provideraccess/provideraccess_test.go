package provideraccess

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/pkg/typesafe"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

type fakeSession struct {
	origin    string
	transport http.RoundTripper
}

func (s fakeSession) Origin() string               { return s.origin }
func (s fakeSession) Transport() http.RoundTripper { return s.transport }
func (s fakeSession) Limits() wire.Limits {
	return wire.Limits{VoyageConcurrency: 4, TypeSafeConcurrency: 4}
}

func signedIn(origin string, transport http.RoundTripper) Loader {
	return func() (Managed, bool, error) { return fakeSession{origin, transport}, true, nil }
}

func signedOut() (Managed, bool, error) { return nil, false, nil }

func testEnv(t *testing.T, vars map[string]string) vaultconfig.Env {
	return vaultconfig.Env{HomeDir: t.TempDir(), LookupEnv: func(key string) (string, bool) {
		value, ok := vars[key]
		return value, ok
	}}
}

func bundleWith(key string) func(vaultconfig.Env, string) string {
	return func(vaultconfig.Env, string) string { return key }
}

func TestPrecedence(t *testing.T) {
	const remote, loopback = "https://team.example.test", "http://127.0.0.1:8787"
	direct := map[string]string{"VOYAGE_API_KEY": "pa-direct", "TYPESAFE_API_KEY": "ts-direct"}
	for _, tc := range []struct {
		name   string
		vars   map[string]string
		load   Loader
		dev    bool
		bundle string
		want   Source
	}{
		{"direct beats team and bundle", direct, signedIn(remote, http.DefaultTransport), false, "bundle-key", SourceDirect},
		{"team beats bundle", nil, signedIn(remote, http.DefaultTransport), false, "bundle-key", SourceTeam},
		{"bundle without a session", nil, signedOut, false, "bundle-key", SourceBundle},
		{"no loader is today's behavior", nil, nil, false, "bundle-key", SourceBundle},
		{"nothing available", nil, signedOut, false, "", SourceNone},
		{"dev build prefers loopback session over direct", direct, signedIn(loopback, http.DefaultTransport), true, "", SourceTeam},
		{"dev build keeps direct over remote session", direct, signedIn(remote, http.DefaultTransport), true, "", SourceDirect},
		{"dev build without session behaves as release", direct, signedOut, true, "bundle-key", SourceDirect},
		{"dev build without session or key uses bundle", nil, signedOut, true, "bundle-key", SourceBundle},
		{"store error falls through to bundle", nil, func() (Managed, bool, error) { return nil, false, errors.New("locked") }, false, "bundle-key", SourceBundle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := Factory{Load: tc.load, DevBuild: tc.dev, bundle: bundleWith(tc.bundle)}
			env := testEnv(t, tc.vars)
			require.Equal(t, tc.want, f.VoyageSource(env))
			require.Equal(t, tc.want, f.TypeSafeSource(env))
		})
	}
}

func TestUnavailableNamesSignIn(t *testing.T) {
	f := Factory{Load: func() (Managed, bool, error) { return nil, false, errors.New("keychain locked") }, bundle: bundleWith("")}
	env := testEnv(t, nil)
	_, _, err := f.NewEmbeddings(env, embeddings.Config{Provider: "voyage"}, "")
	require.ErrorContains(t, err, "voyage provider requires api key")
	require.ErrorContains(t, err, "rzm auth login")
	require.ErrorContains(t, err, "keychain locked")
	_, err = f.NewTypeSafe(env)
	require.ErrorContains(t, err, "TypeSafe credential unavailable")
	require.ErrorContains(t, err, "rzm auth login")
}

// A gateway failure is the answer: the factory chose the team session, so the
// provider never retries with a direct key or the bundle.
func TestGatewayFailureNeverFallsBack(t *testing.T) {
	var calls atomic.Int32
	failing := roundTrip(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Host != "team.example.test" || req.Header.Get("Authorization") != "" {
			t.Errorf("request left the gateway path: %s auth=%q", req.URL.Host, req.Header.Get("Authorization"))
		}
		return nil, &wire.GatewayError{Code: wire.CodeBudgetExhausted, Status: 429, Message: "synthetic"}
	})
	f := Factory{Load: signedIn("https://team.example.test", failing), bundle: bundleWith("bundle-key")}
	env := testEnv(t, nil)

	provider, providerCfg, err := f.NewEmbeddings(env, embeddings.Config{Provider: "voyage", Endpoint: embeddings.DefaultVoyageEndpoint}, "")
	require.NoError(t, err)
	require.NotNil(t, providerCfg.Managed)
	require.Empty(t, providerCfg.APIKey)
	_, err = provider.EmbedTexts(context.Background(), []string{"a", "b"})
	var gatewayErr *wire.GatewayError
	require.ErrorAs(t, err, &gatewayErr)

	client, err := f.NewTypeSafe(env)
	require.NoError(t, err)
	_, err = client.Models(context.Background())
	require.ErrorAs(t, err, &gatewayErr)
	require.Equal(t, int32(2), calls.Load(), "one terminal attempt per provider")
}

func TestTeamSessionTypeSafeUsesGatewayRoute(t *testing.T) {
	var path, auth string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	t.Cleanup(gateway.Close)
	f := Factory{Load: signedIn(gateway.URL, http.DefaultTransport)}
	client, err := f.NewTypeSafe(testEnv(t, nil), typesafe.WithMaxRetries(0))
	require.NoError(t, err)
	_, err = client.Models(context.Background())
	require.NoError(t, err)
	require.Equal(t, wire.RouteTypeSafeModel, path)
	require.Empty(t, auth, "the provider adds no key; the session transport owns the bearer")
}

func TestManagedEndpointConflict(t *testing.T) {
	f := Factory{Load: signedIn("https://team.example.test", http.DefaultTransport)}
	_, _, err := f.NewEmbeddings(testEnv(t, nil), embeddings.Config{Provider: "voyage", Endpoint: "https://proxy.example.test/v1/embeddings"}, "")
	require.ErrorContains(t, err, "team_session_endpoint_conflict")
}

// A20: index identity is provider, model, and dimensions, so an index built
// with a direct key opens unchanged through the team gateway.
func TestSwitchingDirectToTeamKeepsIndexIdentity(t *testing.T) {
	cfg := embeddings.ApplyProviderDefaults(embeddings.Config{Provider: "voyage", Dimensions: 1024})
	direct := Factory{bundle: bundleWith("")}
	team := Factory{Load: signedIn("https://team.example.test", http.DefaultTransport), bundle: bundleWith("")}
	directProvider, directCfg, err := direct.NewEmbeddings(testEnv(t, map[string]string{"VOYAGE_API_KEY": "pa-direct"}), cfg, "")
	require.NoError(t, err)
	teamProvider, teamCfg, err := team.NewEmbeddings(testEnv(t, nil), cfg, "")
	require.NoError(t, err)

	directMeta := embeddings.MetadataForProvider(directProvider, directCfg)
	teamMeta := embeddings.MetadataForProvider(teamProvider, teamCfg)
	require.Equal(t, directMeta, teamMeta)
	require.Equal(t, embedindex.KeyFor(directProvider, directCfg), embedindex.KeyFor(teamProvider, teamCfg))

	path := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(path), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 1024, Metadata: &directMeta})
	require.NoError(t, err)
	require.NoError(t, store.Close())
	require.NoError(t, embsqlite.ValidateExisting(context.Background(), embsqlite.File(path), embsqlite.Notes, teamMeta))

	changed := teamMeta
	changed.Model = "voyage-4"
	require.Error(t, embsqlite.ValidateExisting(context.Background(), embsqlite.File(path), embsqlite.Notes, changed), "the check distinguishes identities")
}

func TestNonVoyageProvidersResolveAsBefore(t *testing.T) {
	f := Factory{Load: signedIn("http://127.0.0.1:1", http.DefaultTransport), DevBuild: true}
	providerCfg, err := f.EmbeddingConfig(testEnv(t, map[string]string{"OPENAI_API_KEY": "sk-synthetic"}), embeddings.Config{Provider: "openai"}, "")
	require.NoError(t, err)
	require.Equal(t, "sk-synthetic", providerCfg.APIKey)
	require.Nil(t, providerCfg.Managed)
}

func TestLoopbackOrigins(t *testing.T) {
	for origin, want := range map[string]bool{
		"http://127.0.0.1:8787": true, "http://localhost:1": true, "http://[::1]:2": true,
		"https://team.example.test": false, "http://127.0.0.1.example.test": false,
	} {
		require.Equal(t, want, isLoopbackOrigin(origin), origin)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
