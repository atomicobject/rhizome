import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { recordResponse, responseGeneration, shareResponse } from "./coverage";
import {
  EVENT_COALESCE_MS,
  EVENT_MAX_WAIT_MS,
  createFrameGeneration,
  type FrameGeneration,
} from "./generation";

type Request = { key: string; answer: (generation: string) => void };

let client: QueryClient;

let frame: FrameGeneration;

let requests: Request[];

let stops: Array<() => void>;

beforeEach(() => {
  vi.useFakeTimers();
  client = new QueryClient({
    defaultOptions: {
      queries: { staleTime: Infinity, retry: false, structuralSharing: shareResponse },
    },
  });
  frame = createFrameGeneration();
  requests = [];
  stops = [frame.attach(client), frame.stop];
});

afterEach(() => {
  for (const stop of stops) stop();
  client.clear();
  vi.useRealTimers();
});

// A server read that the test answers, at a generation it names.
function serverRead(key: string) {
  return new Promise<{ data: { key: string }; generation: string }>((resolve) => {
    requests.push({
      key,
      answer: (generation) => {
        const data = { key };
        recordResponse(data, { generation });
        resolve({ data, generation });
      },
    });
  });
}

/** Mount a visible query that reads through the frame's rule. */
function watch(key: string) {
  const observer = new QueryObserver(client, {
    queryKey: [key],
    queryFn: ({ signal }) => frame.read(() => serverRead(key), signal),
  });

  stops.push(observer.subscribe(() => undefined));

  return observer;
}

const pending = (key?: string) => requests.filter((request) => !key || request.key === key);

async function answer(key: string, generation: string) {
  const index = requests.findIndex((request) => request.key === key);
  const [request] = requests.splice(index, 1);
  request.answer(generation);
  await vi.advanceTimersByTimeAsync(0);
}

const shown = (observer: { getCurrentResult: () => { data?: unknown } }) =>
  responseGeneration(observer.getCurrentResult().data);

const settle = () => vi.advanceTimersByTimeAsync(EVENT_COALESCE_MS);

