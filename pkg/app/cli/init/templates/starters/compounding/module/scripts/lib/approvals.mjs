// Review state (SPEC-0124 US8): which proposals await a decision, and where
// each approval stands. Approval writes files and never commits, so every
// approval records a local decision event naming the file it created, its
// witness: the shard, the finding note, or for a document edit the shared
// decision file beside it. The witness then says where the approval is, in
// order: merged at the primary ref, present in this checkout, committed at a
// local branch tip, or uncommitted in the worktree that wrote it. Otherwise
// it is missing, and the proposal returns to review from the payload the
// event saved. Dismissals are not tied to a checkout.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, realpathSync } from "node:fs";
import { join } from "node:path";
import { loadFindings } from "./findings.mjs";
import { contentHash, git, learningHash, loadDecisions, loadShards, slugify } from "./shards.mjs";
import { worktrees } from "./state.mjs";

// Fields review adds for display, never part of a proposal.
const SHOWN = new Set(["status", "previous", "revision", "head"]);
export const bare = (p) => Object.fromEntries(Object.entries(p).filter(([k]) => !SHOWN.has(k)));
export const revision = (p) => contentHash(JSON.stringify(bare(p)));

export function headOf(root) {
  try {
    return git(root, ["rev-parse", "HEAD"]).trim();
  } catch {
    return null;
  }
}

// The payload is the proposal as approved, edits included, so approving it
// again reproduces the approval.
export function approvalEvent(root, { proposal, response, witness, decider }) {
  const at = new Date().toISOString();
  const p = bare(proposal);
  return {
    learning: learningHash(p),
    cluster: p.cluster ?? null,
    kind: p.kind,
    statement: p.statement,
    appliesTo: p.appliesTo ?? [],
    decision: response === "document" ? "documented" : "approved",
    response,
    ...(response === "document" ? { document: p.edit?.document } : {}),
    witness,
    checkout: realpathSync.native(root),
    headAtPublish: headOf(root),
    publishedAt: at,
    decider,
    at,
    payload: { ...p, response },
  };
}

export const eventName = (e) => `${e.learning}--${e.at.replace(/\D/g, "").slice(0, 14)}-${slugify(e.decider)}.json`;

