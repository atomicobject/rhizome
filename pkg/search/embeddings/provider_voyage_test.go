package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

func TestVoyageProviderDefaultsToHardMaxConcurrency(t *testing.T) {
	for _, configured := range []int{0, 64} {
		provider, err := NewVoyageProvider(ProviderConfig{APIKey: "test-key", MaxConcurrency: configured})
		require.NoError(t, err)
		withDefault, ok := provider.(interface{ DefaultMaxConcurrency() int })
		require.True(t, ok)
		require.Equal(t, 32, withDefault.DefaultMaxConcurrency())
	}
}

func TestVoyageProviderUsesLongerHTTPTimeout(t *testing.T) {
	provider, err := NewVoyageProvider(ProviderConfig{
		APIKey: "test-key",
	})
	require.NoError(t, err)

	p := provider.(*voyageProvider)
	require.Equal(t, 120*time.Second, p.httpClient.Timeout)
}

func TestVoyageProviderAdvertisesLiteRequestCeilings(t *testing.T) {
	provider, err := NewVoyageProvider(ProviderConfig{
		APIKey: "test-key",
		Model:  "voyage-4-lite",
	})
	require.NoError(t, err)

	withBatch, ok := provider.(interface{ DefaultBatchSize() int })
	require.True(t, ok)
	require.Equal(t, 1000, withBatch.DefaultBatchSize())

	withBytes, ok := provider.(interface{ DefaultMaxBatchBytes() int })
	require.True(t, ok)
	require.Equal(t, 900000*4, withBytes.DefaultMaxBatchBytes())
	for _, tc := range []struct {
		model  string
		tokens int
	}{
		{"voyage-4-lite", 900000},
		{"voyage-4", 300000},
		{"voyage-code-3", 110000},
	} {
		require.Equal(t, tc.tokens, voyageMaxTokensForModel(tc.model))
	}
}

func TestVoyageProviderSplitsTransientNetworkFailuresAfterFirstTimeout(t *testing.T) {
	provider, err := NewVoyageProvider(ProviderConfig{
		APIKey:         "test-key",
		BatchSize:      8,
		MaxConcurrency: 1,
	})
	require.NoError(t, err)

	p := provider.(*voyageProvider)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	largeCalls := 0
	p.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var payload struct {
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			if len(payload.Input) > 2 {
				largeCalls++
				return nil, context.DeadlineExceeded
			}
			data := make([]map[string]any, len(payload.Input))
			for i, input := range payload.Input {
				data[i] = map[string]any{"embedding": []float32{float32(input[0])}, "index": i}
			}
			body, err := json.Marshal(map[string]any{"data": data})
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
		}),
	}

	vecs, err := p.EmbedTexts(context.Background(), []string{"a", "b", "c", "d", "e", "f", "g", "h"})
	require.NoError(t, err)
	require.Equal(t, []Embedding{{97}, {98}, {99}, {100}, {101}, {102}, {103}, {104}}, vecs)
	require.Equal(t, 3, largeCalls)
}

func TestVoyageProviderBoundsRetryableHTTPStatuses(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider, err := NewVoyageProvider(ProviderConfig{
				APIKey:         "test-key",
				BatchSize:      1,
				MaxConcurrency: 1,
			})
			require.NoError(t, err)

			p := provider.(*voyageProvider)
			p.sleep = func(ctx context.Context, d time.Duration) error { return nil }

			var calls int32
			p.httpClient = &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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

func TestVoyageProviderHonorsRetryAfterWithinBudget(t *testing.T) {
	provider, err := NewVoyageProvider(ProviderConfig{APIKey: "test-key", BatchSize: 1, MaxConcurrency: 1})
	require.NoError(t, err)

	p := provider.(*voyageProvider)
	p.jitter = func(d time.Duration) time.Duration { return d }
	var sleeps []time.Duration
	p.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	var calls int
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		calls++
		if calls < 3 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"60"}},
				Body:       io.NopCloser(strings.NewReader("slow down")),
			}, nil
		}
		return voyageEmbeddingResponse([]string{"a"}), nil
	})}

	vecs, err := p.EmbedTexts(context.Background(), []string{"a"})
	require.NoError(t, err)
	require.Len(t, vecs, 1)
	require.Equal(t, []time.Duration{60 * time.Second, 60 * time.Second}, sleeps)
}

