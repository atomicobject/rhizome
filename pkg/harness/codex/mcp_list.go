package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
)

// ListMCPServers runs `codex mcp list --json` in dir. It reads the same
// config.toml servers app-server sessions load, but does not start them, so
// a server's status is its configuration: enabled, disabled, or not logged
// in (verified on codex-cli 0.171.0). Only names and statuses are kept;
// commands, URLs, and environment stay out of the result.
func (d *Driver) ListMCPServers(ctx context.Context, dir string) ([]harness.MCPServerState, error) {
	path, err := d.runner.LookPath(d.binary)
	if err != nil {
		return nil, harness.Phase(harness.ErrNotInstalled, err)
	}
	result, err := d.runner.Run(ctx, command.Spec{Path: path, Args: []string{"mcp", "list", "--json"}, Dir: dir})
	if err != nil {
		if ctx.Err() != nil {
			return nil, harness.Phase(harness.ErrTimeout, ctx.Err())
		}
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	if result.ExitCode != 0 {
		return nil, harness.Phase(harness.ErrCommandFailed, fmt.Errorf("codex mcp list: exit %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr)))
	}
	var listed []struct {
		Name       string `json:"name"`
		Enabled    bool   `json:"enabled"`
		AuthStatus string `json:"auth_status"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &listed); err != nil {
		return nil, harness.Phase(harness.ErrDecode, err)
	}
	servers := make([]harness.MCPServerState, 0, len(listed))
	for _, server := range listed {
		state := harness.MCPServerState{Name: server.Name, Status: "enabled", Usable: true}
		switch {
		case !server.Enabled:
			state.Status, state.Usable = "disabled", false
		case server.AuthStatus == "not_logged_in":
			state.Status, state.Usable = "not logged in", false
		}
		servers = append(servers, state)
	}
	return servers, nil
}
