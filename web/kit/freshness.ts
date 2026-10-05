// Keeps a view's queries current and reloads it when its own source changes.
//
// The index catches up with a write asynchronously, so a refetch right after a
// commit can still read the old state. Rhizome announces vault changes on its
// event stream; refetching on those keeps every view current, including for
// edits made anywhere else. A framed view gets those events from the workspace
// that hosts it and opens no stream of its own; a standalone page opens one.
import type { Query, QueryClient } from "@tanstack/react-query";

import { getPublicEventsUrl } from "../src/api/client";
import { openReconnectingEventSource } from "../src/api/eventStream";
import { isJsonObject, isString } from "../src/api/parse";
import { isValidationSnapshotQuery, queryKeys } from "../src/api/queryKeys";
import { VAULT_EVENT_MESSAGE } from "../src/lib/customViewMessages";
import {
  INDEX_EVENTS,
  STREAM_RECONNECTED_EVENT,
  VAULT_EVENTS,
  VIEWS_CHANGED_EVENT,
  publishVaultEvent,
  type VaultEvent,
} from "../src/query/vaultEvents";
import type { ViewOrigin } from "./types";

/**
 * Kit query keys by freshness class. Type documentation is schema; validation
 * reads are validation; every other query, including a view's own `useQuery`
 * keys, is data.
 */
export const kitQueryKeys = {
  displayGroups: () => [...queryKeys.all, "display-groups"] as const,
  schema: () => [...queryKeys.all, "ontology"] as const,
  typeDoc: (name: string) => [...kitQueryKeys.schema(), "type-doc", name] as const,
  validation: () => queryKeys.validationAll(),
  validationEnvelope: () => queryKeys.validation(),
} as const;

type EventClass = "data" | "schema" | "validation" | "everything";

function eventClass(event: string): EventClass | null {
  if (event === STREAM_RECONNECTED_EVENT) return "everything";

  if (event === "schema.invalidated") return "schema";

  if (event === "validate.changed" || event === "validation.invalidated") return "validation";

  // The vault watcher never reports .rhizome, so another view's change is the
  // only signal that the view catalog moved.
  if (event === VIEWS_CHANGED_EVENT) return "data";

  return INDEX_EVENTS.some((name) => name === event) ? "data" : null;
}

const startsWith = (query: Query, prefix: readonly string[]) =>
  prefix.every((part, index) => query.queryKey[index] === part);

const isSchemaQuery = (query: Query) => startsWith(query, kitQueryKeys.schema());

const isValidationQuery = (query: Query) => startsWith(query, kitQueryKeys.validation());

const isDisplayGroupsQuery = (query: Query) => startsWith(query, kitQueryKeys.displayGroups());

/** Refresh the queries each event class makes stale, in one invalidation. */
export function invalidateEventClasses(queryClient: QueryClient, classes: ReadonlySet<EventClass>) {
  if (classes.has("everything")) {
    void queryClient.invalidateQueries();

    return;
  }

  const schema = classes.has("schema");

  const data = schema || classes.has("data");

  const validation = classes.has("validation");

  if (!data && !validation) return;

  // Validation pages and summaries are immutable within their generation; the
  // refreshed envelope selects new keys. Display groups carry issue counts.
  void queryClient.invalidateQueries({
    predicate: (query) => {
      if (isValidationQuery(query)) return validation && !isValidationSnapshotQuery(query);

      if (isSchemaQuery(query)) return schema;

      return data || (validation && isDisplayGroupsQuery(query));
    },
  });
}

/**
 * A burst refreshes once this long after its last event. A refresh cancels
 * in-flight fetches, so waiting for the gap keeps a steady burst from
 * restarting a slow query forever.
 */
export const EVENT_COALESCE_MS = 150;

/** A burst that never pauses still refreshes this often. */
export const EVENT_MAX_WAIT_MS = 1000;

type FreshnessOptions = {
  origin?: ViewOrigin;
  /** This view's folder under `.rhizome/views`, when it has one. */
  folder?: string | null;
  /** True when the workspace frames the view and forwards its events. */
  hosted: boolean;
  reload?: () => void;
  /** Called when the event stream reconnects, since events sent while it was down are lost. */
  onReconnect?: () => void;
};

const normalizeFolder = (folder: string) => folder.replace(/^\.?\/+|\/+$/g, "").replace(/^\.$/, "");

// The stream wraps every payload: {"id", "kind", "data": {"folder"}}.
function changedFolder(data: string | undefined) {
  if (!data) return null;

  try {
    const value: unknown = JSON.parse(data);
    const payload = isJsonObject(value) ? value.data : undefined;

    return isJsonObject(payload) && isString(payload.folder)
      ? normalizeFolder(payload.folder)
      : null;
  } catch {
    return null;
  }
}

/**
 * Invalidate `queryClient` by event class, coalescing bursts, and reload the
 * page when its own view folder changes. Bundled views never reload: their
 * source is part of the binary. Returns a function that stops listening.
 */
export function connectFreshness(queryClient: QueryClient, options: FreshnessOptions) {
  const reload = options.reload ?? (() => window.location.reload());

  const ownFolder =
    options.origin === "bundled" || options.folder == null ? null : normalizeFolder(options.folder);

  const pending = new Set<EventClass>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let burstStart: number | undefined;

  const flush = () => {
    timer = undefined;
    burstStart = undefined;
    const classes = new Set(pending);
    pending.clear();
    invalidateEventClasses(queryClient, classes);
  };

  const schedule = () => {
    burstStart ??= Date.now();
    clearTimeout(timer);
    const untilMaxWait = burstStart + EVENT_MAX_WAIT_MS - Date.now();
    timer = setTimeout(flush, Math.min(EVENT_COALESCE_MS, untilMaxWait));
  };

  const handle = ({ event, data }: VaultEvent) => {
    if (
      !options.hosted &&
      (event === "view_preferences.changed" || event === STREAM_RECONNECTED_EVENT)
    )
      publishVaultEvent({ event, data });

    if (event === VIEWS_CHANGED_EVENT && ownFolder !== null && changedFolder(data) === ownFolder) {
      reload();

      return;
    }

    if (event === STREAM_RECONNECTED_EVENT) options.onReconnect?.();

    const kind = eventClass(event);

    if (!kind) return;
    pending.add(kind);
    schedule();
  };

  const stop = options.hosted ? listenToHost(handle) : listenToStream(handle);

  return () => {
    clearTimeout(timer);
    stop();
  };
}

function listenToHost(handle: (event: VaultEvent) => void) {
  const onMessage = ({ origin, source, data }: MessageEvent) => {
    if (origin !== window.location.origin || source !== window.parent) return;

    if (!isJsonObject(data) || data.type !== VAULT_EVENT_MESSAGE || !isString(data.event)) return;

    handle(isString(data.data) ? { event: data.event, data: data.data } : { event: data.event });
  };

  window.addEventListener("message", onMessage);

  return () => window.removeEventListener("message", onMessage);
}

// The server does not replay events missed while the stream was down, so every
// reconnect refreshes everything.
function listenToStream(handle: (event: VaultEvent) => void) {
  return openReconnectingEventSource(getPublicEventsUrl(), (source, reconnected) => {
    let opened = reconnected;

    source.addEventListener("open", () => {
      if (opened) handle({ event: STREAM_RECONNECTED_EVENT });
      opened = true;
    });

    for (const event of VAULT_EVENTS) {
      source.addEventListener(event, ({ data }: MessageEvent) =>
        handle(isString(data) && data ? { event, data } : { event }),
      );
    }
  });
}
