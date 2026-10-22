// The session-memory handler `rzm hook` runs, against a scratch Git
// repository with synthetic shards written straight to disk.
import assert from "node:assert/strict";
import { execFileSync, execSync, spawnSync } from "node:child_process";
import { cpSync, mkdirSync, readFileSync, realpathSync, utimesSync, writeFileSync } from "node:fs";
import { join, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { scratchRepo } from "./fakes.mjs";

const scripts = new URL("../../templates/starters/session-memory/agents/skills/session-memory/scripts/", import.meta.url);
const S = await import(new URL("lib/shards.mjs", scripts));
const { lookup } = await import(new URL("lookup.mjs", scripts));
const { handle, cachedShards, HOOK_DIR, lookupCommand, moreLine } = await import(new URL("hook.mjs", scripts));

const git = (root, ...args) => execFileSync("git", args, { cwd: root, encoding: "utf8" });

// A current shard: approved hash and fingerprint match the index now.
function shard(root, over) {
  const s = { kind: "rule", appliesTo: ["pkg/store/**/*.go"], commands: [], target: null, reviewedBy: "[[pat-example]]", reviewedOn: "2026-10-01", verifiedOn: "2026-10-01", ...over };
  s.verifiedFingerprint = S.fingerprint(root, S.shardFiles(root, s));
  const path = `${S.SHARD_DIR}/${s.kind}-${S.slugify(s.statement)}.md`;
  mkdirSync(join(root, S.SHARD_DIR), { recursive: true });
  writeFileSync(join(root, path), S.renderShard(s));
  return path;
}

const request = (root, event, over = {}) => ({ version: 1, event: { harness: "claude", root, reset: "", tool: "", paths: [], command: "", ...event }, emitted: [], budget: 9000, deadlineMs: 2500, ...over });
const read = (root, paths, over) => handle(request(root, { event: "post-tool-use", tool: "read", paths }, over));
const shell = (root, command, paths = []) => handle(request(root, { event: "post-tool-use", tool: "shell", command, paths }));
const start = (root, over) => handle(request(root, { event: "session-start" }, over));
const ids = (r) => r.items.map((i) => i.id);
const pathOf = (id) => id.slice(0, id.lastIndexOf("@"));

test("one index read fingerprints a shard exactly as git ls-files -s does", () => {
  const root = scratchRepo();
  for (const f of ["a.go", "b.go", "sub/c.go"]) {
    mkdirSync(join(root, "pkg/store/sub"), { recursive: true });
    writeFileSync(join(root, "pkg/store", f), `package store // ${f}\n`);
  }
  writeFileSync(join(root, "docs/other.md"), "# Other\n");
  git(root, "add", "-A");
  const s = { appliesTo: ["pkg/store/**/*.go"], target: "docs/store.md", checked: [] };
  const index = S.readIndex(root);
  const files = S.shardFiles(root, s, index.tracked);
  assert.equal(files.length, 5);
  assert.equal(S.indexFingerprint(index, files), S.fingerprint(root, files));
  assert.equal(S.indexFingerprint(index, []), null);
});

test("lookup and the hook select the same shards, and match commands by segment prefix", () => {
  const root = scratchRepo();
  const store = shard(root, { statement: "Store rule: a busy timeout of 0 means the default." });
  const docs = shard(root, { kind: "fact", statement: "Docs fact: the store doc is generated.", appliesTo: ["docs/**"] });
  const vendor = shard(root, { statement: "Vendor rule: run make vendor-treesitter afterwards.", appliesTo: [], commands: ["go  mod vendor"] });
  const stale = shard(root, { statement: "Stale rule: written before the store changed.", appliesTo: ["pkg/**"] });
  writeFileSync(join(root, "pkg/store/db.go"), "package store\n\nconst X = 1\n");
  git(root, "add", "-A");
  shard(root, { statement: "Store rule: a busy timeout of 0 means the default." });

  for (const [paths, command, want] of [
    [["pkg/store/db.go"], undefined, [store]],
    [["docs/store.md", "pkg/store/db.go"], undefined, [store, docs]],
    [[], "cd pkg && go mod vendor -v", [vendor]],
    [[], "echo go mod vendor", []],
  ]) {
    const viaLookup = lookup({ root, files: paths, command });
    const viaHook = command ? shell(root, command, paths) : read(root, paths);
    assert.deepEqual(viaLookup.shards.map((s) => s.id), ids(viaHook), `${paths} ${command}`);
    assert.deepEqual(ids(viaHook).map(pathOf).sort(), want.sort());
  }
  assert.deepEqual(lookup({ root, files: ["pkg/store/db.go"] }).withheld.map((w) => w.path), [stale]);
  const [item] = read(root, ["pkg/store/db.go"]).items;
  assert.equal(item.text, `Context shard [rule] Store rule: a busy timeout of 0 means the default. (${store}, verified 2026-10-01)`);
  assert.equal(item.id, `${store}@${S.contentHash(readFileSync(join(root, store), "utf8"))}`);
});

test("emitted shards are left out before the five-shard limit", () => {
  const root = scratchRepo();
  for (let k = 0; k < 7; k++) shard(root, { kind: k < 2 ? "fact" : "rule", statement: `Rule ${k}: the store keeps invariant ${k}.` });
  const first = read(root, ["pkg/store/db.go"]);
  assert.equal(first.items.length, 5);
  assert.ok(first.items.slice(0, 5).every((i) => i.text.startsWith("Context shard [rule]")));
  const second = read(root, ["pkg/store/db.go"], { emitted: ids(first) });
  assert.equal(second.items.length, 2);
  assert.deepEqual([...ids(first), ...ids(second)].sort(), [...new Set([...ids(first), ...ids(second)])].sort());
  assert.ok(second.items.every((i) => i.text.startsWith("Context shard [fact]")));
});

test("the handler publishes approved command prefixes and rewrites them when they change", () => {
  const root = scratchRepo();
  const file = join(root, HOOK_DIR, "command-prefixes.json");
  start(root);
  assert.equal(readFileSync(file, "utf8"), "[]");
  const path = shard(root, { statement: "Vendor rule: run make vendor-treesitter afterwards.", appliesTo: [], commands: ["go  mod vendor", "make vendor"] });
  shard(root, { statement: "Lint rule: format first.", commands: ["npm run lint", "make vendor"] });
  read(root, ["docs/store.md"]);
  assert.deepEqual(JSON.parse(readFileSync(file, "utf8")), ["go mod vendor", "make vendor", "npm run lint"]);
  shard(root, { statement: "Vendor rule: run make vendor-treesitter afterwards.", appliesTo: [], commands: ["go mod tidy"] });
  start(root);
  assert.deepEqual(JSON.parse(readFileSync(file, "utf8")), ["go mod tidy", "make vendor", "npm run lint"]);
  // A hand edit breaks the approved hash, so its prefixes are not published.
  writeFileSync(join(root, path), readFileSync(join(root, path), "utf8").replace('"go mod tidy"', '"go mod verify"'));
  start(root);
  assert.deepEqual(JSON.parse(readFileSync(file, "utf8")), ["make vendor", "npm run lint"]);
});

test("a shard is withheld once the index changes, but not for an unstaged edit", () => {
  const root = scratchRepo();
  shard(root, { statement: "Store rule: a busy timeout of 0 means the default." });
  writeFileSync(join(root, "pkg/store/db.go"), "package store // unstaged\n");
  assert.equal(read(root, ["pkg/store/db.go"]).items.length, 1);
  git(root, "add", "pkg/store/db.go");
  assert.deepEqual(read(root, ["pkg/store/db.go"]).items, []);
});

test("delivery stays within the character allowance and the dispatcher's smaller budget", () => {
  const root = scratchRepo();
  for (let k = 0; k < 4; k++) shard(root, { statement: `Rule ${k}: ${"the store must stay consistent ".repeat(45)}` });
  const size = (r) => r.items.reduce((n, i) => n + Buffer.byteLength(i.text), 0);
  const full = read(root, ["pkg/store/db.go"]);
  assert.equal(full.items.length, 2);
  assert.ok(size(full) <= 4000);
  const tight = read(root, ["pkg/store/db.go"], { budget: 2000 });
  assert.equal(tight.items.length, 1);
  assert.ok(size(tight) <= 2000);
});

test("session start delivers only shards that apply everywhere, then says how many more apply", () => {
  const root = scratchRepo();
  shard(root, { statement: "Store rule: a busy timeout of 0 means the default." });
  shard(root, { statement: "Vendor rule: run make vendor-treesitter afterwards.", appliesTo: [], commands: ["go mod vendor"] });
  const everywhere = Array.from({ length: 7 }, (_, k) => shard(root, { kind: "fact", statement: `Global fact ${k}: every package logs through one logger ${"and reports through one channel ".repeat(6)}`, appliesTo: [] }));
  const r = start(root);
  assert.equal(r.items.length, 6);
  assert.ok(ids(r).slice(0, 5).every((id) => everywhere.includes(pathOf(id))));
  const more = r.items[5];
  assert.match(more.id, /^session-memory:more@[0-9a-f]{16}$/);
  assert.equal(more.text, moreLine(2, lookupCommand(5)));
  // The command the line names shows exactly the shards it did not deliver.
  const shown = execSync(more.text.match(/run (node .+) to see them\.$/)[1], { cwd: root, encoding: "utf8" });
  const delivered = ids(r).slice(0, 5).map(pathOf);
  assert.deepEqual(everywhere.filter((p) => shown.includes(p)), everywhere.filter((p) => !delivered.includes(p)));
  assert.deepEqual(ids(handle(request(root, { event: "subagent-start", reset: "compact" }))), ids(r));
  assert.equal(read(root, ["docs/store.md"]).items.length, 0);

  // Room for the closing line is reserved inside a smaller budget: three
  // shards would fit it, but then the line would not.
  const lineSize = Buffer.byteLength(r.items[0].text);
  assert.ok(Buffer.byteLength(moreLine(1, lookupCommand(7))) < lineSize - 5);
  const tight = start(root, { budget: 3 * lineSize + 5 });
  assert.deepEqual(tight.items.map((i) => i.text).slice(2), [moreLine(5, lookupCommand(2))]);
  const rest = start(root, { emitted: ids(r).slice(0, 5) });
  assert.deepEqual(rest.items.map((i) => pathOf(i.id)), everywhere.filter((p) => !ids(r).map(pathOf).includes(p)));
});

test("the closing line names lookup.mjs where the skill is installed", async () => {
  // The dispatcher passes the canonical root.
  const root = realpathSync(scratchRepo());
  const installed = join(root, ".claude/skills/session-memory/scripts");
  cpSync(fileURLToPath(scripts), installed, { recursive: true });
  const copy = await import(new URL("hook.mjs", `file://${installed}/`).href);
  assert.equal(copy.lookupCommand(5), `node ${join(installed, "lookup.mjs").split(sep).join("/")} --skip 5`);
  // It runs from a subdirectory, where the agent's shell may be.
  mkdirSync(join(root, "src"), { recursive: true });
  execSync(copy.lookupCommand(0), { cwd: join(root, "src"), encoding: "utf8" });
});

test("parsed shards are cached after a consistent read and reused until the listing changes", () => {
  const root = scratchRepo();
  const path = shard(root, { statement: "Store rule: a busy timeout of 0 means the default." });
  const file = join(root, HOOK_DIR, "shard-cache.json");
  // A shard edited while it is read leaves no cache behind.
  cachedShards(root, (r) => {
    const out = S.loadShards(r);
    writeFileSync(join(r, path), `${readFileSync(join(r, path), "utf8")}\n`);
    return out;
  });
  assert.throws(() => readFileSync(file), /ENOENT/);

  read(root, ["pkg/store/db.go"]);
  const cache = JSON.parse(readFileSync(file, "utf8"));
  assert.equal(cache.shards.length, 1);
  cache.shards[0].verifiedOn = "1999-01-01";
  writeFileSync(file, JSON.stringify(cache));
  assert.match(read(root, ["pkg/store/db.go"]).items[0].text, /verified 1999-01-01/);
  // Eligibility is still checked against the index on a cache hit.
  writeFileSync(join(root, "pkg/store/db.go"), "package store // staged\n");
  git(root, "add", "pkg/store/db.go");
  assert.deepEqual(read(root, ["pkg/store/db.go"]).items, []);
  git(root, "reset", "-q", "pkg/store/db.go");
  writeFileSync(join(root, "pkg/store/db.go"), "package store\n");
  const later = new Date(Date.now() + 5000);
  utimesSync(join(root, path), later, later);
  assert.match(read(root, ["pkg/store/db.go"]).items[0].text, /verified 2026-10-01/);
});

test("the CLI reads one request from stdin and always exits 0", () => {
  const root = scratchRepo();
  shard(root, { statement: "Store rule: a busy timeout of 0 means the default." });
  const cli = fileURLToPath(new URL("hook.mjs", scripts));
  const run = (input) => spawnSync(process.execPath, [cli], { cwd: root, input, encoding: "utf8" });
  const ok = run(JSON.stringify(request(root, { event: "post-tool-use", tool: "edit", paths: ["pkg/store/db.go"] })));
  assert.equal(ok.status, 0);
  assert.equal(JSON.parse(ok.stdout).items.length, 1);
  const huge = JSON.stringify({ ...request(root, { event: "post-tool-use", tool: "edit", paths: ["pkg/store/db.go"] }), pad: "x".repeat(1 << 20) });
  for (const bad of ["not json", JSON.stringify({ version: 2 }), huge]) {
    const r = run(bad);
    assert.equal(r.status, 0);
    assert.deepEqual(JSON.parse(r.stdout), { items: [] });
    assert.match(r.stderr, /session-memory hook/);
  }
});
