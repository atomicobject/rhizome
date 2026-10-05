package codex

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestCapturedMCPStartupErrorEnvelopeIsIgnored(t *testing.T) {
	data, err := os.ReadFile("testdata/mcp-startup-error.golden.jsonl")
	require.NoError(t, err)
	transport := newJSONTransport(strings.NewReader(string(data)), io.Discard, nil)
	message, err := transport.Recv(context.Background())
	require.NoError(t, err)
	event, completed := mapNotification(message)
	require.Empty(t, event.Kind)
	require.False(t, completed)
}

func TestNotificationMapping(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		params    string
		kind      harness.EventKind
		completed bool
	}{
		{"thread started", "thread/started", `{"thread":{"id":"thr"}}`, "", false},
		{"turn started", "turn/started", `{"threadId":"thr","turn":{"id":"turn","status":"inProgress"}}`, harness.EventTurnStarted, false},
		{"turn completed", "turn/completed", `{"threadId":"thr","turn":{"id":"turn","status":"completed"}}`, harness.EventTurnCompleted, true},
		{"assistant", "item/agentMessage/delta", `{"threadId":"thr","turnId":"turn","itemId":"i","delta":"hi"}`, harness.EventAssistantTextDelta, false},
		{"assistant completed", "item/completed", `{"threadId":"thr","turnId":"turn","item":{"id":"i","type":"agentMessage","text":"hi"}}`, harness.EventAssistantTextDelta, false},
		{"user started", "item/started", `{"threadId":"thr","turnId":"turn","item":{"id":"i","type":"userMessage","text":"hi"}}`, "", false},
		{"user completed", "item/completed", `{"threadId":"thr","turnId":"turn","item":{"id":"i","type":"userMessage","text":"hi"}}`, "", false},
		{"reasoning", "item/reasoning/textDelta", `{"threadId":"thr","turnId":"turn","itemId":"i","delta":"why"}`, harness.EventReasoning, false},
		{"reasoning summary", "item/reasoning/summaryTextDelta", `{"threadId":"thr","turnId":"turn","itemId":"i","delta":"why"}`, harness.EventReasoning, false},
		{"command delta", "item/commandExecution/outputDelta", `{"threadId":"thr","turnId":"turn","itemId":"i","delta":"ok"}`, harness.EventCommandExecution, false},
		{"command completed", "item/completed", `{"threadId":"thr","turnId":"turn","item":{"id":"i","type":"commandExecution","command":"go test","cwd":"/repo","status":"completed","aggregatedOutput":"ok"}}`, harness.EventCommandExecution, false},
		{"file patch", "item/fileChange/patchUpdated", `{"threadId":"thr","turnId":"turn","itemId":"i","changes":[{"path":"a.go","kind":"update"}]}`, harness.EventFileChange, false},
		{"file completed", "item/completed", `{"threadId":"thr","turnId":"turn","item":{"id":"i","type":"fileChange","status":"completed","changes":[{"path":"a.go","kind":"update"}]}}`, harness.EventFileChange, false},
		{"tool call", "item/started", `{"threadId":"thr","turnId":"turn","item":{"id":"i","type":"mcpToolCall","server":"rhizome","tool":"search","status":"inProgress"}}`, harness.EventToolCall, false},
		{"tool progress", "item/mcpToolCall/progress", `{"threadId":"thr","turnId":"turn","itemId":"i","message":"working"}`, harness.EventToolCall, false},
		{"usage", "thread/tokenUsage/updated", `{"threadId":"thr","turnId":"turn","tokenUsage":{"total":{"inputTokens":1,"cachedInputTokens":2,"outputTokens":3,"reasoningOutputTokens":4,"totalTokens":10},"last":{}}}`, harness.EventTokenUsage, false},
		{"error", "error", `{"threadId":"thr","turnId":"turn","willRetry":false,"error":{"message":"boom"}}`, harness.EventError, false},
		{"warning", "warning", `{"threadId":"thr","turnId":"turn","message":"heads up"}`, harness.EventDiagnostic, false},
		{"mcp startup status", "mcpServer/startupStatus/updated", `{"threadId":"thr","error":"connection refused"}`, "", false},
		{"hook", "hook/started", `{"threadId":"thr"}`, "", false},
		{"thread status", "thread/status/changed", `{"threadId":"thr"}`, "", false},
		{"remote control status", "remoteControl/status/changed", `{"threadId":"thr"}`, "", false},
		{"rate limits", "account/rateLimits/updated", `{}`, "", false},
		{"unknown", "future/event", `{}`, harness.EventDiagnostic, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, completed := mapNotification(rpcMessage{Method: test.method, Params: json.RawMessage(test.params)})
			require.Equal(t, test.kind, event.Kind)
			require.Equal(t, test.completed, completed)
		})
	}
}

func TestWarningUsesWarningText(t *testing.T) {
	event, _ := mapNotification(rpcMessage{Method: "warning", Params: json.RawMessage(`{"message":"heads up"}`)})
	require.Equal(t, "heads up", event.Text)
}

func TestRichItemMapping(t *testing.T) {
	command, _ := mapNotification(rpcMessage{Method: "item/completed", Params: json.RawMessage(`{"threadId":"thr","turnId":"turn","item":{"id":"i","type":"commandExecution","command":"go test","status":"failed","aggregatedOutput":"boom","exitCode":2}}`)})
	require.Equal(t, harness.PhaseFailed, command.Phase)
	require.Equal(t, 2, *command.ExitCode)
	require.JSONEq(t, `"boom"`, string(command.Output))

	tool, _ := mapNotification(rpcMessage{Method: "item/started", Params: json.RawMessage(`{"threadId":"thr","turnId":"turn","item":{"id":"i","type":"mcpToolCall","server":"rhizome","tool":"search","arguments":{"query":"session"},"status":"inProgress"}}`)})
	require.Equal(t, harness.PhaseStarted, tool.Phase)
	require.JSONEq(t, `{"query":"session"}`, string(tool.Input))

	file, _ := mapNotification(rpcMessage{Method: "item/fileChange/patchUpdated", Params: json.RawMessage(`{"changes":[{"path":"a.go","kind":"update","diff":"@@ changed"}]}`)})
	require.Equal(t, "@@ changed", file.Diff)
}

func TestCommandOutputIsBounded(t *testing.T) {
	content, err := json.Marshal(map[string]any{"threadId": "thr", "turnId": "turn", "item": map[string]any{"id": "i", "type": "commandExecution", "aggregatedOutput": strings.Repeat("x", maxOutputBytes+100)}})
	require.NoError(t, err)
	event, _ := mapNotification(rpcMessage{Method: "item/completed", Params: content})
	require.Equal(t, harness.EventCommandExecution, event.Kind)
	require.True(t, event.Truncated)
	require.LessOrEqual(t, len(event.Output), maxOutputBytes)
	require.True(t, json.Valid(event.Output))
}
