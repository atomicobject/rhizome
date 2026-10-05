import test from "node:test";
import assert from "node:assert/strict";
import * as fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { verifiedReport, validateDecision, recordReviews } from "./reviews.mjs";
import { documentMetadata } from "./triggers.mjs";
import { runAudit } from "./run.mjs";

const metadata = () => new Map([["guide.md", documentMetadata("guide.md", "# Guide\nSerial.\n")]]);
const packet = (id = "a") => ({
  id,
  evidenceHash: "hash-" + id,
  evidence: [{ sources: [{ path: "guide.md" }] }],
});
const decision = (id = "a") => ({
  id,
  evidenceHash: "hash-" + id,
  verdict: "confirmed",
  reviewer: "source-review",
  reason: "The current implementation and prose differ.",
  title: "Correct contract",
  correction: "Update the stale serial claim.",
  scope: "Current execution",
  editTarget: "guide.md",
  changeKey: "execution-contract",
  claims: ["The guide requires serial execution.", "The implementation permits concurrency."],
  evidence: [
    {
      path: "guide.md",
      startLine: 1,
      endLine: 2,
      sourceHash: metadata().get("guide.md").sourceHash,
    },
  ],
});

test("only fresh confirmations become improvements; duplicate corroboration yields one edit", async () => {
  const out = await fs.mkdtemp(path.join(os.tmpdir(), "jev-reviews-"));
  let r = await verifiedReport(out, [packet(), packet("b")], metadata());
  assert.equal(r.improvements.length, 0);
  assert.equal(r.pending.length, 2);
  r = await verifiedReport(out, [packet(), packet("b")], metadata(), [decision(), decision("b")]);
  assert.equal(r.summary.confirmed, 2);
  assert.equal(r.improvements.length, 1);
  assert.equal(r.pending.length, 0);
  const changed = metadata();
  changed.set("guide.md", documentMetadata("guide.md", "# Guide\nConcurrent."));
  r = await verifiedReport(out, [packet(), packet("b")], changed);
  assert.equal(r.improvements.length, 0);
  assert.equal(r.summary.stale, 2);
});

test("review submission is atomic and rejects stale, ungrounded, or out-of-scope confirmations", async () => {
  const out = await fs.mkdtemp(path.join(os.tmpdir(), "jev-reviews-"));
  assert.throws(
    () => validateDecision({ ...decision(), claims: [] }, packet(), metadata()),
    /claims/,
  );
  assert.throws(
    () => validateDecision({ ...decision(), evidenceHash: "old" }, packet(), metadata()),
    /stale/,
  );
  assert.throws(
    () =>
      validateDecision(
        { ...decision(), evidence: [{ path: "ignored.md", sourceHash: "x" }] },
        packet(),
        metadata(),
      ),
    /unavailable/,
  );
  await assert.rejects(
    verifiedReport(out, [packet(), packet("b")], metadata(), [
      decision(),
      { ...decision("b"), scope: "" },
    ]),
    /scope/,
  );
  await assert.rejects(fs.stat(path.join(out, "reviews.json")), /ENOENT/);
});

test("rejected and unresolved decisions remain visible without entering the improvement report", async () => {
  const out = await fs.mkdtemp(path.join(os.tmpdir(), "jev-reviews-"));
  const r = await verifiedReport(out, [packet(), packet("b")], metadata(), [
    { ...decision(), verdict: "rejected" },
    { ...decision("b"), verdict: "unresolved" },
  ]);
  assert.equal(r.improvements.length, 0);
  assert.equal(r.summary.rejected, 1);
  assert.equal(r.summary.unresolved, 1);
});

test("saved audit review runs end-to-end without another Jev call and rejects newly ignored evidence", async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "jev-verified-audit-"));
  await fs.writeFile(
    path.join(root, "guide.md"),
    "# Contract\nExecution must be serial. See [implementation](main.go).\n",
  );
  await fs.writeFile(path.join(root, "main.go"), "package main\nfunc Execute() {}\n");
  let ignored = false,
    calls = 0;
  const rzm = {
    checkPaths: async ({ paths }) => ({
      ok: true,
      payload: {
        paths: paths.map((p) => ({
          input: p.path,
          path: p.path === root ? "" : p.path,
          status: ignored && p.path === "guide.md" ? "ignored" : "allowed",
        })),
      },
    }),
    evaluateBatch: async ({ items }) => {
      calls++;
      return {
        ok: true,
        payload: {
          items: items.map((p) => ({
            id: p.id,
            status: "succeeded",
            response: {
              usage: { input_tokens: 1, output_tokens: 1 },
              answers: {
                role: { choice: "current" },
                evidence: { choice: "sufficient" },
                relation_0: { choice: "contradicts", probabilities: { contradicts: 0.95 } },
                action_0: { choice: "correction" },
              },
            },
          })),
        },
      };
    },
  };
  const output = path.join(root, "audit");
  await runAudit(rzm, "notes", { root, output, execute: true, model: "fixture" });
  assert.equal(JSON.parse(await fs.readFile(output + "/findings.json")).length, 0);
  const candidate = JSON.parse(await fs.readFile(output + "/candidates.json"))[0];
  assert.ok(candidate);
  const source = candidate.evidence[0].sources.find((s) => s.path === "guide.md");
  const verdict = {
    ...decision(),
    id: candidate.id,
    evidenceHash: candidate.evidenceHash,
    evidence: [
      { path: source.path, startLine: 1, endLine: 2, sourceHash: source.document.sourceHash },
    ],
  };
  await recordReviews(rzm, output, [verdict]);
  assert.equal(calls, 1);
  assert.equal(JSON.parse(await fs.readFile(output + "/findings.json")).length, 1);
  ignored = true;
  await assert.rejects(recordReviews(rzm, output, [verdict]), /unavailable/);
  // A normal rebuild clears the prior report when its evidence becomes unavailable.
  const { rebuildReport } = await import("./rebuild-report.mjs");
  await rebuildReport(rzm, output);
  assert.equal(JSON.parse(await fs.readFile(output + "/findings.json")).length, 0);
});
