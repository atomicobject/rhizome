package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func permissionConfig(mode harness.PermissionMode) (string, string) {
	switch mode {
	case harness.PermissionAuto, harness.PermissionAutoAcceptEdits:
		return "on-request", "workspace-write"
	case harness.PermissionFullAccess:
		return "never", "danger-full-access"
	default:
		return "untrusted", "read-only"
	}
}

func sandboxPolicy(mode string) map[string]any {
	switch mode {
	case "workspace-write":
		return map[string]any{"type": "workspaceWrite"}
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	default:
		return map[string]any{"type": "readOnly"}
	}
}

func appServerArgs(servers []harness.MCPServer) ([]string, error) {
	args := []string{"app-server"}
	seen := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		name := strings.TrimSpace(server.Name)
		if name == "" || strings.ContainsAny(name, ".= \t\r\n") {
			return nil, fmt.Errorf("invalid MCP server name %q", server.Name)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate MCP server name %q", name)
		}
		seen[name] = struct{}{}
		if strings.TrimSpace(server.Command) == "" {
			return nil, fmt.Errorf("MCP server %q command is required", name)
		}
		args = appendConfig(args, "mcp_servers."+name+".command", server.Command)
		for _, tool := range server.AutoApproveTools {
			if tool == "" || strings.ContainsAny(tool, ".= \t\r\n") {
				return nil, fmt.Errorf("invalid MCP tool name %q", tool)
			}
			args = appendConfig(args, "mcp_servers."+name+".tools."+tool+".approval_mode", "approve")
		}
		if len(server.Args) > 0 {
			encoded, _ := json.Marshal(server.Args)
			args = append(args, "-c", "mcp_servers."+name+".args="+string(encoded))
		}
		if len(server.Env) > 0 {
			env, err := server.Environment()
			if err != nil {
				return nil, fmt.Errorf("MCP server %q: %w", name, err)
			}
			keys := make([]string, 0, len(env))
			for key := range env {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			// Codex splits override paths literally; environment keys belong in a TOML table.
			entries := make([]string, 0, len(keys))
			for _, key := range keys {
				quotedKey, _ := json.Marshal(key)
				quotedValue, _ := json.Marshal(env[key])
				entries = append(entries, string(quotedKey)+"="+string(quotedValue))
			}
			args = append(args, "-c", "mcp_servers."+name+".env={"+strings.Join(entries, ",")+"}")
		}
	}
	return args, nil
}

func appendConfig(args []string, key, value string) []string {
	encoded, _ := json.Marshal(value)
	return append(args, "-c", key+"="+string(encoded))
}

func approvalsReviewer(mode harness.PermissionMode) string {
	if mode == harness.PermissionAuto {
		return "auto_review"
	}
	return ""
}
