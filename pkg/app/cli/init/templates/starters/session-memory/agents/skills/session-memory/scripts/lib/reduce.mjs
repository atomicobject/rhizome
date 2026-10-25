// Grouping and tiers. New candidates are grouped against the clusters earlier
// runs left, so a later run adds members instead of reshuffling proposals
// awaiting review. Cluster ids come from evidence, never from generated prose.

import { LEARNING_FIELDS } from "./extract.mjs";
import { GROUP_EXISTING, GROUP_PROMPT, RANK_QUESTIONS } from "./prompts.mjs";
import { Stop } from "./calls.mjs";
import { sha } from "./store.mjs";

export const CHUNK = 300;
const EXISTING_CHARS = 150000;
// Jev `checkable` at or above this marks a single-family cluster tier B. On
// D-v1's clusters it keeps 322 of 327 extractor tier B and 27 of 51 tier C.
export const CHECKABLE_MIN = 0.72;

export const GROUP_SCHEMA = {
  type: "object",
  properties: {
    clusters: {
      type: "array",
      items: {
        type: "object",
        properties: { existing: { type: "string" }, members: { type: "array", items: { type: "string" }, minItems: 1 }, ...LEARNING_FIELDS, checkable: { enum: ["code", "environment", "workflow"] } },
        required: ["members", "kind", "statement", "appliesTo", "commands", "checkable"],
      },
    },
  },
  required: ["clusters"],
};

