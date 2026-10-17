package embeddings

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOpenAIProviderDimensionsRace(t *testing.T) {
	provider, err := NewOpenAIProvider(ProviderConfig{
		APIKey:   "test-key",
		Model:    "model",
		Endpoint: "http://example.invalid/v1/embeddings",
	})
	require.NoError(t, err)
	p := provider.(*openAIProvider)
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"data":[{"embedding":[1,2,3]}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = p.EmbedTexts(ctx, []string{fmt.Sprintf("text-%d", i)})
		}()
		go func() {
			defer wg.Done()
			_ = p.Dimensions()
		}()
	}
	wg.Wait()

	assert.Equal(t, 3, p.Dimensions())
}

func TestOllamaProviderDimensionsRace(t *testing.T) {
	provider, err := NewOllamaProvider(ProviderConfig{
		Model:    "model",
		Endpoint: "http://example.invalid/api/embeddings",
	})
	require.NoError(t, err)
	p := provider.(*ollamaProvider)
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"embeddings":[[1,2,3]]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = p.EmbedTexts(ctx, []string{"text"})
		}()
		go func() {
			defer wg.Done()
			_ = p.Dimensions()
		}()
	}
	wg.Wait()

	assert.Equal(t, 3, p.Dimensions())
}
