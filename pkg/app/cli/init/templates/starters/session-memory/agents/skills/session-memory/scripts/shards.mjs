#!/usr/bin/env node
// Review proposals and maintain approved context shards (SPEC-0124 US2, US5).
// Every tracked write happens here, as one atomic replacement.
//
//   shards.mjs list                               proposals awaiting review
//   shards.mjs show <cluster>                     one proposal in full
//   shards.mjs source <cluster> [n]               the nth cited source excerpt, redacted
//   shards.mjs approve <cluster> --confirm-publish [--response R] [--edit FILE] [--assignee PERSON]
//                                                 [--broad] [--rechecked]
//   shards.mjs dismiss <cluster> --reason TEXT (--shared | --local)
//   shards.mjs check                              shards to re-verify, and why
//   shards.mjs renew <shard> --expect HASH
//   shards.mjs revise <shard> --expect HASH --edit FILE --confirm-publish [--broad]
//   shards.mjs revise <guard finding> --expect HASH --edit FILE --confirm-publish [--broad]
//   shards.mjs retire <shard> --expect HASH --reason TEXT
//
// Approval follows the proposal's response, or --response (remember,
// document, fix, guard, ask): remember writes a context shard; document
// applies the drafted edit to its document and records a `documented`
// decision; fix, guard, and ask write an AgentFinding note whose action item
// is assigned to the reviewer, or to --assignee (a Person note's title).
// --edit names a JSON file with any of kind, statement, why, appliesTo,
// commands, target, and the response fields (responseReason, edit, change,
// guardRule, question, owner), replacing the proposal's values. --expect is the
// shard's content hash from `check`; a shard edited since is refused. The
// reviewer is the configured current user (`rzm agent current-user`), and an
// --assignee must resolve to a Person note the same way.
// --broad accepts patterns, and a guard rule's path, that match much of the
// repository. Revising a guard finding replaces its guardRule (the only field
// its --edit file may hold) and approves it again; `check` lists guard
// findings whose rule was edited after approval. Run from the repository
// root.

import { execFileSync } from "node:child_process";
import { existsSync, lstatSync, readFileSync, realpathSync, rmSync } from "node:fs";
import { basename, dirname, isAbsolute, join, normalize, relative, sep } from "node:path";
import { pathToFileURL } from "node:url";
import { validateLearning } from "./lib/extract.mjs";
import { FINDING_DIR, findingPath, frontmatterProblem, guardHash, guardLines, loadFindings, renderFinding, REVISED_FIELDS } from "./lib/findings.mjs";
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
  landPair,
  learningHash,
  loadDecisions,
  loadShards,
  matchesAny,
  parseShard,
  renderShard,
  repoRoot,
  shardFiles,
  slugify,
  readIndex,
  stageAtomic,
  staleness,
  titleOf,
  today,
  writeAtomic,
} from "./lib/shards.mjs";
import { openStore } from "./lib/store.mjs";
import { RESPONSES, applyEdit, guardRuleProblems, responseProblems } from "./lib/verify.mjs";
import { publishPrefixes } from "./hook.mjs";


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

// Proposals a person has not yet approved or dismissed. A finding whose
// action item is open suppresses its learning; a completed one suppresses it
// unless mining found that the friction recurred. A proposal whose local
// cluster no longer holds the verification it came from, because a
// recurrence cleared it or a later run replaced it, is no longer a draft.
export function pending(store, root) {
  const reviewed = store.read("reviewed.json", {});
  const clusters = store.read("pool/clusters.json", {});
  const current = (p) => !clusters[p.cluster] || (clusters[p.cluster].verification ?? clusters[p.cluster].verifyProblems)?.at === p.verified?.at;
  const suppressed = new Set(loadDecisions(root, store.dir).map((d) => d.learning));
  for (const s of loadShards(root)) suppressed.add(learningHash(s));
  const done = new Set();
  for (const f of loadFindings(root)) (f.open ? suppressed : done).add(f.learning);
  return store.read("proposals.json", []).filter((p) => current(p) && !reviewed[p.cluster] && !suppressed.has(learningHash(p)) && (p.recurrence || !done.has(learningHash(p))));
}

