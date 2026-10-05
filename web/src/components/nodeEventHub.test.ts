import { afterEach, describe, expect, it, vi } from "vitest";

import type { NodeEvent, NodeRef } from "../api/types";
import { FakeEventSource, withFakeEventSource } from "../test/fakeEventSource";
import { subscribeNodeEvents } from "./nodeEventHub";

const sources = withFakeEventSource();

const activeUnsubscribes = new Set<() => void>();

const firstRef: NodeRef = { notePath: "notes/a.md", kind: "NOTE" };

const secondRef: NodeRef = { notePath: "notes/b.md", kind: "NOTE" };

function subscribe(refs: readonly NodeRef[], listener: (event: NodeEvent) => void): () => void {
  const unsubscribe = subscribeNodeEvents(refs, listener);
  activeUnsubscribes.add(unsubscribe);

  return () => {
    activeUnsubscribes.delete(unsubscribe);
    unsubscribe();
  };
}

function event(ref: NodeRef): NodeEvent {
  return { id: `event-${ref.notePath}`, kind: "node.changed", ref };
}

function eventRefs(url: string): string[] {
  return new URL(url, "http://localhost").searchParams.getAll("ref");
}

describe("nodeEventHub", () => {
  afterEach(() => {
    activeUnsubscribes.forEach((unsubscribe) => unsubscribe());
    activeUnsubscribes.clear();
  });

  it("shares one source for the union and dispatches only matching refs", () => {
    const firstListener = vi.fn();
    const secondListener = vi.fn();
    const unsubscribeFirst = subscribe([firstRef, secondRef], firstListener);
    const unsubscribeSecond = subscribe([secondRef], secondListener);
    const source = FakeEventSource.latest();

    expect(sources.instances).toHaveLength(1);
    expect(source.closed).toBe(false);
    expect(eventRefs(source.url)).toEqual(["notes/a.md", "notes/b.md"]);

    source.emitJSON(event(firstRef));
    expect(firstListener).toHaveBeenCalledTimes(1);
    expect(secondListener).not.toHaveBeenCalled();

    source.emitJSON(event(secondRef));
    expect(firstListener).toHaveBeenCalledTimes(2);
    expect(secondListener).toHaveBeenCalledTimes(1);

    unsubscribeFirst();
    expect(source.closed).toBe(true);
    expect(FakeEventSource.latest().closed).toBe(false);
    unsubscribeSecond();
    expect(FakeEventSource.latest().closed).toBe(true);
  });

  it("replaces the union source and ignores events from the stale source", () => {
    const firstListener = vi.fn();
    const secondListener = vi.fn();
    subscribe([firstRef], firstListener);
    const staleSource = FakeEventSource.latest();

    subscribe([secondRef], secondListener);
    const currentSource = FakeEventSource.latest();

    expect(sources.instances).toHaveLength(2);
    expect(staleSource.closed).toBe(true);
    expect(currentSource.closed).toBe(false);
    expect(eventRefs(currentSource.url)).toEqual(["notes/a.md", "notes/b.md"]);

    staleSource.emitJSON(event(firstRef));
    expect(firstListener).not.toHaveBeenCalled();

    currentSource.emitJSON(event(firstRef));
    currentSource.emitJSON(event(secondRef));
    expect(firstListener).toHaveBeenCalledTimes(1);
    expect(secondListener).toHaveBeenCalledTimes(1);
  });

  it("closes the source after the last subscriber and makes unsubscribe idempotent", () => {
    const listener = vi.fn();
    const unsubscribeFirst = subscribe([firstRef], listener);
    const source = FakeEventSource.latest();
    const unsubscribeSecond = subscribe([firstRef], listener);

    unsubscribeFirst();
    expect(source.closed).toBe(false);
    unsubscribeSecond();
    expect(source.closed).toBe(true);
    unsubscribeSecond();
    expect(source.closed).toBe(true);
  });

  it("delivers events for a ref the server reports with a structural fingerprint", () => {
    const listener = vi.fn();
    subscribe([firstRef], listener);

    FakeEventSource.latest().emitJSON(event({ ...firstRef, structuralFingerprint: "fp-1" }));

    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("marks nodes stale when the browser reconnects the existing source", () => {
    const listener = vi.fn();
    subscribe([firstRef, secondRef], listener);
    const source = FakeEventSource.latest();

    source.open();
    expect(listener).not.toHaveBeenCalled();
    source.error();
    source.open();

    expect(sources.instances).toHaveLength(1);
    expect(listener).toHaveBeenCalledTimes(2);
    expect(listener).toHaveBeenCalledWith(
      expect.objectContaining({ kind: "node.stale", ref: firstRef }),
    );
    expect(listener).toHaveBeenCalledWith(
      expect.objectContaining({ kind: "node.stale", ref: secondRef }),
    );
  });

  it("reopens a stream the browser gave up on and marks its nodes stale", () => {
    vi.useFakeTimers();

    try {
      const listener = vi.fn();
      subscribe([firstRef], listener);
      const failed = FakeEventSource.latest();

      failed.fail();
      vi.advanceTimersByTime(3_000);
      const reopened = FakeEventSource.latest();
      expect(reopened).not.toBe(failed);
      expect(eventRefs(reopened.url)).toEqual(["notes/a.md"]);

      reopened.open();
      expect(listener).toHaveBeenCalledWith(
        expect.objectContaining({ kind: "node.stale", ref: firstRef }),
      );
    } finally {
      vi.useRealTimers();
    }
  });
});
