// Response routing (SPEC-0124 US2), agent findings, and the friction report
// (US7), against scratch Git repositories. Every proposal, person, and
// credential below is synthetic.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, symlinkSync, utimesSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { fakeClients, fakeDiscover, SESSIONS, session, tempRepo, turn } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/session-memory/agents/skills/session-memory/scripts/", import.meta.url);
const { run } = await import(new URL("shards.mjs", scripts));
const { excerpts, mine } = await import(new URL("mine.mjs", scripts));
const { report } = await import(new URL("report.mjs", scripts));
const { frontmatterProblem, guardHash, loadFindings } = await import(new URL("lib/findings.mjs", scripts));
const { contentHash, landPair, learningHash } = await import(new URL("lib/shards.mjs", scripts));
const { guardRuleProblems, responseProblems, verifyCluster } = await import(new URL("lib/verify.mjs", scripts));

const user = { found: true, typeName: "Person", path: "docs/people/pat-example.md", title: "Pat Example" };
// Sam resolves by title or alias; a Topic note is not a Person.
const people = (root, ref) => (["Sam Example", "sam"].includes(ref) ? { found: true, typeName: "Person", path: "docs/people/Sam Example.md", title: "Sam Example" } : ref === "Store" ? { found: true, typeName: "Topic", path: "docs/store.md", title: "Store" } : { found: false });
// tempRepo sets no repository identity, and CI runners have no global one.
const git = (root, args, date) => execFileSync("git", ["-c", "user.email=test@example.com", "-c", "user.name=Test", ...args], { cwd: root, encoding: "utf8", env: { ...process.env, ...(date ? { GIT_AUTHOR_DATE: date, GIT_COMMITTER_DATE: date } : {}) } });
const secret = ["ghp", "a1B2".repeat(9)].join("_");

function repo(proposals) {
  const root = mkdtempSync(join(tmpdir(), "sm-route-"));
  git(root, ["init", "-q"]);
  mkdirSync(join(root, "pkg/store"), { recursive: true });
  mkdirSync(join(root, "docs"), { recursive: true });
  writeFileSync(join(root, "pkg/store/db.go"), "package store\n");
  writeFileSync(join(root, "docs/store.md"), "# Store\n\nThe store wraps SQLite.\n");
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", "init"]);
  const state = join(root, ".rhizome/session-memory");
  mkdirSync(state, { recursive: true });
  const sm = (...argv) => run(argv, { root, state, user, people });
  // As verification would: record HEAD, and the document a drafted edit read.
  const head = git(root, ["rev-parse", "HEAD"]).trim();
  const read = (doc) => (existsSync(join(root, doc)) ? contentHash(readFileSync(join(root, doc), "utf8")) : undefined);
  const propose = (list) => writeFileSync(join(state, "proposals.json"), JSON.stringify(list.map((p) => ({ ...p, verified: { ...p.verified, commit: p.verified?.commit === null ? head : p.verified?.commit }, ...(p.edit && !("documentHash" in p) ? { documentHash: read(p.edit.document) } : {}) }))));
  propose(proposals);
  return { root, state, sm, propose };
}

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
  verification: [{ path: "pkg/store/db.go", checked: "The default is applied when the value is 0." }],
  evidence: [{ episode: "claude:s1:4", harness: "claude", session: "s1", ts: "2026-10-01T10:00:00Z", lens: "discovery", origin: "agent", toolCalls: 7 }],
  verified: { commit: null },
  ...over,
});

const documentEdit = { document: "docs/store.md", old: "The store wraps SQLite.\n", new: "The store wraps SQLite.\nA busy timeout of 0 means the 30-second default.\n" };
const decisions = (root) => (existsSync(join(root, "docs/memory/context-shards/decisions")) ? readdirSync(join(root, "docs/memory/context-shards/decisions")).map((f) => JSON.parse(readFileSync(join(root, "docs/memory/context-shards/decisions", f), "utf8"))) : []);

test("approving a document response applies the drafted edit and records a documented decision", () => {
  const p = proposal("k-doc", { response: "document", edit: documentEdit });
  const { root, sm, propose } = repo([p]);
  assert.deepEqual(sm("list").map((x) => [x.response, x.responseReason]), [["document", "A deliberate default."]]);
  const r = sm("approve", "k-doc", "--confirm-publish");
  assert.equal(r.documented, "docs/store.md");
  assert.equal(readFileSync(join(root, "docs/store.md"), "utf8"), "# Store\n\nThe store wraps SQLite.\nA busy timeout of 0 means the 30-second default.\n");
  // An ordinary working-tree change, left for the person to commit.
  assert.match(git(root, ["status", "--porcelain", "--", "docs/store.md"]), /^ M docs\/store.md/);
  const [d] = decisions(root);
  assert.deepEqual([d.decision, d.document, d.learning], ["documented", "docs/store.md", learningHash(p)]);
  assert.equal(existsSync(join(root, "docs/memory/context-shards")) && readdirSync(join(root, "docs/memory/context-shards")).filter((f) => f.endsWith(".md")).length, 0);
  // Never proposed again, here or from a teammate's run.
  propose([proposal("k-other", { statement: p.statement })]);
  assert.deepEqual(sm("list"), []);
});

test("a drafted edit that no longer applies, or that adds a credential, is refused", () => {
  const { root, sm } = repo([
    proposal("k-stale", { response: "document", edit: documentEdit }),
    proposal("k-out", { response: "document", edit: { ...documentEdit, document: "../outside.md" } }),
    proposal("k-cred", { response: "document", edit: { ...documentEdit, new: `The store wraps SQLite with ${secret}.\n` } }),
  ]);
  writeFileSync(join(root, "docs/store.md"), "# Store\n\nThe store wraps SQLite and DuckDB.\n");
  git(root, ["commit", "-qam", "edit store doc"]);
  assert.throws(() => sm("approve", "k-stale", "--confirm-publish"), /files changed since verification read them: docs\/store.md/);
  assert.throws(() => sm("approve", "k-stale", "--confirm-publish", "--rechecked"), /no longer applies: edit.old occurs 0 times in docs\/store.md/);
  assert.throws(() => sm("approve", "k-out", "--confirm-publish", "--rechecked"), /no longer applies: edit.document \.\.\/outside.md is not a tracked file/);
  writeFileSync(join(root, "docs/store.md"), "# Store\n\nThe store wraps SQLite.\n");
  git(root, ["commit", "-qam", "restore store doc"]);
  assert.throws(() => sm("approve", "k-cred", "--confirm-publish", "--rechecked"), /credential-shaped/);
  assert.equal(readFileSync(join(root, "docs/store.md"), "utf8"), "# Store\n\nThe store wraps SQLite.\n");
  assert.deepEqual(decisions(root), []);
  assert.equal(sm("list").length, 3);
});

test("a document edit publishes the whole document, so no credential-shaped value reaches it", () => {
  const other = ["ghp", "z9Y8".repeat(9)].join("_");
  const { root, sm } = repo([
    proposal("k-swap", { response: "document", edit: { document: "docs/store.md", old: `token: ${secret}\n`, new: `token: ${other}\n` } }),
    proposal("k-held", { response: "document", edit: documentEdit }),
  ]);
  const doc = `# Store\n\nThe store wraps SQLite.\ntoken: ${secret}\n`;
  writeFileSync(join(root, "docs/store.md"), doc);
  git(root, ["commit", "-qam", "a doc holding a credential"]);
  // Swapping one value for another adds none, but would still publish one.
  assert.throws(() => sm("approve", "k-swap", "--confirm-publish", "--rechecked"), /line 4 of docs\/store.md already holds a credential-shaped value/);
  // An edit elsewhere would republish the value already there.
  assert.throws(() => sm("approve", "k-held", "--confirm-publish", "--rechecked"), /line 4 of docs\/store.md already holds a credential-shaped value/);
  assert.equal(readFileSync(join(root, "docs/store.md"), "utf8"), doc);
  assert.deepEqual(decisions(root), []);
});

