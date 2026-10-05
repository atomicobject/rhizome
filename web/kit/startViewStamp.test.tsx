import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { VAULT_EVENT_MESSAGE } from "../src/lib/customViewMessages";
import { withFakeEventSource } from "../src/test/fakeEventSource";
import { withFakeFetch } from "../src/test/fakeFetch";
import { startView, type ViewConfig } from "./startView";

const http = withFakeFetch();

withFakeEventSource();

const stamp = "/views/_stamp/board";

const board: ViewConfig = {
  id: "board",
  name: "Board",
  origin: "repository",
  folder: "board",
  stamp,
  stampValue: "1",
};

let stop = () => {};

beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  http.on("GET", stamp, () => new Response("1"));
});

afterEach(() => {
  stop();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

async function start(hosted = false, view: ViewConfig = board) {
  const reload = vi.fn();
  stop = await startView(async () => ({}), view, { hosted, reload, render: vi.fn() });

  return reload;
}

function pendingStamp() {
  let resolve: (response: Response) => void = () => {};

  const promise = new Promise<Response>((finish) => (resolve = finish));

  return { promise, resolve };
}

function reconnect() {
  window.dispatchEvent(
    new MessageEvent("message", {
      source: window.parent,
      origin: window.location.origin,
      data: { type: VAULT_EVENT_MESSAGE, event: "stream.reconnected" },
    }),
  );
}

it("keeps one stamp read pending across 3.5 seconds of standalone polling", async () => {
  const pending = pendingStamp();
  http.on("GET", stamp, () => pending.promise);
  const reload = await start();
  await vi.advanceTimersByTimeAsync(3500);
  expect(http.requests("GET", stamp)).toHaveLength(1);

  pending.resolve(new Response("1"));
  await vi.advanceTimersByTimeAsync(500);
  expect(http.requests("GET", stamp)).toHaveLength(2);
  expect(reload).not.toHaveBeenCalled();
});

it.each([
  { hosted: false, phase: "fetch" },
  { hosted: false, phase: "body" },
  { hosted: true, phase: "fetch" },
  { hosted: true, phase: "body" },
])("recovers after a stalled $phase deadline (hosted: $hosted)", async ({ hosted, phase }) => {
  const pending = pendingStamp();
  let finishBody = () => {};

  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      finishBody = () => {
        controller.enqueue(new TextEncoder().encode("2"));
        controller.close();
      };
    },
  });

  let reads = 0;
  http.on("GET", stamp, () => {
    reads += 1;

    return reads === 1 ? pending.promise : new Response(reads === 2 ? "1" : "2");
  });
  const reload = await start(hosted);
  await vi.advanceTimersByTimeAsync(hosted ? 0 : 1000);
  const signal = http.requests("GET", stamp)[0].signal;

  if (hosted) {
    reconnect();
    reconnect();
  }

  await vi.advanceTimersByTimeAsync(9000);

  if (phase === "body") {
    // The deadline covers headers and body together, rather than restarting.
    pending.resolve(new Response(body));
    await vi.advanceTimersByTimeAsync(0);
  }

  await vi.advanceTimersByTimeAsync(999);
  expect(http.requests("GET", stamp)).toHaveLength(1);
  expect(signal?.aborted).toBe(false);
  await vi.advanceTimersByTimeAsync(1);
  expect(signal?.aborted).toBe(true);

  if (!hosted) await vi.advanceTimersByTimeAsync(1000);

  expect(http.requests("GET", stamp)).toHaveLength(2);
  expect(reload).not.toHaveBeenCalled();

  if (phase === "fetch") pending.resolve(new Response("2"));
  else finishBody();

  await vi.advanceTimersByTimeAsync(0);
  expect(reload).not.toHaveBeenCalled();

  if (hosted) reconnect();

  await vi.advanceTimersByTimeAsync(hosted ? 0 : 1000);
  expect(http.requests("GET", stamp)).toHaveLength(3);
  expect(reload).toHaveBeenCalledOnce();
});

it("aborts a standalone read and ignores its changed stamp after stop", async () => {
  const pending = pendingStamp();
  http.on("GET", stamp, () => pending.promise);
  const reload = await start();
  await vi.advanceTimersByTimeAsync(1000);
  const signal = http.requests("GET", stamp)[0].signal;
  stop();
  expect(signal?.aborted).toBe(true);

  pending.resolve(new Response("2"));
  await vi.advanceTimersByTimeAsync(3500);
  expect(http.requests("GET", stamp)).toHaveLength(1);
  expect(reload).not.toHaveBeenCalled();
  expect(vi.getTimerCount()).toBe(0);
});

it.each([
  { hosted: false, outcome: "success" },
  { hosted: false, outcome: "abort" },
  { hosted: true, outcome: "success" },
  { hosted: true, outcome: "abort" },
])(
  "stops during body read and ignores late $outcome (hosted: $hosted)",
  async ({ hosted, outcome }) => {
    let finishBody = () => {};

    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        finishBody = () => {
          if (outcome === "abort") {
            controller.error(new DOMException("Aborted", "AbortError"));
          } else {
            controller.enqueue(new TextEncoder().encode("2"));
            controller.close();
          }
        };
      },
    });

    http.on("GET", stamp, () => new Response(body));
    const readBody = vi.spyOn(Response.prototype, "text");
    const reload = await start(hosted);
    await vi.advanceTimersByTimeAsync(hosted ? 0 : 1000);
    expect(readBody).toHaveBeenCalledOnce();
    const signal = http.requests("GET", stamp)[0].signal;
    stop();
    expect(signal?.aborted).toBe(true);

    finishBody();
    await vi.advanceTimersByTimeAsync(3500);
    expect(reload).not.toHaveBeenCalled();
    expect(http.requests("GET", stamp)).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(0);
  },
);

