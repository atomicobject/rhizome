// Review, lookup, and maintenance of context shards, against a scratch Git
// repository. Every proposal and person below is synthetic.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

const scripts = new URL("../../templates/starters/session-memory/agents/skills/session-memory/scripts/", import.meta.url);
const { globRegExp, loadShards, parseShard, approvedHash } = await import(new URL("lib/shards.mjs", scripts));
const { run } = await import(new URL("shards.mjs", scripts));
const { lookup } = await import(new URL("lookup.mjs", scripts));

const user = { found: true, typeName: "Person", path: "docs/people/pat-example.md", title: "Pat Example" };
const git = (root, ...args) => execFileSync("git", args, { cwd: root, encoding: "utf8" });

function repo(proposals) {
  const root = mkdtempSync(join(tmpdir(), "sm-shards-"));
  git(root, "init", "-q");
  git(root, "config", "user.email", "test@example.com");
  git(root, "config", "user.name", "Test");
  mkdirSync(join(root, "pkg/store"), { recursive: true });
  mkdirSync(join(root, "docs"), { recursive: true });
  writeFileSync(join(root, "pkg/store/db.go"), "package store\n");
  writeFileSync(join(root, "docs/store.md"), "# Store\n");
  git(root, "add", "-A");
  git(root, "commit", "-qm", "init");
  const state = join(root, ".rhizome/session-memory");
  mkdirSync(state, { recursive: true });
  // Verification read the files at the initial commit.
  const commit = git(root, "rev-parse", "HEAD").trim();
  writeFileSync(join(state, "proposals.json"), JSON.stringify(proposals.map((p) => ({ verified: { commit }, ...p }))));
  const sm = (...argv) => run(argv, { root, state, user });
  return { root, state, sm };
}

const proposal = (cluster, over = {}) => ({
  cluster,
  tier: "A",
  families: 2,
  kind: "rule",
  statement: `Busy timeout ${cluster}: a value of 0 means the 30-second default.`,
  why: "Callers expecting no wait block for 30 seconds.",
  appliesTo: ["pkg/store/**/*.go"],
  commands: [],
  verification: [{ path: "pkg/store/db.go", checked: "The default is applied when the value is 0." }],
  evidence: [{ episode: "claude:s1:4", harness: "claude", session: "s1", ts: "2026-10-01T10:00:00Z", lens: "discovery", origin: "agent", toolCalls: 7 }],
  ...over,
});

test("the pattern matcher agrees with the shared doublestar case table", () => {
  const cases = JSON.parse(readFileSync(new URL("patterns.json", import.meta.url), "utf8"));
  for (const [pattern, path, want] of cases) assert.equal(globRegExp(pattern).test(path), want, `${pattern} vs ${path}`);
});

test("approving writes a current shard that lookup returns and review no longer lists", () => {
  const { root, sm } = repo([proposal("k-1")]);
  assert.throws(() => sm("approve", "k-1"), /fit to publish/);
  const { approved } = sm("approve", "k-1", "--confirm-publish");
  assert.match(approved, /^docs\/context-shards\/rule-busy-timeout-k-1/);
  const shard = parseShard(readFileSync(join(root, approved), "utf8"), approved);
  assert.equal(shard.reviewedBy, "[[pat-example]]");
  assert.equal(shard.approvedHash, approvedHash(shard));
  assert.deepEqual(shard.anchors, ["pkg/store/**/*.go"]);
  assert.ok(shard.verifiedFingerprint);
  assert.deepEqual(sm("list"), []);
  git(root, "add", "-A");
  git(root, "commit", "-qm", "approve");
  const hit = lookup({ root, files: [join(root, "pkg/store/db.go")] });
  assert.deepEqual(hit.shards.map((s) => s.path), [approved]);
  assert.deepEqual(lookup({ root, files: ["docs/store.md"] }).shards, []);
  assert.deepEqual(lookup({ root, files: ["/other/.t3/worktrees/proj/branch/pkg/store/db.go"] }).shards, []);

  // A Windows checkout converts the shard to CRLF line endings.
  writeFileSync(join(root, approved), readFileSync(join(root, approved), "utf8").replace(/\n/g, "\r\n"));
  assert.equal(loadShards(root)[0]?.path, approved);
});

test("approval asks for a re-check when files changed after verification read them", () => {
  const { root, sm } = repo([proposal("k-1"), proposal("k-2", { verified: undefined })]);
  writeFileSync(join(root, "pkg/store/db.go"), "package store\n\nconst Default = 30\n");
  git(root, "commit", "-qam", "change store");
  assert.throws(() => sm("approve", "k-1", "--confirm-publish"), /changed since verification read them: pkg\/store\/db.go/);
  assert.ok(sm("approve", "k-1", "--confirm-publish", "--rechecked").approved);
  assert.throws(() => sm("approve", "k-2", "--confirm-publish"), /re-check the statement/);
});

