import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { jsonReply, withFakeFetch } from "../src/test/fakeFetch";
import { ActionError, runAction, useActionCommand, useActionQuery } from "./actions";

const http = withFakeFetch();

const HEALTH = "/api/v1/actions/compounding/health";

function wrapper() {
  const client = new QueryClient();

  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, "visibilityState", { configurable: true, value: state });
  document.dispatchEvent(new Event("visibilitychange"));
}

afterEach(() => {
  vi.useRealTimers();
  setVisibility("visible");
});

describe("runAction", () => {
  it("posts the input to the action route and resolves to the ok result", async () => {
    http.json("POST", "/api/v1/actions/compounding/prompt", {
      status: "ok",
      result: { prompt: "p" },
    });

    await expect(runAction("compounding/prompt", { items: ["proposal:a"] })).resolves.toEqual({
      prompt: "p",
    });
    expect(http.requests("POST", "/api/v1/actions/compounding/prompt")[0].body).toBe(
      '{"items":["proposal:a"]}',
    );
  });

  it("rejects refused, error, and HTTP failures as ActionError", async () => {
    http.json("POST", "/api/v1/actions/m/refuse", { status: "refused", reason: "run rzm trust" });
    http.json("POST", "/api/v1/actions/m/crash", {
      status: "error",
      reason: "exit 1",
      stderrTail: "boom",
    });
    http.json("POST", "/api/v1/actions/m/missing", { error: "unknown action m/missing" }, 404);
    http.json("POST", "/api/v1/actions/m/bad", { error: "input count must be an int" }, 400);

    await expect(runAction("m/refuse")).rejects.toMatchObject({
      name: "ActionError",
      status: "refused",
      reason: "run rzm trust",
      message: "run rzm trust",
    });
    await expect(runAction("m/crash")).rejects.toMatchObject({
      status: "error",
      reason: "exit 1",
      stderrTail: "boom",
    });
    await expect(runAction("m/missing")).rejects.toMatchObject({
      status: "error",
      reason: "unknown action m/missing",
    });
    await expect(runAction("m/bad")).rejects.toBeInstanceOf(ActionError);
    await expect(runAction("m/bad")).rejects.toMatchObject({
      status: "error",
      reason: "input count must be an int",
    });
  });

  it("rejects an id that is not <module>/<name>", async () => {
    await expect(runAction("health")).rejects.toMatchObject({ status: "error" });
    expect(http.calls).toEqual([]);
  });
});

describe("useActionQuery", () => {
  it("polls every 15 s only while the document is visible", async () => {
    vi.useFakeTimers();
    http.json("POST", HEALTH, { status: "ok", result: { score: 1 } });

    const { result } = renderHook(() => useActionQuery("compounding/health"), {
      wrapper: wrapper(),
    });

    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(result.current.data).toEqual({ score: 1 });
    expect(http.count("POST", HEALTH)).toBe(1);

    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(http.count("POST", HEALTH)).toBe(2);

    act(() => setVisibility("hidden"));
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(http.count("POST", HEALTH)).toBe(2);

    act(() => setVisibility("visible"));
    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(http.count("POST", HEALTH)).toBeGreaterThan(2);
  });

  it("does not retry a refused envelope", async () => {
    http.json("POST", HEALTH, { status: "refused", reason: "untrusted" });

    const { result } = renderHook(() => useActionQuery("compounding/health"), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toMatchObject({ status: "refused", reason: "untrusted" });
    expect(http.count("POST", HEALTH)).toBe(1);
  });
});

describe("useActionCommand", () => {
  it("resolves to the result and refreshes action reads once on ok", async () => {
    http.json("POST", HEALTH, { status: "ok", result: { score: 1 } });
    http.json("POST", "/api/v1/actions/compounding/dismiss", { status: "ok", result: null });

    const { result } = renderHook(
      () => ({
        health: useActionQuery("compounding/health"),
        dismiss: useActionCommand("compounding/dismiss"),
      }),
      { wrapper: wrapper() },
    );

    await waitFor(() => expect(result.current.health.isSuccess).toBe(true));

    await act(async () => {
      await expect(
        result.current.dismiss.mutateAsync({ cluster: "c1", reason: "dup" }),
      ).resolves.toBeNull();
    });

    expect(http.count("POST", HEALTH)).toBe(2);
    expect(http.requests("POST", "/api/v1/actions/compounding/dismiss")[0].body).toBe(
      '{"cluster":"c1","reason":"dup"}',
    );
  });

  it("rejects a refused command with ActionError and refreshes nothing", async () => {
    http.json("POST", HEALTH, { status: "ok", result: { score: 1 } });
    http.on("POST", "/api/v1/actions/compounding/hide", () =>
      jsonReply({ status: "refused", reason: "already hidden" }),
    );

    const { result } = renderHook(
      () => ({
        health: useActionQuery("compounding/health"),
        hide: useActionCommand("compounding/hide"),
      }),
      { wrapper: wrapper() },
    );

    await waitFor(() => expect(result.current.health.isSuccess).toBe(true));

    await act(async () => {
      await expect(result.current.hide.mutateAsync({ path: "a.md" })).rejects.toMatchObject({
        status: "refused",
        reason: "already hidden",
      });
    });

    expect(http.count("POST", HEALTH)).toBe(1);
  });
});
