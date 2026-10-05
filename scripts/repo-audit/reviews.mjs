import * as fs from "node:fs/promises";
import { createHash } from "node:crypto";

const hash = (value) => createHash("sha256").update(JSON.stringify(value)).digest("hex");
const required = (record, field) => {
  if (typeof record[field] !== "string" || !record[field].trim())
    throw new Error(`Review requires ${field}`);
};

export function validateDecision(decision, packet, metadata) {
  if (!packet || decision.evidenceHash !== packet.evidenceHash)
    throw new Error("Review evidence is stale or candidate is unavailable");
  if (!["confirmed", "rejected", "unresolved"].includes(decision.verdict))
    throw new Error("Unknown review verdict");
  for (const field of ["reviewer", "reason"]) required(decision, field);
  const sourceHashes = Object.fromEntries(
    packet.evidence.flatMap((e) =>
      e.sources.map((s) => [s.path, metadata.get(s.path)?.sourceHash]),
    ),
  );
  if (decision.verdict === "confirmed") {
    for (const field of ["title", "correction", "scope", "editTarget", "changeKey"])
      required(decision, field);
    if (
      !Array.isArray(decision.claims) ||
      decision.claims.length < 2 ||
      decision.claims.some((c) => typeof c !== "string" || !c.trim())
    )
      throw new Error("Confirmation needs the conflicting claims");
    if (!Array.isArray(decision.evidence) || !decision.evidence.length)
      throw new Error("Confirmation needs source citations");
    for (const cite of decision.evidence) {
      const m = metadata.get(cite.path);
      if (!m || m.stale || m.unavailable || !m.sourceHash || cite.sourceHash !== m.sourceHash)
        throw new Error(`Stale or unavailable citation: ${cite.path}`);
      if (
        !Number.isSafeInteger(cite.startLine) ||
        !Number.isSafeInteger(cite.endLine) ||
        cite.startLine < 1 ||
        cite.endLine < cite.startLine ||
        cite.endLine > m.lineCount
      )
        throw new Error("Invalid citation line range");
      sourceHashes[cite.path] = cite.sourceHash;
    }
    if (!decision.evidence.some((c) => c.path === decision.editTarget))
      throw new Error("Cite the proposed edit target");
  }
  if (Object.values(sourceHashes).some((v) => !v)) throw new Error("Missing source fingerprint");
  return { ...decision, sourceHashes, reviewedAt: new Date().toISOString() };
}

export async function verifiedReport(output, packets, metadata, decisions) {
  let saved = [];
  try {
    saved = JSON.parse(await fs.readFile(output + "/reviews.json", "utf8"));
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  if (!Array.isArray(saved) || (decisions !== undefined && !Array.isArray(decisions)))
    throw new Error("Reviews must be arrays");
  const records = new Map(saved.map((r) => [r.id, r]));
  const byID = new Map(packets.map((p) => [p.id, p]));
  if (decisions) {
    if (new Set(decisions.map((d) => d.id)).size !== decisions.length)
      throw new Error("Duplicate review decisions");
    // Validate the entire submission before persisting any decision.
    const validated = decisions.map((d) => validateDecision(d, byID.get(d.id), metadata));
    for (const d of validated) records.set(d.id, d);
    await fs.writeFile(
      output + "/reviews.json.tmp",
      JSON.stringify([...records.values()], null, 2),
    );
    await fs.rename(output + "/reviews.json.tmp", output + "/reviews.json");
  }
  const counts = { confirmed: 0, rejected: 0, unresolved: 0, pending: 0, stale: 0 };
  const pending = [],
    improvements = new Map();
  for (const packet of packets) {
    const record = records.get(packet.id);
    const current =
      record?.evidenceHash === packet.evidenceHash &&
      record.sourceHashes &&
      Object.entries(record.sourceHashes).every(([p, h]) => {
        const m = metadata.get(p);
        return m && !m.stale && !m.unavailable && m.sourceHash === h;
      });
    if (!record || !current) {
      counts[record ? "stale" : "pending"]++;
      pending.push(packet);
      continue;
    }
    if (!["confirmed", "rejected", "unresolved"].includes(record.verdict))
      throw new Error("Invalid persisted review");
    counts[record.verdict]++;
    if (record.verdict !== "confirmed") continue;
    const id = hash([record.editTarget, record.changeKey]);
    const group = improvements.get(id) || {
      id,
      title: record.title,
      correction: record.correction,
      editTarget: record.editTarget,
      changeKey: record.changeKey,
      reviews: [],
    };
    group.reviews.push(record);
    improvements.set(id, group);
  }
  return {
    improvements: [...improvements.values()],
    pending,
    summary: { ...counts, distinctImprovements: improvements.size },
  };
}

// A stronger agent consumes review-packets.jsonl, inspects cited sources, and
// submits decisions through this function. There is no provider or shell coupling.
export async function recordReviews(rzm, output, decisions) {
  const { rebuildReport } = await import("./rebuild-report.mjs");
  return rebuildReport(rzm, output, { decisions });
}
