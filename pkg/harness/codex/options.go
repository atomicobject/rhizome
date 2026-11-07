package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func permissionConfig(mode harness.PermissionMode) (string, string) {
	switch mode {
	case harness.PermissionAutoAcceptEdits:
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
		if len(server.Args) > 0 {
			encoded, _ := json.Marshal(server.Args)
			args = append(args, "-c", "mcp_servers."+name+".args="+string(encoded))
		}
		if len(server.Env) > 0 {
			env, err := server.Environment()
			if err != nil {
				return nil, fmt.Errorf("MCP server %q: %w", name, err)
			}
			encoded, _ := json.Marshal(env)
			args = append(args, "-c", "mcp_servers."+name+".env="+string(encoded))
		}
	}
	return args, nil
}

func appendConfig(args []string, key, value string) []string {
	encoded, _ := json.Marshal(value)
	return append(args, "-c", key+"="+string(encoded))
}