test("a checked file deleted after verification needs a re-check", () => {
  const { root, sm } = repo([proposal("k-1", { verification: [{ path: "pkg/store/db.go", checked: "Default applied." }, { path: "docs/store.md", checked: "Documents it." }] })]);
  git(root, "rm", "-q", "docs/store.md");
  git(root, "commit", "-qm", "drop doc");
  assert.throws(() => sm("approve", "k-1", "--confirm-publish"), /changed since verification read them: docs\/store.md/);
});

test("a shard on docs/** is not made stale by committing itself", () => {
  const { root, sm } = repo([proposal("k-docs", { appliesTo: ["docs/**"], verification: [] })]);
  const { approved } = sm("approve", "k-docs", "--confirm-publish");
  git(root, "add", "-A");
  git(root, "commit", "-qm", "approve");
  assert.deepEqual(lookup({ root, files: ["docs/store.md"] }).shards.map((s) => s.path), [approved]);
});

test("a change refuses while another review holds the lock", async () => {
  const { openStore } = await import(new URL("lib/store.mjs", scripts));
  const { state, sm } = repo([proposal("k-1")]);
  const other = openStore(state);
  other.lock();
  assert.throws(() => sm("dismiss", "k-1", "--reason", "covered elsewhere", "--local"), /is locked by pid/);
  assert.equal(sm("list").length, 1);
  other.unlock();
  assert.ok(sm("dismiss", "k-1", "--reason", "covered elsewhere", "--local").dismissed);
});

test("approval refuses a scope that names no tracked file", () => {
  const { root, sm } = repo([proposal("k-1")]);
  const edit = join(root, "..", `edit-${Date.now()}.json`);
  writeFileSync(edit, JSON.stringify({ appliesTo: ["pkg/stor/**/*.go"] }));
  assert.throws(() => sm("approve", "k-1", "--confirm-publish", "--edit", edit), /no tracked file matches pkg\/stor/);
  writeFileSync(edit, JSON.stringify({ kind: "where_to_find", target: "docs/missing.md" }));
  assert.throws(() => sm("approve", "k-1", "--confirm-publish", "--edit", edit), /target docs\/missing.md is not a tracked file/);
  assert.deepEqual(sm("list").map((p) => p.cluster), ["k-1"]);
});

test("approval is refused over uncommitted files and credential-shaped text", () => {
  const secret = ["ghp", "a1B2".repeat(9)].join("_");
  const { root, sm } = repo([
    proposal("k-1"),
    proposal("k-2", { statement: `Use token ${secret} for the store tests.` }),
    proposal("k-3", { why: "Tests pass {'password': 'synthetic-secret-value'} to the store." }),
  ]);
  writeFileSync(join(root, "pkg/store/db.go"), "package store // edited\n");
  assert.throws(() => sm("approve", "k-1", "--confirm-publish"), /uncommitted: pkg\/store\/db.go/);
  assert.throws(() => sm("approve", "k-2", "--confirm-publish"), /credential-shaped/);
  assert.throws(() => sm("approve", "k-3", "--confirm-publish"), /credential-shaped/);
  assert.equal(existsSync(join(root, "docs/context-shards")), false);
});

test("every field written to the repository is scanned, including verification notes and dismissal reasons", () => {
  const secret = ["ghp", "a1B2".repeat(9)].join("_");
  const { root, sm } = repo([proposal("k-1", { verification: [{ path: "pkg/store/db.go", checked: `Configured with ${secret}.` }] }), proposal("k-2")]);
  const { approved } = sm("approve", "k-1", "--confirm-publish");
  assert.ok(!readFileSync(join(root, approved), "utf8").includes(secret));
  const yaml = repo([proposal("k-3", { verification: [{ path: "pkg/store/db.go", checked: "Config:\nprivate_key: |\n  first\n\n  after-blank-secret\nnext: ok" }] })]);
  const published = yaml.sm("approve", "k-3", "--confirm-publish").approved;
  assert.ok(!readFileSync(join(yaml.root, published), "utf8").includes("after-blank-secret"));
  assert.throws(() => sm("dismiss", "k-2", "--reason", `rotated ${secret}`, "--shared"), /credential-shaped/);
  assert.equal(existsSync(join(root, "docs/context-shards/decisions")), false);
});

