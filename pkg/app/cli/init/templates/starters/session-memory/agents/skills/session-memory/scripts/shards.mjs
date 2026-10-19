#!/usr/bin/env node
// Review proposals and maintain approved context shards (SPEC-0121 US2, US5).
// Every tracked write happens here, as one atomic replacement.
//
//   shards.mjs list                               proposals awaiting review
//   shards.mjs show <cluster>                     one proposal in full
//   shards.mjs source <cluster> [n]               the nth cited source excerpt, redacted
//   shards.mjs approve <cluster> --confirm-publish [--edit FILE]
//   shards.mjs dismiss <cluster> --reason TEXT (--shared | --local)
//   shards.mjs check                              shards to re-verify, and why
//   shards.mjs renew <shard> --expect HASH
//   shards.mjs revise <shard> --expect HASH --edit FILE --confirm-publish
//   shards.mjs retire <shard> --expect HASH --reason TEXT
//
// --edit names a JSON file with any of kind, statement, why, appliesTo,
// commands, and target, replacing the proposal's values. --expect is the
// shard's content hash from `check`; a shard edited since is refused. The
// reviewer is the configured current user (`rzm agent current-user`). Run
// from the repository root.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { validateLearning } from "./lib/extract.mjs";
import { redact } from "./lib/redact.mjs";
import {
  DECISION_DIR,
  SHARD_DIR,
  approvedHash,
  contentHash,
  dirtyFiles,
  evidenceLines,
  fingerprint,
  git,
  learningHash,
  loadDecisions,
  loadShards,
  matchesAny,
  parseShard,
  renderShard,
  repoRoot,
  shardFiles,
  slugify,
  staleness,
  titleOf,
  today,
  writeAtomic,
} from "./lib/shards.mjs";
import { openStore } from "./lib/store.mjs";


class Refused extends Error {}

function args(argv) {
  const out = { _: [] };
  for (let k = 0; k < argv.length; k++) {
    const a = argv[k];
    if (a.startsWith("--")) out[a.slice(2)] = argv[k + 1] === undefined || argv[k + 1].startsWith("--") ? true : argv[++k];
    else out._.push(a);
  }
  return out;
}

// Proposals a person has not yet approved or dismissed.
export function pending(store, root) {
  const reviewed = store.read("reviewed.json", {});
  const suppressed = new Set(loadDecisions(root, store.dir).map((d) => d.learning));
  for (const s of loadShards(root)) suppressed.add(learningHash(s));
  return store.read("proposals.json", []).filter((p) => !reviewed[p.cluster] && !suppressed.has(learningHash(p)));
}

function find(store, root, cluster) {
  const p = pending(store, root).find((x) => x.cluster === cluster);
  if (!p) throw new Refused(`no pending proposal ${cluster}; run \`shards.mjs list\``);
  return p;
}

function reviewer(root, user) {
  const r = user ?? currentUser(root);
  if (!r?.found || r.typeName !== "Person") throw new Refused('the current user is not a Person note; run `rzm agent current-user set "<Person title>"` first');
  return { link: `[[${r.path.replace(/^.*\//, "").replace(/\.md$/, "")}]]`, name: r.title };
}

function currentUser(root) {
  const rzm = process.env.RZM || (existsSync(join(root, "bin/rzm")) ? join(root, "bin/rzm") : "rzm");
  try {
    return JSON.parse(execFileSync(rzm, ["agent", "current-user", "validate"], { cwd: root, encoding: "utf8" }));
  } catch {
    return null;
  }
}

// Text bound for a tracked file must carry nothing credential-shaped.
function scan(fields) {
  for (const [name, value] of Object.entries(fields)) {
    const text = Array.isArray(value) ? value.join("\n") : value;
    if (text && redact(String(text)).count) throw new Refused(`${name} contains a credential-shaped value; edit it out before approving`);
  }
}

function edited(base, file) {
  const edit = file ? JSON.parse(readFileSync(file, "utf8")) : {};
  const allowed = ["kind", "statement", "why", "appliesTo", "commands", "target"];
  const extra = Object.keys(edit).filter((k) => !allowed.includes(k));
  if (extra.length) throw new Refused(`--edit may change only ${allowed.join(", ")}; got ${extra.join(", ")}`);
  const s = { ...base, ...edit };
  const problems = validateLearning(s, { candidate: false });
  if (problems.length) throw new Refused(problems.join("; "));
  scan({ statement: s.statement, why: s.why, appliesTo: s.appliesTo, commands: s.commands, target: s.target });
  return s;
}