test("a tracked write through a symlinked directory is refused", { skip: process.platform === "win32" && "symlinks need privileges on Windows" }, () => {
  const { root, sm } = repo([
    proposal("k-doc", { response: "document", edit: { document: "ext/notes.md", old: "Old text.\n", new: "New text.\n" } }),
    proposal("k-fix", { response: "fix", change: "Delete the dead fallback." }),
  ]);
  mkdirSync(join(root, "ext"));
  writeFileSync(join(root, "ext/notes.md"), "Old text.\n");
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", "ext"]);
  // The tracked path now leads outside the checkout through a symlinked parent.
  const outside = mkdtempSync(join(tmpdir(), "sm-outside-"));
  writeFileSync(join(outside, "notes.md"), "Old text.\n");
  rmSync(join(root, "ext"), { recursive: true });
  symlinkSync(outside, join(root, "ext"), "dir");
  // Hidden from status, so only containment stands between the edit and the outside file.
  git(root, ["update-index", "--assume-unchanged", "ext/notes.md"]);
  assert.throws(() => sm("approve", "k-doc", "--confirm-publish", "--rechecked"), /ext\/notes.md resolves outside the checkout/);
  assert.equal(readFileSync(join(outside, "notes.md"), "utf8"), "Old text.\n");
  // A decisions directory outside the checkout refuses before the document is edited.
  rmSync(join(root, "ext"));
  git(root, ["update-index", "--no-assume-unchanged", "ext/notes.md"]);
  git(root, ["checkout", "--", "ext"]);
  mkdirSync(join(root, "docs/memory/context-shards"), { recursive: true });
  symlinkSync(outside, join(root, "docs/memory/context-shards/decisions"), "dir");
  assert.throws(() => sm("approve", "k-doc", "--confirm-publish", "--rechecked"), /decisions\/.*resolves outside the checkout/);
  assert.equal(readFileSync(join(root, "ext/notes.md"), "utf8"), "Old text.\n");
  // A symlinked findings directory receives nothing.
  mkdirSync(join(root, "docs/memory"), { recursive: true });
  symlinkSync(outside, join(root, "docs/memory/agent-findings"), "dir");
  assert.throws(() => sm("approve", "k-fix", "--confirm-publish", "--rechecked"), /resolves outside the checkout/);
  assert.deepEqual(readdirSync(outside), ["notes.md"]);
  assert.equal(sm("list").length, 2);
});

test("every response is refused when what verification read is uncommitted or has changed", () => {
  const { root, sm } = repo([proposal("k-fix", { response: "fix", change: "Delete the dead fallback." }), proposal("k-doc", { response: "document", edit: documentEdit })]);
  writeFileSync(join(root, "pkg/store/db.go"), "package store\n\n// edited\n");
  assert.throws(() => sm("approve", "k-fix", "--confirm-publish"), /commit or discard changes first; uncommitted: pkg\/store\/db.go/);
  git(root, ["commit", "-qam", "change the code"]);
  assert.throws(() => sm("approve", "k-fix", "--confirm-publish"), /files changed since verification read them: pkg\/store\/db.go; re-check the proposal/);
  // The document being edited counts, even outside the proposal's patterns.
  writeFileSync(join(root, "docs/store.md"), "# Store\n\nThe store wraps SQLite.\n\nMore.\n");
  assert.throws(() => sm("approve", "k-doc", "--confirm-publish"), /uncommitted: docs\/store.md/);
  git(root, ["commit", "-qam", "change the doc"]);
  assert.throws(() => sm("approve", "k-doc", "--confirm-publish"), /files changed since verification read them: .*docs\/store.md/);
  assert.equal(existsSync(join(root, "docs/memory")), false);
  assert.ok(sm("approve", "k-fix", "--confirm-publish", "--rechecked").finding);
});

test("a staged removal of a checked file, or a checked file that is missing, blocks approval", () => {
  const { root, sm } = repo([proposal("k-fix", { response: "fix", change: "Delete the dead fallback." }), proposal("k-gone", { response: "fix", change: "Delete it.", verification: [{ path: "pkg/store/gone.go", checked: "Read it." }] })]);
  git(root, ["rm", "-q", "--cached", "pkg/store/db.go"]);
  assert.throws(() => sm("approve", "k-fix", "--confirm-publish", "--rechecked"), /commit or discard changes first; uncommitted: pkg\/store\/db.go/);
  git(root, ["reset", "-q"]);
  git(root, ["rm", "-q", "pkg/store/db.go"]);
  assert.throws(() => sm("approve", "k-fix", "--confirm-publish", "--rechecked"), /uncommitted: pkg\/store\/db.go/);
  git(root, ["reset", "-q", "--hard"]);
  assert.throws(() => sm("approve", "k-gone", "--confirm-publish"), /files changed since verification read them: pkg\/store\/gone.go/);
  assert.equal(existsSync(join(root, "docs/memory")), false);
});

test("a removed file the patterns matched blocks approval, staged or committed since verification", () => {
  const { root, sm, propose } = repo([]);
  writeFileSync(join(root, "pkg/store/extra.go"), "package store\n");
  git(root, ["add", "pkg/store/extra.go"]);
  git(root, ["commit", "-qm", "extra"]);
  const verified = { commit: git(root, ["rev-parse", "HEAD"]).trim() };
  propose([proposal("k-fix", { response: "fix", change: "Delete the dead fallback.", appliesTo: ["pkg/store/*.go"], verified })]);
  git(root, ["rm", "-q", "pkg/store/extra.go"]);
  assert.throws(() => sm("approve", "k-fix", "--confirm-publish", "--rechecked"), /uncommitted: pkg\/store\/extra.go/);
  git(root, ["reset", "-q", "--hard"]);
  rmSync(join(root, "pkg/store/extra.go"));
  assert.throws(() => sm("approve", "k-fix", "--confirm-publish", "--rechecked"), /uncommitted: pkg\/store\/extra.go/);
  git(root, ["commit", "-qam", "remove extra"]);
  assert.throws(() => sm("approve", "k-fix", "--confirm-publish"), /files changed since verification read them: pkg\/store\/extra.go/);
  assert.equal(existsSync(join(root, "docs/memory")), false);
});

test("a decision that cannot be written leaves the document unchanged", { skip: process.platform === "win32" || process.getuid?.() === 0 }, () => {
  const { root, sm } = repo([proposal("k-doc", { response: "document", edit: documentEdit })]);
  const dir = join(root, "docs/memory/context-shards/decisions");
  mkdirSync(dir, { recursive: true });
  chmodSync(dir, 0o555);
  try {
    assert.throws(() => sm("approve", "k-doc", "--confirm-publish"), /could not write docs\/store.md and its decision \(EACCES\); nothing was changed/);
  } finally {
    chmodSync(dir, 0o755);
  }
  assert.equal(readFileSync(join(root, "docs/store.md"), "utf8"), "# Store\n\nThe store wraps SQLite.\n");
  assert.deepEqual(readdirSync(dir), []);
  assert.deepEqual(readdirSync(join(root, "docs")).filter((f) => f.endsWith(".tmp")), []);
  assert.equal(sm("list").length, 1);
});

test("document approval is bound to the document verification read", () => {
  const { root, state, sm } = repo([proposal("k-doc", { response: "document", edit: documentEdit, documentHash: contentHash("# Store\n\nAn older intro.\nThe store wraps SQLite.\n") })]);
  // The old text still occurs once, but the text around it is not what was verified.
  assert.throws(() => sm("approve", "k-doc", "--confirm-publish"), /docs\/store.md changed since verification read it; show the person the edit in the context of the current document, then pass --rechecked/);
  assert.equal(readFileSync(join(root, "docs/store.md"), "utf8"), "# Store\n\nThe store wraps SQLite.\n");
  assert.equal(sm("approve", "k-doc", "--confirm-publish", "--rechecked").documented, "docs/store.md");
  assert.equal(JSON.parse(readFileSync(join(state, "reviewed.json"), "utf8"))["k-doc"].documentHash, contentHash("# Store\n\nThe store wraps SQLite.\n"));
});

test("a CRLF document verifies and applies its edit, keeping its line endings", async () => {
  const root = tempRepo();
  writeFileSync(join(root, "README.md"), "# Fixture\r\n\r\nThe loader reads once.\r\n");
  git(root, ["-c", "core.autocrlf=false", "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qam", "crlf"]);
  const edit = { document: "README.md", old: "The loader reads once.\n", new: "The loader reads once.\nRestart after edits.\n" };
  const answer = { decision: "propose", kind: "fact", statement: "Topic x: `README.md` states a fact.", why: "w", appliesTo: [], commands: [], documentation: "", verification: [{ path: "README.md", checked: "read it" }], notes: "", response: "document", responseReason: "Unwritten.", edit };
  const cl = { id: "k-x", statement: answer.statement, kind: "fact", appliesTo: [], commands: [] };
  const v = await verifyCluster({ generate: async () => answer }, { root, search: async () => "" }, { cl, variants: [], sources: [] });
  assert.deepEqual(v.contractProblems, []);
  assert.equal(v.result.documentHash, contentHash(readFileSync(join(root, "README.md"), "utf8")));
  const state = mkdtempSync(join(tmpdir(), "sm-state-"));
  writeFileSync(join(state, "proposals.json"), JSON.stringify([{ ...proposal("k-x"), ...v.result, verified: { commit: git(root, ["rev-parse", "HEAD"]).trim() } }]));
  assert.equal(run(["approve", "k-x", "--confirm-publish"], { root, state, user }).documented, "README.md");
  assert.equal(readFileSync(join(root, "README.md"), "utf8"), "# Fixture\r\n\r\nThe loader reads once.\r\nRestart after edits.\r\n");
});

