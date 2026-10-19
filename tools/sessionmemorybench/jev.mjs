// Jev calls for the benchmark and the Jev lab, through the starter's
// `rzm agent evaluate-batch` client, so credentials stay with Rhizome.

import { CLIENTS } from "./shipped.mjs";

// The episode fields a whole-episode screening question sees.
export function screenState(e) {
  return { lens: e.lens, sessionKind: e.sessionKind, origin: e.origin, previous: e.context?.previous || undefined, sessionTitle: e.context?.title || undefined, message: e.text, filesRead: e.files?.reads?.slice(0, 15), toolCalls: e.toolCalls || undefined };
}

// Runs Jev items in chunks and returns each item's answers, or null when it failed.
export async function jevBatch(items, spend) {
  const out = new Map();
  for (let k = 0; k < items.length; k += 32) {
    const j = await CLIENTS.evaluateBatch({ items: items.slice(k, k + 32), concurrency: 8, budgetMs: 60000 });
    spend.inputTokens += j.usage?.input_tokens ?? 0;
    spend.outputTokens += j.usage?.output_tokens ?? 0;
    for (const it of j.items ?? []) out.set(it.id, it.status === "succeeded" ? it.response?.answers ?? null : null);
  }
  return out;
}
