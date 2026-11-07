/** Saved AO output only: parseSavedAnalysis(text) validates a weights document;
 * savedAssertion(snapshot, id, updatedAt?) returns its verbatim assessment.
 * readSavedAnalysis(readText?) loads .kb/index/kb-weights.json on demand.
 * No band computation or inferred freshness. Invalid/unreadable input is unavailable.
 */
import { readVaultText } from './passage.ts';
import type { ReadText } from './passage.ts';
export type SavedEntry = { slug: string; band: string; basis: string; stored: string; inputs: Record<string, unknown> };
export type SavedAnalysis = { meta: { asOf?: string; generatedAt?: string; builder?: string }; assertions: Record<string, SavedEntry>; concepts: Record<string, unknown>; artifacts: Record<string, unknown> };
export type SavedAssessment = { status: 'unavailable' } | { status: 'not-in-snapshot' } | ({ status: 'available'; band: string; basis: string; mayBeStale?: true } & SavedAnalysis['meta']);
function record(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
export function parseSavedAnalysis(text: string | undefined): SavedAnalysis | undefined {
  if (text === undefined) return undefined;
  try {
    const doc: unknown = JSON.parse(text);
    if (!record(doc) || !record(doc.meta) || !record(doc.assertions) || !record(doc.concepts) || !record(doc.artifacts)) return undefined;
    const meta: SavedAnalysis['meta'] = {};
    for (const key of ['asOf', 'generatedAt', 'builder'] as const) {
      const value = doc.meta[key];
      if (value !== undefined && typeof value !== 'string') return undefined;
      if (value !== undefined) meta[key] = value;
    }
    const assertions: Record<string, SavedEntry> = Object.create(null);
    for (const [id, entry] of Object.entries(doc.assertions)) {
      if (!record(entry) || typeof entry.slug !== 'string' || typeof entry.band !== 'string' || typeof entry.basis !== 'string' || typeof entry.stored !== 'string' || !record(entry.inputs)) return undefined;
      assertions[id] = { slug: entry.slug, band: entry.band, basis: entry.basis, stored: entry.stored, inputs: entry.inputs };
    }
    return { meta, assertions, concepts: doc.concepts, artifacts: doc.artifacts };
  } catch { return undefined; }
}
export function savedAssertion(snapshot: SavedAnalysis | undefined, id: string, updatedAt?: string): SavedAssessment {
  if (!snapshot) return { status: 'unavailable' };
  const entry = Object.hasOwn(snapshot.assertions, id) ? snapshot.assertions[id] : undefined;
  if (!entry) return { status: 'not-in-snapshot' };
  const asOf = snapshot.meta.asOf;
  const authored = comparableDate(updatedAt);
  const assessed = comparableDate(asOf);
  return { status: 'available', band: entry.band, basis: entry.basis, ...snapshot.meta, ...(authored > assessed ? { mayBeStale: true as const } : {}) };
}
function comparableDate(value: string | undefined): number {
  if (!value || !/^\d{4}-\d{2}-\d{2}(?:T.*)?$/.test(value)) return NaN;
  const day = value.slice(0, 10), midnight = Date.parse(day);
  if (!Number.isFinite(midnight) || new Date(midnight).toISOString().slice(0, 10) !== day) return NaN;
  return Date.parse(value);
}
export async function readSavedAnalysis(readText: ReadText = readVaultText): Promise<SavedAnalysis | undefined> {
  try { return parseSavedAnalysis(await readText('.kb/index/kb-weights.json')); } catch { return undefined; }
}
