package init

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/stretchr/testify/require"
)

// hookRepo is a first-run repository with the compounding add-on and both harnesses
// whose hook entries use them.
func hookRepo(t *testing.T, hooks string) string {
	t.Helper()
	stubHookSupport(t, nil)
	root := machineRunRepo(t)
	writeFixtureFile(t, root, ".claude/settings.json", `{"permissions":{"allow":["Bash(ls)"]},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo done"}]}]}}`)
	_, err := Apply(RunOptions{Dir: root, Workflow: "none", Addons: "compounding", Agents: "claude,codex", Search: "off", Hooks: hooks})
	require.NoError(t, err)
	return root
}

// stubHookSupport answers init's check of the rzm the entries launch.
func stubHookSupport(t *testing.T, err error) {
	t.Helper()
	saved := checkHookSupport
	checkHookSupport = func(string, repoconfig.LocalConfig) error { return err }
	t.Cleanup(func() { checkHookSupport = saved })
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
		"PostCompact":   {"rzm hook claude post-compact"},
	}, hookCommands(t, root, ".claude/settings.json"))
	require.Equal(t, map[string][]string{
		"SessionStart":  {"rzm hook codex session-start"},
		"SubagentStart": {"rzm hook codex subagent-start"},
		"PostToolUse":   {"rzm hook codex post-tool-use"},
		"PostCompact":   {"rzm hook codex post-compact"},
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
	require.Contains(t, out, "Compounding: approved context shards")
	require.Contains(t, out, "Claude Code hooks (.claude/settings.json): installed and current")
	require.Contains(t, out, "Codex hooks (.codex/hooks.json): installed and current")
	require.Contains(t, out, "compounding/deliver-shards has not returned context yet")
	require.Contains(t, out, "review it with /hooks in Codex")
}

func TestHookStatusReportsWhenEachHandlerLastReturnedContext(t *testing.T) {
	root := hookRepo(t, "on")
	at := time.Date(2026, 10, 22, 14, 3, 0, 0, time.Local)
	writeFixtureFile(t, root, ".rhizome/hooks/status.json", `{"delivered":{"claude":{"compounding/deliver-shards":"`+at.UTC().Format(time.RFC3339)+`"}}}`)
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "compounding/deliver-shards last returned context 2026-10-22 14:03")
}

// guardFinding is an approved guard finding whose rule refuses go mod vendor.
const guardFinding = `---
response: guard
summary: "Agents run a bare go mod vendor."
guard-rule: {"tool":"shell","command":"go mod vendor","reason":"It deletes hand-vendored C files.","alternative":"make vendor"}
approved-hash: "b9a4a97b3a352156"
reviewed-by: "Sam"
reviewed-on: 2026-10-22
---

# Agents run a bare go mod vendor
`

func TestTheBeforeToolCallEntryFollowsApprovedGuardRules(t *testing.T) {
	root := hookRepo(t, "on")
	require.NotContains(t, hookCommands(t, root, ".claude/settings.json"), "PreToolUse", "no rule, no entry")

	// Only a finding note tracked in Git holds a rule.
	runGit(t, "-C", root, "init", "-q")
	rel := "docs/memory/agent-findings/guard-go-mod-vendor.md"
	writeFixtureFile(t, root, rel, guardFinding)
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "guard rule skipped: "+rel+": not tracked in Git; stage or commit it to activate its rule")
	runGit(t, "-C", root, "add", "--", rel)
	out, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.ErrorContains(t, err, "would make changes")
	require.Contains(t, out, "Claude Code hooks (.claude/settings.json): installed; these changes update them")
	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Equal(t, []string{"rzm hook claude pre-tool-use"}, hookCommands(t, root, ".claude/settings.json")["PreToolUse"])
	require.Equal(t, []string{"rzm hook codex pre-tool-use"}, hookCommands(t, root, ".codex/hooks.json")["PreToolUse"])
	require.Equal(t, []string{"rzm hook claude post-tool-use"}, hookCommands(t, root, ".claude/settings.json")["PostToolUse"])
	requireUpToDate(t, root)

	// Status lists the active rule per host and how often it refused a call.
	writeFixtureFile(t, root, ".rhizome/hooks/status.json", `{"refused":{"codex":{"`+rel+`":3}}}`)
	out, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "guard rule "+rel+" (shell command go mod vendor) refused 0 calls")
	require.Contains(t, out, "guard rule "+rel+" (shell command go mod vendor) refused 3 calls")

	// A rule edited after approval is named and does not count.
	writeFixtureFile(t, root, rel, strings.Replace(guardFinding, `"alternative":"make vendor"`, `"alternative":"anything"`, 1))
	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.NotContains(t, hookCommands(t, root, ".claude/settings.json"), "PreToolUse", "no approved rule remains")
	require.NotContains(t, hookCommands(t, root, ".codex/hooks.json"), "PreToolUse")
	require.Contains(t, hookCommands(t, root, ".claude/settings.json"), "PostToolUse")
	out, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "guard rule skipped: "+rel+": edited after approval; revise it through the compounding review")
	require.Contains(t, out, "compounding/guard-rules has no approved guard rules")
}

