import test from "node:test";
import assert from "node:assert/strict";
import * as fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { runAudit } from "./run.mjs";
import { splitSource, candidateIndex } from "./inventory.mjs";
import { writeReport } from "./report.mjs";
import { documentMetadata } from "./triggers.mjs";
import { rebuildReport } from "./rebuild-report.mjs";

async function fixture() {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "jev-audit-"));
  await fs.mkdir(path.join(root, "ignored"));
  await fs.mkdir(path.join(root, ".rhizome/audits/old"), { recursive: true });
  await fs.writeFile(
    path.join(root, ".rhizome/audits/old/report.md"),
    "# DO NOT SEND\nPrevious findings",
  );
  await fs.writeFile(path.join(root, "ignored/secret.md"), "# DO NOT SEND\nsecret");
  await fs.writeFile(
    path.join(root, "guide.md"),
    "# Frobnicator\nFrobnicator validates inputs. See [code](main.go).",
  );
  await fs.writeFile(
    path.join(root, "main.go"),
    "package frobnicator\n// Frobnicator validates inputs.",
  );
  let calls = 0;
  const rzm = {
    async checkPaths({ paths }) {
      return {
        ok: true,
        payload: {
          paths: paths.map((item) => ({
            input: item.path,
            path: item.path === root ? "" : item.path,
            status: item.path.startsWith("ignored") ? "ignored" : "allowed",
          })),
        },
      };
    },
    async evaluateBatch({ items }) {
      calls++;
      assert.ok(!JSON.stringify(items).includes("DO NOT SEND"));
      return {
        ok: true,
        payload: {
          items: items.map((item) => ({
            id: item.id,
            status: "succeeded",
            attempts: 1,
            response: {
              model: "fixture",
              usage: { input_tokens: 10, output_tokens: 2 },
              answers: {
                role: { choice: "current" },
                impact: { score: 2 },
                evidence: { choice: "partial" },
              },
            },
          })),
        },
      };
    },
  };
  return {
    root,
    rzm,
    getCalls: () => calls,
    options: { root, output: path.join(root, "audit-output"), execute: true, model: "fixture" },
  };
}

test("audit ignores subtrees, caches successes, and changes invalidate checkpoints", async () => {
  const f = await fixture();
  const first = await runAudit(f.rzm, "notes", f.options);
  assert.equal(first.completed, 1);
  assert.equal(f.getCalls(), 1);
  assert.equal(first.filesByStatus.ignored, 1);
  const again = await runAudit(f.rzm, "notes", f.options);
  assert.equal(again.cached, 1);
  assert.equal(f.getCalls(), 1);
  await fs.appendFile(path.join(f.root, "guide.md"), "\nNew requirement.");
  const changed = await runAudit(f.rzm, "notes", f.options);
  assert.equal(changed.cached, 0);
  assert.equal(f.getCalls(), 2);
});

test("a lost response is uncertain and is never replayed on resume", async () => {
  const f = await fixture();
  let calls = 0;
  f.rzm.evaluateBatch = async () => {
    calls++;
    throw new Error("connection died");
  };
  assert.equal((await runAudit(f.rzm, "notes", f.options)).uncertain, 1);
  assert.equal((await runAudit(f.rzm, "notes", f.options)).uncertain, 1);
  assert.equal(calls, 1);
});

test("dry run never invokes Jev and limited coverage is explicit", async () => {
  const f = await fixture();
  await fs.writeFile(
    path.join(f.root, "second.md"),
    "# Another note\nFrobnicator must validate inputs.",
  );
  const result = await runAudit(f.rzm, "notes", { ...f.options, execute: false, limit: 1 });
  assert.equal(result.planned, 1);
  assert.equal(result.omittedByLimit, 1);
  assert.equal(f.getCalls(), 0);
});

test("source chunks preserve line provenance and report rendering escapes source", async () => {
  const chunks = splitSource("a.md", "note", "# First\nalpha\n# Second\nbeta");
  assert.equal(chunks[1].startLine, 3);
  assert.equal(chunks[1].endLine, 4);
  const note = splitSource("b.md", "note", "# Linked\n[alpha](a.md)")[0];
  const found = candidateIndex([...chunks, note])(note);
  assert.equal(found.candidateCoverage.explicitLinkChunks, 2);
  const output = await fs.mkdtemp(path.join(os.tmpdir(), "jev-report-"));
  const focus = {
    path: "<file>",
    kind: "note",
    startLine: 1,
    endLine: 1,
    content: "<script>bad()</script>",
  };
  const candidate = { path: "other.go", kind: "code", startLine: 1, endLine: 1, content: "Other" };
  await writeReport(
    output,
    {},
    [
      {
        action: "investigate_disagreement",
        focus,
        candidate,
        evidence: "sufficient",
        distribution: { contradicts: 0.95 },
      },
    ],
    {
      metadata: new Map(
        [focus, candidate].map((s) => [s.path, documentMetadata(s.path, s.content)]),
      ),
    },
  );
  const html = await fs.readFile(path.join(output, "candidates.html"), "utf8");
  assert.ok(!html.includes("<script>bad()"));
  assert.ok(html.includes("&lt;script&gt;bad()"));
});

test("report recomposition uses cached judgments without calling Jev and rejects changed evidence", async () => {
  const f = await fixture();
  await runAudit(f.rzm, "notes", f.options);
  f.rzm.evaluateBatch = () => {
    throw new Error("must not call Jev during report rebuild");
  };
  const report = await rebuildReport(f.rzm, f.options.output);
  assert.equal(report.reviewPackets, 0);
  let summary = JSON.parse(await fs.readFile(path.join(f.options.output, "summary.json"), "utf8"));
  assert.equal(summary.checkpointSuccessesRead, 1);
  await fs.appendFile(path.join(f.root, "guide.md"), "\nChanged.");
  await rebuildReport(f.rzm, f.options.output);
  summary = JSON.parse(await fs.readFile(path.join(f.options.output, "summary.json"), "utf8"));
  assert.equal(summary.checkpointSuccessesRead, 0);
  assert.equal(summary.invalidatedCheckpoints, 1);
});

test("account failures stop new batches and retain explicit unstarted coverage", async () => {
  const f = await fixture();
  for (let i = 0; i < 18; i++)
    await fs.writeFile(path.join(f.root, `note-${i}.md`), `# Note ${i}\nFrobnicator behavior.`);
  let calls = 0;
  f.rzm.evaluateBatch = async ({ items }) => {
    calls++;
    return {
      ok: true,
      payload: {
        items: items.map((item) => ({
          id: item.id,
          status: "failed",
          statusCode: 402,
          error: "Payment Required",
        })),
      },
    };
  };
  const result = await runAudit(f.rzm, "notes", f.options);
  assert.equal(calls, 1);
  assert.equal(result.failed, 8);
  assert.equal(result.notStarted, 11);
  assert.match(result.stoppedReason, /402/);
});
