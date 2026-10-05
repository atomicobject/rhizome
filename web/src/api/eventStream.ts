import { isCallable } from "./parse";

const RECONNECT_DELAY_MS = 3_000;

/** `EventSource.CLOSED`; test doubles do not define the static constants. */
const CLOSED = 2;

/**
 * Opens an EventSource that reopens after the browser gives up on it. Browsers
 * retry network errors on their own but close for good on an HTTP error, which
 * is what a proxy answers while the Rhizome server restarts. `setup` wires each
 * new source; `reconnected` is true for every source after the first.
 */
export function openReconnectingEventSource(
  url: string | (() => string),
  setup: (source: EventSource, reconnected: boolean) => void,
): () => void {
  let current: EventSource | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let stopped = false;

  const connect = (reconnected: boolean) => {
    const source = new EventSource(isCallable(url) ? url() : url);
    current = source;
    source.addEventListener?.("error", () => {
      if (stopped || source !== current || source.readyState !== CLOSED) return;
      // ponytail: fixed delay, matching the browser's own retry; back off if a
      // long outage ever makes the polling noticeable.
      timer = setTimeout(() => connect(true), RECONNECT_DELAY_MS);
    });
    setup(source, reconnected);
  };

  connect(false);

  return () => {
    stopped = true;
    clearTimeout(timer);
    current?.close();
    current = null;
  };
}
