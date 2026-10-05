import type { ValidationDiagnostic } from "../../api/types";

export type IssueTarget = {
  nodeId?: string;
  field?: string;
  relation?: string;
  unit?: "utf8_bytes" | "line";
  start?: number;
  end?: number;
};

export function diagnosticNoteTarget(diagnostic: ValidationDiagnostic): string | null {
  const path = diagnostic.primaryPath || diagnostic.affectedPaths?.[0];

  if (!path) return null;
  const location = diagnostic.location;

  const target: IssueTarget = {
    nodeId: location?.nodeId || diagnostic.affectedNodeIds?.[0],
    field: location?.field || diagnostic.field,
    relation: location?.relation,
    unit: location?.unit,
    start: location?.start,
    end: location?.end,
  };

  if (!Object.values(target).some((value) => value !== undefined)) return path;

  return `${path}#${serializeIssueTarget(target)}`;
}

export function serializeIssueTarget(target: IssueTarget) {
  const params = new URLSearchParams();

  if (target.nodeId) params.set("node", target.nodeId);

  if (target.field) params.set("field", target.field);

  if (target.relation) params.set("relation", target.relation);

  if (target.unit) params.set("unit", target.unit);

  if (target.start !== undefined) params.set("start", String(target.start));

  if (target.end !== undefined) params.set("end", String(target.end));

  return `issue:${params.toString()}`;
}

export function parseIssueTarget(fragment: string | null | undefined): IssueTarget | null {
  if (!fragment?.startsWith("issue:")) return null;
  const params = new URLSearchParams(fragment.slice("issue:".length));
  const unit = params.get("unit");
  const start = finiteNumber(params.get("start"));
  const end = finiteNumber(params.get("end"));

  return {
    nodeId: params.get("node") || undefined,
    field: params.get("field") || undefined,
    relation: params.get("relation") || undefined,
    unit: unit === "utf8_bytes" || unit === "line" ? unit : undefined,
    start,
    end,
  };
}

export function utf8ByteRangeToLineRange(source: string, start: number, end: number) {
  const bytes = new TextEncoder().encode(source);
  const safeStart = Math.max(0, Math.min(start, bytes.length));
  const safeEnd = Math.max(safeStart, Math.min(end, bytes.length));

  return { startLine: lineAtByte(bytes, safeStart), endLine: lineAtByte(bytes, safeEnd) };
}

export function issueTargetToTextRange(source: string, target: IssueTarget) {
  if (target.start === undefined) return null;

  if (target.unit === "line") {
    const normalized = source.replace(/\r\n/g, "\n");
    const matches = [...normalized.matchAll(/.*(?:\n|$)/g)].filter((match) => match[0].length > 0);
    const startLine = Math.max(1, Math.min(target.start, matches.length || 1));
    const endLine = Math.max(startLine, Math.min(target.end ?? startLine, matches.length || 1));
    const from = matches[startLine - 1]?.index ?? 0;
    const endMatch = matches[endLine - 1];
    const to = endMatch ? (endMatch.index ?? 0) + endMatch[0].replace(/\r?\n$/, "").length : from;

    return { from, to };
  }

  const bytes = new TextEncoder().encode(source);
  const safeStart = Math.max(0, Math.min(target.start, bytes.length));
  const safeEnd = Math.max(safeStart, Math.min(target.end ?? safeStart, bytes.length));
  const decoder = new TextDecoder();

  return {
    from: decoder.decode(bytes.slice(0, safeStart)).replace(/\r\n/g, "\n").length,
    to: decoder.decode(bytes.slice(0, safeEnd)).replace(/\r\n/g, "\n").length,
  };
}

function lineAtByte(bytes: Uint8Array, offset: number) {
  let line = 1;

  for (let index = 0; index < offset; index += 1) if (bytes[index] === 0x0a) line += 1;

  return line;
}

function finiteNumber(value: string | null) {
  if (value === null || value === "") return undefined;
  const number = Number(value);

  return Number.isFinite(number) && number >= 0 ? number : undefined;
}
