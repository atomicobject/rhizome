package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/teamkeys"
	"github.com/atomicobject/rhizome/pkg/typesafe"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/mark3labs/mcp-go/mcp"
)

// EvaluateTool evaluates supplied state with Jev. It needs neither an index nor
// a note runtime; only explicitly supplied content is sent to TypeSafe.
func EvaluateTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	handler := evaluateTool(newEvaluationClient)
	return func(ctx context.Context, call mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if config.Vault != nil {
			ctx = vaultconfig.WithEnv(ctx, config.Vault.Env)
		}
		return handler(ctx, call)
	}
}

func newEvaluationClient(ctx context.Context) (*typesafe.Client, error) {
	key := vaultconfig.ResolveValue(vaultconfig.FromContext(ctx), "TYPESAFE_API_KEY")
	if key == "" {
		key = teamkeys.TypeSafeKey(vaultconfig.FromContext(ctx))
	}
	if key == "" {
		return nil, fmt.Errorf("TypeSafe credential unavailable: configure TYPESAFE_API_KEY or unlock the Atomic key bundle with ATOMIC_RHIZOME_KEY")
	}
	return typesafe.NewClient(key)
}

func evaluateTool(newClient func(context.Context) (*typesafe.Client, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, call mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		data, err := json.Marshal(call.GetArguments())
		if err != nil {
			return mcp.NewToolResultError("evaluate: request is not JSON encodable"), nil
		}
		var request typesafe.Request
		if err := json.Unmarshal(data, &request); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		client, err := newClient(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		response, err := client.Evaluate(ctx, request)
		if err != nil {
			var apiError *typesafe.APIError
			if errors.As(err, &apiError) {
				payload, _ := json.Marshal(map[string]any{"code": "typesafe_api_error", "message": apiError.Error(), "statusCode": apiError.StatusCode, "requestId": apiError.RequestID, "retryAfterMs": apiError.RetryAfter.Milliseconds()})
				return mcp.NewToolResultError(string(payload)), nil
			}
			return mcp.NewToolResultError(err.Error()), nil
		}
		return respondJSON(struct {
			typesafe.Response
			RequestID string `json:"requestId,omitempty"`
		}{response, response.RequestID}, "marshal evaluate failed")
	}
}
