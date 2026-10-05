package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListOpenAICompatibleModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"zeta","owned_by":"org"},{"id":"alpha","owned_by":"openai"}]}`))
	}))
	defer server.Close()

	models, err := listOpenAICompatibleModels(context.Background(), server.Client(), server.URL, "test-key")
	require.NoError(t, err)
	require.Equal(t, []ModelInfo{
		{ID: "alpha", OwnedBy: "openai"},
		{ID: "zeta", OwnedBy: "org"},
	}, models)
}

func TestListAnthropicModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-key", r.Header.Get("x-api-key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-b","display_name":"Claude B"},{"id":"claude-a","display_name":"Claude A"}]}`))
	}))
	defer server.Close()

	models, err := listAnthropicModelsFromEndpoint(context.Background(), server.Client(), server.URL, "test-key")
	require.NoError(t, err)
	require.Equal(t, []ModelInfo{
		{ID: "claude-a", DisplayName: "Claude A"},
		{ID: "claude-b", DisplayName: "Claude B"},
	}, models)
}

func TestListGeminiModelsFiltersGenerateContentModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "key with space", r.URL.Query().Get("key"))
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-embed","baseModelId":"gemini-embed","displayName":"Embed","supportedGenerationMethods":["embedContent"]},{"name":"models/gemini-flash","baseModelId":"gemini-flash","displayName":"Flash","supportedGenerationMethods":["generateContent"]}]}`))
	}))
	defer server.Close()

	models, err := listGeminiModelsFromEndpoint(context.Background(), server.Client(), server.URL, "key with space")
	require.NoError(t, err)
	require.Equal(t, []ModelInfo{{ID: "gemini-flash", DisplayName: "Flash"}}, models)
}
