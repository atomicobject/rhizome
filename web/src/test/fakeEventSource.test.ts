import { render } from "@testing-library/react";
import { createElement, useEffect } from "react";
import { describe, expect, it, vi } from "vitest";

import { FakeEventSource, withFakeEventSource } from "./fakeEventSource";

describe("fakeEventSource", () => {
  const sources = withFakeEventSource();

  it("keeps EventSource installed until mounted components finish cleanup", () => {
    function Subscriber() {
      useEffect(() => {
        const source = new EventSource("/api/v1/events");

        return () => {
          expect(globalThis.EventSource).toBe(FakeEventSource);
          source.close();
        };
      }, []);

      return null;
    }

    render(createElement(Subscriber));
    expect(sources.instances).toHaveLength(1);
    // Leave the component mounted to exercise the helper's afterEach teardown.
  });

  it("registers each opened stream with its URL and tracks close", () => {
    const source = new EventSource("/api/v1/events");

    expect(sources.instances).toHaveLength(1);
    expect(sources.latest().url).toBe("/api/v1/events");
    expect(sources.latest().closed).toBe(false);
    source.close();
    expect(sources.latest().closed).toBe(true);
  });

  it("delivers message data to onmessage and named events to listeners", () => {
    const source = new EventSource("/api/v1/nodes/events");
    const onMessage = vi.fn((event: MessageEvent<string>) => event.data);
    const onChanged = vi.fn((event: Event) => event.type);
    source.onmessage = onMessage;
    source.addEventListener("index.changed", onChanged);

    const fake = FakeEventSource.latest();
    fake.emitJSON({ ref: { notePath: "a.md" } });
    fake.emit("", "index.changed");

    expect(onMessage).toHaveBeenCalledTimes(1);
    expect(JSON.parse(onMessage.mock.results[0]?.value)).toEqual({ ref: { notePath: "a.md" } });
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("dispatches open to both the handler property and listeners", () => {
    const source = new EventSource("/api/v1/events");
    const onOpen = vi.fn();
    const listener = vi.fn();
    source.onopen = onOpen;
    source.addEventListener("open", listener);

    FakeEventSource.latest().open();

    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(listener).toHaveBeenCalledTimes(1);
  });
});
