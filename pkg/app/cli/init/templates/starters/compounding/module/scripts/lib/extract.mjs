// Extraction: batches of screened episodes become candidate learnings. The
// validator, not the model, enforces the schema and limits; one repair call
// fixes what it reports, and anything still invalid is dropped.

import { MAP_PROMPT } from "./prompts.mjs";
import { sha } from "./store.mjs";

export const BATCH_CHARS = 45000;
export const STATEMENT_MAX = 400;

// Batches keep a session's episodes together and never split a session
// across batches unless the session alone exceeds the limit.
export function batches(episodes, maxChars = BATCH_CHARS) {
  const bySession = new Map();
  for (const e of episodes) bySession.set(e.session, [...(bySession.get(e.session) ?? []), episodeView(e)]);
  const out = [];
  let cur = [];
  let size = 0;
  for (const views of bySession.values()) {
    const len = views.reduce((s, v) => s + JSON.stringify(v).length, 0);
    if (cur.length && size + len > maxChars) {
      out.push(cur);
      cur = [];
      size = 0;
    }
    for (const v of views) {
      const vl = JSON.stringify(v).length;
      if (cur.length && size + vl > maxChars) {
        out.push(cur);
        cur = [];
        size = 0;
      }
      cur.push(v);
      size += vl;
    }
  }
  if (cur.length) out.push(cur);
  return out;
}

export function episodeView(e) {
  return {
    id: e.id,
    lens: e.lens,
    origin: e.origin,
    session: e.session,
    title: e.context?.title || undefined,
    task: e.context?.task || undefined,
    previous: e.context?.previous || undefined,
    text: e.text,
    filesRead: e.files?.reads?.length ? e.files.reads : undefined,
    filesEdited: e.files?.edits?.length ? e.files.edits : undefined,
    toolCalls: e.toolCalls || undefined,
  };
}

export const LEARNING_FIELDS = {
  kind: { enum: ["rule", "fact", "where_to_find"] },
  statement: { type: "string" },
  why: { type: "string" },
  appliesTo: { type: "array", items: { type: "string" } },
  commands: { type: "array", items: { type: "string" } },
  target: { type: "string" },
};

export const CANDIDATE_SCHEMA = {
  type: "object",
  properties: {
    candidates: {
      type: "array",
      items: {
        type: "object",
        properties: { ...LEARNING_FIELDS, evidence: { type: "array", items: { type: "string" }, minItems: 1 }, strength: { enum: ["strong", "moderate", "weak"] } },
        required: ["kind", "statement", "why", "appliesTo", "commands", "evidence", "strength"],
      },
    },
  },
  required: ["candidates"],
};

const strings = (v) => Array.isArray(v) && v.every((x) => typeof x === "string");

// Problems with one learning, each phrased as the fix to make. Evidence is
// checked only when the caller knows the valid episode ids.
export function validateLearning(c, { maxBytes = STATEMENT_MAX, episodeIds = null, candidate = true } = {}) {
  const problems = [];
  if (!c || typeof c !== "object") return ["return an object"];
  if (!LEARNING_FIELDS.kind.enum.includes(c.kind)) problems.push(`kind must be one of ${LEARNING_FIELDS.kind.enum.join(", ")}`);
  if (typeof c.statement !== "string" || !c.statement.trim()) problems.push("statement must be a non-empty string");
  else if (Buffer.byteLength(c.statement, "utf8") > maxBytes) problems.push(`statement is ${Buffer.byteLength(c.statement, "utf8")} bytes; the limit is ${maxBytes}`);
  if (candidate && (typeof c.why !== "string" || !c.why.trim())) problems.push("why must be a non-empty string");
  if (!strings(c.appliesTo ?? [])) problems.push("appliesTo must be a list of strings");
  else for (const g of c.appliesTo ?? []) if (/[{}\[\]!]/.test(g)) problems.push(`pattern ${g} uses syntax outside *, **, ?`);
  if (!strings(c.commands ?? [])) problems.push("commands must be a list of strings");
  if (c.kind === "where_to_find" && !c.target) problems.push("where_to_find needs a target");
  if (candidate) {
    if (!strings(c.evidence) || !c.evidence.length) problems.push("evidence must list at least one episode id");
    else if (episodeIds) {
      const bad = c.evidence.filter((id) => !episodeIds.has(id));
      if (bad.length) problems.push(`unknown evidence ids: ${bad.join(", ")}`);
    }
    if (!["strong", "moderate", "weak"].includes(c.strength)) problems.push("strength must be strong, moderate, or weak");
  }
  return problems;
}

// A candidate is named by what it cites, not by its wording, so a reworded
// rerun keeps its id. The ordinal separates learnings that cite the same
// episodes.
export function nameCandidates(candidates) {
  const seen = new Map();
  return candidates.map((c) => {
    const basis = JSON.stringify([c.kind, [...c.evidence].sort()]);
    const n = seen.get(basis) ?? 0;
    seen.set(basis, n + 1);
    return { id: `c-${sha([basis, n]).slice(0, 16)}`, ...c };
  });
}

export async function extractBatch(caller, batch, { estimate = 0, repairEstimate = 0, tally } = {}) {
  const ids = new Set(batch.map((e) => e.id));
  const prompt = `${MAP_PROMPT}\n\n<episodes>\n${batch.map((e) => JSON.stringify(e)).join("\n")}\n</episodes>`;
  const out = await caller.generate({ role: "extract", stage: "extract", prompt, schema: CANDIDATE_SCHEMA, estimate, tally });
  const candidates = Array.isArray(out?.candidates) ? [...out.candidates] : [];
  const bad = candidates.map((c, i) => ({ i, problems: validateLearning(c, { episodeIds: ids }) })).filter((x) => x.problems.length);
  // Repair only the faulty candidates, at low effort.
  if (bad.length) {
    const fix = await caller.generate({
      role: "extract",
      stage: "extract-repair",
      effort: "low",
      prompt: `Fix these candidate learnings so each satisfies its listed problems, keeping their meaning. Return them in the same order.\n\n${bad.map((b) => JSON.stringify({ candidate: candidates[b.i], problems: b.problems })).join("\n")}`,
      schema: CANDIDATE_SCHEMA,
      estimate: repairEstimate,
      tally,
    });
    (fix?.candidates ?? []).forEach((c, j) => bad[j] && (candidates[bad[j].i] = c));
  }
  const valid = candidates.filter((c) => !validateLearning(c, { episodeIds: ids }).length);
  return { candidates: nameCandidates(valid), repaired: bad.length, dropped: candidates.length - valid.length };
}
