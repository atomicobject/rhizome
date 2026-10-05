import { getNodeEventsUrl } from "../api/client";
import { openReconnectingEventSource } from "../api/eventStream";
import { decodeJson, isNodeEvent } from "../api/parse";
import type { NodeEvent, NodeRef } from "../api/types";
import { canonicalNodeRefKey, nodeRefMatches } from "./nodeRef";

export type NodeEventListener = (event: NodeEvent) => void;

type Subscriber = {
  refs: Map<string, NodeRef>;
  listener: NodeEventListener;
};

const subscribers = new Map<number, Subscriber>();

let nextSubscriberID = 0;

let activeStream: (() => void) | null = null;

let activeUnionKey = "";

function refsByCanonicalKey(refs: readonly NodeRef[]): Map<string, NodeRef> {
  const result = new Map<string, NodeRef>();
  refs.forEach((ref) => {
    const key = canonicalNodeRefKey(ref);

    if (key) result.set(key, ref);
  });

  return result;
}

function unionRefs(): NodeRef[] {
  const refs = new Map<string, NodeRef>();
  subscribers.forEach((subscriber) => {
    subscriber.refs.forEach((ref, key) => refs.set(key, ref));
  });

  return Array.from(refs.entries())
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([, ref]) => ref);
}

function unionKey(refs: readonly NodeRef[]): string {
  return JSON.stringify(refs.map(canonicalNodeRefKey));
}

function closeActiveStream(): void {
  activeStream?.();
  activeStream = null;
}

function dispatch(event: NodeEvent): void {
  subscribers.forEach((subscriber) => {
    for (const ref of subscriber.refs.values()) {
      if (!nodeRefMatches(ref, event.ref)) continue;
      subscriber.listener(event);

      return;
    }
  });
}

// Events missed while the stream was down are unknown, so every subscribed
// node is treated as stale once it reconnects.
function dispatchStaleAll(): void {
  subscribers.forEach((subscriber) => {
    subscriber.refs.forEach((ref) =>
      subscriber.listener({ id: "reconnect", kind: "node.stale", ref, cause: "reconnect" }),
    );
  });
}

// Reconcile synchronously so last-subscriber cleanup closes immediately and
// stale sources are invalidated before they can dispatch. A React effect that
// changes its ref union can briefly rebuild during cleanup/setup; stable pane
// keys keep ordinary loading refreshes on the existing source.
function syncSource(): void {
  const refs = unionRefs();
  const nextUnionKey = unionKey(refs);

  if (nextUnionKey === activeUnionKey && activeStream) return;

  closeActiveStream();
  activeUnionKey = nextUnionKey;

  if (refs.length === 0 || typeof EventSource === "undefined") return;

  const stream = openReconnectingEventSource(getNodeEventsUrl(refs), (source, reconnected) => {
    let opened = reconnected;
    source.onopen = () => {
      if (stream !== activeStream) return;

      if (opened) dispatchStaleAll();
      opened = true;
    };

    source.onmessage = (message) => {
      if (stream !== activeStream) return;
      const event = decodeJson(message.data, isNodeEvent);

      if (event) dispatch(event);
    };
  });

  activeStream = stream;
}

/** Subscribe to node changes for the given canonical refs. */
export function subscribeNodeEvents(
  refs: readonly NodeRef[],
  listener: NodeEventListener,
): () => void {
  const subscriberID = nextSubscriberID;
  nextSubscriberID += 1;
  subscribers.set(subscriberID, {
    refs: refsByCanonicalKey(refs),
    listener,
  });
  syncSource();

  let subscribed = true;

  return () => {
    if (!subscribed) return;
    subscribed = false;
    subscribers.delete(subscriberID);
    syncSource();
  };
}
