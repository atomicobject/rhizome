package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// capturingHTTPClient records the JSON body of the last request it received.
type capturingHTTPClient struct {
	responseBody string
	sent         map[string]any
}

func (c *capturingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	c.sent = map[string]any{}
	if err := json.Unmarshal(body, &c.sent); err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(c.responseBody))}, nil
}

func TestAnthropicProviderRequestPayload(t *testing.T) {
	const completeBody = `{"content":[{"type":"text","text":"ok"}]}`
	const streamBody = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"ok\"}}\n\n"

	cases := []struct {
		name       string
		effort     ReasoningEffort
		wantEffort any
	}{
		{name: "unset effort sends no output_config", effort: "", wantEffort: nil},
		{name: "none maps to low", effort: ReasoningNone, wantEffort: "low"},
		{name: "xhigh passes through", effort: ReasoningXHigh, wantEffort: "xhigh"},
	}
	paths := []struct {
		name string
		body string
		call func(*AnthropicProvider, Request) error
	}{
		{name: "complete", body: completeBody, call: func(p *AnthropicProvider, r Request) error {
			_, err := p.Complete(context.Background(), r)
			return err
		}},
		{name: "stream", body: streamBody, call: func(p *AnthropicProvider, r Request) error {
			_, err := p.StreamComplete(context.Background(), r, StreamCallbacks{})
			return err
		}},
	}

	for _, path := range paths {
		for _, tc := range cases {
			t.Run(path.name+"/"+tc.name, func(t *testing.T) {
				client := &capturingHTTPClient{responseBody: path.body}
				provider := &AnthropicProvider{apiKey: "test-key", client: client}
				err := path.call(provider, Request{
					Model:           "claude-opus-5",
					Messages:        []Message{{Role: "user", Content: "hi"}},
					ReasoningEffort: tc.effort,
					Temperature:     0.3,
				})
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if _, ok := client.sent["temperature"]; ok {
					t.Fatalf("temperature must not be sent to Anthropic: %v", client.sent)
				}
				if got := client.sent["max_tokens"]; got != float64(anthropicDefaultMaxTokens) {
					t.Fatalf("max_tokens = %v, want %d", got, anthropicDefaultMaxTokens)
				}
				config, _ := client.sent["output_config"].(map[string]any)
				var gotEffort any
				if config != nil {
					gotEffort = config["effort"]
				}
				if gotEffort != tc.wantEffort {
					t.Fatalf("effort = %v, want %v", gotEffort, tc.wantEffort)
				}
			})
		}
	}
}