// A binary that runs hook entries without applying guard rules gets every
// entry but the before-tool-call one, and status says why.
func TestTheBeforeToolCallEntryWaitsForABinaryThatAppliesGuardRules(t *testing.T) {
	root := hookRepo(t, "on")
	stubHookSupport(t, fmt.Errorf("the rzm on PATH (/bin/rzm) %w, so the before-tool-call entry was not installed; update it with rzm update", errNoGuard))
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err, "no rule needs the entry, so nothing is reported")
	require.NotContains(t, out, "Guard rules do not run")

	runGit(t, "-C", root, "init", "-q")
	rel := "docs/memory/agent-findings/guard-go-mod-vendor.md"
	writeFixtureFile(t, root, rel, guardFinding)
	runGit(t, "-C", root, "add", "--", rel)
	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.NotContains(t, hookCommands(t, root, ".claude/settings.json"), "PreToolUse")
	require.Contains(t, hookCommands(t, root, ".claude/settings.json"), "PostToolUse")
	out, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "Guard rules do not run: the rzm on PATH (/bin/rzm) does not apply guard rules, so the before-tool-call entry was not installed; update it with rzm update")

	// Machine setup reports the same reason.
	fresh := machineRunRepo(t)
	runGit(t, "-C", fresh, "init", "-q")
	writeFixtureFile(t, fresh, rel, guardFinding)
	runGit(t, "-C", fresh, "add", "--", rel)
	plan, err := Plan(RunOptions{Dir: fresh, Workflow: "none", Addons: "compounding", Agents: "claude", Search: "off", Hooks: "on"})
	require.NoError(t, err)
	require.Contains(t, plan.HooksBlocked, "does not apply guard rules")
}

func TestHooksStayOffUntilTurnedOnAndTheSummaryNamesWhatTheyDeliver(t *testing.T) {
	root := hookRepo(t, "")
	require.NotContains(t, hookCommands(t, root, ".claude/settings.json"), "PostToolUse")
	require.NoFileExists(t, filepath.Join(root, ".codex/hooks.json"))
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "Hooks     off")
	require.Contains(t, out, "Hooks are off. Turn them on with rzm init --hooks on to get:")
	require.Contains(t, out, "Compounding: approved context shards")

	out, err = runInit(t, RunOptions{Dir: root, Hooks: "on"})
	require.NoError(t, err)
	require.Contains(t, out, "Hooks: off → on")
	require.Contains(t, hookCommands(t, root, ".codex/hooks.json"), "PostToolUse")
	requireUpToDate(t, root)
}

