package init

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// hookRepo is a first-run repository with session memory and both harnesses
// whose hook entries use them.
func hookRepo(t *testing.T, hooks string) string {
	t.Helper()
	root := machineRunRepo(t)
	writeFixtureFile(t, root, ".claude/settings.json", `{"permissions":{"allow":["Bash(ls)"]},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo done"}]}]}}`)
	_, err := Apply(RunOptions{Dir: root, Workflow: "none", Addons: "session-memory", Agents: "claude,codex", Search: "off", Hooks: hooks})
	require.NoError(t, err)
	return root
}

// hookCommands lists the commands in a settings file by host event.
func hookCommands(t *testing.T, root, rel string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var file struct {
		Hooks map[string][]struct {
			Matcher *string `json:"matcher"`
			Hooks   []struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
				Timeout int      `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(data, &file))
	out := map[string][]string{}
	for event, groups := range file.Hooks {
		for _, g := range groups {
			require.Nil(t, g.Matcher, "entries match every tool")
			for _, h := range g.Hooks {
				if hook.IsEntryCommand(h.Command, h.Args) {
					require.Equal(t, hook.HostTimeoutSeconds, h.Timeout)
					require.Greater(t, time.Duration(h.Timeout)*time.Second, hook.Deadline)
				}
				out[event] = append(out[event], strings.TrimSpace(h.Command+" "+strings.Join(h.Args, " ")))
			}
		}
	}
	return out
}

func TestHooksOnInstallsOneEntryPerHookPointAndPreservesOtherEntries(t *testing.T) {
	root := hookRepo(t, "on")
	require.Equal(t, "on", loadConfigForTest(t, root).Agents.Hooks)
	require.Equal(t, map[string][]string{
		"Stop":          {"echo done"},
		"SessionStart":  {"rzm hook claude session-start"},
		"SubagentStart": {"rzm hook claude subagent-start"},
		"PostToolUse":   {"rzm hook claude post-tool-use"},
	}, hookCommands(t, root, ".claude/settings.json"))
	require.Equal(t, map[string][]string{
		"SessionStart":  {"rzm hook codex session-start"},
		"SubagentStart": {"rzm hook codex subagent-start"},
		"PostToolUse":   {"rzm hook codex post-tool-use"},
	}, hookCommands(t, root, ".codex/hooks.json"))
	settings, err := os.ReadFile(filepath.Join(root, ".claude/settings.json"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(settings), "{\n  \"permissions\""), "other keys keep their place")
	codex, err := os.ReadFile(filepath.Join(root, ".codex/hooks.json"))
	require.NoError(t, err)
	require.Contains(t, string(codex), `"commandWindows": "rzm.exe hook codex post-tool-use"`)

	// Current entries are not rewritten, and the rerun reports them.
	requireUpToDate(t, root)
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "Hooks     on")
	require.Contains(t, out, "Session memory: approved context shards")
	require.Contains(t, out, "Claude Code hooks (.claude/settings.json): installed and current")
	require.Contains(t, out, "Codex hooks (.codex/hooks.json): installed and current")
	require.Contains(t, out, "session-memory/deliver-shards has not returned context yet")
	require.Contains(t, out, "review it with /hooks in Codex")
}

func TestHookStatusReportsWhenEachHandlerLastReturnedContext(t *testing.T) {
	root := hookRepo(t, "on")
	at := time.Date(2026, 10, 22, 14, 3, 0, 0, time.Local)
	writeFixtureFile(t, root, ".rhizome/hooks/status.json", `{"claude":{"session-memory/deliver-shards":"`+at.UTC().Format(time.RFC3339)+`"}}`)
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "session-memory/deliver-shards last returned context 2026-10-22 14:03")
}

func TestHooksStayOffUntilTurnedOnAndTheSummaryNamesWhatTheyDeliver(t *testing.T) {
	root := hookRepo(t, "")
	require.NotContains(t, hookCommands(t, root, ".claude/settings.json"), "PostToolUse")
	require.NoFileExists(t, filepath.Join(root, ".codex/hooks.json"))
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "Hooks     off")
	require.Contains(t, out, "Hooks are off. Turn them on with rzm init --hooks on to get:")
	require.Contains(t, out, "Session memory: approved context shards")

	out, err = runInit(t, RunOptions{Dir: root, Hooks: "on"})
	require.NoError(t, err)
	require.Contains(t, out, "Hooks: off → on")
	require.Contains(t, hookCommands(t, root, ".codex/hooks.json"), "PostToolUse")
	requireUpToDate(t, root)
}

func TestHookEntriesAreRemovedWhenHooksHarnessOrHandlersGo(t *testing.T) {
	t.Run("hooks off", func(t *testing.T) {
		root := hookRepo(t, "on")
		_, err := runInit(t, RunOptions{Dir: root, Hooks: "off"})
		require.NoError(t, err)
		require.Equal(t, map[string][]string{"Stop": {"echo done"}}, hookCommands(t, root, ".claude/settings.json"))
		require.NoFileExists(t, filepath.Join(root, ".codex/hooks.json"), "a file that held only Rhizome entries goes")
		require.Equal(t, "off", loadConfigForTest(t, root).Agents.Hooks)
		requireUpToDate(t, root)
	})
	t.Run("harness disabled", func(t *testing.T) {
		root := hookRepo(t, "on")
		_, err := runInit(t, RunOptions{Dir: root, Agents: "codex"})
		require.NoError(t, err)
		require.Equal(t, map[string][]string{"Stop": {"echo done"}}, hookCommands(t, root, ".claude/settings.json"))
		require.Contains(t, hookCommands(t, root, ".codex/hooks.json"), "PostToolUse")
	})
	t.Run("no starter declares hooks", func(t *testing.T) {
		root := hookRepo(t, "on")
		_, err := runInit(t, RunOptions{Dir: root, RemoveAddon: "session-memory"})
		require.NoError(t, err)
		require.NoFileExists(t, filepath.Join(root, ".codex/hooks.json"))
		require.Equal(t, "on", loadConfigForTest(t, root).Agents.Hooks, "the choice survives add-on changes")
		_, err = runInit(t, RunOptions{Dir: root, AddAddon: "session-memory"})
		require.NoError(t, err)
		require.Contains(t, hookCommands(t, root, ".codex/hooks.json"), "PostToolUse")
	})
}

func TestEditedHookEntriesFollowTheOwnershipRule(t *testing.T) {
	root := hookRepo(t, "on")
	path := filepath.Join(root, ".codex/hooks.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	edited := strings.Replace(string(data), `"timeout": 5`, `"timeout": 30`, 1)
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))

	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err, "an edited entry with no newer version is kept")
	require.Contains(t, out, "Codex hooks (.codex/hooks.json): installed; you edited the entries, and your version is kept")
	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, edited, string(got), "without a terminal the edit is kept")

	out, err = runInit(t, RunOptions{Dir: root, Hooks: "off"})
	require.NoError(t, err)
	require.Contains(t, out, ".codex/hooks.json#rhizome-hooks", "the removal is listed as needing a decision")
	got, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, edited, string(got), "an edited entry is not removed without a decision")
}

func TestClaudeEntriesOnWindowsLaunchRzmExeWithoutAShell(t *testing.T) {
	saved := hookEntryGOOS
	hookEntryGOOS = "windows"
	t.Cleanup(func() { hookEntryGOOS = saved })
	root := hookRepo(t, "on")
	require.Equal(t, []string{"rzm.exe hook claude post-tool-use"}, hookCommands(t, root, ".claude/settings.json")["PostToolUse"])
	data, err := os.ReadFile(filepath.Join(root, ".claude/settings.json"))
	require.NoError(t, err)
	require.Contains(t, string(data), `"args": [`)
}

func TestHookEntriesWaitForABinaryThatSupportsThem(t *testing.T) {
	saved := checkHookSupport
	checkHookSupport = func(string, repoconfig.LocalConfig) error {
		return errors.New("the selected rzm does not support rzm hook")
	}
	t.Cleanup(func() { checkHookSupport = saved })
	root := hookRepo(t, "on")
	require.NotContains(t, hookCommands(t, root, ".claude/settings.json"), "PostToolUse")
	require.NoFileExists(t, filepath.Join(root, ".codex/hooks.json"))
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "Hook entries were left as they are: the selected rzm does not support rzm hook")
}

func TestHookHandlersComeFromTheRegistryOnlyWhenHooksAreOn(t *testing.T) {
	root := hookRepo(t, "")
	handlers, err := HookHandlers(root)
	require.NoError(t, err)
	require.Empty(t, handlers)

	_, err = runInit(t, RunOptions{Dir: root, Hooks: "on"})
	require.NoError(t, err)
	handlers, err = HookHandlers(root)
	require.NoError(t, err)
	require.Len(t, handlers, 1)
	h := handlers[0]
	require.Equal(t, "session-memory/deliver-shards", h.Key())
	require.Equal(t, []string{"node", ".agents/skills/session-memory/scripts/hook.mjs"}, h.Command)
	require.Equal(t, ".rhizome/session-memory/hook/command-prefixes.json", h.CommandPrefixes)
	require.ElementsMatch(t, []string{hook.SessionStart, hook.SubagentStart, hook.PostToolUse}, h.Events)
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(h.Command[1])), "the handler script is installed")

	// An ejected starter keeps its handler.
	_, err = runInit(t, RunOptions{Dir: root, Eject: "session-memory"})
	require.NoError(t, err)
	handlers, err = HookHandlers(root)
	require.NoError(t, err)
	require.Len(t, handlers, 1)
}

func TestHookDeclarationsAreValidated(t *testing.T) {
	good := starterHookMetadata{ID: "h", Delivers: "x", Events: []string{hook.PostToolUse}, Tools: []string{hook.ToolRead}, Command: []string{"node", "h.mjs"}, Timeout: "2s"}
	_, err := good.handler("s")
	require.NoError(t, err)
	for _, bad := range []func(m *starterHookMetadata){
		func(m *starterHookMetadata) { m.Events = []string{"pre-commit"} },
		func(m *starterHookMetadata) { m.Tools = []string{"web"} },
		func(m *starterHookMetadata) { m.Command = nil },
		func(m *starterHookMetadata) { m.Timeout = "10s" },
		func(m *starterHookMetadata) { m.Timeout = "" },
	} {
		m := good
		bad(&m)
		_, err := m.handler("s")
		require.Error(t, err)
	}
}

func TestInvalidHooksOptionFailsBeforeWriting(t *testing.T) {
	root := machineRunRepo(t)
	_, err := runInit(t, RunOptions{Dir: root, Hooks: "maybe"})
	require.ErrorContains(t, err, "invalid --hooks value")
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "config.yml"))
}

func TestRenderHookFileKeepsOrderAndForeignEntries(t *testing.T) {
	existing := []byte(`{"z":1,"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"lint"}]},{"hooks":[{"type":"command","command":"/usr/local/bin/rzm hook claude post-tool-use","timeout":9}]}]},"a":2}`)
	next, err := renderHookFile(existing, desiredHookEntries(hook.HarnessClaude))
	require.NoError(t, err)
	top, err := parseOrderedObject(next)
	require.NoError(t, err)
	require.Equal(t, []string{"z", "hooks", "a"}, top.keys)
	hooks, err := parseOrderedObject(top.values["hooks"])
	require.NoError(t, err)
	require.Equal(t, []string{"PostToolUse", "SessionStart", "SubagentStart"}, hooks.keys)
	require.Contains(t, string(next), `"command": "lint"`)
	require.NotContains(t, string(next), "/usr/local/bin/rzm", "an old Rhizome entry is replaced")

	removed, err := renderHookFile(next, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"z":1,"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"lint"}]}]},"a":2}`, string(removed))
}

func TestTheHooksChoiceSurvivesSettingsEdits(t *testing.T) {
	root := hookRepo(t, "on")
	// Settings > Agents > Stop managing agent files, then apply.
	_, err := runInit(t, RunOptions{Dir: root, Interactive: true, Stdin: bytes.NewBufferString("s\n3\n3\n\ny\n")})
	require.NoError(t, err)
	require.Equal(t, "on", loadConfigForTest(t, root).Agents.Hooks)
	require.NoFileExists(t, filepath.Join(root, ".codex/hooks.json"), "no harness, no entries")

	_, err = runInit(t, RunOptions{Dir: root, Agents: "codex"})
	require.NoError(t, err)
	require.Contains(t, hookCommands(t, root, ".codex/hooks.json"), "PostToolUse")
}
