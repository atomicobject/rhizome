package claude

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestMessageMapping(t *testing.T) {
	tests := []struct {
		name, line string
		kinds      []harness.EventKind
		complete   bool
	}{
		{"text", `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}},"session_id":"sid"}`, []harness.EventKind{harness.EventAssistantTextDelta}, false},
		{"reasoning", `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"why"}},"session_id":"sid"}`, []harness.EventKind{harness.EventReasoning}, false},
		{"command", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"bash","name":"Bash","input":{"command":"go test"}}]},"session_id":"sid"}`, []harness.EventKind{harness.EventCommandExecution}, false},
		{"file", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"write","name":"Write","input":{"file_path":"a.go"}}]},"session_id":"sid"}`, []harness.EventKind{harness.EventFileChange}, false},
		{"tool", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"read","name":"Read","input":{"file_path":"a.go"}}]},"session_id":"sid"}`, []harness.EventKind{harness.EventToolCall}, false},
		{"diagnostic", `{"type":"rate_limit_event","session_id":"sid"}`, []harness.EventKind{harness.EventDiagnostic}, false},
		{"success", `{"type":"result","subtype":"success","result":"done","usage":{"input_tokens":1,"cache_creation_input_tokens":2,"cache_read_input_tokens":3,"output_tokens":4}}`, []harness.EventKind{harness.EventTokenUsage, harness.EventTurnCompleted}, true},
		{"error", `{"type":"result","subtype":"error_max_turns","result":"stopped","is_error":true}`, []harness.EventKind{harness.EventTokenUsage, harness.EventError, harness.EventTurnCompleted}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var msg message
			require.NoError(t, json.Unmarshal([]byte(test.line), &msg))
			events, complete := mapMessage(msg, "turn", map[string]harness.EventKind{})
			require.Equal(t, test.kinds, eventKinds(events))
			require.Equal(t, test.complete, complete)
		})
	}
}

func TestToolResultUpdatesOriginalKind(t *testing.T) {
	items := map[string]harness.EventKind{"bash": harness.EventCommandExecution}
	var msg message
	require.NoError(t, json.Unmarshal([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash","content":"failed","is_error":true}]}}`), &msg))
	events, _ := mapMessage(msg, "turn", items)
	require.Len(t, events, 1)
	require.Equal(t, harness.EventCommandExecution, events[0].Kind)
	require.Equal(t, "failed", events[0].Status)
	require.Equal(t, "failed", events[0].Text)
}

func TestApprovalExtraction(t *testing.T) {
	request := controlRequest{Subtype: "can_use_tool", ToolName: "Bash", ToolUseID: "tool", Description: "run tests", Input: map[string]any{"command": "go test ./..."}}
	event := approvalEvent(message{SessionID: "sid", RequestID: "req"}, request, "turn")
	require.Equal(t, harness.EventApprovalRequested, event.Kind)
	require.Equal(t, "go test ./...", event.Command)
	require.Equal(t, "run tests", event.Reason)
	require.Equal(t, harness.PhaseStarted, event.Phase)
	require.False(t, event.AllowForSession)
	require.JSONEq(t, `{"command":"go test ./..."}`, string(event.Input))

	request.PermissionSuggestions = []permissionSuggestion{{Type: "addRules", Destination: "session"}}
	event = approvalEvent(message{SessionID: "sid", RequestID: "req"}, request, "turn")
	require.True(t, event.AllowForSession)
}
