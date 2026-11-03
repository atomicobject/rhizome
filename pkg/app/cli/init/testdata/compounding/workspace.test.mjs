// The compounding workspace's actions (SPEC-0124 US9), against scratch Git
// repositories. Every proposal, person, and credential below is synthetic.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { isAbsolute, join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const scripts = new URL("../../templates/starters/compounding/module/scripts/", import.meta.url);
const { PUBLISH_STATEMENT, workspace } = await import(new URL("workspace.mjs", scripts));
const { renderFinding } = await import(new URL("lib/findings.mjs", scripts));
const { openStore } = await import(new URL("lib/store.mjs", scripts));
const { report } = await import(new URL("report.mjs", scripts));

const user = {
  found: true,
  typeName: "Person",
  path: "docs/people/pat-example.md",
  title: "Pat Example",
};
const git = (root, ...args) =>
  execFileSync("git", ["-c", "user.email=test@example.com", "-c", "user.name=Test", ...args], {
    cwd: root,
    encoding: "utf8",
  });
const secret = ["ghp", "a1B2".repeat(9)].join("_");

const proposal = (cluster, over = {}) => ({
  cluster,
  tier: "A",
  families: 2,
  kind: "fact",
  statement: `Busy timeout ${cluster}: a value of 0 means the 30-second default.`,
  why: "Callers expecting no wait block for 30 seconds.",
  appliesTo: ["pkg/store/**/*.go"],
  commands: [],
  response: "remember",
  responseReason: "A deliberate default.",
  verification: [
    { path: "pkg/store/db.go", checked: "The default is applied when the value is 0." },
  ],
  evidence: [
    {
      episode: "claude:s1:4",
      harness: "claude",
      session: "s1",
      ts: "2026-10-01T10:00:00Z",
      lens: "discovery",
      origin: "agent",
      toolCalls: 7,
    },
  ],
  ...over,
});

// A finding a teammate approved and committed: this checkout has its note
// but none of its private evidence.
const teammateFinding = (slug, response = "fix") =>
  renderFinding({
    response,
    kind: "fact",
    statement: `Teammate ${slug}: the fallback in db.go is dead.`,
    learning: `hash-${slug}`,
    why: "Agents keep reading it.",
    change: "Delete the fallback.",
    question: "Should it stay?",
    reviewedBy: "[[docs/people/sam-example]]",
    reviewedOn: "2026-10-20",
    families: 3,
    toolCalls: 12,
    evidenceLines: [
      "- claude session `s9`, message `claude:s9:1`, 2026-10-19, discovery, 12 tool calls",
    ],
    assignee: "[[docs/people/sam-example]]",
  });

function repo(proposals, findings = {}) {
  const root = mkdtempSync(join(tmpdir(), "sm-workspace-"));
  git(root, "init", "-q");
  mkdirSync(join(root, "pkg/store"), { recursive: true });
  mkdirSync(join(root, "docs/memory/agent-findings"), { recursive: true });
  writeFileSync(join(root, "pkg/store/db.go"), "package store\n");
  writeFileSync(join(root, ".gitignore"), ".rhizome/compounding/\n");
  for (const [slug, text] of Object.entries(findings))
    writeFileSync(join(root, `docs/memory/agent-findings/${slug}.md`), text);
  git(root, "add", "-A");
  git(root, "commit", "-qm", "init");
  const state = join(root, ".rhizome/compounding");
  mkdirSync(state, { recursive: true });
  const commit = git(root, "rev-parse", "HEAD").trim();
  writeFileSync(
    join(state, "proposals.json"),
    JSON.stringify(proposals.map((p) => ({ verified: { commit }, ...p }))),
  );
  const ws = (op, input) => workspace(op, input, { root, state, user });
  return { root, state, ws };
}

test("recommendations lists pending proposals and teammates' open findings, by response", () => {
  const { ws } = repo(
    [proposal("k-1"), proposal("k-fix", { response: "fix", change: "Delete it." })],
    { "fix-teammate-a": teammateFinding("a") },
  );
  const { items } = ws("recommendations");
  assert.deepEqual(
    items.map((x) => [x.ref, x.kind, x.response]),
    [
      ["proposal:k-1", "proposal", "remember"],
      ["proposal:k-fix", "proposal", "fix"],
      ["finding:docs/memory/agent-findings/fix-teammate-a.md", "finding", "fix"],
    ],
  );
  const [p, , f] = items;
  assert.equal(p.title, "Busy timeout k-1");
  assert.equal(p.status, "pending");
  assert.equal(f.title, "Teammate a");
  assert.equal(f.assignee, "[[docs/people/sam-example]]");
  assert.equal(f.actionItem, "Fix: Teammate a");
});

