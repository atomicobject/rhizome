package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
		provider, err := resolveGenerateProvider(ctx, input.Provider, drivers, configured)
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

// resolveGenerateProvider returns the provider the caller named, else the
// user's agent.harness setting, else the only provider installed and signed in.
func resolveGenerateProvider(ctx context.Context, requested string, drivers map[harness.Kind]harness.Harness, configured func() (harness.Kind, error)) (harness.Kind, error) {
	var choices []string
	for kind := range drivers {
		choices = append(choices, string(kind))
	}
	slices.Sort(choices)
	if requested != "" {
		if drivers[harness.Kind(requested)] == nil {
			return "", fmt.Errorf("provider must be one of %s; got %q", strings.Join(choices, ", "), requested)
		}
		return harness.Kind(requested), nil
	}
	kind, err := configured()
	if err != nil {
		return "", fmt.Errorf("read the agent.harness setting: %w", err)
	}
	if drivers[kind] != nil {
		return kind, nil
	}
	var ready []string
	for _, choice := range choices {
		status, err := drivers[harness.Kind(choice)].Status(ctx)
		if err == nil && status.Installed && status.LoggedIn {
			ready = append(ready, choice)
		}
	}
	if len(ready) == 1 {
		return harness.Kind(ready[0]), nil
	}
	found := "no provider is installed and signed in"
	if len(ready) > 1 {
		found = strings.Join(ready, ", ") + " are installed and signed in"
	}
	return "", fmt.Errorf("name a provider (%s) or set agent.harness: it is unset and %s", strings.Join(choices, ", "), found)
}
