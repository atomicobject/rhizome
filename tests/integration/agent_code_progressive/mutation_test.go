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

func TestPersistentClientPreservesWriteAuthorityAndValidationOutcomes(t *testing.T) {
	node := nodePathOrSkip(t)
	fixture := newProgressiveCodeFixture(t)
	binary := buildProgressiveBinary(t)
	generated := runProgressiveCommand(t, binary, fixture.vault, "agent", "code", "generate", "--operation", "files", "--operation", "note_move", "--operation", "current_user", "--operation", "validate", "--output", t.TempDir())
	require.NoError(t, generated.err, "%s", generated.stderr)
	var manifest agentcode.Manifest
	require.NoError(t, json.Unmarshal([]byte(generated.stdout), &manifest))
	script := filepath.Join(t.TempDir(), "mutations.mjs")
	require.NoError(t, os.WriteFile(script, []byte(mutationScript), 0o644))
	result := runNode(t, node, fixture.vault, map[string]string{
		"PROGRESSIVE_MODULE": manifest.ModulePath,
		"PROGRESSIVE_BINARY": binary,
		"PROGRESSIVE_VAULT":  manifest.VaultPath,
	}, script)
	require.NoError(t, result.err, "stdout=%s stderr=%s", result.stdout, result.stderr)
}

const mutationScript = `import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
const { createClient } = await import(process.env.PROGRESSIVE_MODULE);
const options = { executablePath: process.env.PROGRESSIVE_BINARY, vaultPath: process.env.PROGRESSIVE_VAULT };
const root = options.vaultPath;
const move = { source: "notes/guide.md", target: "notes/moved.md", updateBacklinks: true };
const original = await readFile(join(root, "notes/guide.md"), "utf8");
const readOnly = createClient(options);
try {
 const denied = await readOnly.noteMove(move);
 assert.equal(denied.ok, false);
 assert.equal(denied.diagnostic.code, "write_requires_read_write");
 assert.equal(await readFile(join(root, "notes/guide.md"), "utf8"), original);
 const identity = await readOnly.currentUser({ action: "set", personTitleOrRef: "Fixture Person" });
 assert.equal(identity.ok, false);
 assert.equal(identity.diagnostic.code, "write_requires_read_write");
} finally { await readOnly.close(); }
const client = createClient({ ...options, readWrite: true });
try {
 const identity = await client.currentUser({ action: "set", personTitleOrRef: "Fixture Person" });
 assert.equal(identity.ok, true, JSON.stringify(identity));
 const shown = await client.currentUser({ action: "show" });
 assert.equal(shown.payload.ref, identity.payload.ref);
 const clean = await client.validate({ action: "run", selector: "broken-links" });
 assert.equal(clean.exitCode, 0, JSON.stringify(clean));
 const moved = await client.noteMove(move);
 assert.equal(moved.ok, true, JSON.stringify(moved));
 const source = await client.files({ inputs: ["notes/moved.md"], includeContent: true });
 assert.equal(source.ok, true, JSON.stringify(source));
 assert.equal(source.payload.files[0].content, original);
 const repairPath = join(root, "notes/repair.md");
 const unrepaired = "# Repair\n\n[[notes/moved.md]]\n";
 await writeFile(repairPath, unrepaired);
 const plan = await client.validate({ action: "fix", selector: "link-hygiene" });
 assert.equal(plan.exitCode, 1, JSON.stringify(plan));
 assert.equal(await readFile(repairPath, "utf8"), unrepaired);
 const applied = await client.validate({ action: "fix", selector: "link-hygiene", apply: true });
 assert.equal(applied.exitCode, 0, JSON.stringify(applied));
 assert.equal(await readFile(repairPath, "utf8"), unrepaired.replace("notes/moved.md", "notes/moved"));
 const repaired = await client.validate({ action: "run", selector: "link-hygiene" });
 assert.equal(repaired.exitCode, 0, JSON.stringify(repaired));
 await writeFile(join(root, "notes/moved.md"), original + "\n[[nonexistent-note]]\n");
 const finding = await client.validate({ action: "run", selector: "broken-links" });
 assert.equal(finding.exitCode, 1, JSON.stringify(finding));
 assert.equal(finding.ok, false);
 const invalid = await client.validate({ action: "run", selector: "not-a-check" });
 assert.equal(invalid.exitCode, 2, JSON.stringify(invalid));
 assert.equal(invalid.ok, false);
} finally { await client.close(); }
`
