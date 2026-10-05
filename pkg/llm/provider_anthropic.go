package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const anthropicEndpoint = "https://api.anthropic.com/v1/messages"
const anthropicVersion = "2023-06-01"

// anthropicDefaultMaxTokens leaves room for thinking, which counts toward
// max_tokens and is on by default for current Claude models.
const anthropicDefaultMaxTokens = 16000

// AnthropicProvider implements the Anthropic Messages API.
type AnthropicProvider struct {
	apiKey string
	client httpClient
}

// NewAnthropicProvider returns an Anthropic provider.
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{apiKey: apiKey, client: defaultHTTPClient()}
}

func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("missing Anthropic API key")
	}
	system, messages := splitSystemMessages(req.Messages)
	payload := anthropicRequest{
		Model:        req.Model,
		Messages:     messages,
		System:       system,
		Tools:        toAnthropicTools(req.Tools),
		MaxTokens:    maxTokensOrDefault(req.MaxOutputTokens, anthropicDefaultMaxTokens),
		OutputConfig: anthropicOutputConfigFor(req.ReasoningEffort),
	}

	var response anthropicResponse
	err := doJSON(ctx, p.client, "POST", anthropicEndpoint, map[string]string{
		"x-api-key":         p.apiKey,
		"anthropic-version": anthropicVersion,
	}, payload, &response)
	if err != nil {
		return Response{}, err
	}
	text := response.FirstText()
	calls := response.ToolCalls()
	if text == "" && len(calls) == 0 {
		return Response{}, fmt.Errorf("empty Anthropic response")
	}
	return Response{Content: text, ToolCalls: calls}, nil
}

func (p *AnthropicProvider) StreamComplete(ctx context.Context, req Request, callbacks StreamCallbacks) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("missing Anthropic API key")
	}
	system, messages := splitSystemMessages(req.Messages)
	payload := anthropicRequest{
		Model:        req.Model,
		Messages:     messages,
		System:       system,
		MaxTokens:    maxTokensOrDefault(req.MaxOutputTokens, anthropicDefaultMaxTokens),
		OutputConfig: anthropicOutputConfigFor(req.ReasoningEffort),
		Stream:       true,
	}
	var builder strings.Builder
	err := doSSE(ctx, p.client, "POST", anthropicEndpoint, map[string]string{
		"x-api-key":         p.apiKey,
		"anthropic-version": anthropicVersion,
	}, payload, func(_ string, data []byte) error {
		var payload anthropicStreamEvent
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil
		}
		if payload.Type != "content_block_delta" || payload.Delta.Text == "" {
			if callbacks.Event != nil && payload.Type != "" {
				return callbacks.Event(payload.Type, map[string]any{"type": payload.Type})
			}
			return nil
		}
		builder.WriteString(payload.Delta.Text)
		if callbacks.Delta == nil {
			return nil
		}
		return callbacks.Delta(payload.Delta.Text)
	})
	if err != nil {
		return Response{}, err
	}
	text := builder.String()
	if text == "" {
		return Response{}, fmt.Errorf("empty Anthropic response")
	}
	return Response{Content: text}, nil
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
	// OutputConfig carries effort, the thinking-depth control on current Claude models.
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
}

type anthropicOutputConfig struct {
	Effort string `json:"effort"`
}

// anthropicOutputConfigFor maps a profile's reasoning effort to Claude's effort
// levels. Claude has no "none"; thinking-always-on models use low as the floor.
func anthropicOutputConfigFor(effort ReasoningEffort) *anthropicOutputConfig {
	switch effort {
	case "":
		return nil
	case ReasoningNone:
		return &anthropicOutputConfig{Effort: string(ReasoningLow)}
	default:
		return &anthropicOutputConfig{Effort: string(effort)}
	}
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

type anthropicResponse struct {
	Content []anthropicContent `json:"content"`
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Text string `json:"text"`
	} `json:"delta"`
}

func (r anthropicResponse) FirstText() string {
	for _, content := range r.Content {
		if content.Text != "" {
			return content.Text
		}
	}
	return ""
}

func (r anthropicResponse) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, content := range r.Content {
		if content.Type != "tool_use" {
			continue
		}
		calls = append(calls, ToolCall{ID: content.ID, Name: content.Name, Arguments: content.Input})
	}
	return calls
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

func splitSystemMessages(messages []Message) (string, []anthropicMessage) {
	var systemParts []string
	out := make([]anthropicMessage, 0, len(messages))
	for _, msg := range messages {
		switch strings.ToLower(msg.Role) {
		case "system":
			systemParts = append(systemParts, msg.Content)
		case "assistant", "user":
			if len(msg.ToolCalls) > 0 {
				var content []anthropicContent
				for _, call := range msg.ToolCalls {
					content = append(content, anthropicContent{
						Type:  "tool_use",
						ID:    call.ID,
						Name:  call.Name,
						Input: call.Arguments,
					})
				}
				out = append(out, anthropicMessage{Role: "assistant", Content: content})
				continue
			}
			out = append(out, anthropicMessage{
				Role: msg.Role,
				Content: []anthropicContent{{
					Type: "text",
					Text: msg.Content,
				}},
			})
		case "tool":
			out = append(out, anthropicMessage{
				Role: "user",
				Content: []anthropicContent{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
				}},
			})
		}
	}
	return strings.TrimSpace(strings.Join(systemParts, "\n\n")), out
}

func toAnthropicTools(tools []ToolDefinition) []anthropicTool {
	out := make([]anthropicTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, anthropicTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.Parameters,
		})
	}
	return out
}

func maxTokensOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