func TestVoyageProviderRejectsRetryAfterBeyondRecoveryBudget(t *testing.T) {
	provider, err := NewVoyageProvider(ProviderConfig{APIKey: "test-key", BatchSize: 4, MaxConcurrency: 1})
	require.NoError(t, err)

	p := provider.(*voyageProvider)
	p.withRecoveryBudget = func(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctx, 10*time.Second)
	}
	p.jitter = func(d time.Duration) time.Duration { return d }
	p.sleep = func(context.Context, time.Duration) error {
		t.Fatal("must not retry before Retry-After")
		return nil
	}
	var calls int
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"60"}},
			Body:       io.NopCloser(strings.NewReader("slow down")),
		}, nil
	})}

	_, err = p.EmbedTexts(context.Background(), []string{"a", "b", "c", "d"})
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "retry delay 1m0s exceeds remaining logical batch recovery budget")
	require.Equal(t, 1, calls)
}

func TestVoyageProviderUsesJitteredExponentialStatusBackoff(t *testing.T) {
	provider, err := NewVoyageProvider(ProviderConfig{APIKey: "test-key", BatchSize: 1, MaxConcurrency: 1})
	require.NoError(t, err)

	p := provider.(*voyageProvider)
	p.jitter = func(d time.Duration) time.Duration { return d + d/5 }
	var sleeps []time.Duration
	p.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	var calls int
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("try again")),
			}, nil
		}
		return voyageEmbeddingResponse([]string{"a"}), nil
	})}

	_, err = p.EmbedTexts(context.Background(), []string{"a"})
	require.NoError(t, err)
	require.Equal(t, []time.Duration{1200 * time.Millisecond, 2400 * time.Millisecond}, sleeps)
}

func TestVoyageProviderRetryAfterSleepHonorsCancellation(t *testing.T) {
	provider, err := NewVoyageProvider(ProviderConfig{APIKey: "test-key", BatchSize: 1, MaxConcurrency: 1})
	require.NoError(t, err)

	p := provider.(*voyageProvider)
	p.jitter = func(d time.Duration) time.Duration { return d }
	sleepStarted := make(chan struct{})
	p.sleep = func(ctx context.Context, delay time.Duration) error {
		close(sleepStarted)
		return sleepWithContext(ctx, delay)
	}
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"30"}},
			Body:       io.NopCloser(strings.NewReader("slow down")),
		}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		_, err := p.EmbedTexts(ctx, []string{"a"})
		errs <- err
	}()
	select {
	case <-sleepStarted:
	case <-time.After(time.Second):
		t.Fatal("provider did not begin Retry-After wait")
	}
	cancel()
	select {
	case err := <-errs:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop Retry-After wait")
	}
}

func TestVoyageProviderSharesOverallRecoveryBudgetAcrossSplits(t *testing.T) {
	provider, err := NewVoyageProvider(ProviderConfig{APIKey: "test-key", BatchSize: 4, MaxConcurrency: 1})
	require.NoError(t, err)

	p := provider.(*voyageProvider)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	var budgetContexts int
	var cancelBudget context.CancelFunc
	p.withRecoveryBudget = func(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
		budgetContexts++
		budgetCtx, cancel := context.WithCancel(ctx)
		cancelBudget = cancel
		return budgetCtx, cancel
	}
	var calls int
	p.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		calls++
		var payload struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		if len(payload.Input) > 1 {
			return nil, context.DeadlineExceeded
		}
		if calls == 3 {
			cancelBudget()
		}
		return voyageEmbeddingResponse(payload.Input), nil
	})}

	_, err = p.EmbedTexts(context.Background(), []string{"a", "b", "c", "d"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "voyage logical batch recovery budget exhausted")
	require.Equal(t, 1, budgetContexts)
	require.Equal(t, 3, calls)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func voyageEmbeddingResponse(texts []string) *http.Response {
	data := make([]struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	}, len(texts))
	for i := range texts {
		data[i] = struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{
			Embedding: []float32{float32(i + 1)},
			Index:     i,
		}
	}
	body, _ := json.Marshal(struct {
		Data any `json:"data"`
	}{Data: data})
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func TestVoyageTransportFailureDoesNotReportResponseOrRetry(t *testing.T) {
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), indexingperf.New()), "embed_notes")
	provider, err := NewVoyageProvider(ProviderConfig{APIKey: "test-key", BatchSize: 1, MaxConcurrency: 1})
	require.NoError(t, err)
	p := provider.(*voyageProvider)
	calls := 0
	p.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, io.EOF
	})}
	_, err = p.EmbedTexts(ctx, []string{"one"})
	require.Error(t, err)
	require.Equal(t, 1, calls)
	summary := indexingperf.FromContext(ctx).RenderSummary()
	require.Contains(t, summary, "voyage_attempts=1")
	require.Contains(t, summary, "voyage_network_failures=1")
	require.NotContains(t, summary, "voyage_attempts_returned=")
	require.NotContains(t, summary, "voyage_http_retries=")
	require.NotContains(t, summary, "voyage_splits=")
}
