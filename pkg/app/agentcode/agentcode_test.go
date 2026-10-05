package agentcode

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSurfaceAndDescribeAreSelectiveAndDeterministic(t *testing.T) {
	surface := Surface()
	require.NotEmpty(t, surface.Operations)
	require.Less(t, jsonSize(t, surface), 128*1024)

	first, err := Describe([]string{"files", "file_context"})
	require.NoError(t, err)
	second, err := Describe([]string{"file_context", "files"})
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Less(t, jsonSize(t, first), 128*1024)
	_, err = Describe(nil)
	require.ErrorContains(t, err, "at least one")
	_, err = Describe([]string{"files", "files"})
	require.ErrorContains(t, err, "duplicate")
	_, err = Describe([]string{"not_an_operation"})
	require.ErrorContains(t, err, "unsupported")
}

func TestDescribeProvidesSharedInvocationAndOutcomeMetadata(t *testing.T) {
	description, err := Describe([]string{"files", "find_connections"})
	require.NoError(t, err)
	require.Equal(t, "2", description.FormatVersion)
	require.Equal(t, "rzm.method(input, options?)", description.Invocation.CallSignature)
	require.False(t, description.Invocation.CallsSerialized)
	require.Equal(t, 30000, description.Invocation.CallOptions.TimeoutMSDefault)
	require.Contains(t, description.Invocation.CallOptions.TimeoutMS, "call envelope")
	require.Contains(t, description.Invocation.CallOptions.TimeoutMS, "Queue wait")
	require.Contains(t, description.Invocation.CallOptions.Signal, "not part of operation input")
	require.Len(t, description.Outcome.ResolvedFields, 6)
	require.Contains(t, description.Outcome.Evidence, "warnings")
	require.Contains(t, description.Outcome.Throws, "CodeModeError")
	require.Contains(t, description.Outcome.Throws, "mayHaveExecuted")

	encoded, err := json.Marshal(description)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(encoded), `"callSignature"`))
	require.Equal(t, 1, strings.Count(string(encoded), `"resolvedFields"`))

	surface, err := json.Marshal(Surface())
	require.NoError(t, err)
	require.NotContains(t, string(surface), "callSignature")
	require.NotContains(t, string(surface), "resolvedFields")
	require.Equal(t, FormatVersion, Surface().FormatVersion)
}

func TestDescribeHashIncludesSharedMetadata(t *testing.T) {
	description, err := Describe([]string{"files"})
	require.NoError(t, err)
	changed := description
	changed.Invocation.CallOptions.TimeoutMS = "changed"
	hash, err := descriptionHash(changed)
	require.NoError(t, err)
	require.NotEqual(t, description.ContractHash, hash)
	require.Equal(t, "4", GeneratorVersion)
}

func TestOutcomeMetadataMatchesCallOutcomeEnvelope(t *testing.T) {
	encoded, err := json.Marshal(CallOutcome{})
	require.NoError(t, err)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(encoded, &envelope))
	serialized := make([]string, 0, len(envelope))
	for name := range envelope {
		serialized = append(serialized, name)
	}
	described := []string{}
	for _, field := range codeOutcomeMetadata().ResolvedFields {
		described = append(described, field.Name)
	}
	require.ElementsMatch(t, serialized, described)
}

func TestGenerateRefusesUnownedManifestBeforePublishing(t *testing.T) {
	output := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(output, "manifest.json"), []byte(`{"owner":"someone-else"}`), 0o644))
	_, err := Generate(output, []string{"files"})
	require.ErrorContains(t, err, "unrelated")
	entries, readErr := os.ReadDir(output)
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
}

func TestGenerateRefusesSymlinkManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	output := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.json")
	require.NoError(t, os.WriteFile(target, []byte("outside"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(output, "manifest.json")))
	_, err := Generate(output, []string{"files"})
	require.ErrorContains(t, err, "non-regular")
	content, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Equal(t, "outside", string(content))
}

func TestGeneratedClientRejectsProtocolFault(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	fake := writeFakeServer(t, artifact.ContractHash)
	vault := t.TempDir()
	fault := runNode(t, artifact.ModulePath, fake, vault, `
const client = createClient({executablePath, vaultPath});
try { await client.files({fault:true}); } catch (error) { console.log(JSON.stringify({code:error.code})); }
await client.close();`)
	require.Equal(t, "invalid_input", fault["code"])
}

