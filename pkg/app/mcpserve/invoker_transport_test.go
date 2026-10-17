package mcpserve

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestServerInvokerPreservesArgumentsWriteModeAndCleanup(t *testing.T) {
	for _, readWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "read only", true: "read write"}[readWrite], func(t *testing.T) {
			root := t.TempDir()
			definition := obsidian.VaultDefinition{Path: root}
			config := agentapi.Config{VaultDef: definition, VaultPath: root}
			var received map[string]any
			cleanups := 0
			dispatch, err := NewInvoker(t.Context(), readWrite, Dependencies{
				WorkingDirectory: root,
				VaultDefinition:  func(context.Context) (obsidian.VaultDefinition, error) { return definition, nil },
				RuntimeFree:      func() (agentapi.Config, error) { return config, nil },
				Prepare: func(_ context.Context, _ string, _ oneshotruntime.OperationID, input map[string]any) (agentapi.Config, func(), error) {
					received = input
					return config, func() { cleanups++ }, nil
				},
			})
			require.NoError(t, err)
			server, err := NewServer([]string{"check_paths", "node_link"}, dispatch)
			require.NoError(t, err)

			message := server.HandleMessage(t.Context(), json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"check_paths","arguments":{"paths":[{"path":"notes/new.md","isDir":false}]}}}`))
			var response struct {
				Result mcp.CallToolResult `json:"result"`
			}
			require.NoError(t, unmarshalMessage(message, &response))
			require.False(t, response.Result.IsError)
			require.Equal(t, map[string]any{"paths": []any{map[string]any{"path": "notes/new.md", "isDir": false}}}, received)
			require.Len(t, response.Result.Content, 1)
			text, ok := response.Result.Content[0].(mcp.TextContent)
			require.True(t, ok)
			require.JSONEq(t, `{"paths":[{"input":"notes/new.md","path":"notes/new.md","status":"allowed"}]}`, text.Text)
			require.Equal(t, 1, cleanups)

			message = server.HandleMessage(t.Context(), json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"node_link","arguments":{"ensure":"apply"}}}`))
			require.NoError(t, unmarshalMessage(message, &response))
			require.True(t, response.Result.IsError)
			require.Len(t, response.Result.Content, 1)
			text, ok = response.Result.Content[0].(mcp.TextContent)
			require.True(t, ok)
			if readWrite {
				require.Contains(t, text.Text, "provide refs or targets")
			} else {
				require.Contains(t, text.Text, "apply_requires_read_write")
			}
			require.Equal(t, 2, cleanups)
			require.NoFileExists(t, root+"/.rhizome/db.sqlite")
		})
	}
}

func TestInvokerUsesBoundRecipePlanner(t *testing.T) {
	root := t.TempDir()
	definition := obsidian.VaultDefinition{Path: root}
	config := agentapi.Config{VaultDef: definition, VaultPath: root}
	cleaned := false
	dispatch, err := NewInvoker(t.Context(), false, Dependencies{
		WorkingDirectory: root,
		VaultDefinition:  func(context.Context) (obsidian.VaultDefinition, error) { return definition, nil },
		RuntimeFree:      func() (agentapi.Config, error) { return config, nil },
		Prepare: func(context.Context, string, oneshotruntime.OperationID, map[string]any) (agentapi.Config, func(), error) {
			t.Fatal("recipe run used the unbound planner")
			return config, nil, nil
		},
		PrepareBound: func(_ context.Context, name string, operation oneshotruntime.OperationID, input map[string]any) (agentapi.Config, func(), error) {
			require.Equal(t, "query_recipe", name)
			require.Equal(t, oneshotruntime.OperationID("agent.query-recipe.run"), operation)
			require.Equal(t, "run", input["op"])
			return config, func() { cleaned = true }, nil
		},
	})
	require.NoError(t, err)
	_, err = dispatch(t.Context(), "query_recipe", map[string]any{"op": "run"})
	require.ErrorContains(t, err, "query_recipe")
	require.True(t, cleaned)
	require.NoFileExists(t, root+"/.rhizome/db.sqlite")
}