test("a hand edit or a changed file withholds the shard until it is revised or renewed", () => {
  const { root, sm } = repo([proposal("k-1")]);
  const { approved } = sm("approve", "k-1", "--confirm-publish");
  git(root, "add", "-A");
  git(root, "commit", "-qm", "approve");

  writeFileSync(join(root, "pkg/store/db.go"), "package store\n\nconst Default = 30\n");
  git(root, "commit", "-qam", "change store");
  assert.deepEqual(lookup({ root, files: ["pkg/store/db.go"] }).withheld, [{ path: approved, reason: "files it depends on changed since verification" }]);
  const [listed] = sm("check");
  assert.equal(listed.path, approved);
  assert.throws(() => sm("renew", approved, "--expect", "stale"), /changed since you read it/);
  sm("renew", approved, "--expect", listed.expect);
  assert.equal(lookup({ root, files: ["pkg/store/db.go"] }).shards.length, 1);

  const text = readFileSync(join(root, approved), "utf8");
  writeFileSync(join(root, approved), text.replace("30-second default", "60-second default"));
  assert.match(lookup({ root, files: ["pkg/store/db.go"] }).withheld[0].reason, /approved hash/);
  const [edited] = sm("check");
  assert.throws(() => sm("renew", approved, "--expect", edited.expect), /revise it/);
  const edit = join(root, "edit.json");
  writeFileSync(edit, JSON.stringify({ statement: "A busy timeout of 0 means the 60-second default." }));
  git(root, "add", "-A");
  git(root, "commit", "-qm", "hand edit");
  sm("revise", approved, "--expect", edited.expect, "--edit", edit, "--confirm-publish");
  const [current] = lookup({ root, files: ["pkg/store/db.go"] }).shards;
  assert.equal(current.statement, "A busy timeout of 0 means the 60-second default.");
});

test("approval asks for narrower patterns and warns when too many shards apply everywhere", () => {
  const global = Array.from({ length: 6 }, (_, k) => proposal(`g-${k}`, { statement: `Global fact ${k}.`, appliesTo: [], verification: [] }));
  const { root, sm } = repo([proposal("k-wide", { appliesTo: ["pkg/**"] }), ...global]);
  for (let k = 0; k < 60; k++) writeFileSync(join(root, `pkg/store/f${k}.go`), "package store\n");
  git(root, "add", "-A");
  git(root, "commit", "-qm", "more files");
  // The 60 files were added after verification, so each approval also confirms a re-check.
  assert.throws(() => sm("approve", "k-wide", "--confirm-publish", "--rechecked"), /match 61 of \d+ tracked files/);
  assert.ok(sm("approve", "k-wide", "--confirm-publish", "--broad", "--rechecked").approved);
  const warnings = global.map((p) => sm("approve", p.cluster, "--confirm-publish").warning);
  assert.deepEqual(warnings.slice(0, 5), [undefined, undefined, undefined, undefined, undefined]);
  assert.match(warnings[5], /^6 shards now apply everywhere/);
});

test("dismissing records a decision that suppresses the learning; retiring removes the note", () => {
  const { root, state, sm } = repo([proposal("k-1"), proposal("k-2")]);
  assert.throws(() => sm("dismiss", "k-1", "--reason", "covered elsewhere"), /--shared/);
  const { decision } = sm("dismiss", "k-1", "--reason", "covered elsewhere", "--shared");
  assert.match(decision, /^docs\/context-shards\/decisions\/[0-9a-f]{16}--\d{14}-pat-example\.json$/);
  assert.equal(JSON.parse(readFileSync(join(root, decision), "utf8")).decision, "dismissed");
  assert.deepEqual(sm("list").map((p) => p.cluster), ["k-2"]);

  // A teammate's later run proposes the same learning under another cluster.
  writeFileSync(join(state, "proposals.json"), JSON.stringify([proposal("k-9", { statement: proposal("k-1").statement })]));
  assert.deepEqual(sm("list"), []);

  writeFileSync(join(state, "proposals.json"), JSON.stringify([proposal("k-2")]));
  const { approved } = sm("approve", "k-2", "--confirm-publish", "--rechecked");
  const [s] = loadShards(root);
  assert.throws(() => sm("retire", approved, "--expect", s.contentHash), /--reason/);
  sm("retire", approved, "--expect", s.contentHash, "--reason", "the store was removed");
  assert.equal(existsSync(join(root, approved)), false);
  assert.equal(readdirSync(join(root, "docs/context-shards/decisions")).length, 2);
});

test("lookup returns rules first, at most five, and shards with no patterns everywhere", () => {
  const many = Array.from({ length: 7 }, (_, k) => proposal(`k-${k}`, { kind: k < 3 ? "fact" : "rule" }));
  const { root, sm } = repo([...many, proposal("k-all", { kind: "fact", statement: "Every package logs through the store logger.", appliesTo: [], verification: [] })]);
  for (const p of [...many, { cluster: "k-all" }]) sm("approve", p.cluster, "--confirm-publish");
  git(root, "add", "-A");
  git(root, "commit", "-qm", "approve");
  const hit = lookup({ root, files: ["pkg/store/db.go"] });
  assert.equal(hit.shards.length, 5);
  assert.equal(hit.omitted, 3);
  assert.deepEqual(hit.shards.map((s) => s.kind), ["rule", "rule", "rule", "rule", "fact"]);
  assert.deepEqual(lookup({ root, files: ["docs/store.md"] }).shards.map((s) => s.statement), ["Every package logs through the store logger."]);
  const viaCommand = lookup({ root, command: "cd pkg && sed -n 1,5p store/db.go" });
  assert.equal(viaCommand.shards.length, 5);
});
