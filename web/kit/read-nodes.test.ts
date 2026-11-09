import { describe, expect, it } from "vitest";

import { graphQLRequestBody, jsonReply, withFakeFetch } from "../src/test/fakeFetch";
import { truncation } from "./coverage";
import { NODES_BATCH_MAX, readNodes } from "./read-nodes";

const http = withFakeFetch();

// A server that resolves `n<i>.md` and its alias `alias-<i>` to the same note,
// answers at most `first` refs (default 50), and knows nothing called `missing`.
function serve() {
  http.on("POST", "/api/v1/graphql", (request) => {
    const { variables } = graphQLRequestBody(request);
    const refs = refsOf(variables);
    const first = Number(variables?.first ?? 50);

    const items = refs.slice(0, first).map((requestedRef) => {
      if (requestedRef === "missing")
        return {
          requestedRef,
          ref: null,
          node: null,
          error: { code: "unresolved_ref", message: "not found" },
        };

      const path = requestedRef.replace(/^alias-(\d+)$/, "n$1.md");

      return {
        requestedRef,
        ref: { notePath: path, kind: "NOTE", fragment: null, nodeId: null },
        node: { path },
        error: null,
      };
    });

    return jsonReply({
      data: { nodes: { items, pageInfo: { truncated: refs.length > first } } },
      extensions: { generation: "G1" },
    });
  });
}

const refsOf = (variables: { refs?: unknown } | undefined) =>
  Array.isArray(variables?.refs) ? variables.refs.map(String) : [];

const refs = (count: number) => Array.from({ length: count }, (_, index) => `n${index}.md`);

const batchSizes = () =>
  http.requests("POST", "/api/v1/graphql").map((request) => {
    const { variables } = graphQLRequestBody(request);

    return [refsOf(variables).length, variables?.first];
  });

describe("readNodes", () => {
  it.each([
    [51, [[51, 51]]],
    [
      201,
      [
        [200, 200],
        [1, 1],
      ],
    ],
    [
      600,
      [
        [200, 200],
        [200, 200],
        [200, 200],
      ],
    ],
  ])(
    "reads all %i refs in batches of at most the maximum, with first set",
    async (count, sizes) => {
      serve();
      const result = await readNodes<{ path: string }>(refs(count), "... on Note { path }");

      expect(result.nodes).toHaveLength(count);
      expect(result.nodes.at(-1)).toEqual({ path: `n${count - 1}.md` });
      expect(result.complete).toBe(true);
      expect(batchSizes()).toEqual(sizes);
      expect(Math.max(...batchSizes().map(([size]) => Number(size)))).toBeLessThanOrEqual(
        NODES_BATCH_MAX,
      );
    },
  );

  it("returns a node named by two refs once and reports refs that did not resolve", async () => {
    serve();

    const result = await readNodes<{ path: string }>(
      ["n1.md", "alias-1", "missing", "n2.md", "n1.md"],
      "... on Note { path }",
    );

    expect(result.nodes).toEqual([{ path: "n1.md" }, { path: "n2.md" }]);
    expect(result.unresolved).toEqual([
      { requestedRef: "missing", code: "unresolved_ref", message: "not found" },
    ]);
    expect(result.complete).toBe(false);
    expect(truncation(result)).toEqual([]);
  });
});
