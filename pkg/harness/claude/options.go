package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func approvalResponse(id string, request controlRequest, decision harness.Decision) message {
	body := map[string]any{"behavior": "deny", "message": "denied by user"}
	if decision == harness.DecisionAllow || decision == harness.DecisionAllowForSession {
		body = map[string]any{"behavior": "allow", "updatedInput": request.Input}
	}
	if decision == harness.DecisionAllowForSession {
		// Echo every session-scoped suggestion verbatim (setMode, addRules, ...)
		// so the session-wide permission the UI promised is what Claude applies.
		var session []permissionSuggestion
		for _, suggestion := range request.PermissionSuggestions {
			if suggestion.Destination == "session" {
				session = append(session, suggestion)
			}
		}
		if len(session) > 0 {
			body["updatedPermissions"] = session
		}
	}
	response, _ := json.Marshal(map[string]any{"subtype": "success", "request_id": id, "response": body})
	return message{Type: "control_response", Response: response}
}

func sessionArgs(options harness.SessionOptions, cwd string) ([]string, error) {
	mode, dangerous := permissionMode(options.PermissionMode)
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--strict-mcp-config", "--permission-prompt-tool", "stdio", "--permission-mode", mode}
	if dangerous {
		args = append(args, "--dangerously-skip-permissions")
	}
	if options.Model != "" {
		args = append(args, "--model", options.Model)
	}
	if options.Effort != "" {
		args = append(args, "--effort", options.Effort)
	}
	if options.Resume != "" {
		args = append(args, "--resume", options.Resume)
	}
	if options.Instructions != "" {
		args = append(args, "--append-system-prompt", options.Instructions)
	}
	if len(options.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, options.AllowedTools...)
	}
	if len(options.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools")
		args = append(args, options.DisallowedTools...)
	}
	if len(options.MCPServers) > 0 {
		config, err := mcpConfig(options.MCPServers)
		if err != nil {
			return nil, err
		}
		args = append(args, "--mcp-config", config)
	}
	return append(args, "--add-dir", cwd), nil
}

func permissionMode(mode harness.PermissionMode) (string, bool) {
	switch mode {
	case harness.PermissionAutoAcceptEdits:
		return "acceptEdits", false
	case harness.PermissionFullAccess:
		return "bypassPermissions", true
	default:
		return "default", false
	}
}

func mcpConfig(servers []harness.MCPServer) (string, error) {
	values := make(map[string]any, len(servers))
	for _, server := range servers {
		name := strings.TrimSpace(server.Name)
		if name == "" {
			return "", errors.New("MCP server name is required")
		}
		if _, exists := values[name]; exists {
			return "", fmt.Errorf("duplicate MCP server name %q", name)
		}
		if strings.TrimSpace(server.Command) == "" {
			return "", fmt.Errorf("MCP server %q command is required", name)
		}
		entry := map[string]any{"command": server.Command, "args": server.Args}
		if len(server.Env) > 0 {
			env, err := envMap(server.Env)
			if err != nil {
				return "", fmt.Errorf("MCP server %q: %w", name, err)
			}
			entry["env"] = env
		}
		values[name] = entry
	}
	encoded, err := json.Marshal(map[string]any{"mcpServers": values})
	return string(encoded), err
}

func envMap(entries []string) (map[string]string, error) {
	env := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid environment entry %q", entry)
		}
		env[key] = value
	}
	return env, nil
}
