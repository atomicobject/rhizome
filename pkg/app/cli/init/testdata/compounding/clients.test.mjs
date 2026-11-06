import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

const { rzmClients } = await import(
  new URL("../../templates/starters/compounding/module/scripts/lib/calls.mjs", import.meta.url)
);

function earlyExitClient(t, code, output) {
  const dir = mkdtempSync(join(tmpdir(), "sm-client-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  // Node runs this extensionless CommonJS fixture as `node agent evaluate-batch`.
  writeFileSync(
    join(dir, "agent"),
    `
const { closeSync } = require("node:fs");
closeSync(0);
process.${code ? "stderr" : "stdout"}.write(${JSON.stringify(output + "\n")}, () => process.exit(${code}));
`,
  );
  return rzmClients(process.execPath, { cwd: dir });
}

// Exceed the pipe capacity so closing stdin deterministically interrupts a write.
const request = { text: "x".repeat(2 << 20) };

test("an early CLI rejection retains its stderr while request input is pending", async (t) => {
  const client = earlyExitClient(
    t,
    1,
    JSON.stringify({ error: "TypeSafe request failed: HTTP 401 unauthorized" }),
  );
  await assert.rejects(
    client.evaluateBatch(request),
    /evaluate-batch: TypeSafe request failed: HTTP 401 unauthorized/,
  );
});

test("an early successful CLI reply retains its result while request input is pending", async (t) => {
  const client = earlyExitClient(t, 0, JSON.stringify({ results: [] }));
  assert.deepEqual(await client.evaluateBatch(request), { results: [] });
});
