// Batched node reads (SPEC-0136 R7): `nodes(refs:)` answers at most 200 refs
// and by default only the first 50, so a scope larger than one batch needs
// several reads with `first` set. readNodes does that and merges the answers.
import { queryPublicGraphQLOperation } from "../src/api/client";
import type { PublicGraphQLError } from "../src/api/publicGraphQLTypes";
import { recordResponse, type Truncation } from "./coverage";
import { frameGeneration } from "./generation";

/** The `nodes(refs:)` maximum. */
export const NODES_BATCH_MAX = 200;

/** A ref that did not resolve to a node, with the server's reason. */
export type UnresolvedRef = { requestedRef: string; code: string; message: string };

export type ReadNodesResult<TNode> = {
  /** One entry per distinct node, in the order of the refs that first named it. */
  nodes: TNode[];
  /** Refs that resolved to no node. */
  unresolved: UnresolvedRef[];
  /** GraphQL errors from every batch, such as a field that failed on one record. */
  errors: PublicGraphQLError[];
  /** Capped reads that stopped at their limit; empty when every ref was answered. */
  truncated: Truncation[];
  /** True when every ref resolved, nothing was cut off, and no batch reported an error. */
  complete: boolean;
};

type BatchItem<TNode> = {
  requestedRef: string;
  ref: { notePath: string; kind: string; fragment?: string | null; nodeId?: string | null } | null;
  node: TNode | null;
  error: { code: string; message: string } | null;
};

type BatchData<TNode> = { nodes: { items: BatchItem<TNode>[]; pageInfo: { truncated: boolean } } };

const batchQuery = (selection: string) =>
  `query ReadNodes($refs: [String!]!, $first: Int) { nodes(refs: $refs, first: $first) { items { requestedRef ref { notePath kind fragment nodeId } node { ${selection} } error { code message } } pageInfo { truncated } } }`;

/**
 * Read every node `refs` names with `selection` (fields of the node, usually
 * inline fragments such as `... on Assertion { path title }`), in batches of at
 * most 200. Two refs that name the same node return it once. A batch's field
 * errors and unresolved refs are reported, not thrown; a failed batch throws.
 */
export async function readNodes<TNode>(
  refs: readonly string[],
  selection: string,
  options: { signal?: AbortSignal } = {},
): Promise<ReadNodesResult<TNode>> {
  const unique = [...new Set(refs)];
  const batches: string[][] = [];

  for (let at = 0; at < unique.length; at += NODES_BATCH_MAX)
    batches.push(unique.slice(at, at + NODES_BATCH_MAX));

  const query = batchQuery(selection);

  const read = (batch: string[]) =>
    frameGeneration.read(async () => {
      const response = await queryPublicGraphQLOperation<BatchData<TNode>>(
        query,
        { refs: batch, first: batch.length },
        "ReadNodes",
        { signal: options.signal },
      );

      return { data: response, generation: response.extensions?.generation };
    }, options.signal);

  let responses = await Promise.all(batches.map(read));
  // No refs means no batches, and an empty, complete result with no generation.
  const generationOf = (index: number) => responses[index]?.extensions?.generation;

  // Batches read independently can straddle a write: read the ones behind the
  // frame's generation once more, and name a generation only when all agree.
  if (responses.some((_, index) => generationOf(index) !== generationOf(0))) {
    const current = frameGeneration.current();

    responses = await Promise.all(
      responses.map((response, index) =>
        generationOf(index) === current ? response : read(batches[index]),
      ),
    );
  }

  const generation = responses.every((_, index) => generationOf(index) === generationOf(0))
    ? generationOf(0)
    : undefined;

  const result: ReadNodesResult<TNode> = {
    nodes: [],
    unresolved: [],
    errors: [],
    truncated: [],
    complete: true,
  };

  const seen = new Set<string>();

  for (const response of responses) {
    const batch = response.data?.nodes;

    if (!batch)
      throw new Error(
        response.errors?.map((error) => error.message).join("\n") ||
          "nodes(refs:) returned no data",
      );

    result.errors.push(...(response.errors ?? []));
    result.truncated.push(...(response.extensions?.truncated ?? []));

    if (batch.pageInfo.truncated)
      result.truncated.push({
        path: ["nodes"],
        returned: batch.items.length,
        limit: NODES_BATCH_MAX,
      });

    for (const item of batch.items) {
      if (!item.node || !item.ref) {
        result.unresolved.push({
          requestedRef: item.requestedRef,
          code: item.error?.code ?? "unresolved_ref",
          message: item.error?.message ?? "node ref did not resolve",
        });

        continue;
      }

      const { notePath, kind, fragment, nodeId } = item.ref;
      const identity = JSON.stringify([notePath, kind, fragment ?? null, nodeId ?? null]);

      if (seen.has(identity)) continue;
      seen.add(identity);
      result.nodes.push(item.node);
    }
  }

  result.complete = !result.unresolved.length && !result.errors.length && !result.truncated.length;
  recordResponse(
    result,
    generation === undefined
      ? { truncated: result.truncated }
      : { truncated: result.truncated, generation },
  );

  return result;
}
