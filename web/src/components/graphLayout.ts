import Graph from "graphology";
import forceAtlas2 from "graphology-layout-forceatlas2";

/** What ForceAtlas2 reads: starting positions and weighted edges. */
export type LayoutInput = {
  nodes: { id: string; x: number; y: number; size?: number }[];
  edges: { source: string; target: string; weight: number }[];
  iterations: number;
  /**
   * A summary graph of a few heavy nodes (Explorer's folders): start from a
   * radial tree of its links, so ForceAtlas2 relaxes an uncrossed picture
   * instead of untangling a circle.
   */
  radial?: boolean;
};

export type LayoutPositions = Map<string, { x: number; y: number }>;

// Barnes-Hut approximates repulsion in O(n log n); ForceAtlas2 only turns it on
// above 2,000 nodes, which is exactly the global graph's cap.
const BARNES_HUT_ORDER = 1000;

// Up to this many nodes, the layout keeps node discs apart.
const SIZED_ORDER = 200;

// Smaller layouts finish in a few milliseconds, less than a worker round trip.
const WORKER_ORDER = 400;

/**
 * Starting positions for a summary graph: a radial tree around its heaviest
 * node. The tree keeps each node's heaviest link (a maximum spanning tree),
 * every subtree takes an angular sector sized by its leaves, and each level
 * sits one ring further out, so the links that matter start uncrossed and
 * ForceAtlas2 only has to relax them. Nodes in other components hang off the
 * root's outer ring.
 */
export function radialSeed(
  ids: readonly string[],
  edges: readonly { source: string; target: string; weight: number }[],
  ring = 100,
): LayoutPositions {
  const positions: LayoutPositions = new Map();

  if (ids.length === 0) return positions;
  const strength = new Map(ids.map((id) => [id, 0]));

  for (const { source, target, weight } of edges) {
    strength.set(source, (strength.get(source) ?? 0) + weight);
    strength.set(target, (strength.get(target) ?? 0) + weight);
  }

  const byStrength = [...ids].sort(
    (a, b) => (strength.get(b) ?? 0) - (strength.get(a) ?? 0) || a.localeCompare(b),
  );

  const root = byStrength[0];
  const children = new Map<string, string[]>(ids.map((id) => [id, []]));
  const reached = new Set([root]);
  const heaviest = [...edges].sort((a, b) => b.weight - a.weight);

  // Prim's algorithm on the heaviest links; a node no link reaches hangs off the root.
  while (reached.size < ids.length) {
    const next = heaviest.find(({ source, target }) => reached.has(source) !== reached.has(target));

    const [parent, child] = next
      ? reached.has(next.source)
        ? [next.source, next.target]
        : [next.target, next.source]
      : [root, byStrength.find((id) => !reached.has(id)) ?? root];

    children.get(parent)?.push(child);
    reached.add(child);
  }

  const leaves = new Map<string, number>();

  const count = (id: string): number => {
    const own = (children.get(id) ?? []).reduce((sum, child) => sum + count(child), 0);
    leaves.set(id, Math.max(1, own));

    return Math.max(1, own);
  };

  count(root);

  const place = (id: string, depth: number, from: number, to: number) => {
    const angle = (from + to) / 2;
    positions.set(id, {
      x: depth * ring * Math.cos(angle),
      y: depth * ring * Math.sin(angle),
    });
    let start = from;

    for (const child of children.get(id) ?? []) {
      const span = ((to - from) * (leaves.get(child) ?? 1)) / (leaves.get(id) ?? 1);
      place(child, depth + 1, start, start + span);
      start += span;
    }
  };

  place(root, 0, 0, Math.PI * 2);

  return positions;
}

/** Runs ForceAtlas2 synchronously and returns each node's final position. */
export function layoutPositions({
  nodes,
  edges,
  iterations,
  radial = false,
}: LayoutInput): LayoutPositions {
  const graph = new Graph();

  const seed = radial
    ? radialSeed(
        nodes.map((node) => node.id),
        edges,
      )
    : undefined;

  nodes.forEach(({ id, x, y, size = 1 }) => graph.addNode(id, { x, y, size, ...seed?.get(id) }));
  edges.forEach(({ source, target, weight }) => graph.addEdge(source, target, { weight }));

  const settings = forceAtlas2.inferSettings(graph);
  settings.edgeWeightInfluence = 1;
  // A small graph has room to keep large nodes from overlapping.
  settings.adjustSizes = graph.order <= SIZED_ORDER;

  if (graph.order > BARNES_HUT_ORDER) settings.barnesHutOptimize = true;
  forceAtlas2.assign(graph, { iterations, settings });

  const positions: LayoutPositions = new Map();
  graph.forEachNode((id, attrs) => positions.set(id, { x: attrs.x, y: attrs.y }));

  return positions;
}

/**
 * Lays out a graph and passes the positions to `done`. Large graphs run in a
 * worker so the main thread keeps painting and handling input; small graphs,
 * and environments without workers, run inline and call `done` before this
 * returns. The returned function cancels a pending layout.
 */
export function runLayout(
  input: LayoutInput,
  done: (positions: LayoutPositions) => void,
): () => void {
  if (input.nodes.length <= WORKER_ORDER || !("Worker" in globalThis)) {
    done(layoutPositions(input));

    return () => undefined;
  }

  const worker = new Worker(new URL("./graphLayout.worker.ts", import.meta.url), {
    type: "module",
  });

  worker.onmessage = (event: MessageEvent<LayoutPositions>) => {
    worker.terminate();
    done(event.data);
  };

  // A worker that cannot load still lays the graph out, on the main thread.
  worker.onerror = (event) => {
    event.preventDefault();
    worker.terminate();
    done(layoutPositions(input));
  };

  worker.postMessage(input);

  return () => worker.terminate();
}
