package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

const generateModelsCache = `{"fetched_at":"2026-10-18","models":[
	{"slug":"gpt-a","context_window":272000,"tool_mode":"code_mode_only","multi_agent_version":"v2","use_responses_lite":true,
	 "supports_experimental_context":true,"supports_search_tool":true,"node_repl_disabled":false,
	 "experimental_supported_tools":["send_user_message_async","clock"],"apply_patch_tool_type":"freeform"},
	{"slug":"gpt-b","use_responses_lite":true}]}`

type generateLaunch struct {
	args    []string
	cwd     string
	catalog string
}

func generateDriver(t *testing.T, script *harnesstest.ScriptedIO, version string) (*Driver, *generateLaunch) {
	t.Helper()
	launch := &generateLaunch{}
	return &Driver{
		binary: "codex",
		runner: &fakeRunner{results: []command.Result{{Stdout: "codex-cli " + version}}},
		start: func(_ context.Context, _ string, args []string, cwd string) (transport, error) {
			launch.args, launch.cwd = args, cwd
			catalog, err := os.ReadFile(filepath.Join(cwd, "models.json"))
			require.NoError(t, err)
			launch.catalog = string(catalog)
			return newJSONTransport(script, script, script), nil
		},
	}, launch
}

func codexHome(t *testing.T, cache string) {
	t.Helper()
	home := t.TempDir()
	if cache != "" {
		require.NoError(t, os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(cache), 0o600))
	}
	t.Setenv("CODEX_HOME", home)
}

func written(t *testing.T, script *harnesstest.ScriptedIO, method string) json.RawMessage {
	t.Helper()
	for _, line := range script.Writes() {
		var message rpcMessage
		require.NoError(t, json.Unmarshal([]byte(line), &message))
		if message.Method == method {
			return message.Params
		}
	}
	t.Fatalf("%s not written in %v", method, script.Writes())
	return nil
}

