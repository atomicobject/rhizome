package harness

import (
	"context"
	"fmt"
	"strings"
)

// Environment parses the server's explicit KEY=value entries; the last value wins.
func (s MCPServer) Environment() (map[string]string, error) {
	env := make(map[string]string, len(s.Env))
	for _, entry := range s.Env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid environment entry %q", entry)
		}
		env[key] = value
	}
	return env, nil
}

// MCPServerState is one MCP server a harness CLI lists for the person.
type MCPServerState struct {
	Name string `json:"name"`
	// Status is the CLI's report in lower case. Claude Code checks each
	// server ("connected", "needs authentication", "failed to connect",
	// "pending approval"); Codex reports "enabled", "disabled", or
	// "not logged in".
	Status string `json:"status"`
	// Usable is false when a session would not get the server's tools.
	Usable bool `json:"usable"`
}

// MCPLister is a Harness that lists the MCP servers its sessions load with
// SessionOptions.UserMCPConfig, as the CLI reports them in dir. Listing
// spawns the CLI, and Claude Code starts every server to check it, so
// callers keep it off hot paths.
type MCPLister interface {
	ListMCPServers(ctx context.Context, dir string) ([]MCPServerState, error)
}
