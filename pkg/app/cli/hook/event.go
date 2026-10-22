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
	Reset string `json:"reset,omitempty"`
	// StartDue marks a tool call that also brings the agent's due start
	// items, for a handler that acts on both.
	StartDue bool     `json:"startDue,omitempty"`
	Tool     string   `json:"tool,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Command  string   `json:"command,omitempty"`
}

// payload holds the host fields the dispatcher reads; Claude Code and Codex
// share these names. Neither reports the directory a shell command or patch
// ran in when it differs from cwd.
type payload struct {
	SessionID string          `json:"session_id"`
	TurnID    string          `json:"turn_id"`
	AgentID   string          `json:"agent_id"`
	Cwd       string          `json:"cwd"`
	Source    string          `json:"source"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// toolInput holds Claude Code's file fields and the command text both hosts
// send: a Bash script, or the body of a Codex apply_patch.
type toolInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Command      string `json:"command"`
}

var errNoWorktree = errors.New("no Git worktree contains the hook's working directory")

// toolClass maps a host tool name to the class handlers declare. Codex
// reports its shell and unified exec tools as Bash.
func toolClass(name string) string {
	switch name {
	case "Read":
		return ToolRead
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return ToolEdit
	case "apply_patch":
		return ToolPatch
	case "Bash":
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
	// A main agent's compaction is reset by the session start that follows
	// it; only a subagent's needs this event.
	if point == PostCompact && p.AgentID != "" {
		ev.Reset = "compact"
	}
	if point != PostToolUse {
		return ev, nil
	}
	ev.Tool = toolClass(p.ToolName)
	var in toolInput
	_ = json.Unmarshal(p.ToolInput, &in)
	switch ev.Tool {
	case ToolRead, ToolEdit:
		ev.Paths = containedPaths(root, cwd, []string{firstNonEmpty(in.FilePath, in.NotebookPath)})
	case ToolPatch:
		// A patch may delete its file, so its directory is enough.
		ev.Paths = existing(root, containedPaths(root, cwd, patchPaths(in.Command, cwd)), true)
	case ToolShell:
		ev.Command = in.Command
		// The host does not say where the command ran beyond cwd, so a read
		// made in another directory may be missed or attributed to a
		// same-named file here. Dropping reads of files that do not exist
		// only reduces noise.
		ev.Paths = existing(root, containedPaths(root, cwd, shellReads(ev.Command, cwd)), false)
	}
	return ev, nil
}

// existing keeps the repository-relative paths that exist, or, with
// parentOK, whose directory does.
func existing(root string, rels []string, parentOK bool) []string {
	var out []string
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err == nil {
			out = append(out, rel)
		} else if info, err := os.Stat(filepath.Dir(p)); parentOK && err == nil && info.IsDir() {
			out = append(out, rel)
		}
	}
	return out
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
	rel, err := filepath.Rel(root, physicalPath(p))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return "", false
	}
	return rel, true
}

// physicalPath resolves an absolute path one component at a time, as the
// kernel does: a symlink is followed before a later ".." applies, so
// link/../x names the parent of the link's target, not the link's parent.
// Components after the first missing one cannot be links and apply
// lexically.
func physicalPath(p string) string {
	p = filepath.FromSlash(p)
	vol := filepath.VolumeName(p)
	cur := vol + string(filepath.Separator)
	missing := false
	for _, part := range strings.Split(p[len(vol):], string(filepath.Separator)) {
		switch part {
		case "", ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, part)
		if !missing {
			if resolved, err := filepath.EvalSymlinks(next); err == nil {
				next = resolved
			} else {
				missing = true
			}
		}
		cur = next
	}
	return cur
}
