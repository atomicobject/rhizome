package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func mapNotification(message rpcMessage) (harness.Event, bool) {
	if ignoredNotification(message.Method) {
		return harness.Event{}, false
	}
	var params notificationEnvelope
	if len(message.Params) > 0 {
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return harness.Event{Kind: harness.EventDiagnostic, Text: fmt.Sprintf("could not decode %s: %v", message.Method, err)}, false
		}
	}
	base := harness.Event{
		SessionID: params.ThreadID,
		TurnID:    params.TurnID,
		ItemID:    params.ItemID,
	}
	switch message.Method {
	case "thread/started":
		return harness.Event{}, false
	case "turn/started":
		base.Kind = harness.EventTurnStarted
		base.TurnID = params.Turn.ID
		base.Status = params.Turn.Status
		base.Phase = harness.PhaseStarted
	case "turn/completed":
		base.Kind = harness.EventTurnCompleted
		base.TurnID = params.Turn.ID
		base.Status = params.Turn.Status
		base.Phase = phaseForStatus(params.Turn.Status, true)
		return base, true
	case "item/agentMessage/delta":
		base.Kind = harness.EventAssistantTextDelta
		base.Text = params.Delta
		base.Phase = harness.PhaseUpdated
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta", "item/plan/delta":
		base.Kind = harness.EventReasoning
		base.Text = params.Delta
		base.Phase = harness.PhaseUpdated
	case "item/commandExecution/outputDelta":
		base.Kind = harness.EventCommandExecution
		base.Text = params.Delta
		base.Output, base.Truncated = boundedJSON(params.Delta)
		base.Phase = harness.PhaseUpdated
	case "item/fileChange/outputDelta":
		base.Kind = harness.EventFileChange
		base.Text = params.Delta
		base.Output, base.Truncated = boundedJSON(params.Delta)
		base.Phase = harness.PhaseUpdated
	case "item/fileChange/patchUpdated":
		base.Kind = harness.EventFileChange
		base.Paths = changePaths(params.Changes)
		base.Diff = changeDiff(params.Changes)
		base.Phase = harness.PhaseUpdated
	case "item/mcpToolCall/progress":
		base.Kind = harness.EventToolCall
		base.Text = params.Message
		base.Phase = harness.PhaseUpdated
	case "item/started", "item/completed":
		return mapItem(base, params.Item, message.Method == "item/completed"), false
	case "thread/tokenUsage/updated":
		base.Kind = harness.EventTokenUsage
		base.Usage = toHarnessUsage(params.TokenUsage.Total)
	case "error":
		var turnErr turnError
		if err := json.Unmarshal(params.Error, &turnErr); err != nil {
			base.Kind = harness.EventDiagnostic
			base.Text = fmt.Sprintf("could not decode error notification: %v", err)
			break
		}
		base.Kind = harness.EventError
		base.Phase = harness.PhaseFailed
		base.Text = turnErr.Message
		if turnErr.AdditionalDetails != "" {
			base.Text += ": " + turnErr.AdditionalDetails
		}
		if params.WillRetry {
			base.Status = "retrying"
		}
	case "warning":
		base.Kind = harness.EventDiagnostic
		base.Text = params.Message
	default:
		base.Kind = harness.EventDiagnostic
		base.Text = "unknown Codex notification: " + message.Method
	}
	return base, false
}

func mapItem(base harness.Event, item threadItem, completed bool) harness.Event {
	base.ItemID = item.ID
	base.Status = item.Status
	base.Phase = phaseForStatus(item.Status, completed)
	switch item.Type {
	case "commandExecution":
		base.Kind = harness.EventCommandExecution
		base.Command = item.Command
		base.Cwd = item.Cwd
		base.Text = item.AggregatedOutput
		base.Output, base.Truncated = boundedJSON(item.AggregatedOutput)
		base.ExitCode = item.ExitCode
	case "fileChange":
		base.Kind = harness.EventFileChange
		base.Paths = changePaths(item.Changes)
		base.Diff = changeDiff(item.Changes)
	case "mcpToolCall", "dynamicToolCall", "functionCallOutput":
		base.Kind = harness.EventToolCall
		base.Name = strings.Trim(strings.Join([]string{item.Server, item.Tool, item.Name}, "/"), "/")
		base.Input = append(json.RawMessage(nil), item.Arguments...)
		base.Output, base.Truncated = boundedRaw(item.Result)
	case "reasoning", "plan":
		base.Kind = harness.EventReasoning
		base.Text = item.Text
	case "agentMessage":
		if completed {
			base.Kind = harness.EventAssistantTextDelta
			base.Text = item.Text
		}
	case "userMessage":
		// The user prompt is already known to the caller.
	default:
		base.Kind = harness.EventDiagnostic
		base.Text = "Codex item lifecycle: " + item.Type
	}
	return base
}

func ignoredNotification(method string) bool {
	return strings.HasPrefix(method, "hook/") || method == "mcpServer/startupStatus/updated" ||
		method == "thread/status/changed" || method == "remoteControl/status/changed" ||
		method == "account/rateLimits/updated"
}

func changePaths(changes []fileChange) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.Path != "" {
			paths = append(paths, change.Path)
		}
	}
	return paths
}

func changeDiff(changes []fileChange) string {
	var diffs []string
	for _, change := range changes {
		if change.Diff != "" {
			diffs = append(diffs, change.Diff)
		}
	}
	return strings.Join(diffs, "\n")
}

func phaseForStatus(status string, completed bool) string {
	switch strings.ToLower(status) {
	case "failed", "error":
		return harness.PhaseFailed
	case "declined", "denied", "cancelled", "canceled", "interrupted":
		return harness.PhaseDeclined
	case "completed", "success", "succeeded":
		return harness.PhaseCompleted
	}
	if completed {
		return harness.PhaseCompleted
	}
	return harness.PhaseStarted
}

const maxOutputBytes = 16 * 1024

func boundedJSON(value any) (json.RawMessage, bool) {
	raw, _ := json.Marshal(value)
	return boundedRaw(raw)
}

func boundedRaw(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) <= maxOutputBytes {
		return append(json.RawMessage(nil), raw...), false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		runes := []byte(text)
		if len(runes) > maxOutputBytes-2 {
			runes = runes[:maxOutputBytes-2]
		}
		encoded, _ := json.Marshal(string(runes))
		return encoded, true
	}
	encoded, _ := json.Marshal(string(raw[:maxOutputBytes-2]))
	return encoded, true
}

func toHarnessUsage(usage tokenUsage) harness.TokenUsage {
	return harness.TokenUsage{
		InputTokens:           usage.InputTokens,
		CachedInputTokens:     usage.CachedInputTokens,
		OutputTokens:          usage.OutputTokens,
		ReasoningOutputTokens: usage.ReasoningOutputTokens,
		TotalTokens:           usage.TotalTokens,
	}
}
