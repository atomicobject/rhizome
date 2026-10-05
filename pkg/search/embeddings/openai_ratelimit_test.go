package embeddings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHeaderDuration(t *testing.T) {
	h := http.Header{}
	h.Set("x-ratelimit-reset-requests", "6m0s")
	d, ok := headerDuration(h, "x-ratelimit-reset-requests")
	require.True(t, ok)
	require.Equal(t, 6*time.Minute, d)

	h = http.Header{}
	h.Set("x-ratelimit-reset-requests", "2")
	d, ok = headerDuration(h, "x-ratelimit-reset-requests")
	require.True(t, ok)
	require.Equal(t, 2*time.Second, d)
}

func TestOpenAIRateLimiterReserveSpreadsRequests(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newOpenAIRateLimiter()
	l.now = func() time.Time { return now }
	l.snap = openAIRateLimitSnapshot{
		remainingRequests: 2,
		resetRequests:     2 * time.Second,
		updatedAt:         now,
		hasRequests:       true,
	}

	// First reservation: no delay, and schedules the next request ~1s later.
	require.Equal(t, time.Duration(0), l.reserve(1))
	require.Equal(t, 1, l.snap.remainingRequests)

	// Second reservation at the same time should be delayed by ~1s.
	require.Equal(t, 1*time.Second, l.reserve(1))
	require.Equal(t, 0, l.snap.remainingRequests)
}

func TestOpenAIRateLimiterFallbackPacesEarlyBurst(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newOpenAIRateLimiter()
	l.now = func() time.Time { return now }

	// First call should be immediate, second should incur some delay due to the
	// conservative fallback window even before headers arrive.
	require.Equal(t, time.Duration(0), l.reserve(10))
	delay := l.reserve(10)
	require.True(t, delay > 0, "expected fallback limiter to add delay on second request")
}

func TestOpenAIRateLimiterConcurrentReserveNonDecreasing(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newOpenAIRateLimiter()
	l.now = func() time.Time { return now }
	l.snap = openAIRateLimitSnapshot{
		remainingRequests: 5,
		resetRequests:     5 * time.Second,
		updatedAt:         now,
		hasRequests:       true,
	}

	var wg sync.WaitGroup
	delays := make(chan time.Duration, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			delays <- l.reserve(1)
		}()
	}
	wg.Wait()
	close(delays)
	got := make([]time.Duration, 0, 10)
	for delay := range delays {
		got = append(got, delay)
	}
	slices.Sort(got)
	require.Equal(t, []time.Duration{0, time.Second, 2250 * time.Millisecond, 3916666666 * time.Nanosecond, 5 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second, 6416666666 * time.Nanosecond}, got)

	snap := l.snapshot()
	require.GreaterOrEqual(t, snap.remainingRequests, 0)
}

func TestOpenAIRateLimiterMissingResetPreservesWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newOpenAIRateLimiter()
	l.now = func() time.Time { return now }
	l.snap = openAIRateLimitSnapshot{
		remainingTokens: 80,
		resetTokens:     5 * time.Second,
		hasTokens:       true,
		updatedAt:       now.Add(-time.Minute),
	}

	h := http.Header{}
	h.Set("x-ratelimit-remaining-tokens", "40")
	l.updateFromHeaders(h)

	snap := l.snapshot()
	require.Equal(t, 40, snap.remainingTokens)
	require.Equal(t, 5*time.Second, snap.resetTokens)
	require.True(t, snap.hasTokens)
	require.Equal(t, now, snap.updatedAt)
}

func TestOpenAIProviderRespectsBatchCap(t *testing.T) {
	provider, err := NewOpenAIProvider(ProviderConfig{
		APIKey:         "test-key",
		Model:          "model",
		Endpoint:       "http://example.invalid/v1/embeddings",
		BatchSize:      2,
		MaxConcurrency: 1,
	})
	require.NoError(t, err)
	p := provider.(*openAIProvider)

	var calls int32
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))

			bodyBytes, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			_ = req.Body.Close()

			var payload struct {
				Input []string `json:"input"`
			}
			require.NoError(t, json.Unmarshal(bodyBytes, &payload))

			type item struct {
				Embedding []float32 `json:"embedding"`
			}
			data := make([]item, len(payload.Input))
			for i := range data {
				data[i] = item{Embedding: []float32{1, 2, 3}}
			}
			respBody, err := json.Marshal(map[string]any{"data": data})
			require.NoError(t, err)

			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":                   []string{"application/json"},
					"x-ratelimit-remaining-requests": []string{"59"},
					"x-ratelimit-reset-requests":     []string{"1s"},
				},
				Body: io.NopCloser(strings.NewReader(string(respBody))),
			}, nil
		}),
	}

	vecs, err := p.EmbedTexts(context.Background(), []string{"a", "b", "c"})
	require.NoError(t, err)
	require.Len(t, vecs, 3)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
	require.Equal(t, 3, p.Dimensions())
}

