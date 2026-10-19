// Passage screening (SPEC-0121 "Screening"). Jev answers two questions about
// each paragraph or list item; the extraction model sees only the best
// passages, up to a share of the characters.

import { Stop } from "./calls.mjs";
import { PASSAGE_QUESTIONS } from "./prompts.mjs";

export const SHARE = 0.3;

// Long reports bury one useful bullet among many, so each paragraph or list
// item is judged alone. Short pieces join the next one.
export function passages(text, max = 700) {
  const parts = text.split(/\n\s*\n|\n(?=\s*(?:[-*]|\d+\.)\s)/).map((p) => p.trim()).filter(Boolean);
  const out = [];
  let cur = "";
  for (const p of parts) {
    if (cur && cur.length + p.length > max) {
      out.push(cur);
      cur = "";
    }
    cur = cur ? `${cur}\n${p}` : p;
    if (cur.length >= 120) {
      out.push(cur.slice(0, max));
      cur = "";
    }
  }
  if (cur) out.push(cur);
  return out.length ? out : [text.slice(0, max)];
}

// The state a passage question sees.
export function units(e) {
  return passages(e.text).map((text, k) => ({ id: `${e.id}#${k}`, k, state: { lens: e.lens, origin: e.origin, sessionTitle: e.context?.title || undefined, passage: text } }));
}

// Keeps the best passages up to `share` of all characters and rebuilds each
// kept episode from its passages, in order. Unscored passages rank last.
export function gate(episodes, scoreOf, share = SHARE) {
  const total = episodes.reduce((t, e) => t + e.text.length, 0);
  const all = episodes.flatMap((e) => units(e).map((u) => ({ e, k: u.k, text: u.state.passage, score: scoreOf(u) ?? -1 })));
  all.sort((x, y) => y.score - x.score);
  const keep = new Map();
  let acc = 0;
  for (const p of all) {
    if (acc + p.text.length > share * total) continue;
    acc += p.text.length;
    keep.set(p.e.id, [...(keep.get(p.e.id) ?? []), p]);
  }
  return episodes.filter((e) => keep.has(e.id)).map((e) => ({ ...e, text: keep.get(e.id).sort((x, y) => x.k - y.k).map((p) => p.text).join("\n[...]\n") }));
}

export const JEV_QUESTIONS = Object.fromEntries(Object.entries(PASSAGE_QUESTIONS).map(([id, instructions]) => [id, { type: "noul", instructions }]));

export function screenItems(episodes) {
  return episodes.flatMap(units).map((u) => ({ id: u.id, state: u.state, questions: JEV_QUESTIONS }));
}

// Scores every passage with Jev and gates. Unscored passages stop the run so
// a later run retries them instead of gating on partial scores.
export async function screen(caller, episodes, { share = SHARE, estimatePerItem = 0 } = {}) {
  const items = screenItems(episodes);
  const answers = await caller.evaluate({ stage: "screen", items, estimatePerItem });
  const missing = items.filter((i) => !answers.has(i.id)).length;
  if (missing) throw new Stop("failed", `${missing} of ${items.length} passages are unscored; rerun to retry them`);
  const score = (u) => Math.max(...Object.keys(JEV_QUESTIONS).map((q) => answers.get(u.id)?.[q]?.noul ?? -1));
  const best = new Map(episodes.map((e) => [e.id, Math.max(...units(e).map(score))]));
  return { gated: gate(episodes, score, share), passages: items.length, best };
}
