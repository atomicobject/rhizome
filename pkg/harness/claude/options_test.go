package claude

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

// Allow-for-session must echo every session-scoped suggestion verbatim, not
// only setMode/acceptEdits; otherwise an addRules suggestion silently becomes a
// one-time allow.
func TestApprovalResponseEchoesSessionSuggestionsVerbatim(t *testing.T) {
	var request controlRequest
	require.NoError(t, json.Unmarshal([]byte(`{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"go test"},"permission_suggestions":[
		{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"go test:*"}],"behavior":"allow","destination":"session"},
		{"type":"setMode","mode":"acceptEdits","destination":"localSettings"}
	]}`), &request))
	msg := approvalResponse("req-1", request, harness.DecisionAllowForSession)
	var body struct {
		Response struct {
			Behavior           string            `json:"behavior"`
			UpdatedPermissions []json.RawMessage `json:"updatedPermissions"`
		} `json:"response"`
	}
	require.NoError(t, json.Unmarshal(msg.Response, &body))
	require.Equal(t, "allow", body.Response.Behavior)
	require.Len(t, body.Response.UpdatedPermissions, 1)
	require.JSONEq(t, `{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"go test:*"}],"behavior":"allow","destination":"session"}`, string(body.Response.UpdatedPermissions[0]))
}

func TestAutoPageToolOnStartAndResume(t *testing.T) {
	for _, resume := range []string{"", "session-old"} {
		args, err := sessionArgs(harness.SessionOptions{Resume: resume, PermissionMode: harness.PermissionAuto, Instructions: "Keep answers short.", MCPServers: []harness.MCPServer{{Name: "rhizome_page", Command: "rzm", AutoApproveTools: []string{"page_text"}}}}, "/tmp/project")
		require.NoError(t, err)
		require.Equal(t, "auto", argValue(t, args, "--permission-mode"))
		require.Equal(t, "Keep answers short.", argValue(t, args, "--append-system-prompt"))
		require.Contains(t, args, "mcp__rhizome_page__page_text")
	}
}
