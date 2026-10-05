import { describe, expect, it } from "vitest";
import { withFakeFetch } from "../test/fakeFetch";
import { graphQLExplorerFetcher } from "./GraphQLExplorer";

describe("GraphQLExplorer", () => {
  const http = withFakeFetch();

  it("posts GraphiQL operations to the public GraphQL endpoint", async () => {
    http.json("POST", "/api/v1/graphql", { data: { ok: true } });

    await expect(graphQLExplorerFetcher({ query: "{ validation { ok } }" })).resolves.toEqual({
      data: { ok: true },
    });

    const [request] = http.requests("POST", "/api/v1/graphql");
    expect(request.headers.get("content-type")).toBe("application/json");
    expect(request.headers.get("accept")).toBe("application/json");
    expect(JSON.parse(String(request.body))).toEqual({
      query: "{ validation { ok } }",
    });
  });

  it("reports an actionable error when the workspace service is unavailable", async () => {
    http.on("POST", "/api/v1/graphql", () => new Response(null, { status: 503 }));

    await expect(graphQLExplorerFetcher({ query: "{ validation { ok } }" })).rejects.toThrow(
      "GraphQL workspace request failed (503)",
    );
  });
});
