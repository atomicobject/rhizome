import Graph from "graphology";
import forceAtlas2 from "graphology-layout-forceatlas2";

/** What ForceAtlas2 reads: starting positions and weighted edges. */
export type LayoutInput = {
  nodes: { id: string; x: number; y: number }[];
  edges: { source: string; target: string; weight: number }[];
  iterations: number;
};

export type LayoutPositions = Map<string, { x: number; y: number }>;

// Barnes-Hut approximates repulsion in O(n log n); ForceAtlas2 only turns it on
// above 2,000 nodes, which is exactly the global graph's cap.
const BARNES_HUT_ORDER = 1000;

// Smaller layouts finish in a few milliseconds, less than a worker round trip.
const WORKER_ORDER = 400;

/** Runs ForceAtlas2 synchronously and returns each node's final position. */
export function layoutPositions({ nodes, edges, iterations }: LayoutInput): LayoutPositions {
  const graph = new Graph();
  nodes.forEach(({ id, x, y }) => graph.addNode(id, { x, y }));
  edges.forEach(({ source, target, weight }) => graph.addEdge(source, target, { weight }));

  const settings = forceAtlas2.inferSettings(graph);
  settings.edgeWeightInfluence = 1;

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
