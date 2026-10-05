package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

func metricCount(snapshot indexingperf.Snapshot, name string) int64 {
	for _, counter := range snapshot.Counters {
		if counter.Name == name {
			return counter.Total
		}
	}
	return 0
}

func TestProviderMetricsDistinguishHTTPRetriesFromLogicalCalls(t *testing.T) {
	for _, provider := range []string{"openai", "voyage"} {
		t.Run(provider, func(t *testing.T) {
			var calls atomic.Int32
			endpoint := "http://example.invalid/embed?access_token=synthetic-private-query"
			client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				var input struct {
					Input []string `json:"input"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
				require.Equal(t, []string{"private-alpha", "private-beta"}, input.Input)
				status, body := http.StatusOK, `{"data":[{"index":0,"embedding":[1,2]},{"index":1,"embedding":[3,4]}]}`
				if calls.Add(1) == 1 {
					status, body = 429, "private-error-body"
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			p, err := NewProvider(ProviderConfig{Provider: provider, Model: "private-model", APIKey: "synthetic-private-key", Endpoint: endpoint, Dimensions: 2, MaxConcurrency: 1})
			require.NoError(t, err)
			switch p := p.(type) {
			case *openAIProvider:
				p.httpClient = client
				p.sleep = func(context.Context, time.Duration) error { return nil }
			case *voyageProvider:
				p.httpClient = client
				p.sleep = func(context.Context, time.Duration) error { return nil }
				p.jitter = func(d time.Duration) time.Duration { return d }
			}
			c := indexingperf.NewBounded()
			vectors, err := p.EmbedTexts(indexingperf.WithCollector(context.Background(), c), []string{"private-alpha", "private-beta"})
			require.NoError(t, err)
			require.Equal(t, []Embedding{{1, 2}, {3, 4}}, vectors)
			require.Equal(t, int32(2), calls.Load())
			snapshot := c.Snapshot()
			prefix := "provider.request." + provider
			require.Equal(t, int64(1), metricCount(snapshot, "provider.logical."+provider))
			require.Equal(t, int64(2), metricCount(snapshot, prefix+".attempt"))
			require.Equal(t, int64(1), metricCount(snapshot, prefix+".http_retry"))
			require.Equal(t, int64(1), metricCount(snapshot, prefix+".status.429"))
			require.Equal(t, int64(1), metricCount(snapshot, prefix+".status.200"))
			require.Equal(t, int64(1), metricCount(snapshot, prefix+".backoff.reason.http"))
			require.Equal(t, int64(1), metricCount(snapshot, "provider.logical."+provider+".outcome.ok"))
			require.Greater(t, metricCount(snapshot, prefix+".sent_bytes"), int64(0))
			require.Greater(t, metricCount(snapshot, prefix+".received_bytes"), int64(0))
			require.Len(t, snapshot.ProviderFingerprints, 1)
			encoded, err := json.Marshal(snapshot)
			require.NoError(t, err)
			for _, private := range []string{"private-alpha", "private-beta", "private-model", "private-error-body", "synthetic-private-key", "synthetic-private-query", endpoint} {
				require.NotContains(t, string(encoded), private)
			}
		})
	}
}

func TestProviderMetricsPreserveCancellationDuringNetworkBackoff(t *testing.T) {
	pRaw, err := NewOpenAIProvider(ProviderConfig{APIKey: "synthetic", Model: "model"})
	require.NoError(t, err)
	p := pRaw.(*openAIProvider)
	c := indexingperf.NewBounded()
	ctx, cancel := context.WithCancel(indexingperf.WithCollector(context.Background(), c))
	defer cancel()
	var calls int
	p.httpClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("private upstream marker: %w", io.ErrUnexpectedEOF)
	})}
	p.sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	_, err = p.EmbedTexts(ctx, []string{"private input"})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
	snapshot := c.Snapshot()
	require.Equal(t, int64(1), metricCount(snapshot, "provider.request.openai.attempt"))
	require.Equal(t, int64(1), metricCount(snapshot, "provider.request.openai.transport.transient"))
	require.Equal(t, int64(1), metricCount(snapshot, "provider.request.openai.backoff.interrupted.canceled"))
	require.Equal(t, int64(1), metricCount(snapshot, "provider.logical.openai.outcome.canceled"))
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private upstream marker")
	require.NotContains(t, string(encoded), "private input")
}

func TestCachedProviderMetricsCountPhysicalMissesAndKeepVectorOrder(t *testing.T) {
	inner := &recordingProvider{dims: 2}
	p, err := NewCachedProvider(inner, ProviderConfig{Provider: "test", Model: "model", Dimensions: 2}, filepath.Join(t.TempDir(), "cache.sqlite"))
	require.NoError(t, err)
	defer closeProvider(t, p)
	c := indexingperf.NewBounded()
	ctx := indexingperf.WithCollector(context.Background(), c)
	first, err := p.EmbedTexts(ctx, []string{"alpha", "beta"})
	require.NoError(t, err)
	second, err := p.EmbedTexts(ctx, []string{"beta", "gamma"})
	require.NoError(t, err)
	third, err := p.EmbedTexts(ctx, []string{"gamma", "alpha"})
	require.NoError(t, err)
	require.Equal(t, first[1], second[0])
	require.Equal(t, second[1], third[0])
	require.Equal(t, first[0], third[1])
	require.Equal(t, [][]string{{"alpha", "beta"}, {"gamma"}}, inner.calls)
	snapshot := c.Snapshot()
	require.Equal(t, int64(6), metricCount(snapshot, "provider.cache.inputs"))
	require.Equal(t, int64(3), metricCount(snapshot, "provider.cache.hit"))
	require.Equal(t, int64(3), metricCount(snapshot, "provider.cache.miss"))
	require.Equal(t, int64(3), metricCount(snapshot, "provider.cache.miss.reason.absent"))
	require.Len(t, snapshot.ProviderFingerprints, 1)
}

func TestProviderFingerprintOmitsEndpointCredentials(t *testing.T) {
	a := providerFingerprint("openai", "model", "https://user:secret@example.invalid/embed?key=secret#secret", 2)
	b := providerFingerprint("openai", "model", "https://example.invalid/embed", 2)
	require.Equal(t, a, b)
	require.Len(t, a, 64)
	require.NotEqual(t, a, providerFingerprint("openai", "different", "https://example.invalid/embed", 2))
	require.Equal(t, "other", providerErrorClass(errors.New(strings.Repeat("secret", 10))))
}
