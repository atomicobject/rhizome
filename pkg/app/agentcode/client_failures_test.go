package agentcode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedClientRejectsStaleContractBeforeCalls(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	fake := writeFakeServer(t, "stale-contract")
	result := runNode(t, artifact.ModulePath, fake, t.TempDir(), `
const client = createClient({executablePath, vaultPath});
try { await client.files({inputs:["a.go"]}); }
catch (error) { console.log(JSON.stringify({code:error.code})); }
finally { await client.close(); }`)
	require.Equal(t, "code_mode_artifact_stale", result["code"])
}

func TestGeneratedClientRejectsEveryPendingCallOnProcessDeath(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	fake := writeResponseServer(t, artifact.ContractHash, `process.exit(23);`)
	result := runNode(t, artifact.ModulePath, fake, t.TempDir(), `
const client = createClient({executablePath, vaultPath});
try {
 const settled = await Promise.allSettled([client.files({inputs:["a"]}), client.files({inputs:["b"]})]);
 console.log(JSON.stringify({errors:settled.map(item => item.status === "rejected" ? item.reason.code : "unexpected-success"),uncertainty:settled.map(item=>item.reason?.details.mayHaveExecuted)}));
} finally { await client.close(); }`)
	require.Equal(t, []any{"server_exited", "server_exited"}, result["errors"])
	require.Equal(t, []any{true, true}, result["uncertainty"])
}

func TestGeneratedClientRejectsMalformedAndOversizedResponses(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	tests := []struct{ name, behavior, code string }{
		{"malformed", `process.stdout.write("not-json\n");`, "invalid_protocol_output"},
		{"oversized", `process.stdout.write("x".repeat(2048));`, "output_limit_exceeded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := writeResponseServer(t, artifact.ContractHash, test.behavior)
			result := runNode(t, artifact.ModulePath, fake, t.TempDir(), `
const client = createClient({executablePath, vaultPath, outputLimitBytes:1024});
try { await client.files({inputs:["a"]}); }
catch (error) { console.log(JSON.stringify({code:error.code})); }
finally { await client.close(); }`)
			require.Equal(t, test.code, result["code"])
		})
	}
}

func TestGeneratedClientBoundsEachFrameRatherThanCombinedRead(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	fake := writeResponseServer(t, artifact.ContractHash, `
const reply = {jsonrpc:"2.0",id:frame.id,result:{ok:true,payload:"x".repeat(180)}};
process.stdout.write(JSON.stringify({...reply,id:"unmatched"})+"\n"+JSON.stringify(reply)+"\n");`)
	result := runNode(t, artifact.ModulePath, fake, t.TempDir(), `
const client = createClient({executablePath, vaultPath, outputLimitBytes:300});
try {
 const outcomes = await Promise.all([client.files({inputs:["a"]}), client.files({inputs:["b"]})]);
 console.log(JSON.stringify({ok:outcomes.every(outcome => outcome.ok)}));
} finally { await client.close(); }`)
	require.Equal(t, true, result["ok"])
}

func writeResponseServer(t *testing.T, hash, onCall string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "response-server.mjs")
	source := `#!/usr/bin/env node
import { createInterface } from "node:readline";
const responses = [];
createInterface({input:process.stdin}).on("line", line => {
 const frame = JSON.parse(line);
 if (frame.method === "initialize") process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,result:{protocolVersion:"1",contractHash:` + string(mustJSON(t, hash)) + `}})+"\n");
 else if (frame.method === "call") { ` + onCall + ` }
 else if (frame.method === "shutdown") process.exit(0);
});
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0o755))
	return path
}