test("document approval is bound to the text the verifier was shown, not a later read", async () => {
  const root = tempRepo();
  const committed = "# Fixture\n\nThe loader reads once.\n";
  writeFileSync(join(root, "README.md"), committed);
  git(root, ["-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qam", "readme"]);
  // An editor's temporary save is what the packet reads; the committed text is back before the answer.
  const temporary = "# Fixture\n\nA temporary line.\nThe loader reads once.\n";
  writeFileSync(join(root, "README.md"), temporary);
  const edit = { document: "README.md", old: "The loader reads once.\n", new: "The loader reads once.\nRestart after edits.\n" };
  const answer = { decision: "propose", kind: "fact", statement: "Topic x: `README.md` says the loader reads once.", why: "w", appliesTo: [], commands: [], documentation: "", verification: [{ path: "README.md", checked: "read it" }], notes: "", response: "document", responseReason: "Unwritten.", edit };
  const caller = { generate: async ({ prompt }) => (assert.match(prompt, /A temporary line/), writeFileSync(join(root, "README.md"), committed), answer) };
  const cl = { id: "k-x", statement: answer.statement, kind: "fact", appliesTo: [], commands: [] };
  const v = await verifyCluster(caller, { root, search: async () => "" }, { cl, variants: [], sources: [] });
  assert.equal(v.result.documentHash, contentHash(temporary));
  const state = mkdtempSync(join(tmpdir(), "sm-state-"));
  writeFileSync(join(state, "proposals.json"), JSON.stringify([{ ...proposal("k-x"), ...v.result, verified: { commit: git(root, ["rev-parse", "HEAD"]).trim() } }]));
  assert.throws(() => run(["approve", "k-x", "--confirm-publish"], { root, state, user }), /README.md changed since verification read it/);
  assert.equal(readFileSync(join(root, "README.md"), "utf8"), committed);
});

test("a document the verifier saw only in search results is bound to no hash, so approval asks for a re-check", async () => {
  const root = tempRepo();
  const committed = "# Fixture\n\nThe loader reads once.\n";
  writeFileSync(join(root, "README.md"), committed);
  git(root, ["-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qam", "readme"]);
  // Search shows a temporary save; the committed text is back before the answer.
  writeFileSync(join(root, "README.md"), "# Fixture\n\nA temporary line.\nThe loader reads once.\n");
  const search = async () => readFileSync(join(root, "README.md"), "utf8");
  const edit = { document: "README.md", old: "The loader reads once.\n", new: "The loader reads once.\nRestart after edits.\n" };
  const answer = { decision: "propose", kind: "fact", statement: "Topic x: the loader reads once.", why: "w", appliesTo: [], commands: [], documentation: "", verification: [{ path: "README.md", checked: "read it" }], notes: "", response: "document", responseReason: "Unwritten.", edit };
  const caller = { generate: async ({ prompt }) => (assert.match(prompt, /A temporary line/), writeFileSync(join(root, "README.md"), committed), answer) };
  const cl = { id: "k-x", statement: answer.statement, kind: "fact", appliesTo: [], commands: [] };
  const v = await verifyCluster(caller, { root, search }, { cl, variants: [], sources: [] });
  assert.deepEqual(v.contractProblems, []);
  assert.equal(v.result.documentHash, undefined);
  const state = mkdtempSync(join(tmpdir(), "sm-state-"));
  writeFileSync(join(state, "proposals.json"), JSON.stringify([{ ...proposal("k-x"), ...v.result, verified: { commit: git(root, ["rev-parse", "HEAD"]).trim() } }]));
  assert.throws(() => run(["approve", "k-x", "--confirm-publish"], { root, state, user }), /README.md was not read by verification; show the person the edit in the context of the current document, then pass --rechecked/);
  assert.equal(readFileSync(join(root, "README.md"), "utf8"), committed);
  assert.equal(run(["approve", "k-x", "--confirm-publish", "--rechecked"], { root, state, user }).documented, "README.md");
});

