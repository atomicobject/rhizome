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
	_, err := Apply(RunOptions{Dir: root, Workflow: "none", Agents: "claude", Search: "off"})
	require.NoError(t, err)
	_, err = runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err, "a person's matching value is not a pending change")
	_, err = runInit(t, RunOptions{Dir: root, Agents: "codex"})
	require.NoError(t, err)
	require.Equal(t, "true", localBinding(t, root), "a matching value someone else set stays theirs")
}

func TestCodexProfileWithCRLFLineEndingsIsStillRhizomes(t *testing.T) {
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root, Workflow: "none", Agents: "codex", Search: "off"})
	require.NoError(t, err)
	path := filepath.Join(root, ".codex/config.toml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(data), "\n", "\r\n")+"[projects.\"/x\"]\r\n"), 0o644))
	_, err = runInit(t, RunOptions{Dir: root, Agents: "claude"})
	require.NoError(t, err)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "[projects.\"/x\"]\r\n", string(data), "the block goes even after a checkout converts line endings")
}

func TestCodexProfileConflictsWithQuotedOrTabbedKeys(t *testing.T) {
	block := tomlBlock("codex-sandbox")
	for _, text := range []string{
		"\"default_permissions\" = \":read-only\"\n",
		"default_permissions\t= \":read-only\"\n",
		"[ features ]\nnetwork_proxy = true\n",
		"[[features]]\n",
		"[\"permissions\".mine]\n",
	} {
		require.NotEmpty(t, block.conflict([]byte(text), []byte(codexLoopbackProfile)), text)
	}
	require.Empty(t, block.conflict([]byte("[projects.\"/x\"]\nfeatures_extra = 1\nfeatures = 1\n"), []byte(codexLoopbackProfile)), "a longer key, or one in another table, is not a conflict")
}

func TestCodexProfileJoinsAPersonsFeaturesTable(t *testing.T) {
	root := machineRunRepo(t)
	mine := "[features]\nhooks = true\n\n[tui]\nscreen_reader_detection_done = true\n"
	writeFixtureFile(t, root, ".codex/config.toml", mine)
	result, err := Apply(RunOptions{Dir: root, Workflow: "none", Agents: "codex", Search: "off"})
	require.NoError(t, err)
	require.Empty(t, result.Warnings)
	path := filepath.Join(root, ".codex/config.toml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "[features]\n# BEGIN RZM INIT BLOCK: [features] codex-sandbox\nnetwork_proxy = true\n# END RZM INIT BLOCK: [features] codex-sandbox\nhooks = true\n")
	require.NotContains(t, text, "features.network_proxy", "a dotted key would redefine [features]:\n%s", text)
	require.Contains(t, text, `permissions.rhizome.network.domains."127.0.0.1" = "allow"`)

	out, err := runInit(t, RunOptions{Dir: root, Check: true})
	require.NoError(t, err)
	require.Contains(t, out, "Everything is up to date.", "the split profile reads back as Rhizome's")

	_, err = runInit(t, RunOptions{Dir: root, Agents: "claude"})
	require.NoError(t, err)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, mine, string(data), "removing the profile restores the person's file")
}

func TestCodexProfileSkipsTablesInsideMultilineStrings(t *testing.T) {
	block := tomlBlock("codex-sandbox")
	mine := "developer_instructions = \"\"\"\nExample:\n[features]\n\"\"\"\n\n[features]\nhooks = true\n"
	out, err := block.write([]byte(mine), []byte(codexLoopbackProfile))
	require.NoError(t, err)
	require.Contains(t, string(out), "Example:\n[features]\n\"\"\"\n", "the instructions stay as written")
	require.Contains(t, string(out), "\n[features]\n# BEGIN RZM INIT BLOCK: [features] codex-sandbox\nnetwork_proxy = true\n")
	require.Empty(t, block.conflict([]byte("notes = '''\n[[features]]\nfeatures = 1\n'''\n"), []byte(codexLoopbackProfile)))
	require.Empty(t, block.conflict([]byte("notes = \"\"\"\nsay \\\"\"\" and\n[[features]]\n\"\"\"\n"), []byte(codexLoopbackProfile)), "an escaped quote does not end the string")
	require.NotEmpty(t, block.conflict([]byte("[features]\n\"network\\u005fproxy\" = false\n"), []byte(codexLoopbackProfile)), "an escaped key is the same key")
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