func TestGenerateRunsSandboxedEphemeralTurn(t *testing.T) {
	codexHome(t, generateModelsCache)
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"rhizome/0.163.0"}}`,
		`{"jsonrpc":"2.0","method":"configWarning","params":{"summary":"Codex is ignoring 1 unrecognized configuration setting.\n  config.toml: `+"`tui.legacy`"+` is ignored."}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"config":{"model":"gpt-a","mcp_servers":{"remctl":{"enabled":true},"node_repl":{"enabled":false}}},"origins":{}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"thread":{"id":"thr_1"},"model":"gpt-a","reasoningEffort":"medium"}}`,
		`{"jsonrpc":"2.0","id":4,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`,
	)
	driver, launch := generateDriver(t, script, "0.163.0")
	type outcome struct {
		result harness.GenerateResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := driver.Generate(context.Background(), harness.GenerateRequest{
			Prompt: "Say pong.", Model: "gpt-a", Effort: "low", Schema: json.RawMessage(`{"type":"object"}`),
		})
		done <- outcome{result, err}
	}()
	require.Eventually(t, func() bool { return script.WriteCount() >= 5 }, time.Second, time.Millisecond)
	for _, line := range []string{
		`{"jsonrpc":"2.0","method":"model/rerouted","params":{"threadId":"thr_1","turnId":"turn_1","fromModel":"gpt-a","toModel":"gpt-b","reason":"capacity"}}`,
		`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr_1","turnId":"turn_1","item":{"id":"m1","type":"agentMessage","text":"{\"answer\":\"pong\"}"}}}`,
		`{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thr_1","turnId":"turn_1","tokenUsage":{"total":{"inputTokens":1300,"cachedInputTokens":200,"cacheWriteInputTokens":50,"outputTokens":7,"reasoningOutputTokens":3,"totalTokens":1307},"last":{"inputTokens":99,"cachedInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0,"totalTokens":99}}}}`,
		`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`,
	} {
		script.Push(line)
	}
	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, harness.GenerateResult{
		Output: json.RawMessage(`{"answer":"pong"}`), Model: "gpt-b", ToolPolicy: harness.ToolPolicySandboxed,
		Usage: harness.TokenUsage{InputTokens: 1300, CachedInputTokens: 200, CacheWriteInputTokens: 50, OutputTokens: 7, ReasoningOutputTokens: 3, TotalTokens: 1307},
	}, got.result)

	catalogJSON, err := json.Marshal(filepath.Join(launch.cwd, "models.json"))
	require.NoError(t, err)
	want := []string{"app-server"}
	for _, feature := range strings.Fields("multi_agent memories apps plugins hooks skill_search tool_suggest goals sleep_tool code_mode_host view_image image_generation browser_use computer_use chronicle workspace_dependencies recommended_plugins") {
		want = append(want, "-c", "features."+feature+"=false")
	}
	want = append(want,
		"-c", "include_permissions_instructions=false", "-c", "include_environment_context=false",
		"-c", "include_collaboration_mode_instructions=false", "-c", "include_apps_instructions=false",
		"-c", "skills.include_instructions=false", "-c", "skills.bundled.enabled=false",
		"-c", "tools.experimental_request_user_input={enabled=false}", "-c", `web_search="disabled"`,
		"-c", "model_catalog_json="+string(catalogJSON))
	require.Equal(t, want, launch.args)
	require.JSONEq(t, `{"models":[
		{"slug":"gpt-a","context_window":272000,"use_responses_lite":false,"supports_experimental_context":false,"supports_search_tool":false,
		 "node_repl_disabled":true,"experimental_supported_tools":[],"apply_patch_tool_type":null},
		{"slug":"gpt-b","use_responses_lite":false,"supports_experimental_context":false,"supports_search_tool":false,
		 "node_repl_disabled":true,"experimental_supported_tools":[],"apply_patch_tool_type":null}]}`, launch.catalog)
	_, err = os.Stat(launch.cwd)
	require.True(t, os.IsNotExist(err), "the temporary catalog directory must be removed")

	require.Equal(t, []string{"initialize", "initialized", "config/read", "thread/start", "turn/start"}, transcriptMethods(t, script.Writes()))
	require.JSONEq(t, `{"clientInfo":{"name":"rhizome","title":"Rhizome","version":"dev"},"capabilities":{"experimentalApi":true}}`, string(written(t, script, "initialize")))
	cwd, err := json.Marshal(launch.cwd)
	require.NoError(t, err)
	require.JSONEq(t, `{"cwd":`+string(cwd)+`}`, string(written(t, script, "config/read")))
	require.JSONEq(t, `{"model":"gpt-a","cwd":`+string(cwd)+`,"ephemeral":true,"environments":[],
		"baseInstructions":"Respond to the user's request.","approvalPolicy":"never","sandbox":"read-only",
		"config":{"mcp_servers.remctl.enabled":false,"mcp_servers.node_repl.enabled":false}}`, string(written(t, script, "thread/start")))
	require.JSONEq(t, `{"threadId":"thr_1","input":[{"type":"text","text":"Say pong."}],"effort":"low","outputSchema":{"type":"object"}}`, string(written(t, script, "turn/start")))
	require.True(t, script.Closed())
}

func TestGenerateWithoutSchemaReturnsTextAndReportsTurnFailure(t *testing.T) {
	codexHome(t, generateModelsCache)
	run := func(completion string) (harness.GenerateResult, error) {
		script := harnesstest.NewScriptedIO(
			`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"rhizome/0.163.0"}}`,
			`{"jsonrpc":"2.0","id":2,"result":{"config":{},"origins":{}}}`,
			`{"jsonrpc":"2.0","id":3,"result":{"thread":{"id":"thr_1"},"model":"gpt-b"}}`,
			`{"jsonrpc":"2.0","id":4,"result":{"turn":{"id":"turn_1","status":"inProgress"}}}`,
			`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr_1","turnId":"turn_1","item":{"id":"m1","type":"agentMessage","text":"pong"}}}`,
			completion,
		)
		driver, _ := generateDriver(t, script, "0.164.2")
		return driver.Generate(context.Background(), harness.GenerateRequest{Prompt: "Say pong."})
	}
	result, err := run(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"completed"}}}`)
	require.NoError(t, err)
	require.JSONEq(t, `"pong"`, string(result.Output))
	require.Equal(t, "gpt-b", result.Model, "the configured default model is reported when none was requested")

	_, err = run(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr_1","turn":{"id":"turn_1","status":"failed","error":{"message":"usage limit reached"}}}}`)
	require.ErrorIs(t, err, harness.ErrCommandFailed)
	require.ErrorContains(t, err, "usage limit reached")
}

// Codex may close stdout right after turn/completed, so the completion and the
// closure can be ready at once; the completed reply must win.
func TestAwaitGenerationPrefersACompletedTurn(t *testing.T) {
	ready := func() chan struct{} { c := make(chan struct{}); close(c); return c }
	closedErr := func() error { return harness.ErrTransportClosed }
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for range 100 {
		require.NoError(t, awaitGeneration(context.Background(), ready(), ready(), closedErr))
		require.NoError(t, awaitGeneration(canceled, ready(), make(chan struct{}), closedErr))
	}
	require.ErrorIs(t, awaitGeneration(context.Background(), make(chan struct{}), ready(), closedErr), harness.ErrTransportClosed)
	require.ErrorIs(t, awaitGeneration(canceled, make(chan struct{}), make(chan struct{}), closedErr), harness.ErrTimeout)
}