func TestOpenAIProviderAdaptiveBatchFromHeaders(t *testing.T) {
	provider, err := NewOpenAIProvider(ProviderConfig{
		APIKey:         "test-key",
		Model:          "model",
		Endpoint:       "http://example.invalid/v1/embeddings",
		MaxConcurrency: 1,
	})
	require.NoError(t, err)
	p := provider.(*openAIProvider)

	// Seed the limiter so the *first* request uses header-driven batching.
	seed := http.Header{}
	seed.Set("x-ratelimit-remaining-requests", "2")
	seed.Set("x-ratelimit-remaining-tokens", "120")
	seed.Set("x-ratelimit-reset-requests", "1s")
	seed.Set("x-ratelimit-reset-tokens", "1s")
	p.limiter.updateFromHeaders(seed)

	var (
		batches []int
	)
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			bodyBytes, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			_ = req.Body.Close()

			var payload struct {
				Input []string `json:"input"`
			}
			require.NoError(t, json.Unmarshal(bodyBytes, &payload))
			batches = append(batches, len(payload.Input))

			type item struct {
				Embedding []float32 `json:"embedding"`
			}
			data := make([]item, len(payload.Input))
			for i := range data {
				data[i] = item{Embedding: []float32{1, 2, 3}}
			}
			respBody, err := json.Marshal(map[string]any{"data": data})
			require.NoError(t, err)

			// Return the same headers each time so the limiter snapshot stays stable.
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":                   []string{"application/json"},
					"x-ratelimit-remaining-requests": []string{"2"},
					"x-ratelimit-remaining-tokens":   []string{"120"},
					"x-ratelimit-reset-requests":     []string{"1s"},
					"x-ratelimit-reset-tokens":       []string{"1s"},
				},
				Body: io.NopCloser(strings.NewReader(string(respBody))),
			}, nil
		}),
	}

	// Each input is 40 bytes => estimateTextTokens=10. Budget per request:
	// (120/2)*0.9=54 tokens. With overhead (16), we can fit 3 inputs:
	// 16 + 3*10 = 46 <= 54; 4 inputs would be 56 > 54.
	text := strings.Repeat("a", 40)
	vecs, err := p.EmbedTexts(context.Background(), []string{text, text, text, text, text})
	require.NoError(t, err)
	require.Len(t, vecs, 5)
	require.Equal(t, []int{3, 2}, batches)
}

func TestOpenAIProviderIgnoresLimiterWithoutHeaders(t *testing.T) {
	provider, err := NewOpenAIProvider(ProviderConfig{
		APIKey:         "test-key",
		Model:          "model",
		Endpoint:       "http://example.invalid/v1/embeddings",
		BatchSize:      10,
		MaxConcurrency: 1,
	})
	require.NoError(t, err)
	p := provider.(*openAIProvider)
	p.maxTokens = 100
	p.limiter.snap = openAIRateLimitSnapshot{
		remainingRequests: 1,
		remainingTokens:   30,
		resetRequests:     time.Second,
		resetTokens:       time.Second,
		updatedAt:         time.Now(),
		hasRequests:       true,
		hasTokens:         true,
		fromHeaders:       false,
	}

	var batches []int
	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			bodyBytes, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			_ = req.Body.Close()

			var payload struct {
				Input []string `json:"input"`
			}
			require.NoError(t, json.Unmarshal(bodyBytes, &payload))
			batches = append(batches, len(payload.Input))

			type item struct {
				Embedding []float32 `json:"embedding"`
			}
			data := make([]item, len(payload.Input))
			for i := range data {
				data[i] = item{Embedding: []float32{1, 2, 3}}
			}
			respBody, err := json.Marshal(map[string]any{"data": data})
			require.NoError(t, err)

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(string(respBody))),
			}, nil
		}),
	}

	text := strings.Repeat("a", 40)
	vecs, err := p.EmbedTexts(context.Background(), []string{text, text, text, text, text})
	require.NoError(t, err)
	require.Len(t, vecs, 5)
	require.Equal(t, []int{5}, batches)
}