func TestHookEntriesAreRemovedWhenHooksHarnessOrHandlersGo(t *testing.T) {
	t.Run("hooks off", func(t *testing.T) {
		root := hookRepo(t, "on")
		out, err := runInit(t, RunOptions{Dir: root, Hooks: "off", Check: true})
		require.Error(t, err)
		require.Contains(t, out, "Update 1 Rhizome file\n      .claude/settings.json\n")
		require.Contains(t, out, "Remove 1 file Rhizome no longer ships\n      .codex/hooks.json\n", "a file left empty is listed as removed")
		_, err = runInit(t, RunOptions{Dir: root, Hooks: "off"})
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
		_, err := runInit(t, RunOptions{Dir: root, RemoveAddon: "compounding"})
		require.NoError(t, err)
		require.NoFileExists(t, filepath.Join(root, ".codex/hooks.json"))
		require.Equal(t, "on", loadConfigForTest(t, root).Agents.Hooks, "the choice survives add-on changes")
		_, err = runInit(t, RunOptions{Dir: root, AddAddon: "compounding"})
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

func TestAMatcherAddedToARhizomeGroupIsAnEdit(t *testing.T) {
	root := hookRepo(t, "on")
	path := filepath.Join(root, ".claude/settings.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var file map[string]any
	require.NoError(t, json.Unmarshal(data, &file))
	groups := file["hooks"].(map[string]any)["PostToolUse"].([]any)
	groups[len(groups)-1].(map[string]any)["matcher"] = "Read"
	restricted, err := json.MarshalIndent(file, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, restricted, 0o644))

	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "Claude Code hooks (.claude/settings.json): installed; you edited the entries, and your version is kept")
	out, err = runInit(t, RunOptions{Dir: root, Hooks: "off"})
	require.NoError(t, err)
	require.Contains(t, out, ".claude/settings.json#rhizome-hooks", "the removal is listed as needing a decision")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(restricted), string(got), "the restricted group is not replaced without a decision")
}

func TestClaudeEntriesUseTheSameExecFormOnEveryOS(t *testing.T) {
	root := hookRepo(t, "on")
	var file struct {
		Hooks map[string][]struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude/settings.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &file))
	require.Equal(t, map[string]any{
		"type": "command", "command": "rzm", "args": []any{"hook", "claude", "post-tool-use"}, "timeout": float64(hook.HostTimeoutSeconds),
	}, file.Hooks["PostToolUse"][0].Hooks[0])
}

func TestHookEntriesWaitForABinaryThatSupportsThem(t *testing.T) {
	root := hookRepo(t, "")
	stubHookSupport(t, errors.New("the selected rzm does not support rzm hook"))
	_, err := runInit(t, RunOptions{Dir: root, Hooks: "on"})
	require.NoError(t, err)
	require.NotContains(t, hookCommands(t, root, ".claude/settings.json"), "PostToolUse")
	require.NoFileExists(t, filepath.Join(root, ".codex/hooks.json"))
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "Hook entries were left as they are: the selected rzm does not support rzm hook")
}

func TestMachineSetupSaysWhyNoHookEntriesWereWritten(t *testing.T) {
	stubHookSupport(t, errors.New("there is no rzm on PATH"))
	root := machineRunRepo(t)
	opts := RunOptions{Dir: root, Workflow: "none", Addons: "compounding", Agents: "claude", Search: "off", Hooks: "on"}
	plan, err := Plan(opts)
	require.NoError(t, err)
	require.Equal(t, "there is no rzm on PATH", plan.HooksBlocked)
	result, err := Apply(opts)
	require.NoError(t, err)
	require.Equal(t, "there is no rzm on PATH", result.HooksBlocked)
	require.NoFileExists(t, filepath.Join(root, ".claude/settings.json"))
}

func TestHookSupportCheckProbesTheRzmOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is Unix-only")
	}
	root := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	err := selectedBinarySupportsHooks(root, repoconfig.LocalConfig{})
	require.ErrorContains(t, err, "no rzm on PATH")

	launcher := filepath.Join(bin, "rzm")
	require.NoError(t, os.WriteFile(launcher, []byte("#!/bin/sh\necho 'unknown command \"hook\"' >&2\nexit 1\n"), 0o755))
	err = selectedBinarySupportsHooks(root, repoconfig.LocalConfig{})
	require.ErrorContains(t, err, "the rzm on PATH ("+launcher+") does not support rzm hook")

	// A dispatcher from before guard rules runs entries but not the guard.
	require.NoError(t, os.WriteFile(launcher, []byte("#!/bin/sh\n[ \"$1 $2\" = \"hook --check\" ] && echo 'rzm-hook 1'\n"), 0o755))
	err = selectedBinarySupportsHooks(root, repoconfig.LocalConfig{})
	require.ErrorIs(t, err, errNoGuard)
	require.ErrorContains(t, err, "the rzm on PATH ("+launcher+") does not apply guard rules")

	require.NoError(t, os.WriteFile(launcher, []byte("#!/bin/sh\n[ \"$1 $2\" = \"hook --check\" ] && echo 'rzm-hook 1 guard'\n"), 0o755))
	require.NoError(t, selectedBinarySupportsHooks(root, repoconfig.LocalConfig{}))
}

func TestHookSupportCheckRunsNoBinaryAnUntrustedCheckoutSelects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is Unix-only")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	require.NoError(t, os.WriteFile(filepath.Join(bin, "rzm"), []byte("#!/bin/sh\necho 'rzm-hook 1'\n"), 0o755))
	root := t.TempDir()
	writeFixtureFile(t, root, ".rhizome/config.yml", "rhizome:\n  devBinaryDir: devbin\n")
	marker := filepath.Join(root, "ran")
	dev := filepath.Join(root, "devbin", runtime.GOOS, "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\n: > '"+marker+"'\necho 'rzm-hook 1 guard'\n"), 0o755))
	store := repositorytrust.Store{Dir: t.TempDir()}
	saved := hookTrustStore
	hookTrustStore = func() (repositorytrust.Store, error) { return store, nil }
	t.Cleanup(func() { hookTrustStore = saved })
	cfg := repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{DevBinaryDir: "devbin"}}

	err := selectedBinarySupportsHooks(root, cfg)
	require.ErrorContains(t, err, "is not trusted to select a Rhizome executable")
	require.NoFileExists(t, marker)

	_, err = store.Trust(root)
	require.NoError(t, err)
	require.NoError(t, selectedBinarySupportsHooks(root, cfg))
	require.FileExists(t, marker)
}

func TestHookSupportCheckProbesASourceCheckoutsOwnBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is Unix-only")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	require.NoError(t, os.WriteFile(filepath.Join(bin, "rzm"), []byte("#!/bin/sh\necho 'rzm-hook 1'\n"), 0o755))
	// A Rhizome source checkout with no devBinaryDir and no pin still selects
	// its own bin/<os>/rzm, here a build from before rzm hook.
	root := t.TempDir()
	writeFixtureFile(t, root, "go.mod", "module github.com/atomicobject/rhizome\n\ngo 1.27\n")
	dev := filepath.Join(root, "bin", runtime.GOOS, "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\necho 'unknown command \"hook\"' >&2\nexit 1\n"), 0o755))
	store := repositorytrust.Store{Dir: t.TempDir()}
	saved := hookTrustStore
	hookTrustStore = func() (repositorytrust.Store, error) { return store, nil }
	t.Cleanup(func() { hookTrustStore = saved })
	_, err := store.Trust(root)
	require.NoError(t, err)
	// Init runs as an installed rzm, not a go run build.
	running := filepath.Join(bin, "rzm")
	savedExe := runningExecutable
	runningExecutable = func() (string, error) { return running, nil }
	t.Cleanup(func() { runningExecutable = savedExe })

	err = selectedBinarySupportsHooks(root, repoconfig.LocalConfig{})
	require.ErrorContains(t, err, "the selected rzm ("+dev+") does not support rzm hook")

	// A build from before guard rules runs entries but not the guard.
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\necho 'rzm-hook 1'\n"), 0o755))
	err = selectedBinarySupportsHooks(root, repoconfig.LocalConfig{})
	require.ErrorIs(t, err, errNoGuard)
	require.ErrorContains(t, err, "the selected rzm ("+dev+") does not apply guard rules")

	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\necho 'rzm-hook 1 guard'\n"), 0o755))
	require.NoError(t, selectedBinarySupportsHooks(root, repoconfig.LocalConfig{}))
}

