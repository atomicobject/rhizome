// Package hook is the Rhizome hook dispatcher behind `rzm hook <harness>
// <event>`: it turns a host's hook payload into one normalized event, runs
// the handlers of enabled starters that have something to act on, and
// returns their context to the agent. It never blocks or fails a tool call.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US5]]
package hook

import (
	"strings"
	"time"
)

const (
	// Deadline covers one whole invocation: payload parsing, lock waits,
	// handlers, output, and state.
	Deadline = 4 * time.Second
	// HostTimeoutSeconds is the timeout installed entries give the host,
	// slightly above Deadline so the dispatcher always finishes first.
	HostTimeoutSeconds = 5
	// ProtocolVersion is printed by `rzm hook --check`, which init uses to
	// tell whether the selected binary can run installed entries.
	ProtocolVersion = 1
)

// Harnesses that run installed entries.
const (
	HarnessClaude = "claude"
	HarnessCodex  = "codex"
)

// Hook points Rhizome installs one entry for in each enabled harness.
// Handlers declare the first three; post-compact is the dispatcher's own.
const (
	SessionStart  = "session-start"
	SubagentStart = "subagent-start"
	PostToolUse   = "post-tool-use"
	// PostCompact follows a compaction. Neither host starts a subagent's
	// context again after one, so the dispatcher resets that context and
	// delivers its start items with its next tool call. Codex's source shows
	// a subagent's compaction running it with the agent ID; Claude Code's
	// doing so is expected but not yet verified live.
	PostCompact = "post-compact"
)

// HookPoints lists the installed hook points in a fixed order. Adding one is
// the only change that alters installed entries.
var HookPoints = []string{SessionStart, SubagentStart, PostToolUse, PostCompact}

// HandlerEvents lists the hook points a starter handler may declare.
var HandlerEvents = []string{SessionStart, SubagentStart, PostToolUse}

// HostEvent is the event name Claude Code and Codex both use for a hook
// point, or "" for an unknown one.
func HostEvent(point string) string {
	switch point {
	case SessionStart:
		return "SessionStart"
	case SubagentStart:
		return "SubagentStart"
	case PostToolUse:
		return "PostToolUse"
	case PostCompact:
		return "PostCompact"
	}
	return ""
}

// Tool classes a post-tool-use event can carry.
const (
	ToolRead  = "read"
	ToolEdit  = "edit"
	ToolShell = "shell"
	ToolPatch = "patch"
)

// Handler is one starter hook declaration from Rhizome's built-in registry.
type Handler struct {
	Starter string
	ID      string
	// Delivers says in a few words what the handler gives agents, for init's
	// summary.
	Delivers string
	Events   []string
	Tools    []string
	// Skill is the starter skill whose installed folder the command runs
	// from; Command names it as {skill} until it is resolved.
	Skill string
	// Command is the argument list, relative to the repository root.
	Command []string
	Timeout time.Duration
	// CommandPrefixes is a repository-relative JSON file listing the command
	// prefixes that start the handler for a shell call with no recognized
	// read. Empty means shell calls need a recognized read.
	CommandPrefixes string
}

// Key names the handler in status and diagnostics.
func (h Handler) Key() string { return h.Starter + "/" + h.ID }

func has(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// EntryCommand is the shell command an installed entry runs. Init and the
// dispatcher's own marker check share it.
func EntryCommand(harness, point string) string {
	return "rzm hook " + harness + " " + point
}

// IsEntryCommand reports whether command is a Rhizome entry's command in any
// platform form, which is how init recognizes the entries it owns.
func IsEntryCommand(command string, args []string) bool {
	fields := append(strings.Fields(command), args...)
	if len(fields) < 2 {
		return false
	}
	exe := strings.ToLower(fields[0][strings.LastIndexAny(fields[0], `/\`)+1:])
	return (exe == "rzm" || exe == "rzm.exe") && fields[1] == "hook"
}
