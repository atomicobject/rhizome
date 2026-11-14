package codex

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/stretchr/testify/require"
)

func TestAutoStartAndResume(t *testing.T) {
	for _, resume := range []string{"", "thr_1"} {
		t.Run("resume="+resume, func(t *testing.T) {
			script := harnesstest.NewScriptedIO(
				`{"id":1,"result":{"userAgent":"codex-cli/0.169.0"}}`,
				`{"id":2,"result":{"thread":{"id":"thr_1"}}}`,
				`{"id":3,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`,
			)
			session, err := testDriver(script).StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project", Resume: resume, PermissionMode: harness.PermissionAuto, Instructions: "Keep answers short."})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, session.Stop()) })
			done := make(chan error, 1)
			go func() { done <- session.SendTurn(context.Background(), "edit") }()
			require.Eventually(t, func() bool { return script.WriteCount() >= 4 }, time.Second, time.Millisecond)
			script.Push(`{"method":"item/autoApprovalReview/started","params":{"threadId":"thr_1","turnId":"turn_1"}}`)
			script.Push(`{"method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
			require.NoError(t, <-done)
			writes := script.Writes()
			for _, index := range []int{2, 3} {
				var envelope rpcMessage
				require.NoError(t, json.Unmarshal([]byte(writes[index]), &envelope))
				var params map[string]any
				require.NoError(t, json.Unmarshal(envelope.Params, &params))
				require.Equal(t, "auto_review", params["approvalsReviewer"])
				require.Equal(t, "on-request", params["approvalPolicy"])
				if index == 2 {
					require.Equal(t, "workspace-write", params["sandbox"])
					require.Equal(t, "Keep answers short.", params["developerInstructions"])
				} else {
					require.Equal(t, map[string]any{"type": "workspaceWrite"}, params["sandboxPolicy"])
				}
			}
			require.NoError(t, session.Stop())
			for event := range session.Events() {
				require.NotEqual(t, harness.EventApprovalRequested, event.Kind)
			}
		})
	}
}

func TestMCPToolPreapproval(t *testing.T) {
	args, err := appServerArgs([]harness.MCPServer{{Name: "rhizome_page", Command: "rzm", AutoApproveTools: []string{"page_text"}}})
	require.NoError(t, err)
	require.Contains(t, args, `mcp_servers.rhizome_page.tools.page_text.approval_mode="approve"`)
}
