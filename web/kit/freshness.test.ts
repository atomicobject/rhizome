import { QueryClient, type QueryKey } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { queryKeys } from "../src/api/queryKeys";
import { VAULT_EVENT_MESSAGE } from "../src/lib/customViewMessages";
import { withFakeEventSource } from "../src/test/fakeEventSource";
import { EVENT_COALESCE_MS, EVENT_MAX_WAIT_MS, connectFreshness, kitQueryKeys } from "./freshness";

const events = withFakeEventSource();

const keys = {
  graphql: ["graphql", "query Specs { specs { title } }", {}],
  viewOwn: ["my-view", "rows"],
  groups: kitQueryKeys.displayGroups(),
  typeDoc: kitQueryKeys.typeDoc("Spec"),
  envelope: kitQueryKeys.validationEnvelope(),
  summaries: queryKeys.validationScopeSummaries(4, [{ kind: "type", key: "Spec" }]),
} satisfies Record<string, QueryKey>;

type KeyName = keyof typeof keys;

let client: QueryClient;

let stop: () => void = () => undefined;

beforeEach(() => {
  vi.useFakeTimers();
  client = new QueryClient();

  for (const key of Object.values(keys)) client.setQueryData(key, { cached: true });
});

afterEach(() => {
  stop();
  vi.useRealTimers();
});

function invalidated(): KeyName[] {
  const names: KeyName[] = ["graphql", "viewOwn", "groups", "typeDoc", "envelope", "summaries"];

  return names.filter((name) => client.getQueryState(keys[name])?.isInvalidated);
}

function settle() {
  vi.advanceTimersByTime(EVENT_COALESCE_MS);
}

describe("standalone pages", () => {
  it("open one stream and refresh queries by event class", () => {
    stop = connectFreshness(client, { hosted: false });
    expect(events.instances).toHaveLength(1);
    const stream = events.latest();

    stream.emit("", "index.changed");
    stream.emit("", "node.changed");
    expect(invalidated()).toEqual([]);
    settle();
    expect(invalidated()).toEqual(["graphql", "viewOwn", "groups"]);
  });

  it("refresh type documentation on schema changes and summaries on validation changes", () => {
    stop = connectFreshness(client, { hosted: false });
    events.latest().emit("", "schema.invalidated");
    settle();
    expect(invalidated()).toEqual(["graphql", "viewOwn", "groups", "typeDoc"]);

    client.clear();

    for (const key of Object.values(keys)) client.setQueryData(key, { cached: true });
    events.latest().emit("", "validation.invalidated");
    settle();
    // Summaries are immutable per generation; the envelope selects new keys.
    expect(invalidated()).toEqual(["groups", "envelope"]);
  });

  it("coalesce a burst into one refresh and refresh everything after a reconnect", () => {
    const refresh = vi.spyOn(client, "invalidateQueries");
    stop = connectFreshness(client, { hosted: false });
    const stream = events.latest();

    stream.open();
    stream.emit("", "index.invalidated");
    stream.emit("", "index.changed");
    stream.emit("", "index.invalidated");
    settle();
    expect(refresh).toHaveBeenCalledTimes(1);

    stream.open();
    settle();
    expect(refresh).toHaveBeenLastCalledWith();
    expect(invalidated()).toEqual(Object.keys(keys));
  });
});

it("refresh the view catalog and other data when another view's folder changes", () => {
  const catalog = ["group-views", "catalog"];
  client.setQueryData(catalog, { cached: true });
  stop = connectFreshness(client, { hosted: false, origin: "bundled" });
  events
    .latest()
    .emit(
      JSON.stringify({ id: "v-1", kind: "views.changed", data: { folder: "x" } }),
      "views.changed",
    );
  settle();
  expect(client.getQueryState(catalog)?.isInvalidated).toBe(true);
  expect(invalidated()).toEqual(["graphql", "viewOwn", "groups"]);
});

describe("event bursts", () => {
  it("refresh once per flush even when it covers data and validation", () => {
    const refresh = vi.spyOn(client, "invalidateQueries");
    stop = connectFreshness(client, { hosted: false });
    events.latest().emit("", "node.changed");
    events.latest().emit("", "validate.changed");
    settle();
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(invalidated()).toEqual(["graphql", "viewOwn", "groups", "envelope"]);
  });

  it("wait for a quiet gap, but refresh a long burst at least every max wait", () => {
    const refresh = vi.spyOn(client, "invalidateQueries");
    stop = connectFreshness(client, { hosted: false });
    const steady = EVENT_COALESCE_MS - 50;

    // A refresh cancels in-flight fetches, so each event in a steady burst
    // must not restart them.
    for (let elapsed = 0; elapsed < EVENT_MAX_WAIT_MS - steady; elapsed += steady) {
      events.latest().emit("", "node.changed");
      vi.advanceTimersByTime(steady);
    }

    expect(refresh).not.toHaveBeenCalled();
    events.latest().emit("", "node.changed");
    vi.advanceTimersByTime(steady);
    expect(refresh).toHaveBeenCalledTimes(1);

    events.latest().emit("", "node.changed");
    settle();
    expect(refresh).toHaveBeenCalledTimes(2);
  });
});

describe("hosted frames", () => {
  const forward = (message: { type: string; event?: unknown; data?: unknown }, source: Window) =>
    window.dispatchEvent(
      new MessageEvent("message", { source, origin: window.location.origin, data: message }),
    );

  it("open no stream and accept events only from the hosting workspace", () => {
    stop = connectFreshness(client, { hosted: true });
    expect(events.instances).toHaveLength(0);

    const stranger = document.createElement("iframe");
    document.body.append(stranger);
    forward({ type: VAULT_EVENT_MESSAGE, event: "schema.invalidated" }, stranger.contentWindow!);
    window.dispatchEvent(
      new MessageEvent("message", {
        source: window.parent,
        origin: "https://elsewhere.example",
        data: { type: VAULT_EVENT_MESSAGE, event: "schema.invalidated" },
      }),
    );
    forward({ type: VAULT_EVENT_MESSAGE, event: 7 }, window.parent);
    settle();
    expect(invalidated()).toEqual([]);

    forward({ type: VAULT_EVENT_MESSAGE, event: "validate.changed" }, window.parent);
    settle();
    expect(invalidated()).toEqual(["groups", "envelope"]);
    stranger.remove();
  });

  it("reload only for a change to their own repository folder", () => {
    const reload = vi.fn();

    const changed = (folder: string) =>
      forward(
        {
          type: VAULT_EVENT_MESSAGE,
          event: "views.changed",
          data: JSON.stringify({ id: "g-1", kind: "views.changed", data: { folder } }),
        },
        window.parent,
      );

    stop = connectFreshness(client, {
      hosted: true,
      origin: "repository",
      folder: "board",
      reload,
    });
    changed("other");
    changed("board/nested");
    forward({ type: VAULT_EVENT_MESSAGE, event: "views.changed", data: "not json" }, window.parent);
    expect(reload).not.toHaveBeenCalled();
    changed("board/");
    expect(reload).toHaveBeenCalledTimes(1);
    stop();

    stop = connectFreshness(client, { hosted: true, origin: "bundled", folder: "board", reload });
    changed("board");
    expect(reload).toHaveBeenCalledTimes(1);
  });
});