// Approval and renewal describe committed content only.
function freshPrint(root, shard) {
  const files = shardFiles(root, shard);
  const dirty = dirtyFiles(root, files);
  if (dirty.length) throw new Refused(`commit or discard changes first; uncommitted: ${dirty.slice(0, 5).join(", ")}`);
  return fingerprint(root, files);
}

function shardAt(root, path, expect) {
  const file = join(root, path);
  if (!existsSync(file)) throw new Refused(`no shard at ${path}`);
  const shard = parseShard(readFileSync(file, "utf8"), path);
  if (!shard) throw new Refused(`${path} has no frontmatter`);
  if (!expect) throw new Refused(`pass --expect ${shard.contentHash} (from \`shards.mjs check\`) to change ${path}`);
  if (shard.contentHash !== expect) throw new Refused(`${path} changed since you read it (expected ${expect}, now ${shard.contentHash}); run \`shards.mjs check\` again`);
  return shard;
}

function decide(root, store, { shared, statement, kind, appliesTo, cluster, decision, reason, decider }) {
  const at = new Date().toISOString();
  const event = { learning: learningHash({ kind, statement }), cluster: cluster ?? null, kind, statement, appliesTo: appliesTo ?? [], decision, reason, decider, at };
  const name = `${event.learning}--${at.replace(/\D/g, "").slice(0, 14)}-${slugify(decider)}.json`;
  if (shared) writeAtomic(join(root, DECISION_DIR, name), JSON.stringify(event, null, 2) + "\n");
  else store.write(`decisions/${name}`, event);
  return shared ? `${DECISION_DIR}/${name}` : `.rhizome/session-memory/decisions/${name}`;
}

