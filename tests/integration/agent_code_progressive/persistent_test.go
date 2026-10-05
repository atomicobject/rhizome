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

func TestPersistentClientUsesOneChildAndObservesEdits(t *testing.T) {
	node := nodePathOrSkip(t)
	fixture := newProgressiveCodeFixture(t)
	binary := buildProgressiveBinary(t)
	generated := runProgressiveCommand(t, binary, fixture.vault, "agent", "code", "generate",
		"--operation", "files", "--operation", "file_context", "--output", t.TempDir())
	require.NoError(t, generated.err, "%s", generated.stderr)
	var manifest agentcode.Manifest
	require.NoError(t, json.Unmarshal([]byte(generated.stdout), &manifest))
	script := filepath.Join(t.TempDir(), "persistent.mjs")
	require.NoError(t, os.WriteFile(script, []byte(persistentScript), 0o644))
	result := runNode(t, node, fixture.vault, map[string]string{
		"PROGRESSIVE_MODULE": manifest.ModulePath,
		"PROGRESSIVE_BINARY": binary,
		"PROGRESSIVE_VAULT":  manifest.VaultPath,
	}, script)
	require.NoError(t, result.err, "stdout=%s stderr=%s", result.stdout, result.stderr)
	var receipt struct {
		SpawnCount int  `json:"spawnCount"`
		PID        int  `json:"pid"`
		Reaped     bool `json:"reaped"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.stdout), &receipt))
	require.Equal(t, 1, receipt.SpawnCount)
	require.Positive(t, receipt.PID)
	require.True(t, receipt.Reaped)
}

const persistentScript = `import assert from "node:assert/strict";
import cp from "node:child_process";
import { syncBuiltinESMExports } from "node:module";
import { writeFile, appendFile } from "node:fs/promises";
import { join } from "node:path";

// Observe the actual child lifecycle without inserting another executable.
const spawn = cp.spawn;
const children = [];
cp.spawn = (...args) => {
  assert.deepEqual(args[1], ["agent", "code", "serve"]);
  const child = spawn(...args);
  const record = { child, reaped: false };
  child.on("close", () => { record.reaped = true; });
  children.push(record);
  return child;
};
syncBuiltinESMExports();
const { createClient } = await import(process.env.PROGRESSIVE_MODULE);
const client = createClient({ executablePath: process.env.PROGRESSIVE_BINARY, vaultPath: process.env.PROGRESSIVE_VAULT });
try {
  const input = { inputs: ["notes/guide.md"], includeContent: true, dedupe: false };
  const before = await client.files(input);
  assert.equal(before.ok, true, JSON.stringify(before));
  assert.match(before.payload.files[0].content, /source note remains byte stable/);
  await writeFile(join(process.env.PROGRESSIVE_VAULT, "notes/guide.md"), "# Changed guide\n\nFresh content from an external edit.\n");
  const after = await client.files(input);
  assert.equal(after.ok, true, JSON.stringify(after));
  assert.match(after.payload.files[0].content, /Fresh content from an external edit/);
  const context = await client.fileContext({ files: ["src/example.py"], budgetChars: 4000 });
  assert.equal(context.ok, true, JSON.stringify(context));
  assert.ok(context.payload.text);
  await appendFile(join(process.env.PROGRESSIVE_VAULT, ".rhizome/config.yml"), "\n# Connection configuration changed\n");
  const changed = await client.files(input);
  assert.equal(changed.ok, false);
  assert.equal(changed.diagnostic.code, "code_mode_configuration_changed");
  assert.equal(children.length, 1);
} finally {
  await client.close();
}
assert.equal(children[0].reaped, true);
process.stdout.write(JSON.stringify({ spawnCount: children.length, pid: children[0].child.pid, reaped: children[0].reaped }));
`
