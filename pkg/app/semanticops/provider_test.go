package semanticops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestPrepareProviderBuildsConfiguredProvider(t *testing.T) {
	cfg := embeddings.Config{
		Provider:   "test",
		Model:      "deterministic",
		Dimensions: 8,
	}

	provider, providerCfg, err := PrepareProvider(cfg, " ignored-for-test-provider ")
	require.NoError(t, err)

	require.Equal(t, 8, provider.Dimensions())
	require.Equal(t, "test", providerCfg.Provider)
	require.Equal(t, "deterministic", providerCfg.Model)
	require.Equal(t, 8, providerCfg.Dimensions)

	auth := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1,2,3,4]}]}`))
	}))
	t.Cleanup(server.Close)
	cfg = embeddings.Config{Provider: "openai", Model: "text-embedding-3-small", Dimensions: 4, Endpoint: server.URL}
	t.Setenv("RHIZOME_OPENAI_API_KEY", "fallback-key")
	for _, tc := range []struct{ name, explicit, want string }{
		{"trimmed explicit", "  explicit-key  ", "Bearer explicit-key"},
		{"fallback", "  ", "Bearer fallback-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, _, err := PrepareProvider(cfg, tc.explicit)
			require.NoError(t, err)
			vecs, err := provider.EmbedTexts(context.Background(), []string{"probe"})
			require.NoError(t, err)
			require.Equal(t, []embeddings.Embedding{{1, 2, 3, 4}}, vecs)
			require.Equal(t, tc.want, <-auth)
		})
	}
}
