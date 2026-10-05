//go:build integration

package integration

import (
	"encoding/json"
	"strings"

	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/stretchr/testify/require"
)

func TestPersistentClientObservesIndexReplacement(t *testing.T) {
	node := nodePathOrSkip(t)
	fixture := newProgressiveCodeFixture(t)
	binary := buildProgressiveBinary(t)
	configPath := filepath.Join(fixture.vault, ".rhizome/config.yml")
	config, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, []byte(strings.Replace(string(config), "code:\n", "code:\n  enabled: true\n", 1)), 0o644))
	indexed := runProgressiveCommand(t, binary, fixture.vault, "index")
	require.NoError(t, indexed.err, "stdout=%s stderr=%s", indexed.stdout, indexed.stderr)
	generated := runProgressiveCommand(t, binary, fixture.vault, "agent", "code", "generate", "--operation", "code_symbol", "--output", t.TempDir())
	require.NoError(t, generated.err, "%s", generated.stderr)
	var manifest agentcode.Manifest
	require.NoError(t, json.Unmarshal([]byte(generated.stdout), &manifest))
	script := filepath.Join(t.TempDir(), "index-freshness.mjs")
	require.NoError(t, os.WriteFile(script, []byte(indexFreshnessScript), 0o644))
	result := runNode(t, node, fixture.vault, map[string]string{
		"PROGRESSIVE_MODULE": manifest.ModulePath,
		"PROGRESSIVE_BINARY": binary,
		"PROGRESSIVE_VAULT":  manifest.VaultPath,
	}, script)
	require.NoError(t, result.err, "stdout=%s stderr=%s", result.stdout, result.stderr)
}

const indexFreshnessScript = `import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { rename, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
const { createClient } = await import(process.env.PROGRESSIVE_MODULE);
const root = process.env.PROGRESSIVE_VAULT;
const client = createClient({ executablePath: process.env.PROGRESSIVE_BINARY, vaultPath: root });
try {
 const before = await client.codeSymbol({ symbol: "greet", path: "src/example.py" });
 assert.equal(before.ok, true, JSON.stringify(before));
 assert.match(JSON.stringify(before.payload), /greet/);
 const db = join(root, ".rhizome/db.sqlite");
 const old = await stat(db);
 // Preserve the old store and create a different indexed database in its place.
 for (const suffix of ["", "-wal", "-shm"]) {
  try { await rename(db + suffix, db + ".previous" + suffix); }
  catch (error) { if (error.code !== "ENOENT") throw error; }
 }
 await writeFile(join(root, "src/example.py"), "def farewell(name):\n    return f'goodbye {name}'\n");
 execFileSync(process.env.PROGRESSIVE_BINARY, ["index"], { cwd: root, env: { ...process.env, RZM_SKIP_REPO_DELEGATE: "1" }, timeout: 30000, stdio: "pipe" });
 assert.notEqual((await stat(db)).ino, old.ino);
 const after = await client.codeSymbol({ symbol: "farewell", path: "src/example.py" });
 assert.equal(after.ok, true, JSON.stringify(after));
 assert.match(JSON.stringify(after.payload), /farewell/);
 assert.ok(after.payload.definition, JSON.stringify(after));
 const removed = await client.codeSymbol({ symbol: "greet", path: "src/example.py" });
 assert.equal(removed.ok, true, JSON.stringify(removed));
 assert.equal(removed.payload.definition, undefined, JSON.stringify(removed));
} finally { await client.close(); }
`
