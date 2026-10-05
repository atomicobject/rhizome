import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { beforeEach, expect, it } from "vitest";

import type { ValidationRepairReview } from "../api/types";
import { deferredReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { useValidationRepair } from "./useValidationRepair";

const http = withFakeFetch();

function wrapper({ children }: PropsWithChildren) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

function review(overrides: Partial<ValidationRepairReview> = {}): ValidationRepairReview {
  return {
    id: "review-1",
    vaultIdentity: "vault-1",
    generation: 4,
    planFingerprint: "plan-4",
    selectionFingerprint: "selection-1",
    actionIds: ["action-1"],
    transactionIds: ["transaction-1"],
    affectedPaths: ["notes/a.md"],
    preview: [],
    state: "pending",
    createdAt: "2026-09-11T00:00:00Z",
    lastAccessedAt: "2026-09-11T00:00:00Z",
    expiresAt: "2026-09-11T01:00:00Z",
    ...overrides,
  };
}

beforeEach(() => sessionStorage.clear());

it("restores a server-backed fix review after reload", async () => {
  const saved = review();
  sessionStorage.setItem("rhizome:validation-repair-review", JSON.stringify(saved));
  const server = deferredReply<ValidationRepairReview>();
  http.on("GET", "/api/v1/validation/repair-reviews/review-1", () => server.promise);

  const { result } = renderHook(() => useValidationRepair(4, "plan-4"), { wrapper });

  await waitFor(() =>
    expect(http.count("GET", "/api/v1/validation/repair-reviews/review-1")).toBe(1),
  );
  expect(result.current.busy).toBe("restore");
  expect(result.current.review?.state).toBe("pending");

  await act(async () => {
    server.resolve(
      review({
        state: "applied",
        affectedPaths: ["notes/a.md", "notes/b.md"],
        lastAccessedAt: "2026-09-11T00:30:00Z",
      }),
    );
    await server.promise;
  });

  await waitFor(() => expect(result.current.busy).toBeNull());
  expect(result.current.review).toMatchObject({
    id: "review-1",
    state: "applied",
    affectedPaths: ["notes/a.md", "notes/b.md"],
    lastAccessedAt: "2026-09-11T00:30:00Z",
  });
});

it("retains the reviewed diff when a server restart loses the review", async () => {
  const saved = review();
  sessionStorage.setItem("rhizome:validation-repair-review", JSON.stringify(saved));
  http.on("GET", "/api/v1/validation/repair-reviews/review-1", () =>
    jsonReply({ error: "server restarted", code: "REPAIR_REVIEW_NOT_FOUND" }, 404),
  );

  const { result } = renderHook(() => useValidationRepair(4, "plan-4"), { wrapper });

  await waitFor(() => expect(result.current.review?.state).toBe("stale"));
  expect(result.current.review?.affectedPaths).toEqual(["notes/a.md"]);
  expect(result.current.review?.staleReason).toContain("server restarted");
});

it("preserves a completed repair when validation publishes the next generation", async () => {
  const saved = review({ state: "applied" });
  sessionStorage.setItem("rhizome:validation-repair-review", JSON.stringify(saved));
  http.on("GET", "/api/v1/validation/repair-reviews/review-1", () => jsonReply(saved));

  const { result, rerender } = renderHook(
    ({ generation, fingerprint }) => useValidationRepair(generation, fingerprint),
    { wrapper, initialProps: { generation: 4, fingerprint: "plan-4" } },
  );

  await waitFor(() => expect(result.current.review?.state).toBe("applied"));
  rerender({ generation: 5, fingerprint: "plan-5" });
  expect(result.current.review?.state).toBe("applied");
  expect(result.current.changed).toBe(false);
});

it("keeps a pending review retryable after a transient recovery failure", async () => {
  const saved = review();
  sessionStorage.setItem("rhizome:validation-repair-review", JSON.stringify(saved));
  http.on("GET", "/api/v1/validation/repair-reviews/review-1", () =>
    jsonReply({ error: "Starting", code: "INDEX_INITIALIZING" }, 503),
  );
  const { result } = renderHook(() => useValidationRepair(4, "plan-4"), { wrapper });
  await waitFor(() => expect(result.current.recoveryFailed).toBe(true));
  expect(result.current.review?.state).toBe("pending");
  expect(result.current.changed).toBe(false);
  http.on("GET", "/api/v1/validation/repair-reviews/review-1", () => jsonReply(saved));
  act(() => result.current.retryRecovery());
  await waitFor(() => expect(result.current.busy).toBeNull());
  expect(result.current.recoveryFailed).toBe(false);
  expect(result.current.error).toBeNull();
  expect(result.current.review?.state).toBe("pending");
});
