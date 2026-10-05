package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const cerebrasEndpoint = "https://api.cerebras.ai/v1/chat/completions"

// CerebrasProvider implements the Cerebras Chat Completions API.
type CerebrasProvider struct {
	apiKey string
	client httpClient
}

// NewCerebrasProvider returns a Cerebras provider.
func NewCerebrasProvider(apiKey string) *CerebrasProvider {
	return &CerebrasProvider{apiKey: apiKey, client: defaultHTTPClient()}
}

func (p *CerebrasProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("missing Cerebras API key")
	}
	payload := cerebrasRequest{
		Model:    req.Model,
		Messages: toCerebrasMessages(req.Messages),
		Tools:    toChatTools(req.Tools),
	}
	if req.Temperature > 0 {
		payload.Temperature = &req.Temperature
	}
	if req.MaxOutputTokens > 0 {
		payload.MaxTokens = req.MaxOutputTokens
	}

	var response cerebrasResponse
	err := doJSON(ctx, p.client, "POST", cerebrasEndpoint, map[string]string{
		"Authorization": "Bearer " + p.apiKey,
	}, payload, &response)
	if err != nil {
		return Response{}, err
	}
	text := response.FirstText()
	calls := response.ToolCalls()
	if text == "" && len(calls) == 0 {
		return Response{}, fmt.Errorf("empty Cerebras response")
	}
	return Response{Content: text, ToolCalls: calls}, nil
}

func (p *CerebrasProvider) StreamComplete(ctx context.Context, req Request, callbacks StreamCallbacks) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("missing Cerebras API key")
	}
	payload := cerebrasRequest{
		Model:    req.Model,
		Messages: toCerebrasMessages(req.Messages),
		Stream:   true,
	}
	if req.Temperature > 0 {
		payload.Temperature = &req.Temperature
	}
	if req.MaxOutputTokens > 0 {
		payload.MaxTokens = req.MaxOutputTokens
	}
	var builder strings.Builder
	err := doSSE(ctx, p.client, "POST", cerebrasEndpoint, map[string]string{
		"Authorization": "Bearer " + p.apiKey,
	}, payload, func(_ string, data []byte) error {
		var payload cerebrasStreamChunk
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil
		}
		if len(payload.Choices) == 0 || payload.Choices[0].Delta.Content == "" {
			return nil
		}
		delta := payload.Choices[0].Delta.Content
		builder.WriteString(delta)
		if callbacks.Delta == nil {
			return nil
		}
		return callbacks.Delta(delta)
	})
	if err != nil {
		return Response{}, err
	}
	text := builder.String()
	if text == "" {
		return Response{}, fmt.Errorf("empty Cerebras response")
	}
	return Response{Content: text}, nil
}

type cerebrasRequest struct {
	Model       string            `json:"model"`
	Messages    []cerebrasMessage `json:"messages"`
	Tools       []chatTool        `json:"tools,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
}

type cerebrasMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

type cerebrasResponse struct {
	Choices []cerebrasChoice `json:"choices"`
}

type cerebrasChoice struct {
	Message cerebrasMessage `json:"message"`
}

type cerebrasStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

func (r cerebrasResponse) FirstText() string {
	if len(r.Choices) > 0 {
		return r.Choices[0].Message.Content
	}
	return ""
}

func (r cerebrasResponse) ToolCalls() []ToolCall {
	if len(r.Choices) == 0 {
		return nil
	}
	var calls []ToolCall
	for _, call := range r.Choices[0].Message.ToolCalls {
		args := map[string]any{}
		if call.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
		}
		calls = append(calls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: args})
	}
	return calls
}

func toCerebrasMessages(messages []Message) []cerebrasMessage {
	out := make([]cerebrasMessage, 0, len(messages))
	for _, msg := range messages {
		mapped := cerebrasMessage{Role: strings.ToLower(msg.Role), Content: msg.Content}
		if msg.ToolCallID != "" {
			mapped.ToolCallID = msg.ToolCallID
		}
		if len(msg.ToolCalls) > 0 {
			mapped.ToolCalls = toChatToolCalls(msg.ToolCalls)
		}
		out = append(out, mapped)
	}
	return out
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatCallFunc `json:"function"`
}

type chatCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func toChatTools(tools []ToolDefinition) []chatTool {
	out := make([]chatTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, chatTool{
			Type: "function",
			Function: chatFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return out
}

func toChatToolCalls(calls []ToolCall) []chatToolCall {
	out := make([]chatToolCall, 0, len(calls))
	for _, call := range calls {
		args, _ := json.Marshal(call.Arguments)
		out = append(out, chatToolCall{
			ID:   call.ID,
			Type: "function",
			Function: chatCallFunc{
				Name:      call.Name,
				Arguments: string(args),
			},
		})
	}
	return out
}
