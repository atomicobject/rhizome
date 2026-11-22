package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/loginpath"
)

// harnessResolver is an AgentRunner that names the harness a prompt run
// would use without starting a session. HarnessAgent is one; requirement
// checks and doctor need it for requires.mcp, deny, and sign-in.
type harnessResolver interface {
	Harness(ctx context.Context, task string) (harness.Kind, harness.Harness, error)
}

// missingRequirements reports what a task declares it needs and lacks
// (SPEC-0127 US11), before its check or prompt runs: requires.commands not
// on the login shell's PATH, requires.mcp servers the prompt's harness does
// not list as usable, and a deny list that harness cannot enforce. Only a
// task that declares requires.mcp or deny asks the harness anything, and
// listing MCP servers spawns its CLI, so status must not call this; it
// reads a run's recorded Missing instead (requirementsBlocked).
func (s *Service) missingRequirements(ctx context.Context, def Definition) []string {
	var missing []string
	for _, name := range def.Requires.Commands {
		if name == "rzm" {
			continue // the runtime's own rzm leads the task PATH
		}
		if _, err := loginpath.Look(name); err != nil {
			missing = append(missing, err.Error())
		}
	}
	if len(def.Requires.MCP) == 0 && len(def.Deny) == 0 {
		return missing
	}
	resolver, ok := s.opts.Agent.(harnessResolver)
	if !ok {
		return append(missing, "no agent harness is available to check requires.mcp and deny")
	}
	kind, driver, err := resolver.Harness(ctx, def.ID)
	if err != nil {
		return append(missing, "resolve the agent harness: "+err.Error())
	}
	if len(def.Deny) > 0 && !enforcesDeny(kind) {
		missing = append(missing, fmt.Sprintf("%s cannot enforce deny; use Claude Code for this task or remove deny", kind))
	}
	if len(def.Requires.MCP) == 0 {
		return missing
	}
	servers, err := listMCP(ctx, kind, driver, s.opts.Repo.MainCheckout)
	if err != nil {
		return append(missing, err.Error())
	}
	return append(missing, missingMCP(kind, def.Requires.MCP, servers)...)
}

// enforcesDeny reports whether a harness's driver honors
// SessionOptions.DisallowedTools; Codex refuses a session that sets it
// (docs/reference/subsystems/harness.md).
func enforcesDeny(kind harness.Kind) bool { return kind == harness.KindClaude }

// mcpListTimeout bounds one harness's MCP listing, which health-checks every
// configured server while run.lock is held.
var mcpListTimeout = 30 * time.Second

func listMCP(ctx context.Context, kind harness.Kind, driver harness.Harness, dir string) ([]harness.MCPServerState, error) {
	lister, ok := driver.(harness.MCPLister)
	if !ok {
		return nil, fmt.Errorf("%s cannot list its MCP servers, so requires.mcp cannot be checked", kind)
	}
	probe, cancel := context.WithTimeout(ctx, mcpListTimeout)
	defer cancel()
	servers, err := lister.ListMCPServers(probe, dir)
	if err != nil && ctx.Err() == nil && errors.Is(probe.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("could not list %s MCP servers within %s", kind, mcpListTimeout)
	}
	if err != nil {
		return nil, fmt.Errorf("list %s MCP servers: %w", kind, err)
	}
	return servers, nil
}

// missingMCP names each required server the harness lacks or cannot use.
func missingMCP(kind harness.Kind, required []string, servers []harness.MCPServerState) []string {
	var missing []string
	for _, name := range required {
		if ok, detail := mcpState(kind, name, servers); !ok {
			missing = append(missing, detail)
		}
	}
	return missing
}

// mcpState says whether the harness lists a server as usable, and how.
func mcpState(kind harness.Kind, name string, servers []harness.MCPServerState) (bool, string) {
	i := slices.IndexFunc(servers, func(s harness.MCPServerState) bool { return s.Name == name })
	if i < 0 {
		return false, fmt.Sprintf("MCP server %q is not configured for %s", name, kind)
	}
	if kind == harness.KindCodex && servers[i].Usable {
		// `codex mcp list` reads configuration and starts nothing.
		return true, fmt.Sprintf("MCP server %q for codex is configured (Codex does not report connection status)", name)
	}
	return servers[i].Usable, fmt.Sprintf("MCP server %q for %s reports %q", name, kind, servers[i].Status)
}

func requirementsMessage(missing []string) string {
	return "missing requirements: " + strings.Join(missing, "; ")
}

// requirementsBlocked reports whether a task's latest run failed for a
// missing requirement. It reads only the record, so `rzm tasks status` can
// call it on every poll and report blockedBy "requires".
func requirementsBlocked(last *RunRecord) bool {
	return last != nil && last.Outcome == OutcomeFailed && len(last.Missing) > 0
}
