package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Identity names the agent context an event belongs to. A field the host
// does not give stays empty; a child agent never borrows its parent's.
type Identity struct {
	Worktree string `json:"worktree"`
	Harness  string `json:"harness"`
	Session  string `json:"session"`
	AgentID  string `json:"agentId,omitempty"`
	Turn     string `json:"turn,omitempty"`
}

// Event is the one normalized event every handler receives.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US5-AC4]]
type Event struct {
	Harness string   `json:"harness"`
	Event   string   `json:"event"`
	Root    string   `json:"root"`
	Agent   Identity `json:"agent"`
	// Reset is "compact" or "clear" when the host reset this context.
	Reset   string   `json:"reset,omitempty"`
	Tool    string   `json:"tool,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Command string   `json:"command,omitempty"`
}

// payload holds the host fields the dispatcher reads; Claude Code and Codex
// share these names.
type payload struct {
	SessionID string          `json:"session_id"`
	TurnID    string          `json:"turn_id"`
	AgentID   string          `json:"agent_id"`
	Cwd       string          `json:"cwd"`
	Source    string          `json:"source"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type toolInput struct {
	FilePath     string          `json:"file_path"`
	NotebookPath string          `json:"notebook_path"`
	Command      json.RawMessage `json:"command"`
	Cmd          json.RawMessage `json:"cmd"`
	Workdir      string          `json:"workdir"`
	Patch        string          `json:"patch"`
	Input        string          `json:"input"`
}

var errNoWorktree = errors.New("no Git worktree contains the hook's working directory")

// toolClass maps a host tool name to the class handlers declare.
func toolClass(name string) string {
	switch name {
	case "Read":
		return ToolRead
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return ToolEdit
	case "apply_patch", "ApplyPatch":
		return ToolPatch
	case "Bash", "shell", "exec_command", "local_shell", "container.exec", "unified_exec":
		return ToolShell
	}
	return ""
}

// normalize turns one host payload into an Event. cwd is the process working
// directory, used when the payload has none.
func normalize(harness, point string, raw []byte, cwd string) (Event, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Event{}, fmt.Errorf("parse %s payload: %w", harness, err)
	}
	if p.Cwd != "" {
		cwd = p.Cwd
	}
	root, err := worktreeRoot(cwd)
	if err != nil {
		return Event{}, err
	}
	ev := Event{
		Harness: harness,
		Event:   point,
		Root:    root,
		Agent:   Identity{Worktree: root, Harness: harness, Session: p.SessionID, AgentID: p.AgentID, Turn: p.TurnID},
	}
	if point == SessionStart && (p.Source == "compact" || p.Source == "clear") {
		ev.Reset = p.Source
	}
	if point != PostToolUse {
		return ev, nil
	}
	ev.Tool = toolClass(p.ToolName)
	var in toolInput
	var patch string
	if len(p.ToolInput) > 0 && p.ToolInput[0] == '"' {
		// Some hosts send a patch or command as a bare string.
		_ = json.Unmarshal(p.ToolInput, &patch)
	} else if len(p.ToolInput) > 0 {
		_ = json.Unmarshal(p.ToolInput, &in)
	}
	callDir := cwd
	if in.Workdir != "" {
		callDir = resolveAgainst(cwd, in.Workdir)
	}
	var candidates []string
	switch ev.Tool {
	case ToolRead, ToolEdit:
		candidates = []string{firstNonEmpty(in.FilePath, in.NotebookPath)}
	case ToolPatch:
		candidates = patchPaths(firstNonEmpty(patch, in.Patch, in.Input, commandText(in.Command)), callDir)
	case ToolShell:
		ev.Command = firstNonEmpty(commandText(in.Command), commandText(in.Cmd), patch)
		candidates = shellReads(ev.Command, callDir)
	}
	ev.Paths = containedPaths(root, callDir, candidates)
	return ev, nil
}

// commandText accepts a command string or an argument list; for a list that
// runs a shell script (bash -lc '...'), it returns the script.
func commandText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var argv []string
	if json.Unmarshal(raw, &argv) != nil || len(argv) == 0 {
		return ""
	}
	if n := len(argv); n >= 3 && strings.HasPrefix(argv[n-2], "-") && strings.Contains(argv[n-2], "c") {
		return argv[n-1]
	}
	return strings.Join(argv, " ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// worktreeRoot returns the canonical directory holding the .git entry above
// dir: the session's own worktree.
func worktreeRoot(dir string) (string, error) {
	if dir == "" {
		return "", errNoWorktree
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errNoWorktree
		}
		dir = parent
	}
}

// containedPaths resolves candidates against callDir and keeps the
// repository-relative ones inside root, following symlinks so a link that
// escapes the worktree is dropped. Paths that do not exist (a deleted file)
// are checked through their nearest existing parent.
func containedPaths(root, callDir string, candidates []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range candidates {
		if strings.TrimSpace(c) == "" {
			continue
		}
		rel, ok := containedRel(root, resolveAgainst(callDir, c))
		if ok && !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

func containedRel(root, p string) (string, bool) {
	if !filepath.IsAbs(p) {
		return "", false
	}
	rel, err := filepath.Rel(root, resolveExisting(filepath.Clean(p)))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return "", false
	}
	return rel, true
}

// resolveExisting resolves symlinks in the longest existing prefix of p.
func resolveExisting(p string) string {
	rest := ""
	for cur := p; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
