#!/usr/bin/env node
// The friction report (SPEC-0124 US7): open fix, guard, and ask findings and
// pending proposals, ranked by the agent effort behind them, and completed
// findings marked resolved or recurring. It reads only local state and
// tracked notes, makes no model calls, and shows no transcript text.
//
//   report.mjs [--state DIR]
//
// Recurrence comes from the last mine run, which records the episodes that
// matched each finding; `checkedThrough` says when that was.

import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { loadFindings } from "./lib/findings.mjs";
import { repoRoot } from "./lib/shards.mjs";
import { openStore } from "./lib/store.mjs";
import { pending } from "./shards.mjs";

const day = (ts) => String(ts ?? "").slice(0, 10) || null;

function effort(families, evidence) {
  const days = evidence.map((e) => day(e.ts)).filter(Boolean).sort();
  return { families, sessions: new Set(evidence.map((e) => e.session)).size, toolCalls: evidence.reduce((t, e) => t + (e.toolCalls ?? 0), 0), firstSeen: days[0] ?? null, lastSeen: days.at(-1) ?? null };
}

export function report({ root = repoRoot(), state } = {}) {
  const store = openStore(state ?? join(root, ".rhizome/session-memory"));
  const record = store.read("findings.json", {});
  const links = store.read("finding-links.json", {});
  const open = [];
  const completed = [];
  for (const f of loadFindings(root)) {
    const r = record[f.path]?.completedAt === f.completedAt ? record[f.path] : null;
    if (f.open) {
      // Approved evidence plus the episodes mining matched since approval.
      // Families are a union of identities where this checkout approved the
      // finding; a teammate's finding records only a count, so its later
      // families are added to it. A finding approved before family IDs were
      // recorded has only an approximate count, and says so.
      const later = (r?.episodes ?? []).filter((e) => !f.evidence.some((x) => x.episode === e.id));
      const known = links[f.path]?.families;
      const families = known ? new Set([...known, ...later.map((e) => e.family)]).size : f.families + new Set(later.map((e) => e.family)).size;
      const approximate = !known && links[f.path]?.familiesApproximate ? { familiesApproximate: true } : {};
      open.push({ finding: f.path, response: f.response, statement: f.statement, ...effort(families, [...f.evidence, ...later]), ...approximate });
    } else {
      const episodes = (r?.episodes ?? []).map((e) => ({ id: e.id, date: day(e.ts) }));
      completed.push({ finding: f.path, response: f.response, statement: f.statement, completedOn: day(f.completedAt), status: episodes.length ? "recurring" : "resolved", episodes, checkedThrough: day(r?.checkedAt) });
    }
  }
  for (const p of pending(store, root)) open.push({ proposal: p.cluster, response: p.response ?? null, statement: p.statement, ...effort(p.families ?? 0, p.evidence ?? []) });
  open.sort((a, b) => b.families - a.families || b.sessions - a.sessions || b.toolCalls - a.toolCalls);
  completed.sort((a, b) => (a.status === b.status ? 0 : a.status === "recurring" ? -1 : 1));
  return { open, completed };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const k = process.argv.indexOf("--state");
  console.log(JSON.stringify(report({ state: k > 0 ? process.argv[k + 1] : undefined }), null, 2));
}