// The installed entries run the rzm on PATH, which selects the checkout's
// build; a go run init selects nothing itself, so it must not stop at the
// launcher.
func TestHookSupportCheckSelectsFromTheLauncherWhenInitIsAGoRunBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is Unix-only")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	require.NoError(t, os.WriteFile(filepath.Join(bin, "rzm"), []byte("#!/bin/sh\necho 'rzm-hook 1 guard'\n"), 0o755))
	root := t.TempDir()
	writeFixtureFile(t, root, "go.mod", "module github.com/atomicobject/rhizome\n\ngo 1.27\n")
	dev := filepath.Join(root, "bin", runtime.GOOS, "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(dev), 0o755))
	require.NoError(t, os.WriteFile(dev, []byte("#!/bin/sh\necho 'rzm-hook 1'\n"), 0o755))
	store := repositorytrust.Store{Dir: t.TempDir()}
	saved := hookTrustStore
	hookTrustStore = func() (repositorytrust.Store, error) { return store, nil }
	t.Cleanup(func() { hookTrustStore = saved })
	_, err := store.Trust(root)
	require.NoError(t, err)
	running := filepath.Join(t.TempDir(), "go-build123", "b001", "exe", "rzm")
	savedExe := runningExecutable
	runningExecutable = func() (string, error) { return running, nil }
	t.Cleanup(func() { runningExecutable = savedExe })

	err = selectedBinarySupportsHooks(root, repoconfig.LocalConfig{})
	require.ErrorIs(t, err, errNoGuard)
	require.ErrorContains(t, err, "the selected rzm ("+dev+") does not apply guard rules")
}

// A pin matching init's own version still sends hooks to the pinned binary,
// whatever version the launcher is.
func TestHookSupportCheckProbesThePinnedBinaryAtInitsVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is Unix-only")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	require.NoError(t, os.WriteFile(filepath.Join(bin, "rzm"), []byte("#!/bin/sh\necho 'rzm-hook 1'\n"), 0o755))
	root := t.TempDir()
	writeFixtureFile(t, root, ".rhizome/config.yml", "rhizome:\n  version: "+version.Version+"\n")
	store := repositorytrust.Store{Dir: t.TempDir()}
	saved := hookTrustStore
	hookTrustStore = func() (repositorytrust.Store, error) { return store, nil }
	t.Cleanup(func() { hookTrustStore = saved })
	_, err := store.Trust(root)
	require.NoError(t, err)
	running := filepath.Join(t.TempDir(), "rzm")
	savedExe := runningExecutable
	runningExecutable = func() (string, error) { return running, nil }
	t.Cleanup(func() { runningExecutable = savedExe })
	cfg := repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: version.Version}}

	// Not installed yet: a hook never installs it, so entries wait for it.
	require.ErrorContains(t, selectedBinarySupportsHooks(root, cfg), "is not installed yet")

	pinned := filepath.Join(root, ".rhizome", "bin", appupdate.CurrentPlatformDir(), "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(pinned), 0o755))
	require.NoError(t, os.WriteFile(pinned, []byte("#!/bin/sh\necho 'rzm-hook 1 guard'\n"), 0o755))
	// Without a matching version marker, rzm hook does not hand off to it.
	require.ErrorContains(t, selectedBinarySupportsHooks(root, cfg), "is not marked as the pinned version")
	require.NoError(t, appupdate.WriteVersionMarker(pinned, version.Version))
	require.NoError(t, selectedBinarySupportsHooks(root, cfg), "the older launcher's missing guard does not matter")

	require.NoError(t, os.WriteFile(pinned, []byte("#!/bin/sh\necho 'rzm-hook 1'\n"), 0o755))
	require.ErrorIs(t, selectedBinarySupportsHooks(root, cfg), errNoGuard)
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
	require.Len(t, handlers, 2)
	h := handlers[0]
	require.Equal(t, "compounding/deliver-shards", h.Key())
	require.Equal(t, "compounding/guard-rules", handlers[1].Key())
	require.Equal(t, "docs/memory/agent-findings", handlers[1].GuardRules)
	require.Equal(t, []string{"node", ".agents/skills/compounding/scripts/hook.mjs"}, h.Command)
	require.Equal(t, ".rhizome/compounding/hook/command-prefixes.json", h.CommandPrefixes)
	require.ElementsMatch(t, []string{hook.SessionStart, hook.SubagentStart, hook.PostToolUse}, h.Events)
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(h.Command[1])), "the handler script is installed")

	// An ejected starter keeps its handler, even after it leaves the
	// selection with its files retained.
	_, err = runInit(t, RunOptions{Dir: root, Eject: "compounding"})
	require.NoError(t, err)
	handlers, err = HookHandlers(root)
	require.NoError(t, err)
	require.Len(t, handlers, 2)
	_, err = runInit(t, RunOptions{Dir: root, RemoveAddon: "compounding"})
	require.NoError(t, err)
	require.Contains(t, loadConfigForTest(t, root).WorkflowTemplateManagement.Ejected, "compounding")
	handlers, err = HookHandlers(root)
	require.NoError(t, err)
	require.Len(t, handlers, 2)
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(handlers[0].Command[1])))
}

