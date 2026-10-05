package claude

import "encoding/json"

// These types model only the stream-json surface observed in Claude Code 2.1.269.
type message struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Version   string          `json:"claude_code_version,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
	Response  json.RawMessage `json:"response,omitempty"`
	Event     *streamEvent    `json:"event,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Result    string          `json:"result,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Usage     *usage          `json:"usage,omitempty"`
}

type controlRequest struct {
	Subtype               string                 `json:"subtype"`
	Hooks                 *map[string]any        `json:"hooks,omitempty"`
	ToolName              string                 `json:"tool_name,omitempty"`
	Input                 map[string]any         `json:"input,omitempty"`
	Description           string                 `json:"description,omitempty"`
	ToolUseID             string                 `json:"tool_use_id,omitempty"`
	PermissionSuggestions []permissionSuggestion `json:"permission_suggestions,omitempty"`
}

// permissionSuggestion keeps the vendor's suggestion verbatim so it can be
// echoed back unchanged in updatedPermissions; only the fields the driver
// inspects are decoded.
type permissionSuggestion struct {
	Type        string
	Mode        string
	Destination string
	raw         json.RawMessage
}

func (p *permissionSuggestion) UnmarshalJSON(data []byte) error {
	var fields struct {
		Type        string `json:"type"`
		Mode        string `json:"mode"`
		Destination string `json:"destination"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	p.Type, p.Mode, p.Destination = fields.Type, fields.Mode, fields.Destination
	p.raw = append(json.RawMessage(nil), data...)
	return nil
}

func (p permissionSuggestion) MarshalJSON() ([]byte, error) {
	if len(p.raw) > 0 {
		return p.raw, nil
	}
	return json.Marshal(map[string]string{"type": p.Type, "mode": p.Mode, "destination": p.Destination})
}

type initializeResponse struct {
	Subtype   string `json:"subtype"`
	RequestID string `json:"request_id"`
	Response  struct {
		Models []struct {
			Value                 string   `json:"value"`
			ResolvedModel         string   `json:"resolvedModel"`
			DisplayName           string   `json:"displayName"`
			IsDefault             bool     `json:"isDefault"`
			SupportedEffortLevels []string `json:"supportedEffortLevels"`
		} `json:"models"`
		Account *struct {
			Email            string `json:"email"`
			SubscriptionType string `json:"subscriptionType"`
		} `json:"account"`
	} `json:"response"`
}

type streamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	} `json:"delta"`
}

type contentMessage struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
	Content   any            `json:"content"`
	IsError   bool           `json:"is_error"`
	ExitCode  *int           `json:"exit_code"`
}

type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	OutputTokenDetails       struct {
		ThinkingTokens int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func initializeMessage(id string) message {
	hooks := map[string]any{}
	request, _ := json.Marshal(controlRequest{Subtype: "initialize", Hooks: &hooks})
	return message{Type: "control_request", RequestID: id, Request: request}
}

func userMessage(prompt string) message {
	content, _ := json.Marshal(struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: prompt})
	return message{Type: "user", Message: content}
}