test("item gives a proposal's draft and redacted sources, or a finding's note", () => {
  const { state, ws } = repo([proposal("k-1")], { "fix-teammate-a": teammateFinding("a") });
  // The pool holds the cited excerpt, with a credential-shaped value in it.
  mkdirSync(join(state, "pool"));
  writeFileSync(
    join(state, "pool/episodes.json"),
    JSON.stringify({
      "claude:s1:4": { id: "claude:s1:4", text: `Set the token to ${secret} and retry.` },
    }),
  );
  const p = ws("item", { ref: "proposal:k-1" });
  assert.equal(p.proposal.cluster, "k-1");
  assert.equal(p.proposal.why, "Callers expecting no wait block for 30 seconds.");
  assert.equal(p.current, true);
  assert.equal(p.sources.length, 1);
  assert.ok(!p.sources[0].text.includes(secret));
  const f = ws("item", { ref: "finding:docs/memory/agent-findings/fix-teammate-a.md" });
  assert.match(f.note, /## Proposed change\n\nDelete the fallback\./);
  assert.throws(() => ws("item", { ref: "proposal:k-9" }), /no proposal k-9/);
  assert.throws(
    () => ws("item", { ref: "finding:docs/memory/agent-findings/none.md" }),
    /no finding at/,
  );
});

test("prompt names the selected references of both kinds and the publish statement", () => {
  const findings = Object.fromEntries(
    ["a", "b", "c"].map((s) => [`fix-teammate-${s}`, teammateFinding(s)]),
  );
  const { ws } = repo(
    [proposal("k-1"), proposal("k-2", { response: "document" }), proposal("k-3")],
    findings,
  );
  const listed = ws("recommendations").items;
  const refs = listed.map((x) => x.ref);
  assert.equal(refs.length, 6);
  const chosen = [
    "proposal:k-1",
    "proposal:k-3",
    "finding:docs/memory/agent-findings/fix-teammate-a.md",
    "finding:docs/memory/agent-findings/fix-teammate-c.md",
  ];
  const { prompt } = ws("prompt", { items: chosen });
  for (const r of chosen) assert.ok(prompt.includes(`\`${r}\``), r);
  for (const r of refs.filter((r) => !chosen.includes(r))) assert.ok(!prompt.includes(r), r);
  assert.ok(prompt.includes(PUBLISH_STATEMENT));
  assert.match(prompt, /shards\.mjs show <cluster>/);
  // Each proposal carries the revision the person reviewed, so a redraft is not covered.
  for (const c of ["k-1", "k-3"]) {
    const { revision } = listed.find((x) => x.ref === `proposal:${c}`);
    assert.match(revision, /\S/);
    assert.ok(prompt.includes(`\`proposal:${c}\` (remember), revision \`${revision}\``), c);
  }
  assert.match(prompt, /prints a different revision[^\n]*do not approve/);
  assert.match(prompt, /one branch, ready for one pull request/);
  // References, not content.
  assert.ok(!prompt.includes("Busy timeout"));
  assert.throws(() => ws("prompt", { items: ["proposal:k-9"] }), /no longer recommendations/);
  assert.throws(() => ws("prompt", { items: [] }), /at least one/);
  // The revision the view showed is pinned; a redraft since refuses.
  const shown = listed.find((x) => x.ref === "proposal:k-1").revision;
  assert.match(
    ws("prompt", { items: [`proposal:k-1@${shown}`] }).prompt,
    new RegExp(`revision \`${shown}\``),
  );
  assert.throws(
    () => ws("prompt", { items: ["proposal:k-1@stale"] }),
    /redrafted since you reviewed them: proposal:k-1/,
  );
});

test("dismiss records a local dismissal that removes the proposal", () => {
  const { root, ws } = repo([proposal("k-1"), proposal("k-2")]);
  const { revision } = ws("recommendations").items[0];
  const out = ws("dismiss", { cluster: "k-1", revision, reason: "not relevant" });
  assert.equal(out.dismissed, "k-1");
  assert.ok(out.decision.startsWith(join(root, ".rhizome/compounding/decisions")));
  assert.deepEqual(
    ws("recommendations").items.map((x) => x.ref),
    ["proposal:k-2"],
  );
  assert.equal(git(root, "status", "--porcelain"), "");
});

test("dismiss refuses missing or changed reviewed revisions without hiding a proposal", () => {
  const { state, ws } = repo([proposal("k-1")]);
  const { revision } = ws("recommendations").items[0];
  assert.throws(() => ws("dismiss", { cluster: "k-1", reason: "not relevant" }), /revision/);
  const path = join(state, "proposals.json");
  const proposals = JSON.parse(readFileSync(path, "utf8"));
  proposals[0].statement = "A redrafted rule the person has not reviewed.";
  writeFileSync(path, JSON.stringify(proposals));
  const before = readFileSync(path, "utf8");
  assert.throws(
    () => ws("dismiss", { cluster: "k-1", revision, reason: "not relevant" }),
    /changed since it was shown/,
  );
  assert.equal(readFileSync(path, "utf8"), before);
  assert.equal(ws("recommendations").items[0].cluster, "k-1");
});

test("hide hides a finding locally and leaves its note and action item alone", () => {
  const text = teammateFinding("a");
  const { root, ws } = repo([], { "fix-teammate-a": text });
  const path = "docs/memory/agent-findings/fix-teammate-a.md";
  assert.deepEqual(ws("hide", { path }), { hidden: path });
  assert.deepEqual(ws("recommendations").items, []);
  assert.equal(readFileSync(join(root, path), "utf8"), text);
  assert.match(text, /\n- \[ \] Fix: Teammate a #action-item\n/);
  assert.equal(git(root, "status", "--porcelain"), "");
  assert.throws(() => ws("hide", { path: "docs/memory/agent-findings/none.md" }), /no finding at/);
});

test("the script exits 2 with a one-line reason while the lock is held", () => {
  const { root, state } = repo([proposal("k-1")]);
  const script = fileURLToPath(new URL("workspace.mjs", scripts));
  const call = (op, input) =>
    spawnSync(process.execPath, [script, op], {
      cwd: root,
      input: JSON.stringify(input),
      encoding: "utf8",
    });
  const listed = call("recommendations", {});
  assert.equal(listed.status, 0, listed.stderr);
  assert.equal(JSON.parse(listed.stdout).items[0].ref, "proposal:k-1");
  const { revision } = JSON.parse(listed.stdout).items[0];
  const other = openStore(state);
  other.lock();
  try {
    for (const [op, input] of [
      ["dismiss", { cluster: "k-1", revision, reason: "wrong" }],
      ["hide", { path: "docs/memory/agent-findings/x.md" }],
    ]) {
      const r = call(op, input);
      assert.equal(r.status, 2, r.stderr);
      assert.match(r.stderr.trim().split("\n").at(-1), /^mining in progress/);
      assert.equal(r.stdout, "");
    }
  } finally {
    other.unlock();
  }
  assert.equal(call("nope", {}).status, 2);
});

test("health adds refs and the last mine run's spend and stop; shards-check is check's result", () => {
  const { state, ws } = repo([proposal("k-1")]);
  for (const [id, m] of [
    ["2026-10-01T000000Z-a", { id: "2026-10-01T000000Z-a", status: "done", spend: { usd: 1.5 } }],
    [
      "2026-10-08T000000Z-b",
      {
        id: "2026-10-08T000000Z-b",
        status: "stopped",
        spend: { usd: 10 },
        stoppedReason: "budget",
        stoppedMessage: "spent $10 of $10",
      },
    ],
  ]) {
    mkdirSync(join(state, "runs", id), { recursive: true });
    writeFileSync(join(state, "runs", id, "manifest.json"), JSON.stringify(m));
  }
  const h = ws("health");
  assert.deepEqual(
    h.open.map((x) => x.ref),
    ["proposal:k-1"],
  );
  assert.deepEqual(
    [h.lastRun.run, h.lastRun.spend, h.lastRun.stoppedReason],
    ["2026-10-08T000000Z-b", { usd: 10 }, "budget"],
  );
  assert.deepEqual(ws("shards-check"), []);
});

test("health and CLI report identify their store and show unfinished verification without recommendations", () => {
  const { root, state, ws } = repo([]);
  const store = openStore(state);
  store.write("pool/clusters.json", {
    "k-held": { id: "k-held", members: ["c1"], families: ["s1"], tier: "C", statement: "An unverified workflow claim", worth: 0.8, checkability: 0.3 },
    "k-rejected": { id: "k-rejected", members: ["c2"], families: ["s2"], verification: { decision: "reject", rejectReason: "already_documented", run: "r1", families: 1 } },
    "k-settled": { id: "k-settled", members: ["c3"], families: ["s3"] },
  });
  store.write("reviewed.json", { "k-settled": { action: "dismissed" } });
  store.write("runs/r1/manifest.json", { id: "r1", status: "done", bindings: { verify: { provider: "codex" } }, report: { verified: { planned: 1, proposed: 0, rejected: { already_documented: 1 }, contractProblems: 0 }, held: { tierC: 1, tierBOverBudget: 0 }, proposals: 0 } });
  assert.deepEqual(ws("recommendations").items, []);
  const health = ws("health");
  assert.deepEqual({ path: health.store.path, mode: health.store.mode }, { path: state, mode: "isolated" });
  assert.ok(isAbsolute(health.store.root));
  const expectedRoot = statSync(root, { bigint: true });
  const reportedRoot = statSync(health.store.root, { bigint: true });
  assert.deepEqual([reportedRoot.dev, reportedRoot.ino], [expectedRoot.dev, expectedRoot.ino], "the reported root identifies this checkout despite path aliases");
  assert.equal(health.lastRun.status, "done", "retain the historical run outcome");
  assert.deepEqual(health.lastRun.report.held, { tierC: 1, tierBOverBudget: 0 });
  assert.deepEqual([health.verification.pending, health.verification.complete], [1, false]);
  assert.deepEqual(health.verification.rejected, { already_documented: 1 });
  assert.match(health.verification.next.command, /--harness codex --budget <USD> --state/);
  assert.ok(health.verification.next.command.includes(state));
  assert.deepEqual(report({ root, state }).mining, { store: health.store, lastRun: health.lastRun, verification: health.verification });
  assert.ok(!JSON.stringify(health).includes("An unverified workflow claim"));
  store.write("pool/clusters.json", {});
  const finished = ws("health");
  assert.deepEqual([finished.verification.pending, finished.verification.complete, finished.verification.next], [0, true, null]);
});
