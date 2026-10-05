package embeddings

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOllamaProviderParsesOpenAICompatibleResponse(t *testing.T) {
	provider, err := NewOllamaProvider(ProviderConfig{
		Model:          "nomic-embed-text:latest",
		Endpoint:       "http://example.invalid/v1/embeddings",
		MaxConcurrency: 1,
		BatchSize:      2,
	})
	require.NoError(t, err)
	p := provider.(*ollamaProvider)
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"data":[{"embedding":[1,2,3]},{"embedding":[4,5,6]}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	embs, err := p.EmbedTexts(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Equal(t, []Embedding{{1, 2, 3}, {4, 5, 6}}, embs)
}

func TestOllamaProviderSurfaceErrorField(t *testing.T) {
	provider, err := NewOllamaProvider(ProviderConfig{
		Model:          "nomic-embed-text:latest",
		Endpoint:       "http://example.invalid/v1/embeddings",
		MaxConcurrency: 1,
		BatchSize:      1,
	})
	require.NoError(t, err)
	p := provider.(*ollamaProvider)
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"error":"model not found"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	_, err = p.EmbedTexts(context.Background(), []string{"a"})
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "model not found")
}

func TestOllamaProviderLegacyEndpointIsNormalized(t *testing.T) {
	provider, err := NewOllamaProvider(ProviderConfig{
		Model:          "nomic-embed-text:latest",
		Endpoint:       "http://example.invalid/api/embeddings",
		MaxConcurrency: 1,
		BatchSize:      2,
	})
	require.NoError(t, err)
	p := provider.(*ollamaProvider)
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "http://example.invalid/api/embed", req.URL.String())
			bodyBytes, _ := io.ReadAll(req.Body)
			require.Contains(t, string(bodyBytes), `"input"`)
			respBody := `{"embeddings":[[1,2,3],[4,5,6]]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(respBody)),
			}, nil
		}),
	}

	embs, err := p.EmbedTexts(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Len(t, embs, 2)
	require.Len(t, embs[0], 3)
	require.Len(t, embs[1], 3)
}