describe("the frame generation rule", () => {
  it("applies a response that arrives before its event, and the event changes nothing", async () => {
    const page = watch("a");
    await answer("a", "G1");
    frame.set("G1");
    await settle();

    expect(shown(page)).toBe("G1");
    expect(frame.current()).toBe("G1");
    expect(pending()).toHaveLength(0);
  });

  it("drops a response older than an event that arrived while it was out, and reads once more", async () => {
    frame.set("G0");
    await settle();
    const page = watch("a");
    frame.set("G1");
    await answer("a", "G0");

    expect(shown(page)).toBeUndefined();
    expect(pending()).toHaveLength(0);
    await settle();
    expect(pending("a")).toHaveLength(1);
    await answer("a", "G1");
    await settle();

    expect(shown(page)).toBe("G1");
    expect(pending()).toHaveLength(0);
  });

  it("refetches a sibling read at G0 once when a G1 response arrives before the G1 event", async () => {
    frame.set("G0");
    await settle();
    const sibling = watch("sibling");
    await answer("sibling", "G0");
    const page = watch("a");
    await answer("a", "G1");

    expect(frame.current()).toBe("G1");
    await settle();
    expect(pending("sibling")).toHaveLength(1);
    await answer("sibling", "G1");
    frame.set("G1");
    await settle();

    expect(shown(sibling)).toBe("G1");
    expect(shown(page)).toBe("G1");
    expect(pending()).toHaveLength(0);
  });

  it("refetches once a query without a generation that was in flight when the generation moved", async () => {
    const replies: Array<(data: { rows: string }) => void> = [];

    const observer = new QueryObserver(client, {
      queryKey: ["plain"],
      queryFn: () => new Promise<{ rows: string }>((resolve) => replies.push(resolve)),
    });

    stops.push(observer.subscribe(() => undefined));
    await vi.advanceTimersByTimeAsync(0);

    frame.set("G1");
    replies[0]({ rows: "before G1" });
    await settle();
    expect(replies).toHaveLength(2);

    replies[1]({ rows: "after G1" });
    await settle();
    await settle();
    expect(replies).toHaveLength(2);
    expect(observer.getCurrentResult().data).toEqual({ rows: "after G1" });
  });

  it("applies two concurrent G1 responses with no refetch", async () => {
    frame.set("G0");
    await settle();
    const first = watch("a");
    const second = watch("b");
    await answer("a", "G1");
    await answer("b", "G1");
    await settle();

    expect([shown(first), shown(second)]).toEqual(["G1", "G1"]);
    expect(pending()).toHaveLength(0);
  });

  it("leaves the generation alone after a forced apply and checks that query once more", async () => {
    frame.set("G0");
    await settle();
    const page = watch("a");
    frame.set("G1");
    await answer("a", "G0");
    await settle();
    // The read after the drop applies whatever it gets.
    await answer("a", "G2");

    expect(shown(page)).toBe("G2");
    expect(frame.current()).toBe("G1");
    await settle();
    expect(pending("a")).toHaveLength(1);
    await answer("a", "G2");

    expect(frame.current()).toBe("G2");
    await settle();
    expect(pending()).toHaveLength(0);
  });

  it("renders a cold query under a steady write burst within the maximum wait", async () => {
    frame.set("G0");
    await settle();
    const page = watch("a");
    let generation = 0;
    const started = Date.now();

    // A write every 50 ms, each answered at the newest generation only after
    // the next write lands, so every first response is stale.
    while (shown(page) === undefined && Date.now() - started < 5 * EVENT_MAX_WAIT_MS) {
      generation += 1;
      frame.set(`G${generation}`);
      await vi.advanceTimersByTimeAsync(50);

      for (const request of pending("a")) await answer(request.key, `G${generation - 1}`);
    }

    expect(shown(page)).toBeDefined();
    expect(Date.now() - started).toBeLessThanOrEqual(EVENT_MAX_WAIT_MS + 100);
  });

  it("shows the final generation in every visible query after a burst stops", async () => {
    frame.set("G0");
    await settle();
    const pages = ["a", "b", "c"].map(watch);

    for (const key of ["a", "b", "c"]) await answer(key, "G0");

    for (let generation = 1; generation <= 12; generation += 1) {
      frame.set(`G${generation}`);
      await vi.advanceTimersByTimeAsync(40);

      // The server answers at whatever generation it had when the read began.
      for (const request of pending()) await answer(request.key, `G${generation - 1}`);
    }

    // Every remaining read, and the checks they lead to, answer at G12.
    for (let round = 0; round < 10; round += 1) {
      await settle();

      for (const request of pending()) await answer(request.key, "G12");
    }

    expect(pages.map(shown)).toEqual(["G12", "G12", "G12"]);
    await settle();
    expect(pending()).toHaveLength(0);
  });

  it("leaves a hidden query alone until it is shown, then refetches it once if it is behind", async () => {
    frame.set("G0");
    await settle();
    const page = watch("a");
    await answer("a", "G0");
    const hidden = new QueryObserver(client, { queryKey: ["hidden"] });
    client.setQueryData(["hidden"], () => {
      const data = { key: "hidden" };
      recordResponse(data, { generation: "G0" });

      return data;
    });

    frame.set("G1");
    await settle();
    expect(pending().map(({ key }) => key)).toEqual(["a"]);
    await answer("a", "G1");

    hidden.setOptions({
      queryKey: ["hidden"],
      queryFn: ({ signal }) => frame.read(() => serverRead("hidden"), signal),
    });
    stops.push(hidden.subscribe(() => undefined));
    frame.schedule();
    await settle();

    expect(pending().map(({ key }) => key)).toEqual(["hidden"]);
    expect(shown(page)).toBe("G1");
  });

  it("issues no reads when a page is shown again at an unchanged generation", async () => {
    frame.set("G0");
    await settle();
    watch("a");
    await answer("a", "G0");
    frame.schedule();
    await settle();

    expect(pending()).toHaveLength(0);
  });
});
