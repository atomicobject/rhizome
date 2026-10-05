package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func mapMessage(msg message, turnID string, items map[string]harness.EventKind) ([]harness.Event, bool) {
	base := harness.Event{SessionID: msg.SessionID, TurnID: turnID}
	switch msg.Type {
	case "stream_event":
		if msg.Event == nil || msg.Event.Type != "content_block_delta" {
			base.Kind, base.Text = harness.EventDiagnostic, "Claude stream event: "+streamEventName(msg.Event)
			return []harness.Event{base}, false
		}
		switch msg.Event.Delta.Type {
		case "text_delta":
			base.Kind, base.Text = harness.EventAssistantTextDelta, msg.Event.Delta.Text
			base.Phase = harness.PhaseUpdated
		case "thinking_delta":
			base.Kind, base.Text = harness.EventReasoning, msg.Event.Delta.Thinking
			base.Phase = harness.PhaseUpdated
		default:
			base.Kind, base.Text = harness.EventDiagnostic, "Claude stream delta: "+msg.Event.Delta.Type
			return []harness.Event{base}, false
		}
		return []harness.Event{base}, false
	case "assistant":
		var body contentMessage
		if err := json.Unmarshal(msg.Message, &body); err != nil {
			return diagnostic(base, "could not decode assistant message", err), false
		}
		var events []harness.Event
		for _, block := range body.Content {
			if block.Type != "tool_use" {
				continue
			}
			event := toolEvent(base, block.ID, block.Name, block.Input)
			items[block.ID] = event.Kind
			events = append(events, event)
		}
		return events, false
	case "user":
		var body contentMessage
		if err := json.Unmarshal(msg.Message, &body); err != nil {
			return diagnostic(base, "could not decode user message", err), false
		}
		var events []harness.Event
		for _, block := range body.Content {
			if block.Type != "tool_result" {
				continue
			}
			kind := items[block.ToolUseID]
			if kind == "" {
				kind = harness.EventToolCall
			}
			event := base
			event.Kind, event.ItemID, event.Status, event.Phase = kind, block.ToolUseID, "completed", harness.PhaseCompleted
			if block.IsError {
				event.Status = "failed"
				event.Phase = harness.PhaseFailed
			}
			event.Text = contentText(block.Content)
			event.Output, event.Truncated = boundedJSON(block.Content)
			event.ExitCode = block.ExitCode
			events = append(events, event)
		}
		return events, false
	case "result":
		usageEvent := base
		usageEvent.Kind, usageEvent.Usage = harness.EventTokenUsage, toHarnessUsage(msg.Usage)
		completed := base
		completed.Kind, completed.Status, completed.Text, completed.Usage = harness.EventTurnCompleted, msg.Subtype, msg.Result, usageEvent.Usage
		completed.Phase = harness.PhaseCompleted
		if msg.IsError {
			errEvent := base
			errEvent.Kind, errEvent.Text, errEvent.Status, errEvent.Phase = harness.EventError, msg.Result, msg.Subtype, harness.PhaseFailed
			completed.Phase = harness.PhaseFailed
			if strings.Contains(strings.ToLower(msg.Subtype), "interrupt") || strings.Contains(strings.ToLower(msg.Subtype), "cancel") {
				completed.Phase = harness.PhaseDeclined
			}
			return []harness.Event{usageEvent, errEvent, completed}, true
		}
		return []harness.Event{usageEvent, completed}, true
	case "rate_limit_event", "system", "control_cancel_request":
		base.Kind, base.Text = harness.EventDiagnostic, diagnosticName(msg)
		return []harness.Event{base}, false
	default:
		base.Kind, base.Text = harness.EventDiagnostic, "unknown Claude message: "+diagnosticName(msg)
		return []harness.Event{base}, false
	}
}

func toolEvent(base harness.Event, id, name string, input map[string]any) harness.Event {
	base.ItemID, base.Name = id, name
	base.Phase = harness.PhaseStarted
	base.Input, _ = json.Marshal(input)
	switch name {
	case "Bash":
		base.Kind = harness.EventCommandExecution
		base.Command = stringValue(input["command"])
	case "Write", "Edit", "MultiEdit":
		base.Kind = harness.EventFileChange
		if path := stringValue(input["file_path"]); path != "" {
			base.Paths = []string{path}
		}
		base.Diff = stringValue(input["diff"])
	default:
		base.Kind = harness.EventToolCall
	}
	return base
}

func approvalEvent(msg message, request controlRequest, turnID string) harness.Event {
	input, _ := json.Marshal(request.Input)
	event := harness.Event{Kind: harness.EventApprovalRequested, Phase: harness.PhaseStarted, SessionID: msg.SessionID, TurnID: turnID, ItemID: request.ToolUseID, RequestID: msg.RequestID, Name: request.ToolName, Reason: request.Description, AllowForSession: supportsAllowForSession(request), Input: input}
	if request.ToolName == "Bash" {
		event.Command = stringValue(request.Input["command"])
	}
	if request.ToolName == "Write" || request.ToolName == "Edit" || request.ToolName == "MultiEdit" {
		if path := stringValue(request.Input["file_path"]); path != "" {
			event.Paths = []string{path}
		}
	}
	return event
}

const maxOutputBytes = 16 * 1024

func boundedJSON(value any) (json.RawMessage, bool) {
	raw, _ := json.Marshal(value)
	if len(raw) <= maxOutputBytes {
		return raw, false
	}
	encoded, _ := json.Marshal(string(raw[:maxOutputBytes-2]))
	return encoded, true
}

func contentText(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, entry := range value {
			if object, ok := entry.(map[string]any); ok {
				if text := stringValue(object["text"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}
func stringValue(value any) string { text, _ := value.(string); return text }
func diagnostic(base harness.Event, prefix string, err error) []harness.Event {
	base.Kind = harness.EventDiagnostic
	base.Text = fmt.Sprintf("%s: %v", prefix, err)
	return []harness.Event{base}
}
func diagnosticName(msg message) string {
	if msg.Subtype != "" {
		return msg.Type + "/" + msg.Subtype
	}
	return msg.Type
}
func streamEventName(event *streamEvent) string {
	if event == nil || event.Type == "" {
		return "unknown"
	}
	return event.Type
}

func toHarnessUsage(value *usage) harness.TokenUsage {
	if value == nil {
		return harness.TokenUsage{}
	}
	totalInput := value.InputTokens + value.CacheCreationInputTokens + value.CacheReadInputTokens
	return harness.TokenUsage{InputTokens: value.InputTokens, CachedInputTokens: value.CacheReadInputTokens, OutputTokens: value.OutputTokens, ReasoningOutputTokens: value.OutputTokenDetails.ThinkingTokens, TotalTokens: totalInput + value.OutputTokens}
}
