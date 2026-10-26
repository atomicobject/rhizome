package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/claude"
	"github.com/atomicobject/rhizome/pkg/harness/codex"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/mark3labs/mcp-go/mcp"
)

// generators live for the process so each driver's version-check cache spans calls.
var generators = map[harness.Kind]harness.Harness{
	harness.KindClaude: claude.New(),
	harness.KindCodex:  codex.New(),
}

// GenerateTool runs one tool-free, sessionless model call through pkg/harness
// without initializing a vault runtime.
func GenerateTool(_ Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, call mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		configured := func() (harness.Kind, error) {
			settings, err := agentchat.LoadSettings(vaultconfig.FromContext(ctx))
			return settings.Harness, err
		}
		return generateTool(generators, configured)(ctx, call)
	}
}

func generateTool(drivers map[harness.Kind]harness.Harness, configured func() (harness.Kind, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, call mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input struct {
			Provider string          `json:"provider"`
			Model    string          `json:"model"`
			Effort   string          `json:"effort"`
			Prompt   string          `json:"prompt"`
			Schema   json.RawMessage `json:"schema"`
		}
		data, err := json.Marshal(call.GetArguments())
		if err == nil {
			err = json.Unmarshal(data, &input)
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if strings.TrimSpace(input.Prompt) == "" {
			return mcp.NewToolResultError("prompt is required"), nil
		}
		provider, err := agentchat.ResolveHarness(ctx, input.Provider, drivers, configured)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if string(input.Schema) == "null" {
			input.Schema = nil
		}
		result, err := drivers[provider].Generate(ctx, harness.GenerateRequest{Prompt: input.Prompt, Schema: input.Schema, Model: input.Model, Effort: input.Effort})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return respondJSON(struct {
			Provider string `json:"provider"`
			harness.GenerateResult
		}{string(provider), result}, "marshal generate result failed")
	}
}