func TestGeneratedClientBoundsUnresponsiveServer(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	fake := writeFakeServer(t, artifact.ContractHash)
	vault := t.TempDir()
	startup := runNode(t, artifact.ModulePath, fake, vault, `
const client = createClient({executablePath, vaultPath, timeoutMs:10});
try { await client.files({inputs:["a.go"]}); } catch (error) { console.log(JSON.stringify({code:error.code, mayHaveExecuted:error.details.mayHaveExecuted})); }
await client.close();`, "FAKE_HANG_INIT=1")
	require.Equal(t, "deadline_exceeded", startup["code"])

	warmFake := writeResponseServer(t, artifact.ContractHash, `if(frame.params.input.warmup) process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,result:{ok:true}})+"\n");`)
	call := runNode(t, artifact.ModulePath, warmFake, vault, `
const client = createClient({executablePath, vaultPath});
await client.files({warmup:true});
try { await client.files({inputs:["a.go"]}, {timeoutMs:10}); } catch (error) { console.log(JSON.stringify({code:error.code, mayHaveExecuted:error.details.mayHaveExecuted})); }
await client.close();`)
	require.Equal(t, "deadline_exceeded", call["code"])
	require.True(t, call["mayHaveExecuted"].(bool))
}

func jsonSize(t *testing.T, value any) int {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return len(data)
}

func requireNode(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake executable fixture uses a Unix shebang")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is unavailable")
	}
}

func writeFakeServer(t *testing.T, contractHash string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-rzm.mjs")
	source := `#!/usr/bin/env node
let buffer = "";
process.stdin.on("data", chunk => { buffer += chunk; for (;;) { const line = buffer.indexOf("\n"); if (line < 0) return; const frame = JSON.parse(buffer.slice(0, line)); buffer = buffer.slice(line + 1); if (frame.method === "initialize") { if (!process.env.FAKE_HANG_INIT) process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,result:{protocolVersion:"1",contractHash:"` + contractHash + `",pid:process.pid}})+"\n"); } else if (frame.method === "call") { if (process.env.FAKE_HANG_CALL) continue; if (frame.params.input.fault) process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,error:{code:-32000,message:"bad input",data:{code:"invalid_input"}}})+"\n"); else process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,result:{ok:true,exitCode:0,payload:{operation:frame.params.operation}}})+"\n"); } else if (frame.method === "shutdown") { process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,result:{ok:true}})+"\n"); process.exit(0); } } });
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0o755))
	return path
}

func runNode(t *testing.T, module, executable, vault, body string, env ...string) map[string]any {
	t.Helper()
	runner := filepath.Join(t.TempDir(), "runner.mjs")
	source := `import {createClient} from ` + string(mustJSON(t, module)) + `;
const executablePath = ` + string(mustJSON(t, executable)) + `; const vaultPath = ` + string(mustJSON(t, vault)) + `;
` + body
	require.NoError(t, os.WriteFile(runner, []byte(source), 0o644))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", runner)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(lines[len(lines)-1]))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&value), string(output))
	return value
}

func mustJSON(t *testing.T, value string) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}

func TestGeneratedClientBoundsUnacknowledgedCancellation(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	dispatched := filepath.Join(t.TempDir(), "dispatched")
	// The peer answers warmup, records dispatch of the real call, then never replies or acknowledges cancellation.
	fake := writeResponseServer(t, artifact.ContractHash, `
if (frame.params.input.warmup) process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,result:{ok:true,exitCode:0}})+"\n");
else import("node:fs").then(fs => fs.writeFileSync(`+string(mustJSON(t, dispatched))+`, String(frame.id)));`)
	result := runNode(t, artifact.ModulePath, fake, t.TempDir(), `
import {existsSync} from "node:fs";
const client = createClient({executablePath, vaultPath});
try {
 await client.files({warmup:true});
 const controller = new AbortController();
 const call = client.files({inputs:["a.go"]}, {signal:controller.signal});
 call.catch(() => {});
 while (!existsSync(`+string(mustJSON(t, dispatched))+`)) await new Promise(resolve => setTimeout(resolve, 5));
 const started = Date.now();
 controller.abort();
 try { await call; console.log(JSON.stringify({code:"resolved"})); }
 catch (error) { console.log(JSON.stringify({code:error.code, elapsed:Date.now()-started, mayHaveExecuted:error.details.mayHaveExecuted})); }
} finally { await client.close(); }`)
	require.FileExists(t, dispatched)
	require.Equal(t, "cancelled", result["code"])
	require.Equal(t, true, result["mayHaveExecuted"])
	elapsed, err := result["elapsed"].(json.Number).Int64()
	require.NoError(t, err)
	require.Less(t, elapsed, int64(5000))
}
