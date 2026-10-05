//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/stretchr/testify/require"
)

func TestPersistentClientBoundsFanoutWithoutLosingConnection(t *testing.T) {
	node := nodePathOrSkip(t)
	fixture := newProgressiveCodeFixture(t)
	binary := buildProgressiveBinary(t)
	generated := runProgressiveCommand(t, binary, fixture.vault, "agent", "code", "generate", "--operation", "files", "--output", t.TempDir())
	require.NoError(t, generated.err, "%s", generated.stderr)
	var manifest agentcode.Manifest
	require.NoError(t, json.Unmarshal([]byte(generated.stdout), &manifest))
	script := filepath.Join(t.TempDir(), "fanout.mjs")
	require.NoError(t, os.WriteFile(script, []byte(`import assert from "node:assert/strict";
const {createClient}=await import(process.env.PROGRESSIVE_MODULE);
const client=createClient({executablePath:process.env.PROGRESSIVE_BINARY,vaultPath:process.env.PROGRESSIVE_VAULT});
try {
 const results=await Promise.allSettled(Array.from({length:40},()=>client.files({inputs:["notes/guide.md"],includeContent:true})));
 const passed=results.filter(r=>r.status==="fulfilled");
 const rejected=results.filter(r=>r.status==="rejected");
 assert.equal(passed.length,32);
 assert.equal(rejected.length,8);
 assert(passed.every(r=>r.value.ok));
 assert(rejected.every(r=>r.reason.code==="queue_full" && r.reason.details.mayHaveExecuted===false));
 const after=await client.files({inputs:["notes/guide.md"],includeContent:true});
 assert.equal(after.ok,true);
 assert.equal(after.payload.files[0].content,passed[0].value.payload.files[0].content);
} finally {await client.close();}
`), 0o644))
	result := runNode(t, node, fixture.vault, map[string]string{"PROGRESSIVE_MODULE": manifest.ModulePath, "PROGRESSIVE_BINARY": binary, "PROGRESSIVE_VAULT": fixture.vault}, script)
	require.NoError(t, result.err, "stdout=%s stderr=%s", result.stdout, result.stderr)
}
