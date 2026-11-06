import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { tempRepo } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/compounding/module/scripts/", import.meta.url);
const { resolveRzm } = await import(new URL("lib/calls.mjs", scripts));

test("RZM overrides the repository launcher, which overrides PATH", (t) => {
  const root = mkdtempSync(join(tmpdir(), "sm-rzm-"));
  const prior = process.env.RZM;
  t.after(() => {
    if (prior === undefined) delete process.env.RZM;
    else process.env.RZM = prior;
    rmSync(root, { recursive: true, force: true });
  });
  delete process.env.RZM;
  assert.equal(resolveRzm(root), "rzm");
  mkdirSync(join(root, "bin"));
  writeFileSync(join(root, "bin/rzm"), "synthetic launcher");
  assert.equal(resolveRzm(root), join(root, "bin/rzm"));
  process.env.RZM = "/explicit/synthetic-rzm";
  assert.equal(resolveRzm(root), "/explicit/synthetic-rzm");
});

test("calibration uses the repository launcher for an estimate", {
  skip: process.platform === "win32" && "the fake launcher uses a shebang",
}, (t) => {
  const root = tempRepo();
  writeFileSync(join(root, ".gitignore"), ".rhizome/compounding/\n");
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "bin"));
  const status = [{ kind: "claude", installed: true, loggedIn: true,
    models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"].map((id) => ({ id })) }];
  writeFileSync(join(root, "bin/rzm"), `#!${process.execPath}
if (process.argv.slice(2).join(" ") !== "harness status") process.exit(9);
console.log(${JSON.stringify(JSON.stringify(status))});
`, { mode: 0o755 });
  const env = { ...process.env };
  delete env.RZM;
  const result = spawnSync(process.execPath, [fileURLToPath(new URL("calibrate.mjs", scripts)),
    "--role", "extract", "--harness", "claude"], { cwd: root, env, encoding: "utf8" });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(JSON.parse(result.stdout).status, "estimate");
});
