package embeddings

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIProviderRetries403AndSucceeds(t *testing.T) {
	provider, err := NewOpenAIProvider(ProviderConfig{
		APIKey:         "test-key",
		Model:          "model",
		Endpoint:       "http://example.invalid/v1/embeddings",
		MaxConcurrency: 1,
	})
	require.NoError(t, err)

	p := provider.(*openAIProvider)
	p.sleep = func(ctx context.Context, d time.Duration) error { return nil }

	var calls int32
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader("temporary")),
				}, nil
			}

			body := `{"data":[{"embedding":[1,2,3]}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	vecs, err := p.EmbedTexts(context.Background(), []string{"a"})
	require.NoError(t, err)
	require.Len(t, vecs, 1)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
	require.Equal(t, 3, p.Dimensions())
}

func TestOpenAIProviderRetries403ThenReturnsError(t *testing.T) {
	provider, err := NewOpenAIProvider(ProviderConfig{
		APIKey:         "test-key",
		Model:          "model",
		Endpoint:       "http://example.invalid/v1/embeddings",
		MaxConcurrency: 1,
	})
	require.NoError(t, err)

	p := provider.(*openAIProvider)
	p.sleep = func(ctx context.Context, d time.Duration) error { return nil }

	var calls int32
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader("nope")),
			}, nil
		}),
	}

	_, err = p.EmbedTexts(context.Background(), []string{"a"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "openai embeddings status 403")
	require.Contains(t, err.Error(), "nope")
	require.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestOpenAIProviderBoundsRetryableHTTPStatuses(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider, err := NewOpenAIProvider(ProviderConfig{
				APIKey:         "test-key",
				Model:          "model",
				Endpoint:       "http://example.invalid/v1/embeddings",
				MaxConcurrency: 1,
			})
			require.NoError(t, err)

			p := provider.(*openAIProvider)
			p.sleep = func(ctx context.Context, d time.Duration) error { return nil }

			var calls int32
			p.httpClient = &http.Client{
				Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					atomic.AddInt32(&calls, 1)
					return &http.Response{
						StatusCode: status,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader("retry forever")),
					}, nil
				}),
			}

			_, err = p.EmbedTexts(context.Background(), []string{"a"})
			require.Error(t, err)
			require.Contains(t, err.Error(), fmt.Sprintf("http status %d", status))
			require.Equal(t, int32(3), atomic.LoadInt32(&calls))
		})
	}
}
