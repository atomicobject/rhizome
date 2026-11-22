package claude

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
)

// ListMCPServers runs `claude mcp list` in dir, which loads the same user,
// project, plugin, and claude.ai connector configuration as a session with
// UserMCPConfig and health-checks each server (verified on Claude Code
// 2.1.330). Only names and statuses are kept; commands, URLs, and
// environment stay out of the result.
func (d *Driver) ListMCPServers(ctx context.Context, dir string) ([]harness.MCPServerState, error) {
	path, err := d.runner.LookPath(d.binary)
	if err != nil {
		return nil, harness.Phase(harness.ErrNotInstalled, err)
	}
	result, err := d.runner.Run(ctx, command.Spec{Path: path, Args: []string{"mcp", "list"}, Dir: dir})
	if err != nil {
		if ctx.Err() != nil {
			return nil, harness.Phase(harness.ErrTimeout, ctx.Err())
		}
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	if result.ExitCode != 0 {
		return nil, harness.Phase(harness.ErrCommandFailed, fmt.Errorf("claude mcp list: exit %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr)))
	}
	return parseMCPList(result.Stdout), nil
}

// parseMCPList reads lines of the form `<name>: <command or URL> - <mark>
// <status>`. The header, blank lines, and the empty-list sentence have no
// such separator and are skipped.
func parseMCPList(out string) []harness.MCPServerState {
	servers := []harness.MCPServerState{}
	for _, line := range strings.Split(out, "\n") {
		name, rest, ok := strings.Cut(strings.TrimSpace(line), ": ")
		i := strings.LastIndex(rest, " - ")
		if !ok || i < 0 {
			continue
		}
		status := strings.TrimSpace(rest[i+3:])
		if r, _ := utf8.DecodeRuneInString(status); !unicode.IsLetter(r) {
			_, status, _ = strings.Cut(status, " ") // the mark: ✔, ✗, !, ⏸, -
		}
		status = strings.ToLower(strings.TrimSpace(status))
		servers = append(servers, harness.MCPServerState{Name: name, Status: status, Usable: status == "connected"})
	}
	return servers
}
