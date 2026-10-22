// Agent findings (SPEC-0124 "Tracked data"): a fix, guard, or ask response a
// person approved, as one note with one action item in
// docs/memory/agent-findings/. Only shards.mjs writes them. Mining records
// which later episodes match each finding, so the friction report can show
// whether a completed finding stayed resolved (SPEC-0124 US7).

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { git, learningHash, list, parseFrontmatter, slugify, titleOf } from "./shards.mjs";

export const FINDING_DIR = "docs/memory/agent-findings";
const ACTION = /^- \[([ xX])\] .*#action-item\b/m;
const EVIDENCE = /session `([^`]+)`, message `([^`]+)`, ([\d-]*), \w+, (\d+) tool calls/g;
const VERB = { fix: "Fix", guard: "Add a guard", ask: "Answer" };

// A free path named by response and slug. A completed finding that recurs
// can be approved again, so a taken name gets a numeric suffix.
export function findingPath(root, response, statement) {
  const base = `${FINDING_DIR}/${response}-${slugify(titleOf(statement))}`;
  for (let n = 1; ; n++) {
    const path = n === 1 ? `${base}.md` : `${base}-${n}.md`;
    if (!existsSync(join(root, path))) return path;
  }
}

const q = (s) => JSON.stringify(String(s));

export function renderFinding(f) {
  const fm = ["---", `response: ${f.response}`, `kind: ${f.kind}`, `summary: ${q(f.statement)}`, `learning-hash: ${f.learning}`];
  if (f.appliesTo?.length) fm.push("applies-to:", ...f.appliesTo.map((x) => `  - ${q(x)}`));
  // JSON is YAML flow syntax, so the hook dispatcher reads the rule as a map.
  if (f.guardRule) fm.push(`guard-rule: ${JSON.stringify(f.guardRule)}`);
  fm.push(`reviewed-by: ${q(f.reviewedBy)}`, `reviewed-on: ${f.reviewedOn}`, `families: ${f.families ?? 0}`, `tool-calls: ${f.toolCalls ?? 0}`, "---", "");
  const body = [`# ${titleOf(f.statement)}`, "", f.statement, "", "## Why it matters", "", f.why || "(not recorded)", ""];
  if (f.response === "ask") body.push("## Question", "", f.question, ...(f.owner ? ["", `Owner: ${f.owner}`] : []), "");
  else body.push("## Proposed change", "", f.change, "");
  body.push("## Evidence", "", ...(f.evidenceLines ?? []), "", "## Action item", "", `- [ ] ${VERB[f.response]}: ${titleOf(f.statement)} #action-item`, `  assignee:: ${f.assignee}`, "");
  return [...fm, ...body].join("\n");
}

function guardRule(raw) {
  if (!raw || typeof raw !== "string") return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

const checked = (text) => /[xX]/.test(text?.replace(/\r\n/g, "\n").match(ACTION)?.[1] ?? "");

// When the action item was checked: the latest commit in which it went from
// unchecked (or absent) to checked, or the file's modification time while
// that check is uncommitted. Editing an already-checked line does not move it.
function completedAt(root, path) {
  const at = (rev) => {
    try {
      return git(root, ["show", `${rev}:${path}`]);
    } catch {
      return null;
    }
  };
  if (checked(at("HEAD"))) {
    try {
      const log = git(root, ["log", "--format=%H %cI", "--", path]).trim().split("\n").map((l) => l.split(" "));
      const k = log.findIndex((_, i) => !checked(log[i + 1] && at(log[i + 1][0])));
      if (k >= 0) return new Date(log[k][1]).toISOString();
    } catch {}
  }
  return statSync(join(root, path)).mtime.toISOString();
}

export function loadFindings(root) {
  const dir = join(root, FINDING_DIR);
  if (!existsSync(dir)) return [];
  const out = [];
  for (const name of readdirSync(dir).filter((f) => f.endsWith(".md")).sort()) {
    const path = `${FINDING_DIR}/${name}`;
    const text = readFileSync(join(root, path), "utf8");
    const parsed = parseFrontmatter(text);
    if (!parsed) continue;
    const { fm, section } = parsed;
    // A finding without a checked action item counts as open, so it suppresses.
    const box = text.replace(/\r\n/g, "\n").match(ACTION)?.[1];
    const open = !box || box === " ";
    out.push({
      path,
      response: fm.response,
      kind: fm.kind,
      statement: fm.summary,
      learning: fm["learning-hash"] || learningHash({ kind: fm.kind, statement: fm.summary }),
      appliesTo: list(fm["applies-to"]),
      guardRule: guardRule(fm["guard-rule"]),
      reviewedOn: fm["reviewed-on"],
      families: Number(fm.families ?? 0),
      toolCalls: Number(fm["tool-calls"] ?? 0),
      evidence: [...section("Evidence").matchAll(EVIDENCE)].map((m) => ({ session: m[1], episode: m[2], ts: m[3], toolCalls: Number(m[4]) })),
      open,
      completedAt: open ? null : completedAt(root, path),
    });
  }
  return out;
}

// Records the episodes that matched each finding after it was approved, or,
// once its action item is complete, after completion. A finding's clusters
// are its settled cluster and the local clusters a reviewer turned into it,
// kept in `links` (finding-links.json) apart from review status, since
// reopening clears that status. A completed finding with episodes not
// offered before recurs: the clusters holding them are reopened for review
// and verified again against the new evidence. The caller saves the
// clusters, links, and reviewed.json before the returned record, so a crash
// cannot lose a reopening.
export function recur(prior, { findings, clusters, candidates, episodes, reviewed, links, now = new Date().toISOString() }) {
  const next = {};
  const reopened = new Set();
  for (const f of findings) {
    // Findings approved before links existed are linked through reviewed.json.
    const linked = new Set([...(links[f.path]?.clusters ?? []), ...Object.keys(reviewed).filter((id) => reviewed[id].path === f.path)]);
    if (linked.size) links[f.path] = { ...links[f.path], clusters: [...linked].sort() };
    const ids = [`k-settled-${f.learning.slice(0, 12)}`, ...linked].filter((id) => clusters[id]);
    const since = f.completedAt ?? `${f.reviewedOn}T00:00:00.000Z`;
    const frozen = new Set(f.evidence.map((e) => e.episode));
    const matched = new Map();
    for (const id of ids) {
      for (const m of clusters[id].members) {
        for (const e of candidates[m]?.evidence ?? []) {
          const ep = episodes[e];
          if (ep && !frozen.has(ep.id) && Date.parse(ep.ts) > Date.parse(since)) matched.set(ep.id, { id: ep.id, session: ep.session, family: ep.family, ts: ep.ts, toolCalls: ep.toolCalls ?? 0, cluster: id });
        }
      }
    }
    const eps = [...matched.values()].sort((a, b) => String(a.ts).localeCompare(String(b.ts)));
    const was = prior[f.path]?.completedAt === f.completedAt ? new Set(prior[f.path].offered ?? []) : new Set();
    const fresh = f.completedAt ? eps.filter((e) => !was.has(e.id)) : [];
    for (const id of new Set(fresh.map((e) => e.cluster))) {
      delete reviewed[id];
      delete clusters[id].settled;
      // The friction came back, so the earlier verification and draft no longer stand.
      delete clusters[id].verification;
      delete clusters[id].verifyProblems;
      // `fresh` holds the episodes not offered before, which verification reads first.
      clusters[id].recurrence = { finding: f.path, completedAt: f.completedAt, episodes: eps.map((e) => ({ id: e.id, ts: e.ts })), fresh: fresh.map((e) => e.id) };
      reopened.add(id);
    }
    next[f.path] = { completedAt: f.completedAt, since, episodes: eps, offered: f.completedAt ? eps.map((e) => e.id) : [], checkedAt: now };
  }
  return { reopened, record: next };
}
