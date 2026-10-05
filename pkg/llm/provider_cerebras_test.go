package llm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

type mockHTTPClient struct {
	response *http.Response
	err      error
	calls    int
}

func (m *mockHTTPClient) Do(*http.Request) (*http.Response, error) {
	m.calls++
	return m.response, m.err
}

func TestCerebrasProvider_Complete(t *testing.T) {
	mockResp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(bytes.NewBufferString(`{
			"choices": [{"message": {"role": "assistant", "content": "Hello from Cerebras!"}}]
		}`)),
	}
	provider := &CerebrasProvider{
		apiKey: "test-key",
		client: &mockHTTPClient{response: mockResp},
	}

	resp, err := provider.Complete(context.Background(), Request{
		Model:    "llama3.1-8b",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Cerebras!" {
		t.Fatalf("expected 'Hello from Cerebras!', got %q", resp.Content)
	}
}

func TestCerebrasProvider_ReturnsToolCalls(t *testing.T) {
	mockResp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(bytes.NewBufferString(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"tool_calls": [{
						"id": "call_123",
						"type": "function",
						"function": {
							"name": "workspace_read",
							"arguments": "{\"path\":\"README.md\"}"
						}
					}]
				}
			}]
		}`)),
	}
	provider := &CerebrasProvider{
		apiKey: "test-key",
		client: &mockHTTPClient{response: mockResp},
	}

	resp, err := provider.Complete(context.Background(), Request{
		Model:    "llama3.1-8b",
		Messages: []Message{{Role: "user", Content: "Read README"}},
		Tools: []ToolDefinition{{
			Name:       "workspace_read",
			Parameters: map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "workspace_read" {
		t.Fatalf("expected workspace_read, got %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("expected README.md path, got %#v", resp.ToolCalls[0].Arguments)
	}
}

func TestCerebrasProvider_MissingAPIKey(t *testing.T) {
	client := &mockHTTPClient{err: errors.New("unexpected network call")}
	provider := &CerebrasProvider{apiKey: "", client: client}
	_, err := provider.Complete(context.Background(), Request{
		Model:    "llama3.1-8b",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	})
	if err == nil || err.Error() != "missing Cerebras API key" {
		t.Fatalf("expected missing Cerebras API key error, got %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("expected no network calls, got %d", client.calls)
	}
}

func TestCerebrasProvider_EmptyResponse(t *testing.T) {
	mockResp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(`{"choices": []}`)),
	}
	provider := &CerebrasProvider{
		apiKey: "test-key",
		client: &mockHTTPClient{response: mockResp},
	}

	_, err := provider.Complete(context.Background(), Request{
		Model:    "llama3.1-8b",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Fatalf("expected empty response error")
	}
}

func TestInferProvider_Cerebras(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"llama3.1-8b", "cerebras"},
		{"llama-3.3-70b", "cerebras"},
		{"Llama3.1-8b", "cerebras"},
		{"qwen-2.5-32b", "cerebras"},
		{"Qwen2.5-72b", "cerebras"},
		{"gpt-oss-4o", "cerebras"},
		{"GPT-OSS-4o-mini", "cerebras"},
		{"zai-glm-4", "cerebras"},
		{"ZAI-GLM-4-Plus", "cerebras"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := InferProvider(tt.model)
			if got != tt.expected {
				t.Fatalf("InferProvider(%q) = %q, want %q", tt.model, got, tt.expected)
			}
		})
	}
}

func TestResolveProfile_Cerebras(t *testing.T) {
	t.Setenv("CEREBRAS_API_KEY", "test-cerebras-key")
	resolved, err := ResolveProfile("instant", nil, nil, "cerebras/llama3.1-8b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Provider != "cerebras" {
		t.Fatalf("expected provider cerebras, got %s", resolved.Provider)
	}
	if resolved.Model != "llama3.1-8b" {
		t.Fatalf("expected model llama3.1-8b, got %s", resolved.Model)
	}
	if resolved.APIKey != "test-cerebras-key" {
		t.Fatalf("expected API key test-cerebras-key, got %s", resolved.APIKey)
	}
}
