import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";

/**
 * Stand-in for the browser `EventSource`. Extends `EventTarget` so
 * `addEventListener("index.changed", ...)` and `source.onmessage = ...` both
 * work the way the production code uses them.
 */
export class FakeEventSource extends EventTarget {
  static readonly instances: FakeEventSource[] = [];

  static latest(): FakeEventSource {
    const source = FakeEventSource.instances.at(-1);

    if (!source) throw new Error("fakeEventSource: no EventSource has been opened");

    return source;
  }

  readonly url: string;
  closed = false;
  readyState = 0;
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string | URL) {
    super();
    this.url = String(url);
    FakeEventSource.instances.push(this);
  }

  close(): void {
    this.closed = true;
  }

  /** Deliver a server event. Named events reach `addEventListener(type)`; "message" also reaches `onmessage`. */
  emit(data: string, type = "message"): void {
    const event = new MessageEvent(type, { data });
    this.dispatchEvent(event);

    if (type === "message") this.onmessage?.(event);
  }

  emitJSON<T>(value: T, type = "message"): void {
    this.emit(JSON.stringify(value), type);
  }

  open(): void {
    const event = new Event("open");
    this.dispatchEvent(event);
    this.onopen?.(event);
  }

  /** The browser gave up, as it does on an HTTP error; it will not retry by itself. */
  fail(): void {
    this.readyState = 2;
    this.error();
  }

  error(): void {
    const event = new Event("error");
    this.dispatchEvent(event);
    this.onerror?.(event);
  }
}

/** Installs `FakeEventSource` as `globalThis.EventSource`; returns the restore function. */
export function installFakeEventSource(): () => void {
  FakeEventSource.instances.length = 0;
  const original = Object.getOwnPropertyDescriptor(globalThis, "EventSource");
  Object.defineProperty(globalThis, "EventSource", {
    configurable: true,
    writable: true,
    value: FakeEventSource,
  });

  return () => {
    if (original) {
      Object.defineProperty(globalThis, "EventSource", original);
    } else {
      Reflect.deleteProperty(globalThis, "EventSource");
    }
  };
}

/** Installs the fake before each test and restores after; instances live on `FakeEventSource.instances`. */
export function withFakeEventSource(): typeof FakeEventSource {
  let restore: () => void = () => {};

  beforeEach(() => {
    restore = installFakeEventSource();
  });
  afterEach(() => {
    // Unmount can flush pending effects that still need the browser fake.
    try {
      cleanup();
    } finally {
      restore();
    }
  });

  return FakeEventSource;
}
