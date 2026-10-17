package bootstrap

import (
	"context"
	"encoding/json"

	"fmt"

	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"

	"github.com/atomicobject/rhizome/pkg/llm"

	"github.com/atomicobject/rhizome/pkg/paths"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, val := range values {
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func planAutoToolCalls(ctx context.Context, params CodexParams, vaultPath string, userQueries, userFiles []string) ([]agentchat.ToolCall, []string) {
	warnings := []string{}
	if params.OnboardInstructions == nil {
		return nil, []string{"auto planning disabled: onboarding instruction loader is not configured"}
	}
	globalCfg, localCfg := loadLLMConfigs(ctx, vaultPath)
	resolved, err := llm.ResolveProfile(vaultconfig.FromContext(ctx), params.Profile, localCfg, globalCfg, params.Model)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("auto planning disabled: %v", err))
		return nil, warnings
	}
	provider, err := llm.NewProvider(resolved)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("auto planning disabled: %v", err))
		return nil, warnings
	}
	instructions, err := params.OnboardInstructions()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("auto planning instructions unavailable: %v", err))
	}
	tools := []agentchat.ToolSpec{
		{
			Name:        "semantic_query",
			Description: "Find task-shaped code/doc context. Returns must-read evidence plus raw matches. Prefer one call with multiple queries.",
			ArgsSchema:  `{"queries":["string"],"paths":["string"],"limit":25}`,
		},
		{
			Name:        "file_context",
			Description: "Fetch context for files or directories. Prefer one call with multiple files.",
			ArgsSchema:  `{"files":["string"]}`,
		},
	}
	calls, err := agentchat.PlanToolCalls(ctx, provider, resolved, params.Task, instructions, tools, agentchat.PlanHints{Queries: userQueries, Files: userFiles})
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("auto planning failed: %v", err))
		return nil, warnings
	}
	filtered := make([]agentchat.ToolCall, 0, len(calls))
	for _, call := range calls {
		switch call.Name {
		case "semantic_query", "file_context":
			filtered = append(filtered, call)
		case "vault_context":
			warnings = append(warnings, "auto planning returned vault_context; skipping because it is always included")
		default:
			warnings = append(warnings, fmt.Sprintf("unsupported tool call ignored: %s", call.Name))
		}
	}
	return filtered, warnings
}

func mergeToolCalls(primary, secondary []agentchat.ToolCall) []agentchat.ToolCall {
	seen := make(map[string]struct{})
	out := make([]agentchat.ToolCall, 0, len(primary)+len(secondary))
	add := func(call agentchat.ToolCall) {
		key := toolCallKey(call)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, call)
	}
	for _, call := range primary {
		add(call)
	}
	for _, call := range secondary {
		add(call)
	}
	return out
}

func toolCallKey(call agentchat.ToolCall) string {
	if call.Name == "" {
		return ""
	}
	args := call.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return call.Name + "|" + string(b)
}

func filterFileSeeds(vaultPath string, files []string) ([]string, []string) {
	warnings := []string{}
	if vaultPath == "" {
		return files, warnings
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return files, warnings
	}
	out := make([]string, 0, len(files))
	for _, file := range files {
		trimmed := strings.TrimSpace(file)
		if trimmed == "" {
			continue
		}
		abs := trimmed
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(vaultPaths.Root(), abs)
		}
		if _, err := vaultPaths.RelStrict(abs); err != nil {
			warnings = append(warnings, fmt.Sprintf("skipping file seed outside vault: %s", trimmed))
			continue
		}
		out = append(out, trimmed)
	}
	return out, warnings
}