// Sort so candidates naming the same identifier sit together; the grouper
// fragments learnings less when duplicates are adjacent.
export function identifierKey(c) {
  const tick = (c.statement.match(/`([^`]{3,80})`/) ?? [])[1];
  return (tick ?? c.commands?.[0] ?? c.appliesTo?.[0] ?? c.statement.slice(0, 40)).toLowerCase();
}

// A new cluster is named by its earliest-cited member that no other cluster
// is named by, so new members and rewording leave the id alone.
export function clusterId(members, firstSeen, taken) {
  const order = [...members].sort((x, y) => firstSeen(x) - firstSeen(y) || (x < y ? -1 : x > y ? 1 : 0));
  for (const m of order) {
    const id = `k-${sha(m).slice(0, 12)}`;
    if (!taken.has(id)) return id;
  }
  return `k-${sha(order).slice(0, 12)}`;
}

const view = (c) => ({ id: c.id, kind: c.kind, statement: c.statement, appliesTo: c.appliesTo, commands: c.commands });
const cmp = (x, y) => (x < y ? -1 : x > y ? 1 : 0);

// Groups `fresh` candidate ids in chunks, saving clusters after each chunk.
// Candidates the grouper drops become singletons so nothing is lost silently.
export async function group(caller, { candidates, clusters, fresh, firstSeen, save, estimate = () => 0, tally }) {
  const clustered = () => new Set(Object.values(clusters).flatMap((c) => c.members));
  for (;;) {
    const done = clustered();
    const pending = fresh.filter((id) => !done.has(id)).map((id) => candidates[id]).sort((x, y) => cmp(identifierKey(x), identifierKey(y)) || cmp(x.id, y.id));
    if (!pending.length) return clusters;
    const chunk = pending.slice(0, CHUNK);
    // ponytail: earlier clusters are cut at a character cap, newest kept; a lexical shortlist if pools outgrow it.
    let existing = Object.values(clusters).sort((x, y) => (y.seq ?? 0) - (x.seq ?? 0) || cmp(x.id, y.id)).map(view);
    let chars = 0;
    existing = existing.filter((v) => (chars += JSON.stringify(v).length) <= EXISTING_CHARS);
    const prompt = [GROUP_PROMPT, existing.length ? GROUP_EXISTING : null, existing.length ? `<existing-clusters>\n${existing.map((v) => JSON.stringify(v)).join("\n")}\n</existing-clusters>` : null, `<candidates>\n${chunk.map((c) => JSON.stringify(view(c))).join("\n")}\n</candidates>`].filter(Boolean).join("\n\n");
    const out = await caller.generate({ role: "synthesize", stage: "group", prompt, schema: GROUP_SCHEMA, estimate: estimate(chunk.length, existing.length), tally });
    const ids = new Set(chunk.map((c) => c.id));
    const at = new Date().toISOString();
    // Creation order, not wall time, orders the existing view, so a replay rebuilds identical prompts.
    let seq = Math.max(0, ...Object.values(clusters).map((c) => c.seq ?? 0));
    for (const cl of out?.clusters ?? []) {
      const members = [...new Set((cl.members ?? []).filter((id) => ids.has(id)))];
      if (!members.length) continue;
      const target = cl.existing && clusters[cl.existing];
      if (target) {
        target.members = [...new Set([...target.members, ...members])];
        target.updatedAt = at;
        continue;
      }
      const id = clusterId(members, firstSeen, new Set(Object.keys(clusters)));
      // The driver checks only top-level fields, so a thin cluster borrows its first member's.
      const first = candidates[members[0]];
      const statement = typeof cl.statement === "string" && cl.statement.trim() ? cl.statement : first.statement;
      clusters[id] = { id, seq: ++seq, members, kind: cl.kind ?? first.kind, statement, why: cl.why ?? first.why, appliesTo: cl.appliesTo ?? first.appliesTo ?? [], commands: cl.commands ?? first.commands ?? [], target: cl.target ?? first.target, checkable: cl.checkable, createdAt: at };
    }
    const now = clustered();
    for (const c of chunk) {
      if (now.has(c.id)) continue;
      const id = clusterId([c.id], firstSeen, new Set(Object.keys(clusters)));
      clusters[id] = { id, seq: ++seq, members: [c.id], kind: c.kind, statement: c.statement, why: c.why, appliesTo: c.appliesTo, commands: c.commands, target: c.target, checkable: "code", ungrouped: true, createdAt: at };
    }
    save(clusters);
  }
}

// Families and evidence behind a cluster, from its members' episodes.
export function facts(cl, candidates, episodes) {
  const evidence = [...new Set(cl.members.flatMap((id) => candidates[id]?.evidence ?? []))];
  const eps = evidence.map((id) => episodes[id]).filter(Boolean);
  return {
    evidence,
    families: [...new Set(eps.map((e) => e.family))],
    confirmedCorrection: eps.some((e) => e.lens === "correction" && e.origin === "person" && e.confidence === "confirmed"),
  };
}

// Tier A: two or more families, or a confirmed person's correction. Tier B:
// one family, a claim the code can confirm. Tier C: held for another family.
// Without Jev, the grouper's own checkable label decides B against C.
export function tierOf(f, cl, rank) {
  if (f.families.length >= 2 || f.confirmedCorrection) return "A";
  const checkable = rank?.checkable;
  return (typeof checkable === "number" ? checkable >= CHECKABLE_MIN : cl.checkable === "code") ? "B" : "C";
}

export const RANK_JEV = Object.fromEntries(Object.entries(RANK_QUESTIONS).map(([k, q]) => [k, { type: "noul", instructions: q }]));

// Counts and tiers are left out, so Jev judges only what the learning says.
export function rankState(cl, candidates) {
  const members = cl.members.map((id) => candidates[id]).filter(Boolean);
  return { kind: cl.kind, statement: cl.statement, why: members[0]?.why, appliesTo: cl.appliesTo, commands: cl.commands?.length ? cl.commands : undefined, otherWordings: members.slice(1, 4).map((c) => c.statement) };
}

export async function rank(caller, list, candidates, { estimatePerItem = 0 } = {}) {
  const answers = await caller.evaluate({ stage: "rank", items: list.map((cl) => ({ id: cl.id, state: rankState(cl, candidates), questions: RANK_JEV })), estimatePerItem });
  // Partial ranks would change tiers and freeze them into the verify plan.
  const missing = list.filter((cl) => !answers.has(cl.id)).length;
  if (missing) throw new Stop("failed", `${missing} of ${list.length} clusters are unranked; rerun to retry them`);
  return new Map(list.map((cl) => [cl.id, Object.fromEntries(Object.keys(RANK_JEV).map((k) => [k, answers.get(cl.id)?.[k]?.noul ?? null]))]));
}
