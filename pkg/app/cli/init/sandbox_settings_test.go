package init

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func localBinding(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/settings.json"))
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	value, err := jsonKey{"sandbox", "network", "allowLocalBinding"}.read(data)
	require.NoError(t, err)
	return string(value)
}

func TestClaudeSandboxMayReachTheLocalRuntime(t *testing.T) {
	root := hookRepo(t, "")
	require.Equal(t, "true", localBinding(t, root))
	data, err := os.ReadFile(filepath.Join(root, ".claude/settings.json"))
	require.NoError(t, err)
	require.Contains(t, string(data), `"Bash(ls)"`, "other settings are kept")

	// Without Claude Code, the value Rhizome wrote goes, and so does the
	// sandbox object it created.
	_, err = runInit(t, RunOptions{Dir: root, Agents: "codex"})
	require.NoError(t, err)
	require.Equal(t, "", localBinding(t, root))
	data, err = os.ReadFile(filepath.Join(root, ".claude/settings.json"))
	require.NoError(t, err)
	require.NotContains(t, string(data), "sandbox")
	require.Contains(t, string(data), `"Bash(ls)"`)
}

func TestClaudeSandboxSettingIsAPersonsToTurnOff(t *testing.T) {
	root := hookRepo(t, "")
	writeFixtureFile(t, root, ".claude/settings.json", `{"sandbox":{"network":{"allowLocalBinding":false}}}`)
	_, err := runInit(t, RunOptions{Dir: root})
	require.NoError(t, err)
	require.Equal(t, "false", localBinding(t, root), "an edited value is kept without a terminal")
}

func TestClaudeSandboxSettingRhizomeDidNotWriteStays(t *testing.T) {
	root := machineRunRepo(t)
	writeFixtureFile(t, root, ".claude/settings.json", `{"sandbox":{"network":{"allowLocalBinding":true}}}`)
	_, err := Apply(RunOptions{Dir: root, Workflow: "none", Agents: "codex", Search: "off"})
	require.NoError(t, err)
	require.Equal(t, "true", localBinding(t, root), "a person's own value is never removed")
}

func TestCodexGetsALoopbackOnlyProfileAboveAPersonsSettings(t *testing.T) {
	root := machineRunRepo(t)
	writeFixtureFile(t, root, ".codex/config.toml", "[projects.\"/x\"]\ntrust_level = \"trusted\"\n")
	_, err := Apply(RunOptions{Dir: root, Workflow: "none", Agents: "codex", Search: "off"})
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(root, ".codex/config.toml"))
	require.NoError(t, err)
	text := string(data)
	require.True(t, strings.HasPrefix(text, "# BEGIN RZM INIT BLOCK: codex-sandbox\n"), "top-level keys must precede tables:\n%s", text)
	require.Contains(t, text, `permissions.rhizome.network.domains."127.0.0.1" = "allow"`)
	require.True(t, strings.HasSuffix(text, "[projects.\"/x\"]\ntrust_level = \"trusted\"\n"), "a person's settings stay as they were")

	// Without Codex, only the block goes.
	_, err = runInit(t, RunOptions{Dir: root, Agents: "claude"})
	require.NoError(t, err)
	data, err = os.ReadFile(filepath.Join(root, ".codex/config.toml"))
	require.NoError(t, err)
	require.Equal(t, "[projects.\"/x\"]\ntrust_level = \"trusted\"\n", string(data))
}

func TestCodexProfileIsNotAddedOverAPersonsOwnPermissions(t *testing.T) {
	root := machineRunRepo(t)
	mine := "[features]\nnetwork_proxy = false\n"
	writeFixtureFile(t, root, ".codex/config.toml", mine)
	result, err := Apply(RunOptions{Dir: root, Workflow: "none", Agents: "codex", Search: "off"})
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(root, ".codex/config.toml"))
	require.NoError(t, err)
	require.Equal(t, mine, string(data), "adding the block would redefine [features], which TOML rejects")
	require.Contains(t, strings.Join(result.Warnings, "\n"), "skipping .codex/config.toml: it already defines features")
}

func TestClaudeSandboxSettingSkipsANonObjectSandbox(t *testing.T) {
	root := machineRunRepo(t)
	writeFixtureFile(t, root, ".claude/settings.json", `{"sandbox":true}`)
	result, err := Apply(RunOptions{Dir: root, Workflow: "none", Agents: "claude", Search: "off"})
	require.NoError(t, err, "a settings file Rhizome cannot extend must not stop init")
	data, err := os.ReadFile(filepath.Join(root, ".claude/settings.json"))
	require.NoError(t, err)
	require.Equal(t, `{"sandbox":true}`, string(data))
	require.Contains(t, strings.Join(result.Warnings, "\n"), "skipping .claude/settings.json: sandbox is not a JSON object")
}

func TestRerunSaysWhyTheCodexProfileIsMissing(t *testing.T) {
	root := machineRunRepo(t)
	writeFixtureFile(t, root, ".codex/config.toml", "[features]\nnetwork_proxy = false\n")
	_, err := Apply(RunOptions{Dir: root, Workflow: "none", Agents: "codex", Search: "off"})
	require.NoError(t, err)
	for _, check := range []bool{false, true} {
		out, err := runInit(t, RunOptions{Dir: root, Check: check})
		require.NoError(t, err)
		require.Contains(t, out, "Everything is up to date.")
		require.Contains(t, out, "skipping .codex/config.toml: it already defines features", "check=%v", check)
	}
}
