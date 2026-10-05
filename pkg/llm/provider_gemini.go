package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const geminiEndpointTemplate = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s"

// GeminiProvider implements the Gemini generateContent API.
type GeminiProvider struct {
	apiKey string
	client httpClient
}

// NewGeminiProvider returns a Gemini provider.
func NewGeminiProvider(apiKey string) *GeminiProvider {
	return &GeminiProvider{apiKey: apiKey, client: defaultHTTPClient()}
}

func (p *GeminiProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("missing Gemini API key")
	}
	if req.Model == "" {
		return Response{}, fmt.Errorf("missing Gemini model")
	}
	payload := geminiRequest{
		Contents: toGeminiContents(req.Messages),
		Tools:    toGeminiTools(req.Tools),
	}
	if sys := geminiSystemInstruction(req.Messages); sys != nil {
		payload.SystemInstruction = sys
	}
	if req.Temperature > 0 || req.MaxOutputTokens > 0 {
		payload.GenerationConfig = &geminiGenerationConfig{}
		if req.Temperature > 0 {
			payload.GenerationConfig.Temperature = &req.Temperature
		}
		if req.MaxOutputTokens > 0 {
			payload.GenerationConfig.MaxOutputTokens = &req.MaxOutputTokens
		}
	}

	var response geminiResponse
	url := fmt.Sprintf(geminiEndpointTemplate, req.Model, p.apiKey)
	if err := doJSON(ctx, p.client, "POST", url, nil, payload, &response); err != nil {
		return Response{}, err
	}
	text := response.FirstText()
	calls := response.ToolCalls()
	if text == "" && len(calls) == 0 {
		return Response{}, fmt.Errorf("empty Gemini response")
	}
	return Response{Content: text, ToolCalls: calls}, nil
}

func (p *GeminiProvider) StreamComplete(ctx context.Context, req Request, callbacks StreamCallbacks) (Response, error) {
	if p.apiKey == "" {
		return Response{}, fmt.Errorf("missing Gemini API key")
	}
	if req.Model == "" {
		return Response{}, fmt.Errorf("missing Gemini model")
	}
	payload := geminiRequest{
		Contents: toGeminiContents(req.Messages),
	}
	if sys := geminiSystemInstruction(req.Messages); sys != nil {
		payload.SystemInstruction = sys
	}
	if req.Temperature > 0 || req.MaxOutputTokens > 0 {
		payload.GenerationConfig = &geminiGenerationConfig{}
		if req.Temperature > 0 {
			payload.GenerationConfig.Temperature = &req.Temperature
		}
		if req.MaxOutputTokens > 0 {
			payload.GenerationConfig.MaxOutputTokens = &req.MaxOutputTokens
		}
	}
	var builder strings.Builder
	streamURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?key=%s&alt=sse", url.PathEscape(req.Model), p.apiKey)
	err := doSSE(ctx, p.client, "POST", streamURL, nil, payload, func(_ string, data []byte) error {
		var response geminiResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return nil
		}
		delta := response.FirstText()
		if delta == "" {
			return nil
		}
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
		return Response{}, fmt.Errorf("empty Gemini response")
	}
	return Response{Content: text}, nil
}

type geminiRequest struct {
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	Contents          []geminiContent         `json:"contents"`
	Tools             []geminiTool            `json:"tools,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

func (r geminiResponse) FirstText() string {
	for _, cand := range r.Candidates {
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				return part.Text
			}
		}
	}
	return ""
}

func (r geminiResponse) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, cand := range r.Candidates {
		for _, part := range cand.Content.Parts {
			if part.FunctionCall == nil {
				continue
			}
			id := part.FunctionCall.ID
			if id == "" {
				id = part.FunctionCall.Name
			}
			calls = append(calls, ToolCall{ID: id, Name: part.FunctionCall.Name, Arguments: part.FunctionCall.Args})
		}
	}
	return calls
}

func geminiSystemInstruction(messages []Message) *geminiContent {
	var parts []geminiPart
	for _, msg := range messages {
		if strings.ToLower(msg.Role) == "system" && msg.Content != "" {
			parts = append(parts, geminiPart{Text: msg.Content})
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return &geminiContent{Parts: parts}
}

func toGeminiContents(messages []Message) []geminiContent {
	out := make([]geminiContent, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(msg.Role)
		if role == "system" {
			continue
		}
		if role == "tool" {
			out = append(out, geminiContent{
				Role: "function",
				Parts: []geminiPart{{
					FunctionResponse: &geminiFunctionResponse{
						Name: msg.ToolName,
						Response: map[string]any{
							"tool_call_id": msg.ToolCallID,
							"output":       msg.Content,
						},
					},
				}},
			})
			continue
		}
		mapped := role
		if mapped == "assistant" {
			mapped = "model"
		}
		if len(msg.ToolCalls) > 0 {
			parts := make([]geminiPart, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						ID:   call.ID,
						Name: call.Name,
						Args: call.Arguments,
					},
				})
			}
			out = append(out, geminiContent{Role: "model", Parts: parts})
			continue
		}
		out = append(out, geminiContent{
			Role: mapped,
			Parts: []geminiPart{{
				Text: msg.Content,
			}},
		})
	}
	if len(out) == 0 {
		out = append(out, geminiContent{
			Role:  "user",
			Parts: []geminiPart{{Text: ""}},
		})
	}
	return out
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type geminiFunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

func toGeminiTools(tools []ToolDefinition) []geminiTool {
	if len(tools) == 0 {
		return nil
	}
	declarations := make([]geminiFunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		declarations = append(declarations, geminiFunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		})
	}
	return []geminiTool{{FunctionDeclarations: declarations}}
}
