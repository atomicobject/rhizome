import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { expect, it } from "vitest";

import type { ValidationScope } from "../api/types";
import { jsonReply, type FakeFetchRequest, withFakeFetch } from "../test/fakeFetch";
import { useValidationScopeSummaries } from "./useValidationScopeSummaries";

const http = withFakeFetch();

function wrapper({ children }: PropsWithChildren) {
  return (
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      {children}
    </QueryClientProvider>
  );
}

function requestBody(request: FakeFetchRequest) {
  // SAFETY: the typed client serializes this request body and the fake route
  // only captures calls to the validation summaries endpoint.
  return JSON.parse(request.body || "{}") as { generation: number; scopes: ValidationScope[] };
}

it("loads every requested scope in batches of at most 200", async () => {
  const requested = Array.from({ length: 425 }, (_, index) => ({
    kind: "node" as const,
    key: `node-${index}`,
  }));

  http.on("POST", "/api/v1/validation/summaries", (request) => {
    const body = requestBody(request);
    expect(body.scopes.length).toBeLessThanOrEqual(200);

    return jsonReply({
      generation: body.generation,
      summaries: body.scopes.map((scope) => ({
        scope,
        issueCount: 1,
        affectedFileCount: 1,
        affectedNoteCount: 1,
        repairActionCount: 0,
      })),
    });
  });

  const { result } = renderHook(() => useValidationScopeSummaries(9, requested), { wrapper });

  await waitFor(() => expect(result.current.summaries.size).toBe(425));
  expect(http.requests("POST", "/api/v1/validation/summaries").map(requestBody)).toEqual([
    { generation: 9, scopes: requested.slice(0, 200) },
    { generation: 9, scopes: requested.slice(200, 400) },
    { generation: 9, scopes: requested.slice(400) },
  ]);
});

it("cancels every old batch when the generation changes", async () => {
  const requested = Array.from({ length: 201 }, (_, index) => ({
    kind: "node" as const,
    key: `node-${index}`,
  }));

  http.on("POST", "/api/v1/validation/summaries", (request) => {
    const body = requestBody(request);

    if (body.generation === 1) return new Promise<Response>(() => {});

    return jsonReply({ generation: body.generation, summaries: [] });
  });

  const { rerender } = renderHook(
    ({ generation }) => useValidationScopeSummaries(generation, requested),
    { wrapper, initialProps: { generation: 1 } },
  );

  await waitFor(() =>
    expect(http.requests("POST", "/api/v1/validation/summaries")).toHaveLength(2),
  );
  const firstGeneration = http.requests("POST", "/api/v1/validation/summaries");

  rerender({ generation: 2 });

  await waitFor(() =>
    expect(firstGeneration.every((request) => request.signal?.aborted)).toBe(true),
  );
  await waitFor(() =>
    expect(
      http
        .requests("POST", "/api/v1/validation/summaries")
        .filter((request) => requestBody(request).generation === 2),
    ).toHaveLength(2),
  );
});

it("aborts every outstanding batch when its observer unmounts", async () => {
  const requested = Array.from({ length: 201 }, (_, index) => ({
    kind: "node" as const,
    key: `node-${index}`,
  }));

  http.on("POST", "/api/v1/validation/summaries", () => new Promise<Response>(() => {}));
  const rendered = renderHook(() => useValidationScopeSummaries(1, requested), { wrapper });
  await waitFor(() =>
    expect(http.requests("POST", "/api/v1/validation/summaries")).toHaveLength(2),
  );
  const requests = http.requests("POST", "/api/v1/validation/summaries");

  rendered.unmount();

  expect(requests.every((request) => request.signal?.aborted)).toBe(true);
});
