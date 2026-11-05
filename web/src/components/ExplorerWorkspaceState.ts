import type { GraphResponse } from "../api/types";

export type ExplorerSelection =
  | { kind: "root" }
  | { kind: "file"; path: string }
  | { kind: "module"; path: string };

export const EMPTY_GRAPH: GraphResponse = {
  nodes: [],
  edges: [],
  truncated: false,
};
