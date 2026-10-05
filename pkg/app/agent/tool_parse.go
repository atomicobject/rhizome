package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

type toolCallEnvelope struct {
	ToolCalls []ToolCall `json:"tool_calls"`
}

// ParseToolCalls parses tool calls from a JSON payload.
func ParseToolCalls(raw string) ([]ToolCall, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	payload := trimmed
	if !strings.HasPrefix(payload, "{") {
		start := strings.Index(payload, "{")
		end := strings.LastIndex(payload, "}")
		if start >= 0 && end > start {
			payload = payload[start : end+1]
		}
	}

	var envelope toolCallEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return nil, fmt.Errorf("parse tool calls: %w", err)
	}
	return envelope.ToolCalls, nil
}
