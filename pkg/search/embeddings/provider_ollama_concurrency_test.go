package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOllamaProviderConcurrencyCap(t *testing.T) {
	// Test that concurrent batch requests are capped at MaxConcurrency.
	// Four batches exceed the configured cap of two.
	provider, err := NewOllamaProvider(ProviderConfig{
		Model:          "model",
		Endpoint:       "http://example.invalid/api/embed",
		MaxConcurrency: 2,
		BatchSize:      2,
	})
	require.NoError(t, err)
	p := provider.(*ollamaProvider)

	gate := make(chan struct{})
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate) }) }
	t.Cleanup(unblock)
	var inflight int32
	var maxInflight int32

	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			cur := atomic.AddInt32(&inflight, 1)
			for {
				max := atomic.LoadInt32(&maxInflight)
				if cur <= max {
					break
				}
				if atomic.CompareAndSwapInt32(&maxInflight, max, cur) {
					break
				}
			}

			<-gate
			atomic.AddInt32(&inflight, -1)

			var payload struct {
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, err
			}
			vectors := make([][]float32, len(payload.Input))
			for i, input := range payload.Input {
				vectors[i] = []float32{float32(input[0])}
			}
			body, err := json.Marshal(map[string]any{"embeddings": vectors})
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type outcome struct {
		vectors []Embedding
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		vectors, err := p.EmbedTexts(ctx, []string{"a", "b", "c", "d", "e", "f", "g", "h"})
		done <- outcome{vectors, err}
	}()

	deadline := time.NewTimer(1 * time.Second)
	defer deadline.Stop()
	for atomic.LoadInt32(&maxInflight) < 2 {
		select {
		case <-deadline.C:
			unblock()
			t.Fatalf("expected concurrent requests to reach 2, got %d", atomic.LoadInt32(&maxInflight))
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int32(2), atomic.LoadInt32(&maxInflight), "third request must wait for a slot")
	unblock()
	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, []Embedding{{'a'}, {'b'}, {'c'}, {'d'}, {'e'}, {'f'}, {'g'}, {'h'}}, got.vectors)

	if max := atomic.LoadInt32(&maxInflight); max != 2 {
		t.Fatalf("expected concurrency cap 2, got %d", max)
	}
}

func TestOllamaProviderBatching(t *testing.T) {
	// Test that texts are properly batched.
	var requestCount int32
	var batches [][]string

	provider, err := NewOllamaProvider(ProviderConfig{
		Model:          "model",
		Endpoint:       "http://example.invalid/api/embed",
		MaxConcurrency: 1, // Sequential to make counting deterministic
		BatchSize:      3,
	})
	require.NoError(t, err)
	p := provider.(*ollamaProvider)

	p.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&requestCount, 1)

			// Parse request to count texts
			body, _ := io.ReadAll(req.Body)
			var payload struct {
				Input []string `json:"input"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			batches = append(batches, append([]string(nil), payload.Input...))

			// Return matching number of embeddings
			embs := make([][]float32, len(payload.Input))
			for i := range embs {
				embs[i] = []float32{float32(payload.Input[i][0])}
			}
			respBody, _ := json.Marshal(map[string]any{"embeddings": embs})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(string(respBody))),
			}, nil
		}),
	}

	ctx := context.Background()

	// 7 texts with batch size 3 = 3 batches (3+3+1)
	vecs, err := p.EmbedTexts(ctx, []string{"a", "b", "c", "d", "e", "f", "g"})
	require.NoError(t, err)
	require.Equal(t, []Embedding{{'a'}, {'b'}, {'c'}, {'d'}, {'e'}, {'f'}, {'g'}}, vecs)
	require.Equal(t, int32(3), atomic.LoadInt32(&requestCount), "expected 3 batch requests")
	require.Equal(t, [][]string{{"a", "b", "c"}, {"d", "e", "f"}, {"g"}}, batches)
	vecs, err = p.EmbedTexts(ctx, []string{"s"})
	require.NoError(t, err)
	require.Equal(t, []Embedding{{'s'}}, vecs)
	require.Equal(t, int32(4), atomic.LoadInt32(&requestCount))
	require.Equal(t, []string{"s"}, batches[3])
}
