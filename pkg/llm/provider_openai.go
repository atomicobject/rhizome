package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const openAIEndpoint = "https://api.openai.com/v1/responses"

// OpenAIProvider implements the OpenAI Responses API.
type OpenAIProvider struct {
	apiKey string
	client httpClient
}

// NewOpenAIProvider returns an OpenAI provider.
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{apiKey: apiKey, client: defaultHTTPClient()}
}

func (p *OpenAIProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("missing OpenAI API key")
	}
	payload := openAIResponseRequest{
		Model: req.Model,
		Input: toOpenAIInput(req.Messages),
		Tools: toOpenAITools(req.Tools),
	}
	if req.Temperature > 0 {
		payload.Temperature = &req.Temperature
	}
	if req.MaxOutputTokens > 0 {
		payload.MaxOutputTokens = req.MaxOutputTokens
	}
	if req.ReasoningEffort != "" {
		payload.Reasoning = &openAIReasoning{Effort: string(req.ReasoningEffort)}
	}
	if len(req.Tools) > 0 {
		auto := "auto"
		payload.ToolChoice = &auto
	}

	var response openAIResponse
	err := doJSON(ctx, p.client, "POST", openAIEndpoint, map[string]string{
		"Authorization": "Bearer " + p.apiKey,
	}, payload, &response)
	if err != nil {
		return Response{}, err
	}
	text := response.FirstText()
	calls := response.ToolCalls()
	if text == "" && len(calls) == 0 {
		return Response{}, fmt.Errorf("empty OpenAI response")
	}
	return Response{Content: text, ToolCalls: calls}, nil
}

func (p *OpenAIProvider) StreamComplete(ctx context.Context, req Request, callbacks StreamCallbacks) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("missing OpenAI API key")
	}
	payload := openAIResponseRequest{
		Model:  req.Model,
		Input:  toOpenAIInput(req.Messages),
		Stream: true,
	}
	if req.Temperature > 0 {
		payload.Temperature = &req.Temperature
	}
	if req.MaxOutputTokens > 0 {
		payload.MaxOutputTokens = req.MaxOutputTokens
	}
	if req.ReasoningEffort != "" {
		payload.Reasoning = &openAIReasoning{Effort: string(req.ReasoningEffort)}
	}
	var builder strings.Builder
	err := doSSE(ctx, p.client, "POST", openAIEndpoint, map[string]string{
		"Authorization": "Bearer " + p.apiKey,
	}, payload, func(event string, data []byte) error {
		var payload openAIStreamEvent
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil
		}
		if payload.Type != "response.output_text.delta" && event != "response.output_text.delta" {
			if callbacks.Event != nil && payload.Type != "" {
				return callbacks.Event(payload.Type, map[string]any{"type": payload.Type})
			}
			return nil
		}
		if payload.Delta == "" {
			return nil
		}
		builder.WriteString(payload.Delta)
		if callbacks.Delta == nil {
			return nil
		}
		return callbacks.Delta(payload.Delta)
	})
	if err != nil {
		return Response{}, err
	}
	text := builder.String()
	if text == "" {
		return Response{}, fmt.Errorf("empty OpenAI response")
	}
	return Response{Content: text}, nil
}

type openAIResponseRequest struct {
	Model           string           `json:"model"`
	Input           []any            `json:"input"`
	Tools           []openAITool     `json:"tools,omitempty"`
	ToolChoice      *string          `json:"tool_choice,omitempty"`
	Stream          bool             `json:"stream,omitempty"`
	Reasoning       *openAIReasoning `json:"reasoning,omitempty"`
	Temperature     *float64         `json:"temperature,omitempty"`
	MaxOutputTokens int              `json:"max_output_tokens,omitempty"`
}

type openAIReasoning struct {
	Effort string `json:"effort"`
}

type openAIMessage struct {
	Role    string            `json:"role"`
	Content []openAITextChunk `json:"content"`
}

type openAITextChunk struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openAIResponse struct {
	Output []openAIOutput `json:"output"`
}

type openAIOutput struct {
	ID        string              `json:"id"`
	Type      string              `json:"type"`
	CallID    string              `json:"call_id"`
	Name      string              `json:"name"`
	Arguments string              `json:"arguments"`
	Content   []openAIOutputChunk `json:"content"`
}

type openAIOutputChunk struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openAIStreamEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
}

func (r openAIResponse) FirstText() string {
	for _, output := range r.Output {
		for _, chunk := range output.Content {
			if chunk.Text != "" {
				return chunk.Text
			}
		}
	}
	return ""
}

func (r openAIResponse) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, output := range r.Output {
		if output.Type != "function_call" {
			continue
		}
		args := map[string]any{}
		if output.Arguments != "" {
			_ = json.Unmarshal([]byte(output.Arguments), &args)
		}
		id := output.CallID
		if id == "" {
			id = output.ID
		}
		calls = append(calls, ToolCall{ID: id, Name: output.Name, Arguments: args})
	}
	return calls
}

type openAITool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      bool           `json:"strict,omitempty"`
}

type openAIFunctionCallInput struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIFunctionCallOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

func toOpenAIInput(messages []Message) []any {
	input := make([]any, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "tool" {
			input = append(input, openAIFunctionCallOutput{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: msg.Content,
			})
			continue
		}
		if len(msg.ToolCalls) > 0 {
			for _, call := range msg.ToolCalls {
				args, _ := json.Marshal(call.Arguments)
				input = append(input, openAIFunctionCallInput{
					Type:      "function_call",
					CallID:    call.ID,
					Name:      call.Name,
					Arguments: string(args),
				})
			}
			continue
		}
		chunkType := "input_text"
		if strings.ToLower(msg.Role) == "assistant" {
			chunkType = "output_text"
		}
		input = append(input, openAIMessage{
			Role: msg.Role,
			Content: []openAITextChunk{{
				Type: chunkType,
				Text: msg.Content,
			}},
		})
	}
	return input
}

func toOpenAITools(tools []ToolDefinition) []openAITool {
	out := make([]openAITool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, openAITool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		})
	}
	return out
}