// The turn/start response, the reply, the completion, and EOF can all arrive
// before the request waiter resumes; the delivered response must win.
func TestAwaitResponsePrefersADeliveredResponse(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for range 100 {
		for _, ctx := range []context.Context{context.Background(), canceled} {
			response := make(chan rpcMessage, 1)
			response <- rpcMessage{Result: json.RawMessage(`{}`)}
			_, ok := awaitResponse(ctx, response, ready)
			require.True(t, ok)
		}
	}
	_, ok := awaitResponse(context.Background(), make(chan rpcMessage), ready)
	require.False(t, ok)
}

func TestGenerateFailsClosedBeforeTheTurn(t *testing.T) {
	initialized := `{"jsonrpc":"2.0","id":1,"result":{"userAgent":"rhizome/0.163.0"}}`
	configRead := `{"jsonrpc":"2.0","id":2,"result":{"config":{"mcp_servers":{"remctl":{}}},"origins":{}}}`
	for name, test := range map[string]struct {
		cache, version, model string
		lines                 []string
		reason                string
		spawned               bool
	}{
		"codex older than the verified version": {cache: generateModelsCache, version: "0.162.9", reason: "older than 0.163.0"},
		"models cache missing":                  {version: "0.163.0", reason: "read Codex models cache"},
		"models cache unparseable":              {cache: `{"models":`, version: "0.163.0", reason: "parse Codex models cache"},
		"model not in cache":                    {cache: generateModelsCache, version: "0.163.0", model: "gpt-z", reason: `no entry for model "gpt-z"`},
		"config/read fails": {cache: generateModelsCache, version: "0.163.0", spawned: true, reason: "config/read failed, so the tools cannot be removed: bad cwd",
			lines: []string{initialized, `{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"bad cwd"}}`}},
		"thread/start rejects the overrides": {cache: generateModelsCache, version: "0.163.0", spawned: true, reason: "thread/start failed, so the tools cannot be removed: failed to load configuration",
			lines: []string{initialized, configRead, `{"jsonrpc":"2.0","id":3,"error":{"code":-32600,"message":"failed to load configuration: invalid transport in mcp_servers.remctl"}}`}},
		"unknown feature key is ignored": {cache: generateModelsCache, version: "0.163.0", spawned: true, reason: "features.sleep_tool",
			lines: []string{initialized,
				`{"jsonrpc":"2.0","method":"configWarning","params":{"summary":"Codex is ignoring 1 unrecognized configuration setting.\n  session-flags: ` + "`features.sleep_tool`" + ` is ignored."}}`,
				configRead, `{"jsonrpc":"2.0","id":3,"result":{"thread":{"id":"thr_1"},"model":"gpt-a"}}`}},
		"MCP disable is ignored": {cache: generateModelsCache, version: "0.163.0", spawned: true, reason: "mcp_servers.remctl.enabled",
			lines: []string{initialized, configRead,
				`{"jsonrpc":"2.0","method":"warning","params":{"threadId":"thr_1","message":"session-flags: ` + "`mcp_servers.remctl.enabled`" + ` is ignored."}}`,
				`{"jsonrpc":"2.0","id":3,"result":{"thread":{"id":"thr_1"},"model":"gpt-a"}}`}},
		"configured model not in cache": {cache: generateModelsCache, version: "0.163.0", spawned: true, reason: `selected model "gpt-c"`,
			lines: []string{initialized, configRead, `{"jsonrpc":"2.0","id":3,"result":{"thread":{"id":"thr_1"},"model":"gpt-c"}}`}},
	} {
		t.Run(name, func(t *testing.T) {
			codexHome(t, test.cache)
			script := harnesstest.NewScriptedIO(test.lines...)
			driver, launch := generateDriver(t, script, test.version)
			_, err := driver.Generate(context.Background(), harness.GenerateRequest{Prompt: "Say pong.", Model: test.model})
			require.ErrorIs(t, err, harness.ErrUnsupported)
			require.ErrorContains(t, err, test.reason)
			require.Equal(t, test.spawned, launch.args != nil)
			require.NotContains(t, transcriptMethods(t, script.Writes()), "turn/start")
		})
	}
}
