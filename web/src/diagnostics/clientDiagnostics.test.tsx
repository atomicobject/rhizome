import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  recordCommit,
  recordError,
  recordFrameGap,
  recordReconnect,
  recordRefreshCycle,
  takeBatch,
} from "./clientMetrics";
import { DiagnosticRegion } from "./DiagnosticRegion";
import { observeRefetches, trackRefresh } from "./refreshChurn";
import { CLIENT_DIAGNOSTICS_URL, startClientDiagnostics } from "./startClientDiagnostics";

const identity = {
  session: "0123456789abcdef",
  build: "dev",
  surface: "browser",
  route: "notes",
} as const;

afterEach(() => {
  takeBatch(identity);
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("client diagnostics", () => {
  it("measures a region without adding commits", async () => {
    let renders = 0;

    function Child() {
      renders++;

      return <p>content</p>;
    }

    const stop = startClientDiagnostics(new QueryClient());
    render(
      <DiagnosticRegion name="sidebar">
        <Child />
      </DiagnosticRegion>,
    );

    await act(async () => {
      recordFrameGap(120);
      recordRefreshCycle();
      recordReconnect();
      window.dispatchEvent(new ErrorEvent("error", { error: new TypeError("PRIVATE") }));
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    stop();

    expect(renders).toBe(1);
    const batch = takeBatch(identity);
    expect(batch?.regions.sidebar).toMatchObject({ mount: 1, update: 0, nested_update: 0 });
    // Real animation frames may add slow gaps of their own on a busy machine.
    expect(batch?.frames.longest_ms).toBeGreaterThanOrEqual(120);
    expect(batch?.stability.errors).toEqual({ TypeError: 1 });
  });

  it("sends only windows with measurements, without private detail", () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"] });
    // Keep real frame timing out of the window under test.
    vi.stubGlobal("requestAnimationFrame", vi.fn());

    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve(new Response(null, { status: 204 })),
    );

    vi.stubGlobal("fetch", fetchMock);
    window.history.replaceState({}, "", "/notes/PRIVATE_PATH.md?q=PRIVATE_QUERY");
    const stop = startClientDiagnostics(new QueryClient());

    vi.advanceTimersByTime(10_000);
    expect(fetchMock).not.toHaveBeenCalled();

    recordReconnect();
    recordError("rejections", "Error");
    window.dispatchEvent(new ErrorEvent("error", { error: new RangeError("PRIVATE_MESSAGE") }));
    vi.advanceTimersByTime(10_000);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(
      CLIENT_DIAGNOSTICS_URL,
      expect.objectContaining({ method: "POST", keepalive: true }),
    );
    const body = String(fetchMock.mock.lastCall?.[1]?.body);
    expect(body).not.toContain("PRIVATE");
    expect(JSON.parse(body)).toMatchObject({
      surface: "browser",
      route: "notes",
      stability: { reconnects: 1, errors: { RangeError: 1 }, rejections: { Error: 1 } },
    });

    window.dispatchEvent(new Event("pagehide"));
    vi.advanceTimersByTime(10_000);
    stop();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("counts refetches whose data was structurally unchanged", async () => {
    const client = new QueryClient();
    const cache = client.getQueryCache();
    let value = 1;

    const observer = new QueryObserver(client, {
      queryKey: ["rhizome", "probe"],
      queryFn: () => Promise.resolve({ value }),
    });

    const unsubscribe = observer.subscribe(() => undefined);
    const stop = observeRefetches(cache);
    await vi.waitFor(() => expect(observer.getCurrentResult().data).toEqual({ value: 1 }));
    // The first load is not a refetch.
    takeBatch(identity);

    const refresh = async () => {
      trackRefresh(cache, [{ queryKey: ["rhizome"] }]);
      await client.invalidateQueries({ queryKey: ["rhizome"] });
      await new Promise((resolve) => setTimeout(resolve, 0));
    };

    await refresh();
    value = 2;
    await refresh();
    stop();
    unsubscribe();

    expect(takeBatch(identity)?.refresh).toMatchObject({ cycles: 2, refetched: 2, unchanged: 1 });
  });

  it.each([
    { label: "unchanged data counts as churn", next: 1, churned: 1 },
    { label: "changed data does not", next: 2, churned: 0 },
  ])("decides churn per refresh across a batch boundary: $label", async ({ next, churned }) => {
    const client = new QueryClient();
    const cache = client.getQueryCache();
    let respond = Promise.resolve();
    let value = 1;

    const observer = new QueryObserver(client, {
      queryKey: ["rhizome", "probe"],
      queryFn: async () => {
        await respond;

        return { value };
      },
    });

    const unsubscribe = observer.subscribe(() => undefined);
    const stop = observeRefetches(cache);
    await vi.waitFor(() => expect(observer.getCurrentResult().data).toEqual({ value: 1 }));
    takeBatch(identity);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });

    let release: () => void = () => undefined;
    respond = new Promise((resolve) => (release = resolve));
    value = next;
    trackRefresh(cache, [{ queryKey: ["rhizome"] }]);
    void client.invalidateQueries({ queryKey: ["rhizome"] });
    recordCommit("view-table", "update", 1, 1);

    // The batch closes after the commit but before the refetch resolves.
    const first = takeBatch(identity);
    expect(first?.regions["view-table"]?.commits_after_refresh).toBe(1);
    expect(first?.refresh.churned).toBe(0);

    release();
    await vi.advanceTimersByTimeAsync(1_000);
    stop();
    unsubscribe();

    expect(takeBatch(identity)?.refresh).toMatchObject({ refetched: 1, churned });
  });

  it("does not judge a refresh when another of its refetches fails", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const cache = client.getQueryCache();
    let fail = false;

    const steady = new QueryObserver(client, {
      queryKey: ["rhizome", "steady"],
      queryFn: () => Promise.resolve({ value: 1 }),
    });

    const flaky = new QueryObserver(client, {
      queryKey: ["rhizome", "flaky"],
      queryFn: () => (fail ? Promise.reject(new Error("offline")) : Promise.resolve({ value: 1 })),
    });

    const unsubscribes = [steady, flaky].map((observer) => observer.subscribe(() => undefined));
    const stop = observeRefetches(cache);
    await vi.waitFor(() => expect(flaky.getCurrentResult().data).toEqual({ value: 1 }));
    takeBatch(identity);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });

    fail = true;
    trackRefresh(cache, [{ queryKey: ["rhizome"] }]);
    void client.invalidateQueries({ queryKey: ["rhizome"] });
    recordCommit("view-table", "update", 1, 1);
    await vi.advanceTimersByTimeAsync(1_000);
    stop();
    unsubscribes.forEach((unsubscribe) => unsubscribe());

    expect(takeBatch(identity)?.refresh).toMatchObject({ refetched: 1, unchanged: 1, churned: 0 });
  });

  it("does not judge a refresh whose slow refetch is still outstanding at the cap", async () => {
    const client = new QueryClient();
    const cache = client.getQueryCache();
    let slow = Promise.resolve();
    let slowValue = 1;

    const fast = new QueryObserver(client, {
      queryKey: ["rhizome", "fast"],
      queryFn: () => Promise.resolve({ value: 1 }),
    });

    const delayed = new QueryObserver(client, {
      queryKey: ["rhizome", "slow"],
      queryFn: async () => {
        await slow;

        return { value: slowValue };
      },
    });

    const unsubscribes = [fast, delayed].map((observer) => observer.subscribe(() => undefined));
    const stop = observeRefetches(cache);
    await vi.waitFor(() => expect(delayed.getCurrentResult().data).toEqual({ value: 1 }));
    takeBatch(identity);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });

    let release: () => void = () => undefined;
    slow = new Promise((resolve) => (release = resolve));
    slowValue = 2;
    trackRefresh(cache, [{ queryKey: ["rhizome"] }]);
    void client.invalidateQueries({ queryKey: ["rhizome"] });
    recordCommit("view-table", "update", 1, 1);
    await vi.advanceTimersByTimeAsync(10_000);
    release();
    await vi.advanceTimersByTimeAsync(0);
    stop();
    unsubscribes.forEach((unsubscribe) => unsubscribe());

    expect(takeBatch(identity)?.refresh).toMatchObject({ refetched: 2, unchanged: 1, churned: 0 });
  });
});
