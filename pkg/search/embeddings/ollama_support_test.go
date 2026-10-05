package embeddings

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOllamaEndpointIsLocal(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     bool
	}{
		{name: "empty", endpoint: "", want: true},
		{name: "localhost", endpoint: "http://localhost:11434/api/embeddings", want: true},
		{name: "localhost-no-scheme", endpoint: "localhost:11434/api/embeddings", want: true},
		{name: "loopback", endpoint: "http://127.0.0.1:11434/api/embeddings", want: true},
		{name: "ipv6-loopback", endpoint: "http://[::1]:11434/api/embeddings", want: true},
		{name: "wildcard", endpoint: "http://0.0.0.0:11434/api/embeddings", want: true},
		{name: "remote", endpoint: "https://example.com/api/embeddings", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := OllamaEndpointIsLocal(tc.endpoint); got != tc.want {
				t.Fatalf("OllamaEndpointIsLocal(%q) = %v, want %v", tc.endpoint, got, tc.want)
			}
		})
	}
}

func TestNormalizeOllamaEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     string
	}{
		{name: "empty", endpoint: "", want: DefaultOllamaEndpoint},
		{name: "legacy", endpoint: "http://localhost:11434/api/embeddings", want: "http://localhost:11434/api/embed"},
		{name: "legacy-no-scheme", endpoint: "localhost:11434/api/embeddings", want: "http://localhost:11434/api/embed"},
		{name: "host-only", endpoint: "http://localhost:11434", want: "http://localhost:11434/api/embed"},
		{name: "host-only-no-scheme", endpoint: "localhost:11434", want: "http://localhost:11434/api/embed"},
		{name: "batch", endpoint: "http://localhost:11434/api/embed", want: "http://localhost:11434/api/embed"},
		{name: "batch-trailing-slash", endpoint: "http://localhost:11434/api/embed/", want: "http://localhost:11434/api/embed"},
		{name: "openai-compat", endpoint: "http://localhost:11434/v1/embeddings", want: "http://localhost:11434/v1/embeddings"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NormalizeOllamaEndpoint(tc.endpoint))
		})
	}
}

func TestOllamaContextTokensHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, response string
		want                     int
	}{
		{"parameters", "http://localhost:11434/api/embed", `{"parameters":"temperature 0.7\nnum_ctx 2048\nstop <|end|>\n"}`, 2048},
		{"model info through legacy endpoint", "localhost:11434/api/embeddings", `{"model_info":{"nomic-bert.context_length":2048}}`, 2048},
		{"lower effective limit", "http://localhost:11434/api/embed", `{"parameters":"num_ctx 1024","model_info":{"nomic-bert.context_length":2048}}`, 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, "http://localhost:11434/api/show", req.URL.String())
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{"model":"nomic-embed-text"}`, string(body))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.response)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
			})}
			got, err := OllamaContextTokens(context.Background(), client, tc.endpoint, " nomic-embed-text ")
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEnsureOllamaForConfigSkipsRemoteEndpoint(t *testing.T) {
	t.Setenv("PATH", "")
	cfg := Config{
		Provider: "ollama",
		Model:    "nomic-embed-text:latest",
		Endpoint: "https://example.com/api/embeddings",
	}
	require.NoError(t, EnsureOllamaForConfig(context.Background(), cfg, io.Discard))
}

func TestFormatOllamaEnsureErrorMessages(t *testing.T) {
	msg := FormatOllamaEnsureError(OllamaError{Kind: ErrOllamaNotInstalled}, "")
	require.Contains(t, strings.ToLower(msg), "not installed")

	msg = FormatOllamaEnsureError(OllamaError{Kind: ErrOllamaNotRunning}, "")
	require.Contains(t, strings.ToLower(msg), "not running")

	msg = FormatOllamaEnsureError(OllamaError{Kind: ErrOllamaModelMissing}, "nomic-embed-text:latest")
	require.Contains(t, msg, "nomic-embed-text:latest")
	require.Contains(t, strings.ToLower(msg), "pull")

	msg = FormatOllamaEnsureError(OllamaError{Kind: ErrOllamaModelMissing}, "")
	require.Contains(t, strings.ToLower(msg), "no model is set")
}