// `user` stands in for the configured current user in tests.
export function run(argv, { root = repoRoot(), state, user } = {}) {
  const a = args(argv);
  const [cmd, id, n] = a._;
  const store = openStore(state ?? join(root, ".rhizome/session-memory"));
  switch (cmd) {
    case "list":
      return pending(store, root).map((p) => ({ cluster: p.cluster, tier: p.tier, families: p.families, kind: p.kind, statement: p.statement, appliesTo: p.appliesTo, target: p.target }));
    case "show":
      return find(store, root, id);
    case "source": {
      const p = find(store, root, id);
      const ev = p.evidence[Number(n ?? 1) - 1];
      if (!ev) throw new Refused(`${id} cites ${p.evidence.length} sources`);
      const e = store.read("pool/episodes.json", {})[ev.episode];
      return { ...ev, text: e?.text ? redact(e.text).text : "(the excerpt is no longer in the local pool)" };
    }
    case "approve": {
      if (!a["confirm-publish"]) throw new Refused("ask the reviewer to confirm the statement is fit to publish in this repository, then pass --confirm-publish");
      const p = find(store, root, id);
      const s = edited(p, a.edit);
      const path = `${SHARD_DIR}/${s.kind.replace(/_/g, "-")}-${slugify(titleOf(s.statement))}.md`;
      if (existsSync(join(root, path))) throw new Refused(`${path} already exists; if it is the same learning, revise that shard instead`);
      const who = reviewer(root, user);
      const shard = { ...s, reviewedBy: who.link, reviewedOn: today(), verifiedOn: today(), checked: (s.verification ?? []).map((v) => v.path), toolCalls: p.evidence.reduce((t, e) => t + (e.toolCalls ?? 0), 0), evidenceLines: evidenceLines(p.evidence) };
      shard.verifiedFingerprint = freshPrint(root, shard);
      store.lock();
      try {
        writeAtomic(join(root, path), renderShard(shard));
        store.write("reviewed.json", { ...store.read("reviewed.json", {}), [p.cluster]: { action: "approved", path, at: new Date().toISOString() } });
      } finally {
        store.unlock();
      }
      return { approved: path, approvedHash: approvedHash(shard) };
    }
    case "dismiss": {
      if (!a.reason || a.reason === true) throw new Refused("pass --reason with why the learning is not wanted");
      if (!a.shared === !a.local) throw new Refused("ask the reviewer whether to share the decision through the repository (--shared) or keep it local (--local)");
      const p = find(store, root, id);
      const who = reviewer(root, user);
      store.lock();
      try {
        const file = decide(root, store, { shared: Boolean(a.shared), ...p, decision: "dismissed", reason: a.reason, decider: who.name });
        store.write("reviewed.json", { ...store.read("reviewed.json", {}), [p.cluster]: { action: "dismissed", decision: file, at: new Date().toISOString() } });
        return { dismissed: p.cluster, decision: file };
      } finally {
        store.unlock();
      }
    }
    case "check": {
      const tracked = git(root, ["ls-files", "-z"]).split("\0").filter(Boolean);
      const old = Date.now() - 30 * 24 * 3600 * 1000;
      const out = [];
      for (const s of loadShards(root)) {
        const reasons = [];
        const why = staleness(root, s, tracked);
        if (why) reasons.push(why);
        if ([...s.anchors].sort().join("\n") !== [...s.appliesTo].sort().join("\n")) reasons.push("code-anchors differ from applies-to");
        if (!s.appliesTo.length && Date.parse(s.verifiedOn) < old) reasons.push("no patterns, and verified more than 30 days ago");
        if (reasons.length) out.push({ path: s.path, expect: s.contentHash, kind: s.kind, statement: s.statement, verifiedOn: s.verifiedOn, reasons, matched: tracked.filter((f) => matchesAny(s.appliesTo, f)).slice(0, 20) });
      }
      return out;
    }
    case "renew": {
      const s = shardAt(root, id, a.expect);
      if (approvedHash(s) !== s.approvedHash) throw new Refused(`${id} was edited by hand; revise it instead of renewing`);
      const print = freshPrint(root, s);
      const text = readFileSync(join(root, id), "utf8").replace(/^verified-on: .*$/m, `verified-on: ${today()}`);
      const next = print
        ? /^verified-fingerprint: /m.test(text)
          ? text.replace(/^verified-fingerprint: .*$/m, `verified-fingerprint: ${print}`)
          : text.replace(/^(verified-on: .*)$/m, `$1\nverified-fingerprint: ${print}`)
        : text.replace(/^verified-fingerprint: .*\n/m, "");
      writeAtomic(join(root, id), next);
      return { renewed: id, verifiedFingerprint: print };
    }
    case "revise": {
      if (!a["confirm-publish"]) throw new Refused("ask the reviewer to confirm the revised statement is fit to publish, then pass --confirm-publish");
      const old = shardAt(root, id, a.expect);
      const s = edited({ kind: old.kind, statement: old.statement, why: old.why, appliesTo: old.appliesTo, commands: old.commands, target: old.target }, a.edit);
      const who = reviewer(root, user);
      const verification = old.checked.map((path) => ({ path, checked: "checked at the original approval" }));
      const shard = { ...s, reviewedBy: who.link, reviewedOn: today(), verifiedOn: today(), families: old.families, toolCalls: old.toolCalls, checked: old.checked, verification, evidenceLines: old.evidenceSection.split("\n").filter(Boolean) };
      shard.verifiedFingerprint = freshPrint(root, shard);
      writeAtomic(join(root, id), renderShard(shard));
      return { revised: id, approvedHash: approvedHash(shard) };
    }
    case "retire": {
      if (!a.reason || a.reason === true) throw new Refused("pass --reason with why the shard no longer holds");
      const s = shardAt(root, id, a.expect);
      const who = reviewer(root, user);
      const file = decide(root, store, { shared: true, ...s, decision: "retired", reason: a.reason, decider: who.name });
      rmSync(join(root, id));
      return { retired: id, decision: file };
    }
    default:
      throw new Refused("usage: shards.mjs list|show|source|approve|dismiss|check|renew|revise|retire (see the header)");
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    console.log(JSON.stringify(run(process.argv.slice(2)), null, 2));
  } catch (err) {
    console.error(err instanceof Refused ? `refused: ${err.message}` : err.stack);
    process.exitCode = err instanceof Refused ? 2 : 1;
  }
}
