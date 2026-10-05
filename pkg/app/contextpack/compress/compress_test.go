package compress

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/llm"
	"github.com/stretchr/testify/require"
)

func TestLLMCompressorConfig(t *testing.T) {
	t.Run("missing API key returns error", func(t *testing.T) {
		// Use a provider that definitely won't have an API key in tests
		cfg := Config{
			Provider: "nonexistent",
			Model:    "test-model",
		}

		_, err := NewLLMCompressor(cfg)
		if err == nil {
			t.Fatal("expected error for missing API key")
		}
		if !strings.Contains(err.Error(), "no API key") {
			t.Errorf("expected 'no API key' error, got: %v", err)
		}
	})
}

func TestLocalCompressionConfigRequests(t *testing.T) {
	t.Setenv("CEREBRAS_API_KEY", "test-key")
	t.Setenv("OPENAI_API_KEY", "test-key")
	for _, tc := range []struct {
		name          string
		config        *LocalCompressionConfig
		maxInputChars int
		provider      string
		model         string
		effort        llm.ReasoningEffort
		outputTokens  int
		requests      int
	}{
		{
			name: "defaults", maxInputChars: 160000, provider: "cerebras", model: "gpt-oss-120b",
			effort: llm.ReasoningMedium, outputTokens: 1000, requests: 1,
		},
		{
			name: "nonpositive values use defaults",
			config: &LocalCompressionConfig{
				TimeoutMS: -1, MaxInputTokens: -1, MaxOutputTokens: -1,
				ReasoningTokenReserve: -1, ChunkChars: -1, Parallelism: -1,
			},
			maxInputChars: 160000, provider: "cerebras", model: "gpt-oss-120b",
			effort: llm.ReasoningMedium, outputTokens: 1000, requests: 1,
		},
		{
			name: "explicit overrides",
			config: &LocalCompressionConfig{
				Provider: "openai", Model: "custom", MaxInputTokens: 200, MaxOutputTokens: 123,
				ChunkChars: 6, Parallelism: 2, ReasoningEffort: "low",
			},
			maxInputChars: 640, provider: "openai", model: "custom",
			effort: llm.ReasoningLow, outputTokens: 123, requests: 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.config == nil {
				tc.config = &LocalCompressionConfig{}
			}
			enabled := true
			tc.config.Enabled = &enabled
			compressor := NewFromLocalConfig(tc.config, false)
			require.NotNil(t, compressor)
			require.Equal(t, tc.maxInputChars, compressor.MaxInputChars())
			provider := &stubProvider{}
			compressor.provider = provider
			result, err := compressor.Compress(context.Background(), Request{
				Pieces: []contextpack.Piece{{Key: "a", Text: "aaaaa"}, {Key: "b", Text: "bbbbb"}, {Key: "c", Text: "ccccc"}},
				Budget: 4000,
				Intent: "preserve configuration behavior",
			})
			require.NoError(t, err)
			require.True(t, result.Compressed)
			require.Equal(t, tc.provider, result.Provider)
			require.Equal(t, tc.model, result.Model)
			require.Len(t, provider.requests, tc.requests)
			for _, request := range provider.requests {
				require.Equal(t, tc.model, request.Model)
				require.Equal(t, tc.effort, request.ReasoningEffort)
				require.Equal(t, tc.outputTokens, request.MaxOutputTokens)
				message := request.Messages[0].Content
				require.Contains(t, message, "preserve configuration behavior")
				if tc.requests == 1 {
					require.Contains(t, message, "4000")
				} else {
					require.Contains(t, message, "Hard limit")
				}
				require.Contains(t, message, "### ")
			}
		})
	}
	for _, tc := range []struct {
		name, intent string
		want, absent []string
	}{
		{"specific intent", "understand authentication flow", []string{"understand authentication flow", "5000", "main.go", "package main", "util.go", "2 items"}, []string{"empty.go"}},
		{"generic intent", "", []string{"5000", "main.go", "2 items"}, []string{"Agent's Intent", "empty.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabled := true
			compressor := NewFromLocalConfig(&LocalCompressionConfig{Enabled: &enabled}, false)
			require.NotNil(t, compressor)
			provider := &stubProvider{}
			compressor.provider = provider
			_, err := compressor.Compress(context.Background(), Request{
				Pieces: []contextpack.Piece{{Key: "main.go", Text: "package main"}, {Key: "empty.go", Text: "   "}, {Key: "util.go", Text: "package util"}},
				Budget: 5000, Intent: tc.intent,
			})
			require.NoError(t, err)
			require.Len(t, provider.requests, 1)
			prompt := provider.requests[0].Messages[0].Content
			for _, want := range tc.want {
				require.Contains(t, prompt, want)
			}
			for _, absent := range tc.absent {
				require.NotContains(t, prompt, absent)
			}
		})
	}
}

type stubProvider struct {
	mu           sync.Mutex
	requests     []llm.Request
	thirdStarted chan struct{}
}

func (p *stubProvider) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	if p.thirdStarted != nil && len(p.requests) == 3 {
		close(p.thirdStarted)
	}
	p.mu.Unlock()
	content := req.Messages[0].Content
	key := "unknown"
	afterContent := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## Content to compress") {
			afterContent = true
			continue
		}
		if afterContent && strings.HasPrefix(line, "### ") {
			key = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			break
		}
	}
	if key == "a.go" && p.thirdStarted != nil {
		select {
		case <-p.thirdStarted:
		case <-ctx.Done():
			return llm.Response{}, ctx.Err()
		}
	}
	return llm.Response{Content: "compressed:" + key}, nil
}

func TestLLMCompressorChunkingConcatenatesInOrder(t *testing.T) {
	provider := &stubProvider{thirdStarted: make(chan struct{})}
	c := &LLMCompressor{
		provider:        provider,
		model:           "test-model",
		timeout:         time.Second,
		maxInputTokens:  1000,
		maxOutputTokens: 0,
		chunkChars:      6,
		parallelism:     2,
		name:            "cerebras",
		reasoningEffort: llm.ReasoningLow,
	}

	pieces := []contextpack.Piece{
		{Key: "a.go", Text: "aaaaa"},
		{Key: "b.go", Text: "bbbbb"},
		{Key: "c.go", Text: "ccccc"},
	}

	result, err := c.Compress(context.Background(), Request{
		Pieces: pieces,
		Budget: 1000,
		Intent: "test chunking",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	provider.mu.Lock()
	requestCount := len(provider.requests)
	provider.mu.Unlock()
	if requestCount != 3 {
		t.Fatalf("expected 3 chunk requests, got %d", requestCount)
	}

	expected := "compressed:a.go\n\ncompressed:b.go\n\ncompressed:c.go"
	if result.Text != expected {
		t.Errorf("unexpected concatenation order:\nwant: %q\ngot:  %q", expected, result.Text)
	}
}

func TestCompressionRequiresExplicitEnablement(t *testing.T) {
	t.Setenv("CEREBRAS_API_KEY", "synthetic-cerebras")
	disabled := false
	for _, cfg := range []*LocalCompressionConfig{nil, {}, {Provider: "cerebras"}, {Enabled: &disabled}} {
		require.Nil(t, NewFromLocalConfig(cfg, false))
	}
}