func TestHookHandlersRunFromTheInstalledSkillFolder(t *testing.T) {
	stubHookSupport(t, nil)
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root, Workflow: "none", Addons: "compounding", Agents: "claude", Search: "off", Hooks: "on"})
	require.NoError(t, err)
	cfg := loadConfigForTest(t, root)
	cfg.Agents.AgentSkills = "off"
	require.NoError(t, repoconfig.SaveLocalConfig(root, *cfg))
	_, err = runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)

	handlers, err := HookHandlers(root)
	require.NoError(t, err)
	require.Len(t, handlers, 2)
	require.Equal(t, []string{"node", ".claude/skills/compounding/scripts/hook.mjs"}, handlers[0].Command)
	require.FileExists(t, filepath.Join(root, ".claude", "skills", "compounding", "scripts", "hook.mjs"))
}

func TestAHandlerWhoseSkillFolderIsOffDoesNotRun(t *testing.T) {
	stubHookSupport(t, nil)
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root, Workflow: "none", Addons: "compounding", Agents: "codex", Search: "off", Hooks: "on"})
	require.NoError(t, err)
	cfg := loadConfigForTest(t, root)
	cfg.Agents.AgentSkills = "off"
	require.NoError(t, repoconfig.SaveLocalConfig(root, *cfg))
	require.DirExists(t, filepath.Join(root, ".agents", "skills", "compounding"), "an old copy remains")

	handlers, err := HookHandlers(root)
	require.NoError(t, err)
	require.Len(t, handlers, 1, "the copy in a disabled folder is not run")
	require.Equal(t, "compounding/guard-rules", handlers[0].Key(), "the built-in guard needs no skill")
	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "compounding/deliver-shards does not run: no enabled skill folder holds its compounding skill")
}

