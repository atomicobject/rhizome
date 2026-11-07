import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

const calls = new URL("../../templates/starters/compounding/module/scripts/lib/calls.mjs", import.meta.url);
const noWindows = { skip: process.platform === "win32" && "the fake rzm uses a shebang" };

for (const exitCode of [1, 0]) {
  test(`rzmClients preserves the response when stdin closes early with exit ${exitCode}`, noWindows, (t) => {
    const root = mkdtempSync(join(tmpdir(), "sm-early-exit-"));
    t.after(() => rmSync(root, { recursive: true, force: true }));
    const fake = join(root, "rzm");
    writeFileSync(fake, `#!${process.execPath}
const fs = require("node:fs");
fs.closeSync(0);
fs.writeSync(${exitCode === 0 ? 1 : 2}, ${JSON.stringify(exitCode === 0 ? '{"items":[]}\n' : '{"error":"TypeSafe request failed: HTTP 401 unauthorized"}\n')});
process.exit(${exitCode});
`, { mode: 0o755 });
    const probe = join(root, "probe.mjs");
    writeFileSync(probe, `
import { rzmClients } from ${JSON.stringify(calls.href)};
try {
  const result = await rzmClients(${JSON.stringify(fake)}).evaluateBatch({ payload: "x".repeat(8 * 1024 * 1024) });
  console.log(JSON.stringify({ status: "fulfilled", result }));
} catch (error) {
  console.log(JSON.stringify({ status: "rejected", message: error.message }));
}
`);
    const result = spawnSync(process.execPath, [probe], { encoding: "utf8", timeout: 10000 });
    assert.equal(result.error, undefined, result.error?.message);
    assert.equal(result.status, 0, result.stderr);
    const outcome = JSON.parse(result.stdout);
    if (exitCode === 0) {
      assert.deepEqual(outcome, { status: "fulfilled", result: { items: [] } });
    } else {
      assert.equal(outcome.status, "rejected", result.stdout);
      assert.match(outcome.message, /HTTP 401 unauthorized/);
    }
  });
}
