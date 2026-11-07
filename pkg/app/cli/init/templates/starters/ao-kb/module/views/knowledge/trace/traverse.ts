import { traversal, MENTIONS, RELATED_CONCEPTS } from "../lib/relations.ts";
import {
  edgeKey,
  type Direction,
  type Expansion,
  type Record,
  type Trace,
  type TraceEdge,
} from "./model.ts";

export type Reader = (
  paths: string[],
  direction: "related" | Direction,
  hop: number,
  remaining: number,
) => Promise<Expansion>;

/** Read only the current frontier. Boundary-to-boundary links need another hop. */
export async function traverse(
  focus: Record,
  options: { up: number; down: number; mentions: boolean; budget: number },
  read: Reader,
): Promise<Trace> {
  const trace: Trace = {
    focus,
    nodes: new Map([[focus.path, { record: focus, lane: "focus", hop: 0 }]]),
    edges: [],
    listOnly: [],
    diagnostics: [],
  };
  const edges = new Map<string, TraceEdge>();
  const listOnly = new Map<string, TraceEdge>();
  const consider = (
    batch: Expansion,
    lane: "related" | Direction,
    hop: number,
    frontier: Set<string>,
  ) => {
    trace.diagnostics.push(...batch.diagnostics);
    if (batch.stop) trace.stop ??= batch.stop;
    const records = new Map(batch.records.map((r) => [r.path, r]));
    const next = new Set<string>();
    for (const edge of batch.edges) {
      const kind = traversal(edge.rel);
      if (
        lane === "related" &&
        kind === "unclassified" &&
        (edge.from === focus.path || edge.to === focus.path)
      ) {
        listOnly.set(edgeKey(edge), edge);
      }
      const concept = edge.rel === MENTIONS || edge.rel === RELATED_CONCEPTS;
      if (
        lane === "related"
          ? kind !== "lateral" || (!options.mentions && concept)
          : kind !== "reference" && kind !== "structure"
      )
        continue;
      let parent: string | undefined;
      let child: string | undefined;
      if (lane === "related") {
        if (edge.from === focus.path) {
          parent = focus.path;
          child = edge.to;
        } else if (edge.to === focus.path) {
          parent = focus.path;
          child = edge.from;
        }
      } else {
        const forward = kind === "reference" ? lane === "up" : lane === "down";
        parent = forward ? edge.from : edge.to;
        child = forward ? edge.to : edge.from;
        if (!frontier.has(parent)) continue;
      }
      if (!parent || !child || child === parent) continue;
      const record = records.get(child) ?? trace.nodes.get(child)?.record;
      if (!record) continue;
      if (!trace.nodes.has(child)) {
        if (trace.nodes.size >= options.budget) {
          trace.stop ??= {
            direction:
              lane === "up" ? "upstream" : lane === "down" ? "downstream" : "related records",
            hop,
            reason: "budget",
          };
          break;
        }
        trace.nodes.set(child, { record, lane, hop, parent, edge });
      }
      edges.set(edgeKey(edge), edge);
      next.add(child);
    }
    return next;
  };
  const remaining = () => Math.max(1, options.budget - trace.nodes.size);
  consider(
    await read([focus.path], "related", 1, remaining()),
    "related",
    1,
    new Set([focus.path]),
  );
  for (const direction of ["up", "down"] satisfies Direction[]) {
    if (trace.stop) break;
    let frontier = new Set([focus.path]);
    const expanded = new Set<string>();
    for (let hop = 1; hop <= options[direction] && frontier.size; hop++) {
      const paths = [...frontier].filter((path) => !expanded.has(path));
      if (!paths.length) break;
      paths.forEach((path) => expanded.add(path));
      frontier = consider(
        await read(paths, direction, hop, remaining()),
        direction,
        hop,
        new Set(paths),
      );
      if (trace.stop) break;
    }
  }
  trace.edges = [...edges.values()];
  trace.listOnly = [...listOnly.values()];
  trace.diagnostics = [...new Set(trace.diagnostics)];
  return trace;
}