func TestHookDeclarationsAreValidated(t *testing.T) {
	good := starterHookMetadata{ID: "h", Delivers: "x", Events: []string{hook.PostToolUse}, Tools: []string{hook.ToolRead}, Skill: "s", Command: []string{"node", "{skill}/h.mjs"}, Timeout: "2s"}
	_, err := good.handler("s")
	require.NoError(t, err)
	for _, bad := range []func(m *starterHookMetadata){
		func(m *starterHookMetadata) { m.Events = []string{"pre-commit"} },
		// The host takes no context after a compaction.
		func(m *starterHookMetadata) { m.Events = []string{hook.PostCompact} },
		func(m *starterHookMetadata) { m.Tools = []string{"web"} },
		func(m *starterHookMetadata) { m.Command = nil },
		func(m *starterHookMetadata) { m.Timeout = "10s" },
		func(m *starterHookMetadata) { m.Timeout = "" },
		func(m *starterHookMetadata) { m.Skill = "" },
		// Only the built-in guard acts before a tool call.
		func(m *starterHookMetadata) { m.Events = []string{hook.PreToolUse} },
	} {
		m := good
		bad(&m)
		_, err := m.handler("s")
		require.Error(t, err)
	}

	guard := starterHookMetadata{ID: "g", Delivers: "refusals", Events: []string{hook.PreToolUse}, Tools: []string{hook.ToolShell}, GuardRules: "docs/findings"}
	h, err := guard.handler("s")
	require.NoError(t, err)
	require.Equal(t, "docs/findings", h.GuardRules)
	for _, bad := range []func(m *starterHookMetadata){
		func(m *starterHookMetadata) { m.Command = []string{"node", "g.mjs"} },
		func(m *starterHookMetadata) { m.Events = []string{hook.PostToolUse} },
		func(m *starterHookMetadata) { m.Tools = nil },
	} {
		m := guard
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
	next, err := renderHookFile(existing, desiredHookEntries(hook.HarnessClaude, false))
	require.NoError(t, err)
	top, err := parseOrderedObject(next)
	require.NoError(t, err)
	require.Equal(t, []string{"z", "hooks", "a"}, top.keys)
	hooks, err := parseOrderedObject(top.values["hooks"])
	require.NoError(t, err)
	require.Equal(t, []string{"PostToolUse", "SessionStart", "SubagentStart", "PostCompact"}, hooks.keys)
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

func TestRhizomeOwnsOnlyItsOwnCommandsInAHookGroup(t *testing.T) {
	root := hookRepo(t, "on")
	path := filepath.Join(root, ".claude/settings.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Someone adds their own command to Rhizome's PostToolUse group.
	mine := `{"type": "command", "command": "echo mine"}, {`
	edited := strings.Replace(string(data), "{\n            \"args\": [\n              \"hook\",\n              \"claude\",\n              \"post-tool-use\"", mine+"\n            \"args\": [\n              \"hook\",\n              \"claude\",\n              \"post-tool-use\"", 1)
	require.NotEqual(t, string(data), edited)
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))
	requireUpToDate(t, root)

	_, err = runInit(t, RunOptions{Dir: root, Hooks: "off"})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"Stop": {"echo done"}, "PostToolUse": {"echo mine"}}, hookCommands(t, root, ".claude/settings.json"))
}

func TestHookEntriesWithoutAnOwnershipRecordNeedADecision(t *testing.T) {
	handWritten := `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"rzm","args":["hook","claude","post-tool-use"],"timeout":30},{"type":"command","command":"echo mine"}]}]}}`
	for _, hooks := range []string{"", "on"} {
		t.Run("hooks "+firstNonEmpty(hooks, "off"), func(t *testing.T) {
			root := hookRepo(t, "")
			writeFixtureFile(t, root, ".claude/settings.json", handWritten)
			out, err := runInit(t, RunOptions{Dir: root, Hooks: hooks})
			require.NoError(t, err)
			got, err := os.ReadFile(filepath.Join(root, ".claude/settings.json"))
			require.NoError(t, err)
			require.Equal(t, handWritten, string(got), "entries Rhizome has no record of writing are kept")
			require.Contains(t, out, ".claude/settings.json#rhizome-hooks")
			out, err = runInit(t, RunOptions{Dir: root, Check: true})
			require.NoError(t, err)
			require.Contains(t, out, "Rhizome has no record of writing these entries")
		})
	}
}

func TestTheFirstRunSaysWhetherEntriesAreInstalledYet(t *testing.T) {
	stubHookSupport(t, nil)
	opts := RunOptions{Workflow: "none", Addons: "compounding", Agents: "claude", Search: "off", Hooks: "on"}
	opts.Dir, opts.Check = machineRunRepo(t), true
	out, err := runInit(t, opts)
	require.ErrorIs(t, err, ErrChangesPending)
	require.Contains(t, out, "Claude Code hooks (.claude/settings.json): not installed yet; these changes install them")

	opts.Dir, opts.Check = machineRunRepo(t), false
	out, err = runInit(t, opts)
	require.NoError(t, err)
	require.Contains(t, out, "Claude Code hooks (.claude/settings.json): installed and current")
	require.NotContains(t, out, "not installed yet")
}
