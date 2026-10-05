package llm

import "context"

// ReasoningEffort captures the provider-specific reasoning effort tier.
type ReasoningEffort string

const (
	ReasoningNone   ReasoningEffort = "none"
	ReasoningLow    ReasoningEffort = "low"
	ReasoningMedium ReasoningEffort = "medium"
	ReasoningHigh   ReasoningEffort = "high"
	ReasoningXHigh  ReasoningEffort = "xhigh"
)

// Message is a single LLM message.
type Message struct {
	Role       string
	Content    string
	ToolCallID string
	ToolName   string
	ToolCalls  []ToolCall
}

// ToolDefinition describes one callable function available to a model.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// ToolCall is a model request to execute a named tool.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// Request captures a single LLM completion request.
type Request struct {
	Model           string
	Messages        []Message
	Tools           []ToolDefinition
	ReasoningEffort ReasoningEffort
	Temperature     float64
	MaxOutputTokens int
}

// Response captures a single LLM completion response.
type Response struct {
	Content   string
	ToolCalls []ToolCall
}

// Provider executes LLM requests for a specific backend.
type Provider interface {
	Complete(context.Context, Request) (Response, error)
}

// StreamingProvider can stream text deltas for requests that do not need tool
// calls. Tool decisions still use Complete so providers can return structured
// calls before the final answer streams.
type StreamingProvider interface {
	StreamComplete(context.Context, Request, StreamCallbacks) (Response, error)
}

// StreamCallbacks receives provider-native streaming lifecycle events and text
// deltas. Event payloads are intentionally generic because provider schemas
// differ.
type StreamCallbacks struct {
	Delta func(string) error
	Event func(string, map[string]any) error
}
