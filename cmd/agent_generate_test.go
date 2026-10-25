package cmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

// The fake claude binary records what the real driver passes it, so this test
// covers the CLI front, the shared handler, and the Claude driver together.
func TestAgentGenerateRunsToolFreeClaudeAndReturnsEnvelope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI is a shell script")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "2.1.280 (Claude Code)"; exit 0; fi
printf '%s\n' "$@" > "` + dir + `/args"
printf '%s' "$FORCE_PROMPT_CACHING_5M" > "` + dir + `/cache"
cat > "` + dir + `/stdin"
echo '{"result":"","structured_output":{"answer":"pong"},"usage":{"input_tokens":2,"cache_creation_input_tokens":900,"cache_read_input_tokens":0,"output_tokens":5},"total_cost_usd":0.01,"modelUsage":{"claude-haiku-5-5":{"inputTokens":2,"outputTokens":5}}}'
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	prompt := filepath.Join(dir, "prompt.txt")
	schema := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(prompt, []byte("Say pong."), 0o600))
	require.NoError(t, os.WriteFile(schema, []byte(`{"type":"object","required":["answer"],"properties":{"answer":{"type":"string"}}}`), 0o600))

	stdout, stderr, err := runRootCLI(t, context.Background(), []string{"agent", "generate", "--provider", "claude", "--model", "haiku", "--effort", "low", "--prompt-file", prompt, "--schema", schema})
	require.NoError(t, err, stderr)
	require.JSONEq(t, `{"provider":"claude","output":{"answer":"pong"},"model":"claude-haiku-5-5","toolPolicy":"none",
		"usage":{"inputTokens":2,"cachedInputTokens":0,"cacheWriteInputTokens":900,"outputTokens":5,"reasoningOutputTokens":0,"totalTokens":907,"costUsd":0.01}}`, stdout)

	args, err := os.ReadFile(filepath.Join(dir, "args"))
	require.NoError(t, err)
	for _, want := range []string{"-p", "--tools\n\n", "--no-session-persistence", "--safe-mode", "--system-prompt", "--model\nhaiku", "--effort\nlow", "--json-schema"} {
		require.Contains(t, string(args), want)
	}
	cache, _ := os.ReadFile(filepath.Join(dir, "cache"))
	require.Equal(t, "1", string(cache))
	input, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	require.Equal(t, "Say pong.", strings.TrimSpace(string(input)))
}

func TestAgentGenerateWithoutProviderUsesAgentHarnessSetting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI is a shell script")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "2.1.280 (Claude Code)"; exit 0; fi
echo '{"result":"pong","usage":{"input_tokens":2,"output_tokens":1}}'
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	configFile := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(configFile, []byte("agent:\n  harness: claude\n"), 0o600))
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv
	env.CLIConfigFile = configFile
	// Like the test above, the command resolves this repository's vault from the working directory.
	env.WorkingDir, _ = os.Getwd()
	prompt := filepath.Join(dir, "prompt.txt")
	require.NoError(t, os.WriteFile(prompt, []byte("Say pong."), 0o600))

	stdout, stderr, err := runRootCLI(t, vaultconfig.WithEnv(context.Background(), env), []string{"agent", "generate", "--prompt-file", prompt})
	require.NoError(t, err, stderr)
	require.JSONEq(t, `{"provider":"claude","output":"pong","toolPolicy":"none",
		"usage":{"inputTokens":2,"cachedInputTokens":0,"outputTokens":1,"reasoningOutputTokens":0,"totalTokens":3}}`, stdout)
}