test("only document text that reached the verifier binds approval: not a cut grep hit, not an empty range", async () => {
  const root = tempRepo();
  // Seven files, two long matching lines each: the grep budget runs out inside d.md's first hit.
  const long = `markerq ${"x".repeat(260)}`;
  for (const f of ["a", "b", "c", "d", "e", "f", "g"]) writeFileSync(join(root, `${f}.md`), `${long}\n${long}\n`);
  writeFileSync(join(root, "x.md"), "one\ntwo\n");
  git(root, ["add", "-A"]);
  git(root, ["-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "docs"]);
  const verify = async (document, requests) => {
    const edit = { document, old: document === "x.md" ? "two\n" : `${long}\n${long}\n`, new: "added\n" };
    const answer = { decision: "propose", kind: "fact", statement: "Topic x: `markerq` marks things.", why: "w", appliesTo: [], commands: [], documentation: "", verification: [{ path: document, checked: "read it" }], notes: "", response: "document", responseReason: "Unwritten.", edit };
    let round = 0;
    const caller = { generate: async ({ prompt }) => (round++ === 0 && requests ? (assert.match(prompt, /d\.md:1:m/), { decision: "need_evidence", verification: [], documentation: "", notes: "", requests }) : answer) };
    const cl = { id: "k-x", statement: answer.statement, kind: "fact", appliesTo: [], commands: [] };
    return (await verifyCluster(caller, { root, search: async () => "" }, { cl, variants: [], sources: [] })).result;
  };
  assert.equal((await verify("c.md")).documentHash, contentHash(readFileSync(join(root, "c.md"), "utf8")));
  assert.equal((await verify("d.md")).documentHash, undefined);
  assert.equal((await verify("x.md", [{ kind: "file", value: "x.md", startLine: 999 }])).documentHash, undefined);
});

test("a document is restored before cleanup when its decision cannot land", () => {
  const calls = [];
  const step = (name, fail) => () => {
    calls.push(name);
    if (fail) throw Object.assign(new Error(name), { code: name });
  };
  const doc = { commit: step("doc-commit"), discard: step("doc-discard") };
  const decision = { commit: step("decision-commit", true), discard: step("decision-discard", true) };
  assert.throws(() => landPair(doc, decision, step("restore")), (err) => err.code === "decision-commit" && !err.restoreError);
  assert.deepEqual(calls, ["doc-commit", "decision-commit", "restore", "decision-discard"]);
  calls.length = 0;
  assert.throws(() => landPair(doc, decision, step("restore", true)), (err) => err.restoreError?.code === "restore");
  const first = { commit: step("doc-commit", true), discard: step("doc-discard") };
  calls.length = 0;
  assert.throws(() => landPair(first, { commit: step("decision-commit"), discard: step("decision-discard") }, step("restore")), (err) => err.firstFailed);
  assert.deepEqual(calls, ["doc-commit", "doc-discard", "decision-discard"]);
});

test("approving fix, guard, and ask writes one AgentFinding note each, with one action item", () => {
  const rule = { tool: "shell", command: "go mod vendor", reason: "It deletes hand-vendored C files.", alternative: "make vendor-treesitter" };
  const { root, sm } = repo([
    proposal("k-fix", { response: "fix", change: "Delete the dead fallback in pkg/store/db.go." }),
    proposal("k-guard", { response: "guard", change: "Refuse a bare go mod vendor.", guardRule: rule }),
    proposal("k-ask", { response: "ask", question: "Should 0 keep meaning the default?", owner: "Sam Example" }),
  ]);
  const fix = sm("approve", "k-fix", "--confirm-publish");
  assert.match(fix.finding, /^docs\/memory\/agent-findings\/fix-busy-timeout-k-fix\.md$/);
  const text = readFileSync(join(root, fix.finding), "utf8");
  const p = proposal("k-fix");
  for (const line of ["response: fix", "kind: fact", `summary: ${JSON.stringify(p.statement)}`, `learning-hash: ${learningHash(p)}`, "applies-to:", '  - "pkg/store/**/*.go"', 'reviewed-by: "[[docs/people/pat-example]]"', "families: 2", "tool-calls: 7"]) assert.ok(text.split("\n").includes(line), line);
  assert.match(text, /## Why it matters\n\nCallers expecting no wait block for 30 seconds\.\n\n## Proposed change\n\nDelete the dead fallback in pkg\/store\/db\.go\.\n\n## Evidence\n\n- claude session `s1`, message `claude:s1:4`, 2026-10-01, discovery, 7 tool calls\n/);
  assert.equal(text.match(/#action-item/g).length, 1);
  assert.match(text, /\n- \[ \] Fix: Busy timeout k-fix #action-item\n  assignee:: \[\[docs\/people\/pat-example\]\]\n$/);

  for (const nobody of ["Sam Nobody", "Store"]) assert.throws(() => sm("approve", "k-guard", "--confirm-publish", "--assignee", nobody), new RegExp(`--assignee ${nobody} does not resolve to a Person note`));
  assert.equal(existsSync(join(root, "docs/memory/agent-findings/guard-busy-timeout-k-guard.md")), false);
  const guard = sm("approve", "k-guard", "--confirm-publish", "--assignee", "sam");
  const gtext = readFileSync(join(root, guard.finding), "utf8");
  assert.match(gtext, /\n  assignee:: \[\[docs\/people\/Sam Example\]\]\n$/);
  const ask = sm("approve", "k-ask", "--confirm-publish");
  assert.match(readFileSync(join(root, ask.finding), "utf8"), /## Question\n\nShould 0 keep meaning the default\?\n\nOwner: Sam Example\n/);

  const loaded = Object.fromEntries(loadFindings(root).map((f) => [f.response, f]));
  assert.deepEqual(loaded.guard.guardRule, rule);
  assert.ok(Object.values(loaded).every((f) => f.open && f.completedAt === null));
  assert.equal(existsSync(join(root, "docs/memory/context-shards")), false);
  assert.deepEqual(sm("list"), []);
});

// The hook dispatcher's Go test pins the same value (GuardHash).
test("a guard rule's approved hash is the one the hook dispatcher checks", () => {
  assert.equal(guardHash("guard", { tool: "shell", command: "go mod vendor", reason: "It deletes hand-vendored C files.", alternative: "make vendor" }), "b9a4a97b3a352156");
});

// The hook dispatcher's Go test reads the same table (guard-paths.json), so
// a pattern approval accepts is one the dispatcher matches as the breadth
// check counted it.
test("a guard rule's path uses only the shard pattern dialect", () => {
  const cases = JSON.parse(readFileSync(new URL("guard-paths.json", import.meta.url), "utf8"));
  for (const [path, ok] of cases) assert.equal(guardRuleProblems({ tool: "read", path, reason: "r", alternative: "a" }).length === 0, ok, path);
});

// The hook dispatcher's Go test reads the same table (guard-commands.json),
// so a command approval accepts is one the dispatcher loads.
test("a guard rule's command names a literal command word, as the dispatcher requires", () => {
  const cases = JSON.parse(readFileSync(new URL("guard-commands.json", import.meta.url), "utf8"));
  for (const [command, ok] of cases) assert.equal(guardRuleProblems({ tool: "shell", command, reason: "r", alternative: "a" }).length === 0, ok, command);
});

// The hook dispatcher's YAML parser rejects these, or folds U+0085, U+2028,
// and U+2029 as line breaks, so the rule would never load; its Go test loads
// the others.
test("a guard rule's text uses only characters the dispatcher's YAML parser reads", () => {
  for (const c of ["\u007f", "\u0085", "\u009f", "\u2028", "\u2029", "\ufffe", "\ud800"]) assert.equal(guardRuleProblems({ tool: "shell", command: "make x", reason: `r${c}`, alternative: "a" }).length, 1, escape(c));
  assert.deepEqual(guardRuleProblems({ tool: "shell", command: "make x", reason: "r \t\u0001\u001f\u00a0\u00e9 \u2014 \u{1f600}", alternative: "a" }), []);
});

// The hook dispatcher's Go test reads the same table (guard-frontmatter.json),
// so frontmatter the scripts accept is frontmatter the dispatcher loads. A
// case with its own guard-rule replaces the canonical one.
test("a guard finding's frontmatter is accepted only in the form the scripts write", () => {
  const cases = JSON.parse(readFileSync(new URL("guard-frontmatter.json", import.meta.url), "utf8"));
  const rule = `guard-rule: {"tool":"shell","command":"go mod vendor","reason":"r","alternative":"a"}\n`;
  for (const [lines, accepted, loads] of cases) {
    const front = `response: guard\n${lines}\n${lines.includes("guard-rule:") ? "" : rule}approved-hash: "h"\nreviewed-by: "Sam"`;
    assert.equal(frontmatterProblem(front) === null, accepted, lines);
    assert.ok(!accepted || loads, `the dispatcher loads what the scripts accept: ${lines}`);
  }
});

test("approving a guard rule binds its text, refuses an overbroad or unusable rule, and revise approves an edit again", () => {
  const rule = { tool: "shell", command: "go mod vendor", reason: "It deletes hand-vendored C files.", alternative: "make vendor" };
  const { root, sm, state } = repo([
    proposal("k-guard", { response: "guard", change: "Refuse a bare go mod vendor.", guardRule: { ...rule, note: "dropped" } }),
    proposal("k-glob", { response: "guard", change: "c", guardRule: { ...rule, command: "sudo *" } }),
    proposal("k-long", { response: "guard", change: "c", guardRule: { ...rule, reason: "x".repeat(301) } }),
    proposal("k-wide", { response: "guard", change: "c", guardRule: { tool: "read", path: "gen/**", reason: "Generated.", alternative: "Read the source." } }),
    proposal("k-brace", { response: "guard", change: "c", guardRule: { tool: "read", path: "{gen,src}/**", reason: "Generated.", alternative: "Read the source." } }),
  ]);
  const { finding } = sm("approve", "k-guard", "--confirm-publish");
  const text = readFileSync(join(root, finding), "utf8");
  assert.ok(text.includes(`\nguard-rule: ${JSON.stringify(rule)}\napproved-hash: "${guardHash("guard", rule)}"\nreviewed-by: `), text);

  assert.throws(() => sm("approve", "k-glob", "--confirm-publish"), /guardRule.command must name a literal command word, not a glob such as \*/);
  assert.throws(() => sm("approve", "k-long", "--confirm-publish"), /guardRule reason and alternative must each be at most 300 characters/);
  // A path rule that matches more than 50 tracked files and a tenth of the
  // repository needs --broad, as a shard's patterns do.
  mkdirSync(join(root, "gen"));
  for (let n = 0; n < 60; n++) writeFileSync(join(root, `gen/f${n}.go`), "package gen\n");
  git(root, ["add", "-A", "gen"]);
  git(root, ["commit", "-qm", "gen"]);
  assert.throws(() => sm("approve", "k-wide", "--confirm-publish", "--rechecked"), /the patterns match 60 of 6\d tracked files; .*--broad if the reviewer confirms the rule should refuse calls that widely/);
  assert.ok(sm("approve", "k-wide", "--confirm-publish", "--rechecked", "--broad").finding);
  // The breadth check counts no match for a brace, which the dispatcher
  // would expand, so the pattern is refused outright.
  assert.throws(() => sm("approve", "k-brace", "--confirm-publish", "--rechecked"), /guardRule.path may use only literal names, \*, \*\*, and \?/);

  // A rule edited by hand is listed by check, and revise approves it again.
  assert.deepEqual(sm("check"), []);
  writeFileSync(join(root, finding), text.replace('"alternative":"make vendor"', '"alternative":"anything"'));
  const [stale] = sm("check");
  assert.deepEqual([stale.path, stale.reasons], [finding, ["guard rule edited after approval; revise it"]]);
  const edit = join(state, "..", `guard-edit-${Date.now()}.json`);
  writeFileSync(edit, JSON.stringify({ guardRule: { ...rule, alternative: "make vendor-treesitter" }, statement: "x" }));
  assert.throws(() => sm("revise", finding, "--expect", stale.expect, "--edit", edit, "--confirm-publish"), /--edit may change only guardRule for a guard finding; got statement/);
  writeFileSync(edit, JSON.stringify({ guardRule: { ...rule, alternative: "make vendor-treesitter" } }));
  assert.throws(() => sm("revise", finding, "--expect", "0000", "--edit", edit, "--confirm-publish"), /changed since you read it/);
  assert.deepEqual(sm("revise", finding, "--expect", stale.expect, "--edit", edit, "--confirm-publish").guardRule.alternative, "make vendor-treesitter");
  const revised = readFileSync(join(root, finding), "utf8");
  assert.equal(revised.match(/^guard-rule: /gm).length, 1);
  assert.match(revised, /\napproved-hash: "[0-9a-f]{16}"\nreviewed-by: "\[\[docs\/people\/pat-example\]\]"\nreviewed-on: \d{4}-\d\d-\d\d\nfamilies: 2\n/);
  assert.deepEqual(sm("check"), []);
  assert.equal(revised.split("\n---\n")[1], text.split("\n---\n")[1], "the body is kept");
  // A rule whose reviewer was removed is skipped by the hook, so check lists it.
  writeFileSync(join(root, finding), revised.replace(/\nreviewed-by: [^\n]*/, ""));
  assert.deepEqual(sm("check").map((f) => f.reasons), [["no reviewed-by, so the hook skips the rule; revise it to approve it again"]]);
});

test("a guard rule rewritten as a YAML block is reported by check and replaced whole by revise", () => {
  const rule = { tool: "shell", command: "go mod vendor", reason: "It deletes hand-vendored C files.", alternative: "make vendor" };
  const { root, sm, state } = repo([proposal("k-guard", { response: "guard", change: "Refuse a bare go mod vendor.", guardRule: rule })]);
  const { finding } = sm("approve", "k-guard", "--confirm-publish");
  const text = readFileSync(join(root, finding), "utf8");
  // A hand edit the dispatcher reads as a changed rule and skips.
  const block = text.replace(`guard-rule: ${JSON.stringify(rule)}\n`, "guard-rule:\n  tool: shell\n  command: go mod vendor\n  reason: It deletes hand-vendored C files.\n  alternative: anything\n");
  writeFileSync(join(root, finding), block);
  const [stale] = sm("check");
  assert.deepEqual([stale?.path, stale?.reasons], [finding, ["guard rule unreadable or not the JSON the scripts write; revise it with the rule in --edit"]]);

  const edit = join(state, "..", `guard-edit-${Date.now()}.json`);
  writeFileSync(edit, JSON.stringify({ guardRule: { ...rule, alternative: "make vendor-treesitter" } }));
  sm("revise", finding, "--expect", stale.expect, "--edit", edit, "--confirm-publish");
  const revised = readFileSync(join(root, finding), "utf8");
  const front = revised.split("\n---\n")[0].split("\n").slice(1);
  assert.deepEqual(front.filter((l) => !/^[\w-]+:/.test(l) && !/^  - /.test(l)), [], revised);
  assert.deepEqual(sm("check"), []);

  // Frontmatter revise cannot rewrite safely is refused, and left as it is.
  const broken = revised.replace(/^(summary: .*)$/m, "$1\n  a continuation under a scalar");
  writeFileSync(join(root, finding), broken);
  const [bad] = sm("check");
  assert.match(bad.reasons.join(), /frontmatter the hook cannot read/);
  assert.throws(() => sm("revise", finding, "--expect", bad.expect, "--edit", edit, "--confirm-publish"), /frontmatter the hook cannot read \(line 5 is not a key/);
  assert.equal(readFileSync(join(root, finding), "utf8"), broken);

  // YAML the dispatcher cannot parse, which line-by-line reading would pass:
  // an unclosed quote, and a malformed nested block.
  for (const bad of [
    revised.replace(/^summary: (.*)"$/m, "summary: $1"),
    revised.replace(/^(applies-to:)$/m, "$1\n  foo: bar"),
  ]) {
    writeFileSync(join(root, finding), bad);
    const [reported] = sm("check");
    assert.match(reported?.reasons.join() ?? "", /frontmatter the hook cannot read/, bad);
    assert.throws(() => sm("revise", finding, "--expect", reported.expect, "--edit", edit, "--confirm-publish"), /frontmatter the hook cannot read/);
    assert.equal(readFileSync(join(root, finding), "utf8"), bad);
  }
  // In a field revise rewrites, it is reported, and revise repairs it.
  writeFileSync(join(root, finding), revised.replace(/^reviewed-by: (.*)"$/m, "reviewed-by: $1"));
  const [unclosed] = sm("check");
  assert.deepEqual(unclosed?.reasons, ["frontmatter the hook cannot read (line 10 has a value the scripts do not write; use a JSON string); revise it"]);
  sm("revise", finding, "--expect", unclosed.expect, "--edit", edit, "--confirm-publish");
  assert.deepEqual(sm("check"), []);
});

// JSON.parse reads a duplicate key or reordered keys as the same rule; the
// hook dispatcher's YAML parser rejects the first and the dispatcher skips
// the second, so check reports both and revise rewrites the rule.
test("a guard rule not written as the scripts write it is reported by check and repaired by revise", () => {
  const rule = { tool: "shell", command: "go mod vendor", reason: "It deletes hand-vendored C files.", alternative: "make vendor" };
  const { root, sm, state } = repo([proposal("k-guard", { response: "guard", change: "Refuse a bare go mod vendor.", guardRule: rule })]);
  const { finding } = sm("approve", "k-guard", "--confirm-publish");
  const text = readFileSync(join(root, finding), "utf8");
  const json = JSON.stringify(rule);
  const edit = join(state, "..", `guard-edit-${Date.now()}.json`);
  writeFileSync(edit, JSON.stringify({ guardRule: rule }));
  for (const written of [json.replace("}", ',"tool":"shell"}'), JSON.stringify({ command: rule.command, ...rule })]) {
    writeFileSync(join(root, finding), text.replace(json, written));
    const [stale] = sm("check");
    assert.deepEqual([stale?.path, stale?.reasons], [finding, ["guard rule unreadable or not the JSON the scripts write; revise it with the rule in --edit"]], written);
    sm("revise", finding, "--expect", stale.expect, "--edit", edit, "--confirm-publish");
    assert.ok(readFileSync(join(root, finding), "utf8").includes(`\nguard-rule: ${json}\n`));
    assert.deepEqual(sm("check"), []);
  }
});

test("an assignee whose file name another Person shares is linked by path", () => {
  const { root, sm } = repo([proposal("k-1", { response: "fix", change: "Delete the dead fallback." })]);
  for (const team of ["team-a", "team-b"]) {
    mkdirSync(join(root, `people/${team}`), { recursive: true });
    writeFileSync(join(root, `people/${team}/sam.md`), `---\ntype: Person\n---\n# Sam\n`);
  }
  const teamA = (r, ref) => (ref === "Sam A" ? { found: true, typeName: "Person", path: "people/team-a/sam.md", title: "Sam" } : { found: false });
  const { finding } = run(["approve", "k-1", "--confirm-publish", "--assignee", "Sam A"], { root, state: join(root, ".rhizome/session-memory"), user, people: teamA });
  assert.match(readFileSync(join(root, finding), "utf8"), /\n  assignee:: \[\[people\/team-a\/sam\]\]\n$/);
});

test("the reviewer can change the response, and review asks for the artifact it needs", () => {
  const { root, sm } = repo([proposal("k-1"), proposal("k-2", { response: "fix", change: "Remove the default." }), proposal("k-3", { response: "ask", question: "Keep it?" })]);
  assert.throws(() => sm("approve", "k-1", "--confirm-publish", "--response", "later"), /--response must be one of remember, document, fix, guard, ask/);
  assert.throws(() => sm("approve", "k-1", "--confirm-publish", "--response", "fix"), /a fix response needs change, a sketch of the fix; write it to the --edit file/);
  const edit = join(root, "..", `edit-${Date.now()}.json`);
  writeFileSync(edit, JSON.stringify({ change: "Make 0 mean no wait." }));
  const r = sm("approve", "k-1", "--confirm-publish", "--response", "fix", "--edit", edit);
  assert.match(readFileSync(join(root, r.finding), "utf8"), /## Proposed change\n\nMake 0 mean no wait\./);
  // Redirecting a fix to remember writes a shard; dismissing still works for any response.
  assert.ok(sm("approve", "k-2", "--confirm-publish", "--response", "remember", "--rechecked").approved);
  assert.ok(sm("dismiss", "k-3", "--reason", "already decided", "--local").dismissed);
  assert.deepEqual(sm("list"), []);
});

test("a finding suppresses its learning while its action item is open, and after completion unless it recurs", () => {
  const { root, sm, propose } = repo([proposal("k-1", { response: "guard", change: "Add a lint." })]);
  const { finding } = sm("approve", "k-1", "--confirm-publish");
  const again = proposal("k-9", { statement: proposal("k-1").statement, response: "guard", change: "Add a lint." });
  propose([again]);
  assert.deepEqual(sm("list"), []);
  writeFileSync(join(root, finding), readFileSync(join(root, finding), "utf8").replace("- [ ] ", "- [x] "));
  assert.deepEqual(sm("list"), []);
  propose([{ ...again, recurrence: { finding, completedAt: "2026-10-02T00:00:00.000Z", episodes: [{ id: "claude:s7:3", ts: "2026-10-05T10:00:00Z" }] } }]);
  assert.deepEqual(sm("list").map((x) => [x.cluster, x.recurrence]), [["k-9", { finding, episodes: 1 }]]);
});

test("the verifier's response and drafted artifact are validated with precise retry messages", async () => {
  assert.deepEqual(responseProblems({ response: "remember", responseReason: "x" }), []);
  assert.deepEqual(responseProblems({ response: "keep" }), ["response must be one of remember, document, fix, guard, ask"]);
  assert.deepEqual(responseProblems({ response: "ask", responseReason: "x" }), ["an ask response needs question, the decision its owner must make"]);
  assert.deepEqual(responseProblems({ response: "guard", responseReason: "x", change: "c", guardRule: { tool: "shell", command: "go mod vendor", path: "vendor/**", reason: "r", alternative: "a" } }), ["guardRule needs exactly one of command (a command prefix) or path (a glob using *, **, ?)"]);
  assert.deepEqual(guardRuleProblems({ tool: "read", path: "docs/**", command: [], reason: "r", alternative: "a" }), ["guardRule command must be text"]);
  assert.deepEqual(guardRuleProblems({ tool: "read", command: "cat docs/x.md", reason: "r", alternative: "a" }), ["guardRule.command needs tool shell, since only a shell call has command words; a read, edit, or patch rule needs a path"]);
  assert.deepEqual(responseProblems({ response: "fix", responseReason: "x", change: "c", guardRule: { tool: "shell", command: "x", reason: "r", alternative: "a" } }), ["only a guard response may carry guardRule"]);

  // A drafted edit must apply to the document the verifier read.
  const root = tempRepo();
  const prompts = [];
  const answer = { decision: "propose", kind: "fact", statement: "Topic x states a fact.", why: "w", appliesTo: [], commands: [], documentation: "", verification: [], notes: "", response: "document", responseReason: "Unwritten.", edit: { document: "README.md", old: "# Missing\n", new: "# Missing\nmore\n" } };
  const caller = { generate: async ({ prompt }) => (prompts.push(prompt), answer) };
  const cl = { id: "k-x", statement: "Topic x states a fact.", kind: "fact", appliesTo: [], commands: [] };
  const v = await verifyCluster(caller, { root, search: async () => "" }, { cl, variants: [], sources: [] });
  assert.deepEqual(v.contractProblems, ["edit.old occurs 0 times in README.md; it must occur exactly once"]);
  assert.match(prompts.at(-1), /^Revise this verified proposal[\s\S]*edit\.old occurs 0 times in README\.md/);
  const ask = { ...answer, response: "ask", edit: undefined };
  const w = await verifyCluster({ generate: async () => ask }, { root, search: async () => "" }, { cl, variants: [], sources: [] });
  assert.deepEqual(w.contractProblems, ["an ask response needs question, the decision its owner must make"]);
});

// Session s5 sees omega a day earlier; s3 sees alpha again after the fix.
const s5 = () => session("s5", [turn(9, "omega")], "2026-10-09T10:00:00Z");
const routes = {
  alpha: { response: "fix", responseReason: "A misleading loader.", change: "Make the loader reload its configuration." },
  beta: { response: "ask", responseReason: "Intent is unclear.", question: "Should beta stay?" },
  omega: { response: "guard", responseReason: "Agents keep running it.", change: "Refuse make omega.", guardRule: { tool: "shell", command: "make omega", reason: "It wipes local state.", alternative: "make omega-safe" } },
};

test("the friction report ranks open findings and marks completed ones resolved or recurring", async () => {
  const root = tempRepo();
  const state = mkdtempSync(join(tmpdir(), "sm-state-"));
  const mineWith = (sessions, st = state) => mine({ root, state: st, budget: 5, harness: "claude", discover: fakeDiscover(sessions), log: () => {} }, fakeClients({ routes }));
  await mineWith([s5, SESSIONS.s1, SESSIONS.s2]);
  const sm = (...argv) => run(argv, { root, state, user });
  const listed = Object.fromEntries(sm("list").map((p) => [p.statement.match(/Topic (\w+)/)[1], p]));
  assert.deepEqual(Object.fromEntries(Object.entries(listed).map(([t, p]) => [t, p.response])), { alpha: "fix", beta: "ask", omega: "guard" });
  const path = {};
  for (const [t, p] of Object.entries(listed)) path[t] = sm("approve", p.cluster, "--confirm-publish").finding;
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", "findings"], "2026-10-10T11:30:00Z");
  // alpha and beta are fixed and answered; omega stays open.
  for (const t of ["alpha", "beta"]) writeFileSync(join(root, path[t]), readFileSync(join(root, path[t]), "utf8").replace("- [ ] ", "- [x] "));
  git(root, ["commit", "-qam", "done"], "2026-10-10T11:45:00Z");

  const second = await mineWith([s5, SESSIONS.s1, SESSIONS.s2, SESSIONS.s3]);
  assert.equal(second.report.recurring, 1);
  const offered = sm("list");
  const alpha = offered.find((p) => /Topic alpha/.test(p.statement));
  assert.deepEqual(alpha.recurrence, { finding: path.alpha, episodes: 1 });
  assert.ok(!offered.some((p) => /Topic (beta|omega)/.test(p.statement)));

  const r = report({ root, state });
  assert.deepEqual(r.open.filter((x) => x.finding).map((x) => [x.finding, x.response, x.families, x.sessions, x.toolCalls, x.firstSeen, x.lastSeen]), [[path.omega, "guard", 1, 1, 1, "2026-10-09", "2026-10-09"]]);
  assert.deepEqual(r.open.filter((x) => x.proposal).map((x) => x.statement.match(/Topic (\w+)/)[1]).sort(), ["alpha", "delta"]);
  const completed = Object.fromEntries(r.completed.map((c) => [c.finding, c]));
  assert.equal(completed[path.beta].status, "resolved");
  assert.deepEqual(completed[path.beta].episodes, []);
  assert.equal(completed[path.alpha].status, "recurring");
  assert.deepEqual(completed[path.alpha].episodes.map((e) => [e.id.split(":").slice(0, 2).join(":"), e.date]), [["claude:s3", "2026-10-10"]]);
  assert.equal(completed[path.alpha].completedOn, "2026-10-10");

  // The command reads local state and notes only: no model or rzm call, and no transcript text.
  const marker = join(state, "rzm-called");
  const fakeRzm = join(state, "rzm.sh");
  writeFileSync(fakeRzm, `#!/bin/sh\ntouch '${marker}'\n`, { mode: 0o755 });
  const out = spawnSync(process.execPath, [fileURLToPath(new URL("report.mjs", scripts)), "--state", state], { cwd: root, encoding: "utf8", env: { ...process.env, RZM: fakeRzm } });
  assert.equal(out.status, 0, out.stderr);
  assert.deepEqual(JSON.parse(out.stdout), r);
  assert.equal(existsSync(marker), false);
  for (const transcript of ["reads its configuration", "Please look into", "Filler paragraph"]) assert.ok(!out.stdout.includes(transcript), transcript);

  // A later run still matches the recurrence, though reopening cleared the cluster's review status.
  const s4 = () => session("s4", [turn(7, "zeta")], "2026-10-11T10:00:00Z");
  assert.equal((await mineWith([s5, SESSIONS.s1, SESSIONS.s2, SESSIONS.s3, s4])).status, "done");
  const later = Object.fromEntries(report({ root, state }).completed.map((c) => [c.finding, c]));
  assert.equal(later[path.alpha].status, "recurring");
  assert.deepEqual(later[path.alpha].episodes.map((e) => e.id.split(":").slice(0, 2).join(":")), ["claude:s3"]);

  // A rerun over the same window changes nothing, and reviewing the recurrence settles it.
  assert.equal((await mineWith([s5, SESSIONS.s1, SESSIONS.s2, SESSIONS.s3, s4])).status, "nothing-new");
  sm("dismiss", alpha.cluster, "--reason", "fixed again upstream", "--local");
  assert.ok(!sm("list").some((p) => /Topic alpha/.test(p.statement)));

  // A teammate's fresh run does not propose the open finding's learning.
  const teammate = await mineWith([s5], mkdtempSync(join(tmpdir(), "sm-state-")));
  assert.equal(teammate.report.proposals, 0);
});

test("a proposal verified before responses existed is verified again, and review asks for a response", async () => {
  const root = tempRepo();
  const state = mkdtempSync(join(tmpdir(), "sm-state-"));
  const mineWith = (clients) => mine({ root, state, budget: 5, harness: "claude", discover: fakeDiscover([SESSIONS.s1, SESSIONS.s2]), log: () => {} }, clients);
  await mineWith(fakeClients());
  // As a run before response routing left them: verified, with no response.
  const strip = (x) => (delete x.response, delete x.responseReason, x);
  const clusters = JSON.parse(readFileSync(join(state, "pool/clusters.json"), "utf8"));
  for (const c of Object.values(clusters)) if (c.verification) strip(c.verification);
  writeFileSync(join(state, "pool/clusters.json"), JSON.stringify(clusters));
  writeFileSync(join(state, "proposals.json"), JSON.stringify(JSON.parse(readFileSync(join(state, "proposals.json"), "utf8")).map(strip)));
  // Their verify prompt predates routing, so no cached answer matches.
  rmSync(join(state, "cache"), { recursive: true });
  const sm = (...argv) => run(argv, { root, state, user });
  const listed = sm("list");
  assert.deepEqual(listed.map((p) => p.response), [null, null]);
  assert.throws(() => sm("approve", listed[0].cluster, "--confirm-publish"), /has no proposed response, because it was verified before responses existed; ask the reviewer which response fits and pass --response remember\|document\|fix\|guard\|ask/);
  assert.equal(existsSync(join(root, "docs/memory")), false);
  // The reviewer chooses beta's response; a reviewed proposal is not verified again.
  const beta = listed.find((p) => /Topic beta/.test(p.statement)).cluster;
  assert.ok(sm("approve", beta, "--confirm-publish", "--response", "remember").approved);

  // The next run verifies alpha again, though nothing new arrived.
  const again = fakeClients({ routes: { alpha: routes.alpha } });
  assert.equal((await mineWith(again)).status, "done");
  assert.equal(again.calls.prompts.filter((p) => p.prompt.includes("<candidate>")).length, 1);
  assert.deepEqual(sm("list").map((p) => [p.statement.match(/Topic (\w+)/)[1], p.response]), [["alpha", "fix"]]);
  assert.equal((await mineWith(fakeClients())).status, "nothing-new");
});

test("a finding's completion time is when its action item was checked, not a later edit", () => {
  const { root, sm } = repo([proposal("k-1", { response: "fix", change: "Delete the dead fallback." })]);
  const { finding } = sm("approve", "k-1", "--confirm-publish");
  const file = join(root, finding);
  const edit = (from, to) => writeFileSync(file, readFileSync(file, "utf8").replace(from, to));
  const at = () => loadFindings(root)[0].completedAt;
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", "finding"], "2026-10-01T10:00:00Z");
  edit("- [ ] ", "- [x] ");
  git(root, ["commit", "-qam", "done"], "2026-10-02T10:00:00Z");
  edit("Fix: ", "Fixed: ");
  git(root, ["commit", "-qam", "reword the checked item"], "2026-10-03T10:00:00Z");
  assert.equal(at(), "2026-10-02T10:00:00.000Z");
  // Reopened in a commit, then checked again without committing: the note's time.
  edit("- [x] ", "- [ ] ");
  git(root, ["commit", "-qam", "reopen"], "2026-10-04T10:00:00Z");
  edit("- [ ] ", "- [x] ");
  utimesSync(file, new Date("2026-10-05T10:00:00Z"), new Date("2026-10-05T10:00:00Z"));
  assert.equal(at(), "2026-10-05T10:00:00.000Z");
  // Committing that check makes its commit the completion.
  git(root, ["commit", "-qam", "done again"], "2026-10-06T10:00:00Z");
  assert.equal(at(), "2026-10-06T10:00:00.000Z");
});

// A completed alpha finding, approved from s1 and s2 and checked at 11:45.
async function completedAlpha() {
  const root = tempRepo();
  const state = mkdtempSync(join(tmpdir(), "sm-state-"));
  const mineWith = (sessions, clients = fakeClients({ routes }), over = {}) => mine({ root, state, budget: 5, harness: "claude", discover: fakeDiscover(sessions), log: () => {}, ...over }, clients);
  await mineWith([SESSIONS.s1, SESSIONS.s2]);
  const sm = (...argv) => run(argv, { root, state, user });
  const alpha = sm("list").find((p) => /Topic alpha/.test(p.statement));
  const { finding } = sm("approve", alpha.cluster, "--confirm-publish");
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", "finding"], "2026-10-10T11:30:00Z");
  writeFileSync(join(root, finding), readFileSync(join(root, finding), "utf8").replace("- [ ] ", "- [x] "));
  git(root, ["commit", "-qam", "done"], "2026-10-10T11:45:00Z");
  return { root, state, sm, mineWith, finding, cluster: alpha.cluster };
}
// s3 continues s1's family and sees alpha again after the fix.
const s3 = () => ({ ...SESSIONS.s3(), family: "s1" });
const alphaVerifies = (clients) => clients.calls.prompts.filter((p) => p.prompt.includes("<candidate>") && p.prompt.includes("Topic alpha"));

test("a recurrence is verified again against its new evidence", async () => {
  const { sm, mineWith, finding, cluster } = await completedAlpha();
  const again = fakeClients({ routes: { alpha: { response: "guard", responseReason: "The fix did not hold.", change: "Fail the build when the loader caches." } } });
  await mineWith([SESSIONS.s1, SESSIONS.s2, s3], again);
  // The new episode is the excerpt for its family, not the one the fix answered.
  const [prompt] = alphaVerifies(again).map((p) => p.prompt);
  assert.match(prompt, /area 5/);
  assert.doesNotMatch(prompt, /area 1\b/);
  const offered = sm("list").find((p) => p.cluster === cluster);
  assert.deepEqual([offered.response, offered.recurrence.finding], ["guard", finding]);
});

test("a recurrence the verifier rejects stays in review with the reason", async () => {
  const { sm, mineWith, finding, cluster } = await completedAlpha();
  await mineWith([SESSIONS.s1, SESSIONS.s2, s3], fakeClients({ rejectVerify: true, routes: { alpha: { response: "remember", responseReason: "A lasting fact." } } }));
  const offered = sm("list").find((p) => p.cluster === cluster);
  assert.deepEqual([offered.response, offered.responseReason, offered.rejected.reason, offered.recurrence], [null, undefined, "single_session", { finding, episodes: 1 }]);
  assert.throws(() => sm("approve", cluster, "--confirm-publish"), /no proposed response, because the verifier rejected the recurrence \(single_session\); ask the reviewer/);
  assert.ok(sm("dismiss", cluster, "--reason", "seen once", "--local").dismissed);
});

test("a rejected proposal needs an explicit response even when it carries one", () => {
  const { sm } = repo([proposal("k-1", { rejected: { reason: "one_off", notes: "seen once" } })]);
  assert.throws(() => sm("approve", "k-1", "--confirm-publish"), /no proposed response, because the verifier rejected the recurrence \(one_off\)/);
  assert.ok(sm("approve", "k-1", "--confirm-publish", "--response", "remember").approved);
});

// s6 continues s1's family and sees alpha again two days later.
const s6 = () => ({ ...session("s6", [turn(8, "alpha")], "2026-10-12T10:00:00Z"), family: "s1" });
const failAlpha = () => fakeClients({ routes, fail: (prompt) => prompt.includes("<candidate>") && prompt.includes("Topic alpha") });

test("a later recurrence is verified against its newest episode", async () => {
  const { mineWith } = await completedAlpha();
  await mineWith([SESSIONS.s1, SESSIONS.s2, s3]);
  // Still in review when s6 brings the same friction back from the same family.
  const again = fakeClients({ routes });
  await mineWith([SESSIONS.s1, SESSIONS.s2, s3, s6], again);
  const [prompt] = alphaVerifies(again).map((p) => p.prompt);
  assert.match(prompt, /area 8/);
  assert.doesNotMatch(prompt, /area 5\b/);
});

test("a recurrence a newer open finding covers is recorded but not verified again", async () => {
  const { root, state, sm, mineWith, finding, cluster } = await completedAlpha();
  await mineWith([SESSIONS.s1, SESSIONS.s2, s3]);
  // The recurrence becomes a second finding, still open when s6 brings alpha back.
  const second = sm("approve", cluster, "--confirm-publish").finding;
  const again = fakeClients({ routes });
  assert.equal((await mineWith([SESSIONS.s1, SESSIONS.s2, s3, s6], again)).status, "done");
  assert.deepEqual(alphaVerifies(again), []);
  assert.equal(JSON.parse(readFileSync(join(state, "reviewed.json"), "utf8"))[cluster]?.path, second);
  const first = report({ root, state }).completed.find((c) => c.finding === finding);
  assert.deepEqual(first.episodes.map((e) => e.id.split(":").slice(0, 2).join(":")), ["claude:s3", "claude:s6"]);
});

test("a run with nothing new still dates a finding checked since the last run", async () => {
  const { root, state, mineWith, finding } = await completedAlpha();
  const clients = fakeClients({ routes });
  assert.equal((await mineWith([SESSIONS.s1, SESSIONS.s2], clients)).status, "nothing-new");
  assert.deepEqual(clients.calls.prompts, []);
  const done = report({ root, state }).completed.find((c) => c.finding === finding);
  assert.deepEqual([done.status, done.checkedThrough], ["resolved", new Date().toISOString().slice(0, 10)]);
});

test("a run with nothing new verifies a recurrence the pool already holds", async () => {
  const root = tempRepo();
  const state = mkdtempSync(join(tmpdir(), "sm-state-"));
  const mineWith = (sessions, clients = fakeClients({ routes })) => mine({ root, state, budget: 5, harness: "claude", discover: fakeDiscover(sessions), log: () => {} }, clients);
  await mineWith([SESSIONS.s1, SESSIONS.s2]);
  const sm = (...argv) => run(argv, { root, state, user });
  const { finding } = sm("approve", sm("list").find((p) => /Topic alpha/.test(p.statement)).cluster, "--confirm-publish");
  // s6 arrives while the finding is open, and the check is committed with an earlier date.
  await mineWith([SESSIONS.s1, SESSIONS.s2, s6]);
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", "finding"], "2026-10-11T10:00:00Z");
  writeFileSync(join(root, finding), readFileSync(join(root, finding), "utf8").replace("- [ ] ", "- [x] "));
  git(root, ["commit", "-qam", "done"], "2026-10-11T11:00:00Z");
  const again = fakeClients({ routes });
  assert.equal((await mineWith([SESSIONS.s1, SESSIONS.s2, s6], again)).status, "done");
  assert.equal(alphaVerifies(again).length, 1);
});

test("two fresh recurrences from one family are verified against the newer", () => {
  const ep = (id, ts) => ({ id, ts, family: "s1", lens: "discovery", origin: "agent", text: `excerpt ${id}` });
  const eps = { a: ep("a", "2026-10-05T10:00:00Z"), b: ep("b", "2026-10-06T10:00:00Z"), old: ep("old", "2026-10-01T10:00:00Z") };
  const cl = { evidence: ["old", "a", "b"], recurrence: { episodes: [{ id: "a" }, { id: "b" }], fresh: ["a", "b"] } };
  assert.deepEqual(excerpts(cl, eps).map((e) => e.text), ["excerpt b"]);
});

test("a recurrence whose verification stops leaves no earlier draft to approve", async () => {
  const { root, sm, mineWith, cluster } = await completedAlpha();
  await mineWith([SESSIONS.s1, SESSIONS.s2, s3]);
  // The first recurrence becomes a second finding, which is completed too.
  const { finding } = sm("approve", cluster, "--confirm-publish");
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", "second finding"], "2026-10-11T10:00:00Z");
  writeFileSync(join(root, finding), readFileSync(join(root, finding), "utf8").replace("- [ ] ", "- [x] "));
  git(root, ["commit", "-qam", "done again"], "2026-10-11T11:00:00Z");
  assert.equal((await mineWith([SESSIONS.s1, SESSIONS.s2, s3, s6], failAlpha())).status, "stopped");
  assert.ok(!sm("list").some((p) => p.cluster === cluster));
  assert.throws(() => sm("approve", cluster, "--confirm-publish"), /no pending proposal/);
});

test("a resumed run verifies a recurrence reopened since it stopped", async () => {
  const root = tempRepo();
  const state = mkdtempSync(join(tmpdir(), "sm-state-"));
  const mineWith = (sessions, clients = fakeClients({ routes })) => mine({ root, state, budget: 5, harness: "claude", discover: fakeDiscover(sessions), log: () => {} }, clients);
  const sm = (...argv) => run(argv, { root, state, user });
  await mineWith([SESSIONS.s1, SESSIONS.s2]);
  const alpha = sm("list").find((p) => /Topic alpha/.test(p.statement));
  const { finding } = sm("approve", alpha.cluster, "--confirm-publish");
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", "finding"], "2026-10-10T11:30:00Z");
  // While the finding is open, a run that sees alpha again stops on delta, its
  // plan frozen without alpha.
  const stopped = await mineWith([SESSIONS.s1, SESSIONS.s2, s3], fakeClients({ routes, fail: (prompt) => prompt.includes("<candidate>") && prompt.includes("Topic delta") }));
  assert.equal(stopped.status, "stopped");
  // The finding is completed before s3's episodes, so the resumed run reopens it.
  writeFileSync(join(root, finding), readFileSync(join(root, finding), "utf8").replace("- [ ] ", "- [x] "));
  git(root, ["commit", "-qam", "done"], "2026-10-10T11:45:00Z");
  assert.equal((await mineWith([SESSIONS.s1, SESSIONS.s2, s3])).status, "done");
  assert.equal(sm("list").find((p) => p.cluster === alpha.cluster)?.recurrence?.finding, finding);
});

test("a recurrence whose re-verification broke the contract stays in review for a person to decide", async () => {
  const { sm, mineWith, finding, cluster } = await completedAlpha();
  await mineWith([SESSIONS.s1, SESSIONS.s2, s3], fakeClients({ routes, badVerify: true }));
  const offered = sm("list").find((p) => p.cluster === cluster);
  assert.deepEqual([offered?.recurrence?.finding, offered?.rejected?.reason, offered?.response], [finding, "contract", null]);
  assert.throws(() => sm("approve", cluster, "--confirm-publish"), /no proposed response/);
  // A run with nothing new keeps it there.
  await mineWith([SESSIONS.s1, SESSIONS.s2, s3]);
  assert.ok(sm("list").some((p) => p.cluster === cluster));
});

test("a document edit keeps the document's permissions", { skip: process.platform === "win32" && "no POSIX modes on Windows" }, () => {
  const { root, sm } = repo([proposal("k-doc", { response: "document", edit: documentEdit })]);
  chmodSync(join(root, "docs/store.md"), 0o755);
  git(root, ["commit", "-qam", "mode"]);
  sm("approve", "k-doc", "--confirm-publish", "--rechecked");
  assert.equal(statSync(join(root, "docs/store.md")).mode & 0o777, 0o755);
});

test("redirecting a guard proposal to fix drops its guard rule", () => {
  const rule = { tool: "shell", command: "go mod vendor", reason: "It deletes vendored C files.", alternative: "make vendor-treesitter" };
  const { root, sm } = repo([proposal("k-guard", { response: "guard", change: "Refuse a bare go mod vendor.", guardRule: rule })]);
  const { finding } = sm("approve", "k-guard", "--confirm-publish", "--response", "fix");
  const note = readFileSync(join(root, finding), "utf8");
  assert.match(note, /^response: fix$/m);
  assert.doesNotMatch(note, /guard-rule/);
});

test("a proposal whose cluster verification was cleared or replaced is not pending", () => {
  const { state, sm, propose } = repo([proposal("k-1"), proposal("k-2")]);
  propose([proposal("k-1", { verified: { commit: null, at: "2026-10-01T10:00:00.000Z" } }), proposal("k-2", { verified: { commit: null, at: "2026-10-01T10:00:00.000Z" } })]);
  mkdirSync(join(state, "pool"));
  writeFileSync(join(state, "pool/clusters.json"), JSON.stringify({ "k-1": { id: "k-1", members: [] }, "k-2": { id: "k-2", members: [], verification: { decision: "propose", at: "2026-10-02T10:00:00.000Z" } } }));
  assert.deepEqual(sm("list"), []);
});

test("abandoning a run restores recurrence bookkeeping and keeps later decisions", async () => {
  const { state, sm, mineWith, finding, cluster } = await completedAlpha();
  const beta = sm("list").find((p) => /Topic beta/.test(p.statement)).cluster;
  const stopped = await mineWith([SESSIONS.s1, SESSIONS.s2, s3], fakeClients({ routes, fail: (prompt) => prompt.includes("<candidate>") && prompt.includes("Topic alpha") }));
  assert.equal(stopped.status, "stopped");
  // A decision made while the run is open survives abandoning it.
  sm("dismiss", beta, "--reason", "not ours", "--local");
  assert.equal((await mineWith([], fakeClients(), { abandon: true })).status, "abandoned");
  // The review status the run cleared to reopen alpha is back, and the dismissal stays.
  const reviewed = JSON.parse(readFileSync(join(state, "reviewed.json"), "utf8"));
  assert.deepEqual([reviewed[cluster]?.path, reviewed[beta]?.action], [finding, "dismissed"]);
  await mineWith([SESSIONS.s1, SESSIONS.s2, s3]);
  const listed = sm("list");
  assert.ok(listed.find((p) => p.cluster === cluster)?.recurrence);
  assert.ok(!listed.some((p) => p.cluster === beta));
});

test("a proposal without family IDs recovers them from the pool, or marks its count approximate in the report", () => {
  const evidence = (session, n) => ({ episode: `claude:${session}:${n}`, harness: "claude", session, ts: "2026-10-01T10:00:00Z", lens: "discovery", origin: "agent", toolCalls: 1 });
  const { root, state, sm } = repo([proposal("k-1", { response: "fix", change: "Delete it.", evidence: [evidence("s1", 2), evidence("s2", 3)] }), proposal("k-2", { response: "fix", change: "Delete that.", evidence: [evidence("s8", 1)] })]);
  mkdirSync(join(state, "pool"));
  // s2 continues s1, so the two sessions are one family.
  writeFileSync(join(state, "pool/episodes.json"), JSON.stringify({ "claude:s1:2": { id: "claude:s1:2", family: "s1" }, "claude:s2:3": { id: "claude:s2:3", family: "s1" } }));
  const one = sm("approve", "k-1", "--confirm-publish").finding;
  const two = sm("approve", "k-2", "--confirm-publish").finding;
  const links = JSON.parse(readFileSync(join(state, "finding-links.json"), "utf8"));
  assert.deepEqual(links[one], { clusters: ["k-1"], families: ["s1"] });
  assert.deepEqual(links[two], { clusters: ["k-2"], familiesApproximate: true });
  const open = Object.fromEntries(report({ root, state }).open.map((o) => [o.finding, o]));
  assert.deepEqual([open[one].familiesApproximate, open[two].familiesApproximate, open[two].families], [undefined, true, 2]);
});

test("a teammate's finding without family identities reports an approximate count", () => {
  const { root, state, sm } = repo([proposal("k-1", { response: "fix", change: "Delete the dead fallback.", families: 2, familyIds: ["s1", "s2"] })]);
  const { finding } = sm("approve", "k-1", "--confirm-publish");
  // As a teammate's checkout sees it: the finding, but no local links.
  writeFileSync(join(state, "finding-links.json"), "{}");
  const later = [
    { id: "claude:s1:9", session: "s1", family: "s1", ts: "2026-10-11T10:00:00Z", toolCalls: 2 },
    { id: "claude:s4:2", session: "s4", family: "s4", ts: "2026-10-12T10:00:00Z", toolCalls: 3 },
  ];
  writeFileSync(join(state, "findings.json"), JSON.stringify({ [finding]: { completedAt: null, episodes: later } }));
  const [open] = report({ root, state }).open;
  assert.deepEqual([open.finding, open.families, open.familiesApproximate], [finding, 2, true]);
});

test("an open finding counts families as a union of identities", () => {
  const { root, state, sm } = repo([proposal("k-1", { response: "fix", change: "Delete the dead fallback.", families: 2, familyIds: ["s1", "s2"] })]);
  const { finding } = sm("approve", "k-1", "--confirm-publish");
  // Mining matched two later episodes: one from a family already counted, one new.
  const later = [
    { id: "claude:s1:9", session: "s1", family: "s1", ts: "2026-10-11T10:00:00Z", toolCalls: 2 },
    { id: "claude:s4:2", session: "s4", family: "s4", ts: "2026-10-12T10:00:00Z", toolCalls: 3 },
  ];
  writeFileSync(join(state, "findings.json"), JSON.stringify({ [finding]: { completedAt: null, episodes: later } }));
  const [open] = report({ root, state }).open;
  assert.deepEqual([open.finding, open.families, open.sessions, open.toolCalls], [finding, 3, 2, 12]);
});
