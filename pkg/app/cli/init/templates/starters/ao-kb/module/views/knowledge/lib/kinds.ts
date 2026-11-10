/** Kind groups and stored-confidence buckets for the Knowledge site (SPEC-0135 R3).
 *
 * A kind group is a Rhizome presentation convention that collects raw kinds, current
 * and AO's retired names alike. It never judges truth, strength, or resolution, and
 * never touches confidence. Pure and kit-free so `node --test` can import it. */

export type GroupId = "statements" | "intent" | "risks" | "other";
export type KindGroup = { id: GroupId; label: string; kinds: string[]; legacy: string[] };

export const KIND_GROUPS: KindGroup[] = [
  { id: "statements", label: "Statements", kinds: ["fact", "observation", "need"], legacy: ["tribal-knowledge", "customer-voice"] },
  // `principle` is a custom AO kind, placed here by Drew on 2026-11-09 (R3).
  { id: "intent", label: "Intent and rules", kinds: ["decision", "rule", "preference", "goal", "principle"], legacy: ["constraint", "trade-off"] },
  { id: "risks", label: "Risks and questions", kinds: ["risk", "hypothesis", "question"], legacy: ["assumption"] },
  { id: "other", label: "Other kinds", kinds: [], legacy: [] },
];
export const NO_KIND = "Kind not recorded";

/** The group of a raw kind. A missing or custom kind is "Other kinds". */
export function groupOf(kind: string | null | undefined): GroupId {
  return KIND_GROUPS.find((g) => !!kind && (g.kinds.includes(kind) || g.legacy.includes(kind)))?.id ?? "other";
}

/** The stored-confidence bucket the site counts: exactly these raw values, never `high` or any other. */
export const STORED_CONFIDENCE = ["hypothesis", "guess"] as const;
export const STORED_LABEL = "Stored confidence: hypothesis or guess";
/** Values AO documents; anything else is named by the confidence-other gap. */
export const KNOWN_CONFIDENCE = ["validated", "hypothesis", "guess"];
export const isStored = (confidence: string | null | undefined) =>
  (STORED_CONFIDENCE as readonly (string | null | undefined)[]).includes(confidence);

export type KindFields = { path: string; kind?: string | null; legacyKind?: string | null; confidence?: string | null };

/** Both `kind` and `assertionKind` recorded, with different values; `kind` is shown (R2). */
export const kindConflict = (a: KindFields) => a.kind != null && a.legacyKind != null && a.kind !== a.legacyKind;

export const cmp = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

/** US4: the Assertions list's one-line breakdown of the rows it shows. `kind: null`
 * is "Kind not recorded" and sets no param. */
export function breakdown(rows: KindFields[]) {
  const kinds = new Map<string | null, number>();
  for (const a of rows) kinds.set(a.kind ?? null, (kinds.get(a.kind ?? null) ?? 0) + 1);
  const groups = KIND_GROUPS.map((g) => {
    const ks = [...kinds]
      .filter(([k]) => groupOf(k) === g.id)
      .map(([kind, count]) => ({ kind, count }))
      .sort((x, y) => y.count - x.count || cmp(x.kind ?? "", y.kind ?? ""));
    return { id: g.id, label: g.label, count: ks.reduce((n, k) => n + k.count, 0), kinds: ks };
  }).filter((g) => g.count);
  const confidence = STORED_CONFIDENCE.map((value) => ({ value, count: rows.filter((a) => a.confidence === value).length }));
  return { groups, confidence };
}

const CONFIDENCE_ORDER = ["guess", "hypothesis", "validated"];
/** Order for a list "ordered by stored confidence": guess, hypothesis, validated, then any other value. */
export function byStoredConfidence(a: KindFields & { title?: string }, b: KindFields & { title?: string }) {
  const rank = (x: KindFields) => {
    const i = CONFIDENCE_ORDER.indexOf(x.confidence ?? "");
    return i < 0 ? CONFIDENCE_ORDER.length : i;
  };
  return rank(a) - rank(b) || cmp(a.title ?? "", b.title ?? "") || cmp(a.path, b.path);
}
