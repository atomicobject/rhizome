package agentcode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecuteUsesOneClientAndOnlyExposesOperations(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `
calls++;
reply({ok:true, exitCode:0, payload:{calls, input:frame.params.input}});`)
	result, err := Execute(context.Background(), executeOptions(t, fake, `
const first = await rzm.files({name:"first"});
const second = await rzm.fileContext({name:"second"});
return {calls:[first.payload.calls, second.payload.calls], close:typeof rzm.close, files:typeof rzm.files};`))
	require.NoError(t, err)
	require.True(t, result.OK)
	var value map[string]any
	require.NoError(t, json.Unmarshal(result.Result, &value))
	require.Equal(t, []any{float64(1), float64(2)}, value["calls"])
	require.Equal(t, "undefined", value["close"])
	require.Equal(t, "function", value["files"])
}

func TestExecuteForwardsSessionAndReadWrite(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `reply({ok:true, exitCode:0, payload:{args:process.argv.slice(2), initialize, sessionId:frame.params.sessionId}});`)
	options := executeOptions(t, fake, `return await rzm.files({name:"read"}, {sessionId:"call-session"});`)
	options.SessionID, options.ReadWrite = "connection-session", true
	result, err := Execute(context.Background(), options)
	require.NoError(t, err)
	require.True(t, result.OK)
	var outcome struct {
		Payload struct {
			Args       []string       `json:"args"`
			Initialize map[string]any `json:"initialize"`
			SessionID  string         `json:"sessionId"`
		} `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &outcome))
	require.Contains(t, outcome.Payload.Args, "--read-write")
	require.Equal(t, "connection-session", outcome.Payload.Initialize["sessionId"])
	require.Equal(t, "call-session", outcome.Payload.SessionID)
}

func TestExecuteExposesInputToScript(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `reply({ok:true, exitCode:0, payload:{}});`)
	options := executeOptions(t, fake, `return {input};`)
	options.Input = json.RawMessage(`{"path":"Notes/a.md","limit":3}`)
	result, err := Execute(context.Background(), options)
	require.NoError(t, err)
	require.True(t, result.OK)
	require.JSONEq(t, `{"input":{"path":"Notes/a.md","limit":3}}`, string(result.Result))

	result, err = Execute(context.Background(), executeOptions(t, fake, `return {input};`))
	require.NoError(t, err)
	require.JSONEq(t, `{"input":null}`, string(result.Result))

	options.Input = json.RawMessage(`{"path":`)
	_, err = Execute(context.Background(), options)
	require.ErrorContains(t, err, "input must be one JSON value")
}

func TestExecuteCapturesLogsAndScriptFailures(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `reply({ok:true, exitCode:0, payload:{}});`)
	t.Run("logs", func(t *testing.T) {
		result, err := Execute(context.Background(), executeOptions(t, fake, `console.log("hello", 3); return {text:"with spaces"};`))
		require.NoError(t, err)
		require.True(t, result.OK)
		require.JSONEq(t, `{"text":"with spaces"}`, string(result.Result))
		require.Contains(t, strings.Join(result.Diagnostics, "\n"), "hello 3")
	})
	for name, test := range map[string]struct{ code, message string }{
		"throw":  {`throw new Error("boom")`, "boom"},
		"syntax": {`return (`, "Unexpected"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Execute(context.Background(), executeOptions(t, fake, test.code))
			require.NoError(t, err)
			require.False(t, result.OK)
			require.NotNil(t, result.Error)
			require.Equal(t, "script_failed", result.Error.Code)
			require.Contains(t, result.Error.Message, test.message)
		})
	}
}

func TestExecuteRejectsUnserializableAndUnawaitedResults(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `
if (frame.params.input.fail) reply({ok:false, exitCode:1, payload:{reason:"failed"}});
else reply({ok:true, exitCode:0, payload:{}});`)
	for name, code := range map[string]string{
		"bigint":    `return 1n`,
		"circular":  `const value={}; value.self=value; return value`,
		"function":  `return () => 42`,
		"symbol":    `return Symbol("unrepresentable")`,
		"unawaited": `rzm.files({fail:true}); return "done"`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Execute(context.Background(), executeOptions(t, fake, code))
			require.NoError(t, err)
			require.False(t, result.OK)
			require.NotNil(t, result.Error)
			if name == "unawaited" {
				require.Equal(t, "unawaited_operation_failed", result.Error.Code)
			} else {
				require.Equal(t, "result_not_json_serializable", result.Error.Code)
			}
		})
	}
	t.Run("awaited domain failure remains a result", func(t *testing.T) {
		result, err := Execute(context.Background(), executeOptions(t, fake, `return await rzm.files({fail:true});`))
		require.NoError(t, err)
		require.True(t, result.OK)
		var outcome CallOutcome
		require.NoError(t, json.Unmarshal(result.Result, &outcome))
		require.False(t, outcome.OK)
		require.Equal(t, 1, outcome.ExitCode)
	})
}

func TestExecuteFinishesWhenScriptReturnsDespiteBackgroundTimer(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `reply({ok:true, exitCode:0, payload:{}});`)
	options := executeOptions(t, fake, `setInterval(() => {}, 10_000); await rzm.files({}); return "finished";`)
	options.Timeout = 500 * time.Millisecond
	result, err := Execute(context.Background(), options)
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Error)
	require.JSONEq(t, `"finished"`, string(result.Result))
}

func TestExecutePreCancelledContextDoesNotClaimExecution(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `reply({ok:true, exitCode:0, payload:{}});`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Execute(ctx, executeOptions(t, fake, `return await rzm.files({});`))
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Equal(t, "cancelled", result.Error.Code)
	require.Equal(t, false, result.Error.Details.(map[string]any)["mayHaveExecuted"])
}

func TestExecuteDoesNotDispatchOperationsScheduledAfterReturn(t *testing.T) {
	requireNode(t)
	marker := filepath.Join(t.TempDir(), "late-call")
	fake := writeExecuteServer(t, `
if (frame.params.input.marker) { writeFileSync(frame.params.input.marker, "dispatched"); reply({ok:true}); }
else setTimeout(() => reply({ok:true}), 20);`)
	code := `rzm.files({}).then(() => rzm.files({marker:` + string(mustJSON(t, marker)) + `})); return "finished";`
	result, err := Execute(context.Background(), executeOptions(t, fake, code))
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Equal(t, "execution_finished", result.Error.Code)
	require.NoFileExists(t, marker, "no new operation may start after the script returns")
}

func TestExecuteBoundsResultAndDiagnostics(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `reply({ok:true});`)
	for name, code := range map[string]string{
		"result":       `return "x".repeat(1048577)`,
		"escaped JSON": `return "<".repeat(1048570)`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Execute(context.Background(), executeOptions(t, fake, code))
			require.NoError(t, err)
			require.False(t, result.OK)
			require.Contains(t, result.Error.Code, "limit_exceeded")
		})
	}
	result, err := Execute(context.Background(), executeOptions(t, fake, `
console.log("x".repeat(100000));
await new Promise(resolve => process.stdout.write("callback diagnostic", resolve));
return 42;`))
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Error)
	require.LessOrEqual(t, len(strings.Join(result.Diagnostics, "")), maxExecuteDiagnostics)
}

func TestExecuteMissingNodeIsSetupError(t *testing.T) {
	vault := t.TempDir()
	executable := filepath.Join(vault, "rzm")
	require.NoError(t, os.WriteFile(executable, []byte("fixture"), 0o755))
	t.Setenv("PATH", t.TempDir())
	options := ExecuteOptions{Code: `return null`, ExecutablePath: executable, VaultPath: vault}
	_, err := Execute(context.Background(), options)
	require.ErrorContains(t, err, "node_runtime_unavailable")
}

func TestExecuteBoundsAsyncAndSynchronousWork(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `reply({ok:true, exitCode:0, payload:{}});`)
	for name, work := range map[string]string{
		"async": `await new Promise(resolve => setTimeout(resolve, 60_000)); return null`,
		"sync":  `while (true) {}`,
	} {
		t.Run(name, func(t *testing.T) {
			entered := filepath.Join(t.TempDir(), "entered")
			code := `(await import("node:fs")).writeFileSync(` + string(mustJSON(t, entered)) + `, "entered");` + "\n" + work
			options := executeOptions(t, fake, code)
			// Long enough for Node startup to reach the script; far shorter than the workload.
			options.Timeout = 2 * time.Second
			started := time.Now()
			result, err := Execute(context.Background(), options)
			require.NoError(t, err)
			require.FileExists(t, entered, "the deadline must interrupt running script work, not startup")
			require.False(t, result.OK)
			require.Equal(t, "deadline_exceeded", result.Error.Code)
			require.Less(t, time.Since(started), 10*time.Second)
		})
	}
}

func executeOptions(t *testing.T, executable, code string) ExecuteOptions {
	t.Helper()
	return ExecuteOptions{Code: code, ExecutablePath: executable, VaultPath: t.TempDir(), Timeout: 5 * time.Second}
}

func writeExecuteServer(t *testing.T, onCall string) string {
	t.Helper()
	description, err := Describe(supportedOperationNames())
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "execute-server.mjs")
	source := `#!/usr/bin/env node
import { createInterface } from "node:readline";
import { spawn } from "node:child_process";
import { writeFileSync } from "node:fs";
let calls = 0, initialize, activeFrame;
const hash = ` + string(mustJSON(t, description.ContractHash)) + `;
const reply = result => process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:activeFrame.id,result})+"\n");
createInterface({input:process.stdin}).on("line", line => {
 const frame = activeFrame = JSON.parse(line);
 if (frame.method === "initialize") { initialize = frame.params; reply({protocolVersion:"1", contractHash:hash}); }
 else if (frame.method === "call") { ` + onCall + ` }
 else if (frame.method === "shutdown") { reply({ok:true}); process.exit(0); }
});`
	require.NoError(t, os.WriteFile(path, []byte(source), 0o755))
	return path
}
