package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeClaudeAndCodexPayloads(t *testing.T) {
	root := repo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "my notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "my notes", "a.md"), nil, 0o644))
	sub := filepath.Join(root, "pkg")

	for _, tc := range []struct {
		name, harness, point, payload string
		want                          Event
	}{{
		name: "claude subagent edit", harness: HarnessClaude, point: PostToolUse,
		payload: fmt.Sprintf(`{"session_id":"s","agent_id":"a1","cwd":%q,"tool_name":"MultiEdit","tool_input":{"file_path":%q}}`, root, filepath.Join(root, "pkg", "a.go")),
		want:    Event{Tool: ToolEdit, Paths: []string{"pkg/a.go"}, Agent: Identity{Session: "s", AgentID: "a1"}},
	}, {
		name: "claude bash with cd and quoted spaces", harness: HarnessClaude, point: PostToolUse,
		payload: fmt.Sprintf(`{"session_id":"s","cwd":%q,"tool_name":"Bash","tool_input":{"command":"cd ../docs && cat 'my notes/a.md' ../../outside.md"}}`, sub),
		want:    Event{Tool: ToolShell, Paths: []string{"docs/my notes/a.md"}, Command: "cd ../docs && cat 'my notes/a.md' ../../outside.md", Agent: Identity{Session: "s"}},
	}, {
		name: "codex shell argv with workdir", harness: HarnessCodex, point: PostToolUse,
		payload: fmt.Sprintf(`{"session_id":"s","turn_id":"t","cwd":%q,"tool_name":"shell","tool_input":{"command":["bash","-lc","head -n 3 a.go"],"workdir":"pkg"}}`, root),
		want:    Event{Tool: ToolShell, Paths: []string{"pkg/a.go"}, Command: "head -n 3 a.go", Agent: Identity{Session: "s", Turn: "t"}},
	}, {
		name: "codex patch that moves and deletes", harness: HarnessCodex, point: PostToolUse,
		payload: fmt.Sprintf(`{"session_id":"s","cwd":%q,"tool_name":"apply_patch","tool_input":"*** Begin Patch\n*** Update File: pkg/a.go\n*** Move to: pkg/moved.go\n*** Delete File: gone/old.go\n*** End Patch"}`, root),
		want:    Event{Tool: ToolPatch, Paths: []string{"gone/old.go", "pkg/a.go", "pkg/moved.go"}, Agent: Identity{Session: "s"}},
	}, {
		name: "subagent start without an agent ID borrows nothing", harness: HarnessCodex, point: SubagentStart,
		payload: fmt.Sprintf(`{"session_id":"parent","turn_id":"t2","cwd":%q}`, root),
		want:    Event{Agent: Identity{Session: "parent", Turn: "t2"}},
	}, {
		name: "clear resets", harness: HarnessClaude, point: SessionStart,
		payload: fmt.Sprintf(`{"session_id":"s","cwd":%q,"source":"clear"}`, root),
		want:    Event{Reset: "clear", Agent: Identity{Session: "s"}},
	}, {
		name: "unknown tool", harness: HarnessClaude, point: PostToolUse,
		payload: fmt.Sprintf(`{"session_id":"s","cwd":%q,"tool_name":"WebFetch","tool_input":{"url":"x"}}`, root),
		want:    Event{Agent: Identity{Session: "s"}},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := normalize(tc.harness, tc.point, []byte(tc.payload), "/nowhere")
			require.NoError(t, err)
			want := tc.want
			want.Harness, want.Event, want.Root = tc.harness, tc.point, root
			want.Agent.Worktree, want.Agent.Harness = root, tc.harness
			require.Equal(t, want, ev)
		})
	}
}

func TestContainmentDropsSymlinkEscapesAndOtherWorktrees(t *testing.T) {
	root := repo(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.go"), nil, 0o644))
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	require.NoError(t, os.Symlink(filepath.Join(root, "pkg"), filepath.Join(root, "alias")))
	other := repo(t)

	got := containedPaths(root, root, []string{
		"escape/secret.go",                        // a link out of the worktree
		"escape/new.go",                           // a new file under that link
		filepath.Join(other, "pkg", "a.go"),       // another worktree
		"alias/a.go",                              // a link inside the worktree
		"pkg/deleted.go",                          // gone after a patch
		".git/config",                             // Git's own files
		"../" + filepath.Base(root) + "/pkg/a.go", // back into the worktree
	})
	require.Equal(t, []string{"pkg/a.go", "pkg/deleted.go"}, got)
}

func TestNormalizeOutsideAWorktreeIsNotAnError(t *testing.T) {
	_, err := normalize(HarnessClaude, SessionStart, []byte(fmt.Sprintf(`{"cwd":%q}`, t.TempDir())), "")
	require.ErrorIs(t, err, errNoWorktree)
}

func TestDotDotAfterASymlinkFollowsTheLinkFirst(t *testing.T) {
	root := repo(t)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "dir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.go"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.go"), nil, 0o644))
	if err := os.Symlink(filepath.Join(outside, "dir"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// link/../secret.go opens the outside file, though it cleans to secret.go.
	require.Empty(t, containedPaths(root, root, []string{"link/../secret.go", filepath.Join(root, "link") + "/../secret.go"}))
	require.Empty(t, containedPaths(root, root, shellReads("cd link/.. && cat secret.go", root)), "cd link/.. leaves the worktree before the read")
	require.Equal(t, []string{"secret.go"}, containedPaths(root, root, []string{"pkg/../secret.go"}))
}
