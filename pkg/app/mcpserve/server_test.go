package mcpserve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestServerAllowlistListsAndDispatchesSelectedTool(t *testing.T) {
	var calledName string
	var calledArgs map[string]any
	server, err := NewServer([]string{"vault_health", "files"}, func(_ context.Context, name string, args map[string]any) ([]byte, error) {
		calledName = name
		calledArgs = args
		return []byte(`{"ok":true}`), nil
	})
	require.NoError(t, err)
	filesDescriptor, ok := agentapi.CodeOperationDescriptor("files")
	require.True(t, ok)

	listMessage := server.HandleMessage(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	var listResponse struct {
		Result mcp.ListToolsResult `json:"result"`
	}
	require.NoError(t, unmarshalMessage(listMessage, &listResponse))
	require.Len(t, listResponse.Result.Tools, 2)
	require.Equal(t, []string{"files", "vault_health"}, []string{listResponse.Result.Tools[0].Name, listResponse.Result.Tools[1].Name})
	description := listResponse.Result.Tools[0].Description
	require.True(t, strings.HasPrefix(description, filesDescriptor.CodeContract.Summary))
	for _, effect := range filesDescriptor.CodeContract.Effects {
		require.Contains(t, description, effect)
	}
	for _, descriptor := range sharedDescriptors() {
		if descriptor.CodeContract == nil {
			continue
		}
		lower := strings.ToLower(toolDescription(descriptor.CodeContract))
		require.NotContains(t, lower, "code mode", descriptor.Name)
		require.NotContains(t, lower, "agent cli", descriptor.Name)
		switch descriptor.Name {
		case "file_context", "node_link", "note_rename_heading":
			require.Contains(t, lower, "applies changes when requested on a read-write connection", descriptor.Name)
		}
	}

	callMessage := server.HandleMessage(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"files","arguments":{"inputs":["README.md"]}}}`))
	var callResponse struct {
		Result mcp.CallToolResult `json:"result"`
	}
	require.NoError(t, unmarshalMessage(callMessage, &callResponse))
	require.Equal(t, "files", calledName)
	require.Equal(t, map[string]any{"inputs": []any{"README.md"}}, calledArgs)
	require.False(t, callResponse.Result.IsError)
	require.Len(t, callResponse.Result.Content, 1)
	text, ok := callResponse.Result.Content[0].(mcp.TextContent)
	require.True(t, ok)
	require.Equal(t, `{"ok":true}`, text.Text)
}

func unmarshalMessage(message mcp.JSONRPCMessage, target any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
