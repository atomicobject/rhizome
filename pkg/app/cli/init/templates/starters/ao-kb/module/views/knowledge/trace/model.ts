import type { Edge } from "../lib/relations.ts";

export type Ref = {
  notePath: string;
  kind: string;
  fragment?: string;
  nodeId?: string;
  typeName?: string;
  structuralFingerprint?: string;
};
export type Record = {
  path: string;
  aoId: string | null;
  title: string;
  headline?: string | null;
  name?: string | null;
  __typename: string;
  ref: Ref;
  /** The type's label, such as "Use case", when it differs from its GraphQL name. */
  kind?: string;
};
export type TraceEdge = Edge & { locations: { path: string; section: string; ref?: Ref }[] };
export type Direction = "up" | "down";
export type Visit = {
  record: Record;
  lane: "focus" | "related" | Direction;
  hop: number;
  parent?: string;
  edge?: TraceEdge;
};
export type Stop = { direction: string; hop: number; reason: "budget" | "field"; detail?: string };
export type Expansion = {
  edges: TraceEdge[];
  records: Record[];
  stop?: Stop;
  diagnostics: string[];
};
export type Trace = {
  focus: Record;
  nodes: Map<string, Visit>;
  edges: TraceEdge[];
  listOnly: TraceEdge[];
  stop?: Stop;
  diagnostics: string[];
};
export const edgeKey = (e: Edge) => `${e.from}\u0000${e.rel}\u0000${e.to}`;
export const label = (record: Record) => record.headline || record.name || record.title;
export const kind = (record: Record) => record.kind ?? record.__typename;
export const presentation = (record: Record) =>
  record.__typename === "Assertion"
    ? "ao-kb.assertion"
    : record.__typename === "Source"
      ? "ao-kb.source"
      : undefined;
export function depth(value: unknown): number {
  return value === 1 || value === 3 ? value : 2;
}
export function queryBudget(value: unknown): number {
  return typeof value === "number" && Number.isInteger(value) && value >= 2
    ? Math.min(value, 200)
    : 200;
}