function find(store, root, cluster) {
  const p = pending(store, root).find((x) => x.cluster === cluster);
  if (!p) throw new Refused(`no pending proposal ${cluster}; run \`shards.mjs list\``);
  return p;
}

// A path-qualified link, so a Person whose file name another note shares
// still resolves to exactly one note.
const personLink = (r) => `[[${r.path.replace(/\.md$/, "")}]]`;

function reviewer(root, user) {
  const r = user ?? currentUser(root);
  if (!r?.found || r.typeName !== "Person") throw new Refused('the current user is not a Person note; run `rzm agent current-user set "<Person title>"` first');
  return { link: personLink(r), name: r.title };
}

const rzm = (root, argv) => JSON.parse(execFileSync(process.env.RZM || (existsSync(join(root, "bin/rzm")) ? join(root, "bin/rzm") : "rzm"), argv, { cwd: root, encoding: "utf8" }));

function currentUser(root) {
  try {
    return rzm(root, ["agent", "current-user", "validate"]);
  } catch {
    return null;
  }
}

// Resolves a Person title, alias, or ref with the query `rzm agent
// current-user validate` runs for the configured user.
const PERSON_QUERY = "query Person($ref: String!) { resolve(ref: $ref) { found path title resolvedType } }";
function lookupPerson(root, ref) {
  try {
    const r = rzm(root, ["agent", "ontology-query", "--json", JSON.stringify({ query: PERSON_QUERY, variables: { ref } })]).data?.resolve;
    return r && { found: r.found, typeName: r.resolvedType, path: r.path, title: r.title };
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

// Where a credential-shaped value sits in text, by line and never by value,
// or null when there is none.
function leak(text) {
  const line = text.split("\n").findIndex((l) => redact(l).count);
  return line >= 0 ? `line ${line + 1}` : redact(text).count ? "a multiline value" : null;
}

const CREDENTIAL_FILE = /(^|\/)\.env(\.|$)/;

// The fully resolved file a repository path names. A symlinked parent can
// point outside the checkout or at a credential file, so containment is
// checked on the real path of the nearest existing parent, and the file
// itself must be a regular file or not exist yet.
function inside(root, path) {
  const top = realpathSync(root);
  const rest = [basename(path)];
  let dir = dirname(join(root, path));
  while (!existsSync(dir)) {
    rest.unshift(basename(dir));
    dir = dirname(dir);
  }
  const file = join(realpathSync(dir), ...rest);
  if (!file.startsWith(top + sep) || CREDENTIAL_FILE.test(relative(top, file).split(sep).join("/"))) throw new Refused(`${path} resolves outside the checkout or to a credential file`);
  if (existsSync(file) && !lstatSync(file).isFile()) throw new Refused(`${path} is not a regular file`);
  return file;
}

// Every tracked write goes through here: the path must resolve inside the
// checkout, and the whole text is scanned, so no field escapes the check.
// The checked write is returned unstaged, so a change that writes several
// files checks all of them before the first lands.
function prepare(root, path, text) {
  const file = inside(root, path);
  const where = leak(text);
  if (where) throw new Refused(`${where} of ${basename(path)} would publish a credential-shaped value; edit it out first`);
  return () => stageAtomic(file, text);
}

const publish = (root, path, text) => prepare(root, path, text)().commit();

const LEARNING_EDITS = ["kind", "statement", "why", "appliesTo", "commands", "target"];
const RESPONSE_EDITS = ["responseReason", "edit", "change", "guardRule", "question", "owner"];

function edited(base, file, { response } = {}) {
  const edit = file ? JSON.parse(readFileSync(file, "utf8")) : {};
  const allowed = response ? [...LEARNING_EDITS, ...RESPONSE_EDITS] : LEARNING_EDITS;
  const extra = Object.keys(edit).filter((k) => !allowed.includes(k));
  if (extra.length) throw new Refused(`--edit may change only ${allowed.join(", ")}; got ${extra.join(", ")}`);
  const s = { ...base, ...edit, ...(response ? { response } : {}) };
  // A guard rule belongs to the guard response; redirecting away drops it
  // unless the reviewer's edit supplies one.
  if (response && response !== "guard" && !("guardRule" in edit)) delete s.guardRule;
  // A reviewer who redirects the response writes its reason only if they want to.
  if (response && response !== base.response && !edit.responseReason) s.responseReason = `The reviewer chose ${response}.`;
  const problems = validateLearning(s, { candidate: false });
  if (response) problems.push(...responseProblems(s).map((x) => `${x}; write it to the --edit file`));
  if (problems.length) throw new Refused(problems.join("; "));
  scan({ statement: s.statement, why: s.why, appliesTo: s.appliesTo, commands: s.commands, target: s.target, responseReason: s.responseReason, change: s.change, question: s.question, owner: s.owner, guardRule: s.guardRule && JSON.stringify(s.guardRule) });
  return s;
}

// The repository paths a shard names outright: its target and what
// verification checked. A path leading outside the checkout names nothing Git
// can report on.
const named = (shard) => [shard.target, ...(shard.checked ?? [])].filter((f) => f && !isAbsolute(f) && !normalize(f).startsWith(".."));

// The files a commit held that a shard depends on, so a pattern match since
// removed from the index or the working tree still counts. None when the
// commit is not in this clone.
function committedFiles(root, shard, rev) {
  try {
    return shardFiles(root, shard, git(root, ["ls-tree", "-r", "-z", "--name-only", rev]).split("\0").filter(Boolean));
  } catch {
    return [];
  }
}

// Approval and renewal describe committed content only. Named paths and
// committed pattern matches are checked even when they are gone from the
// index, so a staged removal counts.
function freshPrint(root, shard) {
  const files = shardFiles(root, shard);
  const dirty = dirtyFiles(root, [...new Set([...files, ...committedFiles(root, shard, "HEAD"), ...named(shard)])]);
  if (dirty.length) throw new Refused(`commit or discard changes first; uncommitted: ${dirty.slice(0, 5).join(", ")}`);
  return fingerprint(root, files);
}

// Verification read the files at its recorded commit, while approval vouches
// for the committed files now. Files that differ, or a proposal with no
// recorded commit, need the statement re-checked first. Checked paths and
// pattern matches at that commit are compared even when they are no longer
// tracked, and a checked path missing from the index or the working tree
// counts as changed.
function changedSinceVerified(root, p, shard) {
  const commit = p.verified?.commit;
  const files = [...new Set([...shardFiles(root, shard), ...(commit ? committedFiles(root, shard, commit) : []), ...named(shard)])];
  if (!files.length) return [];
  if (!commit) return files;
  const indexed = new Set(git(root, ["ls-files", "-z"]).split("\0"));
  const missing = named(shard).filter((f) => !indexed.has(f) || !existsSync(join(root, f)));
  try {
    return [...new Set([...missing, ...git(root, ["--literal-pathspecs", "diff", "--name-only", "-z", commit, "HEAD", "--", ...files]).split("\0").filter(Boolean)])];
  } catch {
    return files;
  }
}

// A shard whose scope matches nothing would be withheld as stale the moment
// it was written, so its patterns and target must name tracked files.
function checkScope(root, s) {
  const tracked = git(root, ["ls-files", "-z"]).split("\0").filter(Boolean);
  const empty = (s.appliesTo ?? []).filter((g) => !tracked.some((f) => matchesAny([g], f)));
  if (empty.length) throw new Refused(`no tracked file matches ${empty.join(", ")}; fix the pattern with --edit`);
  if (s.target && !tracked.includes(s.target)) throw new Refused(`target ${s.target} is not a tracked file; fix it with --edit`);
}

// A shard whose patterns match much of the repository reaches nearly every
// agent, and a guard rule's path that does refuses nearly every call, so
// approval asks for narrower patterns unless the reviewer confirms.
function checkBreadth(root, s, broad, what = "the shard applies") {
  if (broad || !s.appliesTo?.length) return;
  const tracked = git(root, ["ls-files", "-z"]).split("\0").filter(Boolean);
  const n = tracked.filter((f) => matchesAny(s.appliesTo, f)).length;
  if (n > 50 && n > tracked.length / 10) throw new Refused(`the patterns match ${n} of ${tracked.length} tracked files; narrow them with --edit, or pass --broad if the reviewer confirms ${what} that widely`);
}

const checkGuardBreadth = (root, g, broad) => g?.path && checkBreadth(root, { appliesTo: [g.path.trim()] }, broad, "the rule should refuse calls");

const everywhere = (s) => !s.appliesTo?.length && !s.commands?.length;

function shardAt(root, path, expect) {
  const file = inside(root, path);
  if (!existsSync(file)) throw new Refused(`no shard at ${path}`);
  const shard = parseShard(readFileSync(file, "utf8"), path);
  if (!shard) throw new Refused(`${path} has no frontmatter`);
  if (!expect) throw new Refused(`pass --expect ${shard.contentHash} (from \`shards.mjs check\`) to change ${path}`);
  if (shard.contentHash !== expect) throw new Refused(`${path} changed since you read it (expected ${expect}, now ${shard.contentHash}); run \`shards.mjs check\` again`);
  return shard;
}

// A decision event's path and its checked, unmade write; a shared one can
// also be staged.
function decision(root, store, { shared, statement, kind, appliesTo, cluster, decision, reason, decider, document }) {
  const at = new Date().toISOString();
  const event = { learning: learningHash({ kind, statement }), cluster: cluster ?? null, kind, statement, appliesTo: appliesTo ?? [], decision, ...(document ? { document } : {}), reason, decider, at };
  const name = `${event.learning}--${at.replace(/\D/g, "").slice(0, 14)}-${slugify(decider)}.json`;
  if (shared) {
    const stage = prepare(root, `${DECISION_DIR}/${name}`, JSON.stringify(event, null, 2) + "\n");
    return { path: `${DECISION_DIR}/${name}`, stage, write: () => stage().commit() };
  }
  return { path: `.rhizome/session-memory/decisions/${name}`, write: () => store.write(`decisions/${name}`, event) };
}

function decide(root, store, fields) {
  const d = decision(root, store, fields);
  d.write();
  return d.path;
}

const toolCalls = (p) => (p.evidence ?? []).reduce((t, e) => t + (e.toolCalls ?? 0), 0);
const markReviewed = (store, cluster, entry) => store.write("reviewed.json", { ...store.read("reviewed.json", {}), [cluster]: { ...entry, at: new Date().toISOString() } });

// Applies the drafted edit as an ordinary working-tree change for the person
// to commit. The change plan is the document as verification read it: its
// content hash must still match unless the reviewer re-checked the edit in
// context, and the edit's old text must still occur exactly once.
function applyDocument(root, store, p, s, who, rechecked) {
  const { document } = s.edit;
  // Only a tracked document, never a credential file, and the whole list is
  // read so a path outside the checkout is simply not found. A tracked path
  // under a symlinked directory is then refused before it is read.
  const tracked = !CREDENTIAL_FILE.test(document) && git(root, ["ls-files", "-z"]).split("\0").includes(document);
  const file = tracked && inside(root, document);
  const before = file && existsSync(file) ? readFileSync(file, "utf8") : null;
  const { problems, text } = applyEdit(before, s.edit);
  if (problems.length) throw new Refused(`the drafted edit no longer applies: ${problems[0]}; re-draft it against the current document with --edit`);
  const bound = document === p.edit?.document ? p.documentHash : undefined;
  if (contentHash(before) !== bound && !rechecked) throw new Refused(`${document} ${bound ? "changed since verification read it" : "was not read by verification"}; show the person the edit in the context of the current document, then pass --rechecked`);
  // The whole document is published, so a value already in it would be too.
  const held = leak(before);
  if (held) throw new Refused(`${held} of ${document} already holds a credential-shaped value; remove it from the document and commit that first, since applying the edit would republish it`);
  // Both tracked writes are checked and staged before either lands, and a
  // decision that cannot land restores the document, so an approval changes
  // both files or neither.
  const stageDocument = prepare(root, document, text);
  const d = decision(root, store, { shared: true, ...s, cluster: p.cluster, decision: "documented", document, reason: s.responseReason, decider: who.name });
  const staged = [];
  const code = (err) => err.code ?? err.message;
  try {
    staged.push(stageDocument());
    staged.push(d.stage());
  } catch (err) {
    for (const w of staged) w.discard();
    throw new Error(`could not write ${document} and its decision (${code(err)}); nothing was changed`);
  }
  try {
    landPair(staged[0], staged[1], () => writeAtomic(file, before));
  } catch (err) {
    if (err.firstFailed) throw new Error(`could not write ${document} and its decision (${code(err)}); nothing was changed`);
    if (err.restoreError) throw new Error(`could not record the decision for ${document} (${code(err)}) or restore the document (${code(err.restoreError)}); run git checkout -- ${document} before approving again`);
    throw new Error(`could not record the decision for ${document} (${code(err)}); the document was restored and nothing was approved`);
  }
  markReviewed(store, p.cluster, { action: "documented", document, documentHash: contentHash(before), decision: d.path });
  return { documented: document, decision: d.path, next: `review and commit the change to ${document} with the decision` };
}

// One AgentFinding note with one action item, assigned to the reviewer
// unless they name another Person, which must resolve before anything is written.
function writeFinding(root, store, p, s, who, assignee, people = lookupPerson) {
  if (assignee === true) throw new Refused("pass --assignee with the title of a Person note");
  const to = assignee && people(root, String(assignee));
  if (assignee && (!to?.found || to.typeName !== "Person")) throw new Refused(`--assignee ${assignee} does not resolve to a Person note; name an existing Person by title, or create the note first`);
  const path = findingPath(root, s.response, s.statement);
  const finding = {
    ...s,
    learning: learningHash(s),
    reviewedBy: who.link,
    reviewedOn: today(),
    families: p.families,
    toolCalls: toolCalls(p),
    evidenceLines: evidenceLines(p.evidence),
    assignee: to ? personLink(to) : who.link,
  };
  publish(root, path, renderFinding(finding));
  // Which clusters and families the finding came from, so mining can match
  // later episodes to it and the report can count families exactly. A
  // proposal from before family IDs were recorded recovers them from its
  // local cluster or the episode pool; failing both, its count is marked
  // approximate rather than counting sessions as families.
  const links = store.read("finding-links.json", {});
  const pool = store.read("pool/episodes.json", {});
  const pooled = (p.evidence ?? []).map((e) => pool[e.episode]?.family);
  const families = p.familyIds ?? store.read("pool/clusters.json", {})[p.cluster]?.families ?? (pooled.length && pooled.every(Boolean) ? pooled : null);
  store.write("finding-links.json", { ...links, [path]: { clusters: [...new Set([...(links[path]?.clusters ?? []), p.cluster])].sort(), ...(families ? { families: [...new Set(families)].sort() } : { familiesApproximate: true }) } });
  markReviewed(store, p.cluster, { action: "finding", path });
  return { finding: path, assignee: finding.assignee };
}

// Replaces a guard finding's rule and approves it again, so a rule edited by
// hand, which the dispatcher skips, applies once a person approves the text.
function reviseGuard(root, id, a, user) {
  const file = inside(root, id);
  const text = existsSync(file) ? readFileSync(file, "utf8").replace(/\r\n/g, "\n") : null;
  const f = text && loadFindings(root).find((x) => x.path === id);
  if (f?.response !== "guard") throw new Refused(`${id} is not a guard finding`);
  if (!a.expect) throw new Refused(`pass --expect ${f.contentHash} (from \`shards.mjs check\`) to change ${id}`);
  if (f.contentHash !== a.expect) throw new Refused(`${id} changed since you read it (expected ${a.expect}, now ${f.contentHash}); run \`shards.mjs check\` again`);
  const edit = a.edit ? JSON.parse(readFileSync(a.edit, "utf8")) : {};
  const extra = Object.keys(edit).filter((k) => k !== "guardRule");
  if (extra.length) throw new Refused(`--edit may change only guardRule for a guard finding; got ${extra.join(", ")}`);
  const rule = edit.guardRule ?? f.guardRule;
  const problems = rule ? guardRuleProblems(rule) : ["pass the rule as guardRule in the --edit file"];
  if (problems.length) throw new Refused(problems.join("; "));
  checkGuardBreadth(root, rule, a.broad);
  scan({ guardRule: JSON.stringify(rule) });
  const who = reviewer(root, user);
  const [, front, body] = text.match(/^---\n([\s\S]*?)\n---\n([\s\S]*)$/);
  // Each replaced field goes whole, with any indented lines under it, and
  // the result must read the same to the dispatcher's YAML parser.
  const problem = frontmatterProblem(front, REVISED_FIELDS);
  if (problem) throw new Refused(`${id} has frontmatter the hook cannot read (${problem}); fix it by hand, then revise`);
  const lines = [];
  let dropping = false;
  for (const l of front.split("\n")) {
    if (/^[\w-]+:/.test(l)) dropping = /^(guard-rule|approved-hash|reviewed-by|reviewed-on):/.test(l);
    if (!dropping) lines.push(l);
  }
  const at = lines.findIndex((l) => l.startsWith("families:"));
  lines.splice(at < 0 ? lines.length : at, 0, ...guardLines("guard", rule), `reviewed-by: ${JSON.stringify(who.link)}`, `reviewed-on: ${today()}`);
  publish(root, id, `---\n${lines.join("\n")}\n---\n${body}`);
  return { revised: id, guardRule: rule };
}

// `user` stands in for the configured current user in tests, and
// `people(root, ref)` for the Person lookup.
export function run(argv, { root = repoRoot(), state, user, people } = {}) {
  const a = args(argv);
  const store = openStore(state ?? join(root, ".rhizome/session-memory"));
  if (!CHANGES.has(a._[0])) return dispatch(a, { root, store, user, people });
  // A change holds the lock from its first read to its last write, so two
  // reviews cannot both pass the same check.
  store.lock();
  try {
    const out = dispatch(a, { root, store, user, people });
    // The hook starts for a shell call only on a published prefix, so a
    // changed command shard is published now, not at the next session start.
    try {
      publishPrefixes(root, loadShards(root));
    } catch {}
    return out;
  } finally {
    store.unlock();
  }
}

const CHANGES = new Set(["approve", "dismiss", "renew", "revise", "retire"]);

function dispatch(a, { root, store, user, people }) {
  const [cmd, id, n] = a._;
  switch (cmd) {
    case "list":
      return pending(store, root).map((p) => ({ response: p.response ?? null, responseReason: p.responseReason, rejected: p.rejected, workaround: p.workaround, cluster: p.cluster, tier: p.tier, families: p.families, toolCalls: toolCalls(p), kind: p.kind, statement: p.statement, appliesTo: p.appliesTo, target: p.target, recurrence: p.recurrence && { finding: p.recurrence.finding, episodes: p.recurrence.episodes.length } }));
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
      // A rejected recurrence proposes nothing, whatever fields its answer carried.
      const response = a.response ?? (p.rejected ? undefined : p.response);
      if (!response) throw new Refused(`${id} has no proposed response, because ${p.rejected ? `the verifier rejected the recurrence (${p.rejected.reason})` : "it was verified before responses existed"}; ask the reviewer which response fits and pass --response ${RESPONSES.join("|")}`);
      if (!RESPONSES.includes(response)) throw new Refused(`--response must be one of ${RESPONSES.join(", ")}`);
      const remember = response === "remember";
      const s = remember ? edited(p, a.edit) : edited({ ...p, response: p.response ?? "remember" }, a.edit, { response });
      const path = `${SHARD_DIR}/${s.kind.replace(/_/g, "-")}-${slugify(titleOf(s.statement))}.md`;
      if (response === "guard") checkGuardBreadth(root, s.guardRule, a.broad);
      if (remember) {
        checkScope(root, s);
        checkBreadth(root, s, a.broad);
        if (existsSync(join(root, path))) throw new Refused(`${path} already exists; if it is the same learning, revise that shard instead`);
      }
      const who = reviewer(root, user);
      // Every response vouches for what verification read, including the
      // document an edit changes, so each needs committed, unchanged files.
      const verification = (s.verification ?? []).map((v) => ({ ...v, checked: redact(v.checked ?? "").text }));
      const shard = { ...s, verification, reviewedBy: who.link, reviewedOn: today(), verifiedOn: today(), checked: [...verification.map((v) => v.path), ...(response === "document" ? [s.edit.document] : [])], toolCalls: toolCalls(p), evidenceLines: evidenceLines(p.evidence) };
      shard.verifiedFingerprint = freshPrint(root, shard);
      const changed = changedSinceVerified(root, p, shard);
      if (changed.length && !a.rechecked) throw new Refused(`files changed since verification read them: ${changed.slice(0, 5).join(", ")}; re-check the ${remember ? "statement" : "proposal"} against them, then pass --rechecked`);
      if (response === "document") return applyDocument(root, store, p, s, who, a.rechecked);
      if (!remember) return writeFinding(root, store, p, s, who, a.assignee, people);
      publish(root, path, renderShard(shard));
      store.write("reviewed.json", { ...store.read("reviewed.json", {}), [p.cluster]: { action: "approved", path, at: new Date().toISOString() } });
      const global = everywhere(s) ? loadShards(root).filter(everywhere).length : 0;
      const warning = global > 5 ? `${global} shards now apply everywhere, and session start delivers five; give this one a pattern or propose an AGENTS.md edit for it` : undefined;
      return { approved: path, approvedHash: approvedHash(shard), warning };
    }
    case "dismiss": {
      if (!a.reason || a.reason === true) throw new Refused("pass --reason with why the learning is not wanted");
      if (!a.shared === !a.local) throw new Refused("ask the reviewer whether to share the decision through the repository (--shared) or keep it local (--local)");
      const p = find(store, root, id);
      const who = reviewer(root, user);
      const file = decide(root, store, { shared: Boolean(a.shared), ...p, decision: "dismissed", reason: a.reason, decider: who.name });
      store.write("reviewed.json", { ...store.read("reviewed.json", {}), [p.cluster]: { action: "dismissed", decision: file, at: new Date().toISOString() } });
      return { dismissed: p.cluster, decision: file };
    }
    case "check": {
      const index = readIndex(root);
      const { tracked } = index;
      const old = Date.now() - 30 * 24 * 3600 * 1000;
      const out = [];
      for (const s of loadShards(root)) {
        const reasons = [];
        const why = staleness(root, s, index);
        if (why) reasons.push(why);
        if ([...s.anchors].sort().join("\n") !== [...s.appliesTo].sort().join("\n")) reasons.push("code-anchors differ from applies-to");
        if (!s.appliesTo.length && Date.parse(s.verifiedOn) < old) reasons.push("no patterns, and verified more than 30 days ago");
        if (reasons.length) out.push({ path: s.path, expect: s.contentHash, kind: s.kind, statement: s.statement, verifiedOn: s.verifiedOn, reasons, matched: tracked.filter((f) => matchesAny(s.appliesTo, f)).slice(0, 20) });
      }
      // The hook dispatcher skips a guard rule edited after approval, or one
      // it cannot read.
      for (const f of loadFindings(root)) {
        if (f.response !== "guard" || !f.hasGuardRule) continue;
        const reasons = [];
        if (f.frontmatterProblem) reasons.push(`frontmatter the hook cannot read (${f.frontmatterProblem}); fix it by hand, then revise`);
        else if (!f.guardRule) reasons.push("guard rule unreadable or not the JSON the scripts write; revise it with the rule in --edit");
        else if (f.unreadableFrontmatter) reasons.push(`frontmatter the hook cannot read (${f.unreadableFrontmatter}); revise it`);
        else if (guardHash(f.response, f.guardRule) !== f.approvedHash) reasons.push("guard rule edited after approval; revise it");
        if (reasons.length) out.push({ path: f.path, expect: f.contentHash, statement: f.statement, guardRule: f.guardRule, reasons });
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
      publish(root, id, next);
      return { renewed: id, verifiedFingerprint: print };
    }
    case "revise": {
      if (!a["confirm-publish"]) throw new Refused("ask the reviewer to confirm the revised statement is fit to publish, then pass --confirm-publish");
      if (String(id ?? "").startsWith(`${FINDING_DIR}/`)) return reviseGuard(root, id, a, user);
      const old = shardAt(root, id, a.expect);
      const s = edited({ kind: old.kind, statement: old.statement, why: old.why, appliesTo: old.appliesTo, commands: old.commands, target: old.target }, a.edit);
      checkScope(root, s);
      checkBreadth(root, s, a.broad);
      const who = reviewer(root, user);
      const verification = old.checked.map((path) => ({ path, checked: "checked at the original approval" }));
      const shard = { ...s, reviewedBy: who.link, reviewedOn: today(), verifiedOn: today(), families: old.families, toolCalls: old.toolCalls, checked: old.checked, verification, evidenceLines: old.evidenceSection.split("\n").filter(Boolean) };
      shard.verifiedFingerprint = freshPrint(root, shard);
      publish(root, id, renderShard(shard));
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