// The configured primary ref (`tasks.primaryRef` in .rhizome/config.yml),
// else origin/HEAD, as the tasks runner resolves it; null when neither is set.
function primaryRef(root) {
  try {
    const m = readFileSync(join(root, ".rhizome/config.yml"), "utf8").match(/^tasks:[ \t]*\r?\n(?:[ \t]+.*\r?\n)*?[ \t]+primaryRef:[ \t]*["']?([^"'\s#]+)/m);
    if (m) return m[1];
  } catch {}
  try {
    return git(root, ["symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"]).trim() || null;
  } catch {
    return null;
  }
}

// Where each approval's witness is. Branch tips come from for-each-ref and
// are probed in one `cat-file --batch-check`, the batched `cat-file -e`;
// history is never searched, since a deletion commit or a stash also holds
// the path.
export function standing(root, events) {
  const out = new Map();
  if (!events.length) return out;
  const primary = primaryRef(root);
  const branches = git(root, ["for-each-ref", "--format=%(refname)", "refs/heads"]).split("\n").filter(Boolean);
  const refs = [...(primary ? [primary] : []), ...branches];
  const queries = events.flatMap((e) => refs.map((r) => `${r}:${e.witness}`));
  const answers = execFileSync("git", ["cat-file", "--batch-check"], { cwd: root, input: queries.join("\n") + "\n", encoding: "utf8", maxBuffer: 256 << 20 }).split("\n");
  const hit = new Set(queries.filter((q, k) => /^[0-9a-f]{40,64} /.test(answers[k] ?? "")));
  const checkouts = new Set(worktrees(root));
  for (const e of events) {
    const at = (ref) => hit.has(`${ref}:${e.witness}`);
    const branch = branches.find(at);
    out.set(
      e,
      primary && at(primary)
        ? { status: "merged", ref: primary }
        : existsSync(join(root, e.witness))
          ? { status: "present" }
          : branch
            ? { status: "committed-elsewhere", branch: branch.replace(/^refs\/heads\//, "") }
            : checkouts.has(e.checkout) && existsSync(join(e.checkout, e.witness))
              ? { status: "published-elsewhere", checkout: e.checkout }
              : { status: "missing" },
    );
  }
  return out;
}

// `draft` marks an approval from before decision records whose approved
// wording could not be recovered: the payload is the draft it was approved from.
const earlier = (e) => ({ decision: e.decision, response: e.response, witness: e.witness, checkout: e.checkout, headAtPublish: e.headAtPublish, publishedAt: e.publishedAt, decider: e.decider, ...(e.payload?.exact === false ? { draft: true } : {}) });

// Proposals awaiting a decision, and approvals with where each stands. A
// finding whose action item is open suppresses its learning; a completed one
// suppresses it unless mining found that the friction recurred. A proposal
// whose local cluster no longer holds the verification it came from, because
// a recurrence cleared it or a later run replaced it, is no longer a draft.
// Only the newest approval of a cluster counts, and the approval of the
// finding a recurrence reopened does not cover the recurrence.
export function review(store, root) {
  const reviewed = store.read("reviewed.json", {});
  const clusters = store.read("pool/clusters.json", {});
  const current = (p) => !clusters[p.cluster] || (clusters[p.cluster].verification ?? clusters[p.cluster].verifyProblems)?.at === p.verified?.at;
  const latest = loadDecisions(root, store.dir);
  const suppressed = new Set(latest.filter((d) => !d.witness).map((d) => d.learning));
  for (const s of loadShards(root)) suppressed.add(learningHash(s));
  const done = new Set();
  for (const f of loadFindings(root)) (f.open ? suppressed : done).add(f.learning);
  const open = (p) => !suppressed.has(learningHash(p)) && (p.recurrence || !done.has(learningHash(p)));
  const events = latest.filter((d) => d.witness && d.payload).sort((a, b) => String(b.at).localeCompare(String(a.at)));
  const approvalOf = (p) => events.find((e) => (e.cluster === p.cluster || e.learning === learningHash(p)) && e.witness !== p.recurrence?.finding);
  const pending = store.read("proposals.json", []).filter((p) => current(p) && !approvalOf(p) && !reviewed[p.cluster] && open(p));
  const newest = [...new Map(events.map((e) => [e.cluster, e]).reverse()).values()];
  const where = standing(root, newest);
  const approved = [];
  for (const e of newest) {
    const w = where.get(e);
    if (w.status !== "missing") approved.push({ cluster: e.cluster, kind: e.kind, statement: e.statement, response: e.response, ...w, approval: earlier(e), proposal: e.payload });
    else if (!pending.some((p) => p.cluster === e.cluster) && open(e.payload)) pending.push({ ...e.payload, status: "missing", previous: earlier(e) });
  }
  return { pending, approved };
}

export const pending = (store, root) => review(store, root).pending;

// The proposal a verified cluster offers for review, with its cited episodes
// from the pool.
export function proposalOf(cl, eps) {
  const broken = cl.verifyProblems;
  const v = cl.verification ?? { decision: "reject", rejectReason: "contract", notes: broken.problems.join("; "), run: broken.run, at: broken.at };
  // A rejection drafts nothing, so no response field it carried is proposed.
  const r = v.decision === "reject" ? {} : v;
  return {
    cluster: cl.id,
    tier: cl.tier,
    families: cl.families.length,
    familyIds: cl.families,
    rejected: v.decision === "reject" ? { reason: v.rejectReason ?? "other", notes: v.notes } : undefined,
    kind: v.kind ?? cl.kind,
    statement: v.statement ?? cl.statement,
    why: v.why ?? cl.why,
    appliesTo: v.appliesTo ?? cl.appliesTo,
    commands: v.commands ?? cl.commands,
    target: v.target ?? cl.target,
    response: r.response,
    responseReason: r.responseReason,
    workaround: r.workaround,
    edit: r.edit,
    documentHash: r.documentHash,
    change: r.change,
    guardRule: r.guardRule,
    question: r.question,
    owner: r.owner,
    recurrence: cl.recurrence,
    documentation: v.documentation,
    verification: v.verification,
    notes: v.notes,
    evidence: cl.evidence.map((id) => eps[id]).filter(Boolean).map((e) => ({ episode: e.id, harness: e.harness, session: e.session, ts: e.ts, lens: e.lens, origin: e.origin, toolCalls: e.toolCalls ?? 0 })),
    verified: { run: v.run, commit: v.commit, model: v.model, at: v.at },
  };
}
