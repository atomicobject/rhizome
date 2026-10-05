package agentcode

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecuteReleasesDiscardedCompletedPayloads(t *testing.T) {
	requireNode(t)
	fake := writeExecuteServer(t, `reply({ok:true,exitCode:0,payload:{body:frame.params.input.barrier ? "done" : "x".repeat(262144)}});`)
	for _, observed := range []bool{true, false} {
		name, call := "unawaited", "rzm.files({}); if (index % 20 === 19) await rzm.fileContext({barrier:true});"
		if observed {
			name, call = "awaited", "await rzm.files({});"
		}
		t.Run(name, func(t *testing.T) {
			options := executeOptions(t, fake, fmt.Sprintf(`
(await import("node:v8")).setFlagsFromString("--expose_gc");
const gc = (await import("node:vm")).runInNewContext("gc");
gc(); const before = process.memoryUsage().heapUsed;
for (let index = 0; index < 400; index++) { %s }
await rzm.fileContext({barrier:true});
gc(); const after = process.memoryUsage().heapUsed;
return {retained:after-before};`, call))
			options.Timeout = 30 * time.Second
			result, err := Execute(context.Background(), options)
			require.NoError(t, err)
			require.True(t, result.OK, "%+v", result.Error)
			var memory struct {
				Retained int64 `json:"retained"`
			}
			require.NoError(t, json.Unmarshal(result.Result, &memory))
			t.Logf("discarded 400 x 256 KiB results; retained heap = %d bytes", memory.Retained)
			require.Less(t, memory.Retained, int64(16<<20), "completed discarded payloads must not accumulate")
		})
	}
}

func TestExecuteObservesAwaitedOperations(t *testing.T) {
	requireNode(t)
	fake := writeTrackingServer(t)
	for name, code := range map[string]string{
		"caught rejection": `try { await rzm.files({name:"caught",reject:true}); } catch (error) { return {code:error.code,name:error.details.name}; }`,
		"Promise.all":      `const rows = await Promise.all([rzm.files({name:"failed",domainFailure:true}), rzm.fileContext({name:"success"})]); return rows.map(row => ({ok:row.ok,name:row.payload.name}));`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Execute(context.Background(), executeOptions(t, fake, code))
			require.NoError(t, err)
			require.True(t, result.OK, "%+v", result.Error)
			if name == "caught rejection" {
				require.JSONEq(t, `{"code":"fixture_failed","name":"caught"}`, string(result.Result))
			} else {
				require.JSONEq(t, `[{"ok":false,"name":"failed"},{"ok":true,"name":"success"}]`, string(result.Result))
			}
		})
	}
}

func TestExecuteKeepsFirstUnobservedFailureInInvocationOrder(t *testing.T) {
	requireNode(t)
	fake := writeTrackingServer(t)
	for _, test := range []struct{ name, observer string }{
		{name: "unobserved"},
		{name: "then", observer: "await first.then(() => {}, () => {});"},
		{name: "catch", observer: "await first.catch(() => {});"},
		{name: "finally", observer: "await first.finally(() => {}).catch(() => {});"},
	} {
		t.Run(test.name, func(t *testing.T) {
			code := `
const first = rzm.files({name:"first",reject:true,hold:true});
rzm.files({name:"second",domainFailure:true});
await rzm.fileContext({release:true});
` + test.observer + `return "done";`
			result, err := Execute(context.Background(), executeOptions(t, fake, code))
			require.NoError(t, err)
			require.False(t, result.OK)
			require.Equal(t, "unawaited_operation_failed", result.Error.Code)
			details := result.Error.Details.(map[string]any)
			require.Equal(t, true, details["mayHaveExecuted"])
			if test.observer == "" {
				cause := details["cause"].(map[string]any)
				require.Equal(t, "fixture_failed", cause["code"])
				require.Equal(t, "first", cause["details"].(map[string]any)["name"])
			} else {
				outcome := details["outcome"].(map[string]any)
				require.Equal(t, false, outcome["ok"])
				require.Equal(t, "second", outcome["payload"].(map[string]any)["name"])
			}
		})
	}
}

func TestExecuteObservesSettledDomainFailure(t *testing.T) {
	requireNode(t)
	fake := writeTrackingServer(t)
	code := `const failed = rzm.files({name:"failed",domainFailure:true}); await rzm.fileContext({name:"barrier"}); return await failed;`
	result, err := Execute(context.Background(), executeOptions(t, fake, code))
	require.NoError(t, err)
	require.True(t, result.OK, "%+v", result.Error)
	require.JSONEq(t, `{"ok":false,"exitCode":1,"payload":{"name":"failed"}}`, string(result.Result))
}

func TestExecutePreservesUnhandledChains(t *testing.T) {
	requireNode(t)
	fake := writeTrackingServer(t)
	for _, test := range []struct{ name, chain, code, message string }{
		{name: "then", chain: ".then(() => {})", code: "fixture_failed", message: "rejected operation"},
		{name: "finally", chain: ".finally(() => {})", code: "fixture_failed", message: "rejected operation"},
		{name: "catch", chain: `.catch(() => { throw new Error("chain failed"); })`, code: "unhandled_rejection", message: "chain failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := Execute(context.Background(), executeOptions(t, fake, `rzm.files({name:"rejected",reject:true})`+test.chain+`; return "done";`))
			require.NoError(t, err)
			require.False(t, result.OK)
			require.Equal(t, test.code, result.Error.Code)
			require.Contains(t, result.Error.Message, test.message)
		})
	}
}

func TestExecuteDrainsAcceptedWritesBeforeCleanup(t *testing.T) {
	requireNode(t)
	fake := writeTrackingServer(t)
	for _, fails := range []bool{false, true} {
		t.Run(fmt.Sprintf("script_fails_%t", fails), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "accepted-write")
			code := `rzm.files({marker:` + string(mustJSON(t, marker)) + `}); `
			if fails {
				code += `throw new Error("script failed");`
			} else {
				code += `return "done";`
			}
			result, err := Execute(context.Background(), executeOptions(t, fake, code))
			require.NoError(t, err)
			require.FileExists(t, marker, "accepted write must complete before the owned client is closed")
			require.Equal(t, !fails, result.OK)
			if fails {
				require.Equal(t, "script_failed", result.Error.Code)
			}
		})
	}
}

func writeTrackingServer(t *testing.T) string {
	t.Helper()
	return writeExecuteServer(t, `
const input = frame.params.input;
const finish = () => {
  if (input.marker) writeFileSync(input.marker, "completed");
  const response = input.reject
    ? {error:{code:-32000,message:"rejected operation",data:{code:"fixture_failed",name:input.name,mayHaveExecuted:true}}}
    : {result:{ok:!input.domainFailure,exitCode:input.domainFailure ? 1 : 0,payload:{name:input.name}}};
  process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,...response})+"\n");
};
const held = globalThis.heldResponses ??= [];
if (input.hold) held.push(finish);
else if (input.release) { for (const respond of held.splice(0)) respond(); finish(); }
else if (input.marker) setTimeout(finish, 25);
else finish();`)
}
