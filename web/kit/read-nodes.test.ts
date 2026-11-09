import { describe, expect, it, vi } from "vitest";

import { graphQLRequestBody, jsonReply, withFakeFetch } from "../src/test/fakeFetch";
import { responseGeneration, truncation } from "./coverage";
import { frameGeneration } from "./generation";
import { NODES_BATCH_MAX, readNodes } from "./read-nodes";

const http = withFakeFetch();

// A server that resolves `n<i>.md` and its alias `alias-<i>` to the same note,
// answers at most `first` refs (default 50), and knows nothing called `missing`.
// `at` names the generation of each answer, in request order.
function serve(at: (request: number) => string | Promise<string> = () => "G1") {
  let requests = 0;

  http.on("POST", "/api/v1/graphql", async (request) => {
    const generation = await at(requests++);
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
      extensions: { generation },
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
      expect(responseGeneration(result)).toBe("G1");
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

  it("reads a batch behind the frame's generation once more, so the batches agree", async () => {
    let releaseSecond = () => {};

    const second = new Promise<void>((resolve) => (releaseSecond = resolve));

    // The first batch answers at G5; a write moves the frame to G6 before the
    // second batch answers there; the first batch, read again, answers at G6.
    serve(async (request) => {
      if (request !== 1) return request === 0 ? "G5" : "G6";

      await second;

      return "G6";
    });
    const reading = readNodes<{ path: string }>(refs(201), "... on Note { path }");
    await vi.waitFor(() => expect(frameGeneration.current()).toBe("G5"));
    frameGeneration.set("G6");
    releaseSecond();
    const result = await reading;

    expect(batchSizes()).toEqual([
      [200, 200],
      [1, 1],
      [200, 200],
    ]);
    expect(result.nodes).toHaveLength(201);
    expect(responseGeneration(result)).toBe("G6");
    frameGeneration.stop();
  });
});