it("recovers on later ticks after fetch, HTTP, and body-read failures", async () => {
  let reads = 0;
  http.on("GET", stamp, () => {
    reads += 1;

    if (reads === 1) throw new TypeError("Network unavailable");

    if (reads === 2) return new Response("Unavailable", { status: 503 });

    if (reads === 3) {
      return new Response(
        new ReadableStream({
          start(controller) {
            controller.error(new TypeError("Body unavailable"));
          },
        }),
      );
    }

    return new Response(reads === 4 ? "1" : "2");
  });
  const reload = await start();
  await vi.advanceTimersByTimeAsync(4000);
  expect(http.requests("GET", stamp)).toHaveLength(4);
  expect(reload).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(1000);
  expect(http.requests("GET", stamp)).toHaveLength(5);
  expect(reload).toHaveBeenCalledOnce();
});

it("skips hidden-page polls and detects an edit when the page becomes visible", async () => {
  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  http.on("GET", stamp, () => new Response("2"));
  const reload = await start();
  await vi.advanceTimersByTimeAsync(3500);
  expect(http.requests("GET", stamp)).toHaveLength(0);
  hidden.mockReturnValue(false);
  await vi.advanceTimersByTimeAsync(500);
  expect(http.requests("GET", stamp)).toHaveLength(1);
  expect(reload).toHaveBeenCalledOnce();
});

it("uses the served baseline to catch an edit before the first standalone poll", async () => {
  http.on("GET", stamp, () => new Response("2"));
  const reload = await start();
  await vi.advanceTimersByTimeAsync(1000);
  expect(reload).toHaveBeenCalledOnce();
  await vi.advanceTimersByTimeAsync(1000);
  expect(reload).toHaveBeenCalledOnce();
});

it("establishes a missing standalone baseline without reloading", async () => {
  let current = "1";
  http.on("GET", stamp, () => new Response(current));
  const reload = await start(false, { ...board, stampValue: undefined });
  await vi.advanceTimersByTimeAsync(1000);
  expect(reload).not.toHaveBeenCalled();
  current = "2";
  await vi.advanceTimersByTimeAsync(1000);
  expect(reload).toHaveBeenCalledOnce();
});

it.each(["startup", "reconnect"] as const)(
  "aborts a hosted %s check and ignores its late changed stamp",
  async (phase) => {
    const pending = pendingStamp();

    if (phase === "startup") http.on("GET", stamp, () => pending.promise);
    const reload = await start(true);
    await vi.advanceTimersByTimeAsync(0);

    if (phase === "reconnect") {
      http.on("GET", stamp, () => pending.promise);
      reconnect();
      await vi.advanceTimersByTimeAsync(0);
    }

    const requests = http.requests("GET", stamp);
    const signal = requests.at(-1)?.signal;
    stop();
    expect(signal?.aborted).toBe(true);

    pending.resolve(new Response("2"));
    reconnect();
    await vi.advanceTimersByTimeAsync(3500);
    expect(http.requests("GET", stamp)).toHaveLength(requests.length);
    expect(reload).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  },
);

it("checks again after a reconnect during startup and during the next pending check", async () => {
  const startup = pendingStamp();
  const catchUp = pendingStamp();
  let reads = 0;
  http.on("GET", stamp, () => {
    reads += 1;

    if (reads === 1) return startup.promise;

    if (reads === 2) return catchUp.promise;

    return new Response("2");
  });
  const reload = await start(true);
  reconnect();
  reconnect();
  await vi.advanceTimersByTimeAsync(0);
  expect(http.requests("GET", stamp)).toHaveLength(1);

  startup.resolve(new Response("1"));
  await vi.advanceTimersByTimeAsync(0);
  expect(http.requests("GET", stamp)).toHaveLength(2);
  expect(reload).not.toHaveBeenCalled();
  reconnect();
  catchUp.resolve(new Response("1"));
  await vi.advanceTimersByTimeAsync(0);
  expect(http.requests("GET", stamp)).toHaveLength(3);
  expect(reload).toHaveBeenCalledOnce();
});

it("discards a queued hosted reconnect check when stopped", async () => {
  const pending = pendingStamp();
  http.on("GET", stamp, () => pending.promise);
  const reload = await start(true);
  reconnect();
  stop();
  pending.resolve(new Response("2"));
  await vi.advanceTimersByTimeAsync(0);
  expect(http.requests("GET", stamp)).toHaveLength(1);
  expect(reload).not.toHaveBeenCalled();
  expect(vi.getTimerCount()).toBe(0);
});

it.each([false, true])("clears successful read deadlines (hosted: %s)", async (hosted) => {
  const reload = await start(hosted);
  await vi.advanceTimersByTimeAsync(hosted ? 0 : 1000);
  const signal = http.requests("GET", stamp)[0].signal;
  await vi.advanceTimersByTimeAsync(11000);
  expect(signal?.aborted).toBe(false);
  expect(reload).not.toHaveBeenCalled();
  stop();
  expect(vi.getTimerCount()).toBe(0);
});

it.each([false, true])("never checks bundled stamps (hosted: %s)", async (hosted) => {
  const reload = await start(hosted, { ...board, origin: "bundled" });
  reconnect();
  await vi.advanceTimersByTimeAsync(3500);
  expect(http.requests("GET", stamp)).toHaveLength(0);
  expect(reload).not.toHaveBeenCalled();
});
