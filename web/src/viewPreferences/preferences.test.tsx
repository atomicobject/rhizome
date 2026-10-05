import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { isString, isFiniteNumber } from "../api/parse";
import { deferredReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { publishVaultEvent } from "../query/vaultEvents";
import { resetViewPreferences } from "./api";
import { createPreferenceAccessor, useScopedViewPreference, useViewPreferences } from "./hooks";
import { clearPreferenceStores, getPreferenceStore, PreferenceStore } from "./store";
import { preferenceServer } from "./testServer";
import {
  canonicalPreferenceScope,
  type ViewPreferenceResponse,
  type ViewPreferenceScope,
} from "./types";

const http = withFakeFetch();

const scope: ViewPreferenceScope = {
  viewId: "dashboard",
  context: { kind: "group", group: "Delivery" },
};

afterEach(clearPreferenceStores);

describe("personal view preferences", () => {
  it("shares mounted consumers, queues independent keys, and preserves a concurrent remote edit", async () => {
    const server = preferenceServer(http);

    const first = renderHook(() =>
      useScopedViewPreference(scope, "vault", "density", {
        defaultValue: "two",
        validate: isString,
      }),
    );

    const second = renderHook(() =>
      useScopedViewPreference(scope, "vault", "columns", {
        defaultValue: 2,
        validate: isFiniteNumber,
      }),
    );

    await waitFor(() => expect(first.result.current.loading).toBe(false));
    server.change(scope, { remote: "retained" });
    await act(async () => {
      await Promise.all([first.result.current.set("one"), second.result.current.set(3)]);
    });
    expect(server.read(scope).values).toEqual({ remote: "retained", density: "one", columns: 3 });
    expect(first.result.current.value).toBe("one");
    expect(second.result.current.value).toBe(3);
    expect(first.result.current.pending).toBe(false);
  });

  it("hydrates before reporting ready and isolates subjects and explicit slots", async () => {
    preferenceServer(http);
    const delayed = deferredReply<ViewPreferenceResponse>();
    http.on("GET", "/api/v1/view-preferences", () => delayed.promise);

    const { result } = renderHook(() =>
      useScopedViewPreference(scope, "vault", "density", {
        defaultValue: "two",
        validate: isString,
      }),
    );

    expect(result.current.loading).toBe(true);
    await act(async () =>
      delayed.resolve({ scope, revision: 4, values: { density: "one" }, migrationClosed: true }),
    );
    expect(result.current.value).toBe("one");
    expect(result.current.loading).toBe(false);

    const isolated = getPreferenceStore(
      { ...scope, context: { kind: "group", group: "Other" } },
      "vault",
    );

    const slot = getPreferenceStore({ ...scope, slot: "right" }, "vault");
    const vault = getPreferenceStore(scope, "other-vault");
    expect(isolated).not.toBe(getPreferenceStore(scope, "vault"));
    expect(slot).not.toBe(getPreferenceStore(scope, "vault"));
    expect(vault).not.toBe(getPreferenceStore(scope, "vault"));
  });

  it("follows changed defaults after reset and never writes defaults while reading", async () => {
    const server = preferenceServer(http);

    const { result, rerender } = renderHook(
      ({ defaultValue }) =>
        useScopedViewPreference(scope, "vault", "density", { defaultValue, validate: isString }),
      { initialProps: { defaultValue: "two" } },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(server.writes).toEqual([]);
    await act(async () => {
      await result.current.set("one");
      await result.current.reset();
    });
    rerender({ defaultValue: "compact" });
    expect(result.current.value).toBe("compact");
    expect(result.current.overridden).toBe(false);
    expect(server.read(scope).values).toEqual({});
  });

  it("reports unsaved errors, retries failed writes, and allows reset to discard a failed queue", async () => {
    const server = preferenceServer(http);
    const store = getPreferenceStore(scope, "vault");
    await store.refresh();

    const accessor = createPreferenceAccessor(store, "density", {
      defaultValue: "two",
      validate: isString,
    });

    server.failWrites("disk full");
    await expect(accessor.set("one")).rejects.toThrow("disk full");
    expect(accessor.getSnapshot()).toMatchObject({ value: "one", pending: true });
    expect(accessor.getSnapshot().error?.message).toBe("disk full");
    server.failWrites(null);
    await accessor.retry();
    expect(accessor.getSnapshot()).toMatchObject({ value: "one", pending: false, error: null });
    server.failWrites("disk full");
    await expect(accessor.set("three")).rejects.toThrow("disk full");
    server.failWrites(null);
    await store.resetAll();
    expect(accessor.getSnapshot()).toMatchObject({ value: "two", pending: false, error: null });
  });

  it("recovers initial read failure and rejects invalid response/input data", async () => {
    preferenceServer(http);
    const delayed = deferredReply<ViewPreferenceResponse>();
    http.on("GET", "/api/v1/view-preferences", () => delayed.promise);
    const store = new PreferenceStore(scope, "vault", []);
    delayed.reject(new Error("offline"));
    await expect(store.refresh()).rejects.toThrow("offline");
    expect(store.getSnapshot()).toMatchObject({ loading: false });
    preferenceServer(http);
    await store.retry();

    const accessor = createPreferenceAccessor(store, "width", {
      defaultValue: 1,
      validate: isFiniteNumber,
    });

    await expect(accessor.set(Infinity)).rejects.toThrow("Invalid preference");
    http.on("GET", "/api/v1/view-preferences", () =>
      jsonReply({ scope, values: {}, revision: -1, migrationClosed: true }),
    );
    await expect(store.refresh()).rejects.toThrow("Invalid view preferences response");
  });

  it("imports every source once, accepts late sources, and prevents reset resurrection in another context", async () => {
    const server = preferenceServer(http);

    const store = getPreferenceStore(scope, "vault", [
      { migrationId: "old-query", values: { sort: "title" } },
    ]);

    await store.refresh();
    getPreferenceStore(scope, "vault", [
      { migrationId: "old-columns", values: { columns: ["title"] } },
    ]);
    await store.refresh();
    expect(server.read(scope).values).toEqual({ sort: "title", columns: ["title"] });
    await store.resetAll();
    getPreferenceStore(scope, "vault", [
      { migrationId: "leftover-density", values: { density: "one" } },
    ]);
    await store.refresh();
    expect(server.read(scope).values).toEqual({});
    const other = { ...scope, context: { kind: "group" as const, group: "Other" } };

    const otherStore = getPreferenceStore(other, "vault", [
      { migrationId: "leftover-density", values: { density: "one" } },
    ]);

    await otherStore.refresh();
    expect(server.read(other).values).toEqual({});
  });

  it("registers a later migration when a hook keeps the same scope", async () => {
    preferenceServer(http);

    const { result, rerender } = renderHook(
      ({ migrate }) =>
        useViewPreferences(
          scope,
          "vault",
          migrate ? [{ migrationId: "late-control", values: { density: "one" } }] : [],
        ),
      { initialProps: { migrate: false } },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    rerender({ migrate: true });
    await waitFor(() => expect(result.current.values.density).toBe("one"));
  });

  it("refreshes the matching instance after the server preference event without losing another key", async () => {
    const server = preferenceServer(http);
    const { result } = renderHook(() => useViewPreferences(scope, "vault"));
    await waitFor(() => expect(result.current.loading).toBe(false));
    server.change(scope, { density: "one", columns: ["title"] });
    act(() =>
      publishVaultEvent({
        event: "view_preferences.changed",
        data: JSON.stringify({ scope, vaultKey: "vault", revision: 1 }),
      }),
    );
    await waitFor(() =>
      expect(result.current.values).toEqual({ density: "one", columns: ["title"] }),
    );
  });

  it("keeps node identity stable when positions and fallback fragments change", () => {
    const node = {
      viewId: "node",
      context: {
        kind: "node" as const,
        type: "Task",
        ref: {
          notePath: "./notes/task.md",
          kind: "embedded",
          nodeId: "task-1",
          fragment: "old",
          startByte: 1,
        },
      },
    };

    expect(canonicalPreferenceScope(node)).toEqual(
      canonicalPreferenceScope({
        ...node,
        context: {
          ...node.context,
          ref: { ...node.context.ref, notePath: "notes/task.md", fragment: "new", startByte: 99 },
        },
      }),
    );
  });

  it("ignores another vault's preference event and refreshes a matching host-frame event", async () => {
    const server = preferenceServer(http);
    const { result } = renderHook(() => useViewPreferences(scope, "vault"));
    await waitFor(() => expect(result.current.loading).toBe(false));
    const initialReads = http.requests("GET", "/api/v1/view-preferences").length;
    await act(async () =>
      publishVaultEvent({
        event: "view_preferences.changed",
        data: JSON.stringify({ scope, vaultKey: "other" }),
      }),
    );
    expect(http.requests("GET", "/api/v1/view-preferences").length).toBe(initialReads);
    server.change(scope, { density: "one" });
    act(() =>
      window.dispatchEvent(
        new MessageEvent("message", {
          source: window.parent,
          origin: window.location.origin,
          data: {
            type: "rhizome:event",
            event: "view_preferences.changed",
            data: JSON.stringify({ scope, vaultKey: "vault" }),
          },
        }),
      ),
    );
    await waitFor(() => expect(result.current.values.density).toBe("one"));
  });

  it("publishes a visible error when a valid updater becomes invalid after conflict rebase", async () => {
    const server = preferenceServer(http);
    server.change(scope, { count: 4 });
    const store = getPreferenceStore(scope, "vault");
    await store.refresh();

    const count = createPreferenceAccessor(store, "count", {
      defaultValue: 0,
      validate: (value): value is number => isFiniteNumber(value) && value <= 5,
    });

    server.change(scope, { count: 5 });
    await expect(count.update((value) => value + 1)).rejects.toThrow(
      "Invalid preference value: count",
    );
    expect(count.getSnapshot()).toMatchObject({ value: 5, pending: true });
    expect(count.getSnapshot().error?.message).toBe("Invalid preference value: count");
    await store.resetAll();
    expect(count.getSnapshot()).toMatchObject({ value: 0, pending: false, error: null });
  });

  it("resets every widget in the host family, including persisted unmounted widgets, and leaves other hosts intact", async () => {
    const server = preferenceServer(http);
    const host = { ...scope, slot: "left" };
    const child = { ...host, widgetSlot: "chart" };
    const unmounted = { ...host, widgetSlot: "hidden" };
    const otherHost = { ...scope, slot: "right", widgetSlot: "chart" };
    server.change(host, { base: true });
    server.change(child, { density: "compact" });
    server.change(unmounted, { density: "compact" });
    server.change(otherHost, { density: "compact" });
    const hostStore = getPreferenceStore(host, "vault");
    const childStore = getPreferenceStore(child, "vault");
    const otherStore = getPreferenceStore(otherHost, "vault");
    await Promise.all([hostStore.refresh(), childStore.refresh(), otherStore.refresh()]);

    const widget = createPreferenceAccessor(childStore, "density", {
      defaultValue: "normal",
      validate: isString,
    });

    await widget.reset();
    expect(server.read(unmounted).values).toEqual({ density: "compact" });
    expect(server.read(host).values).toEqual({ base: true });
    await widget.set("compact");
    await hostStore.resetFamily();
    expect(hostStore.getSnapshot().values).toEqual({});
    expect(widget.getSnapshot()).toMatchObject({ value: "normal", overridden: false });
    expect(server.read(unmounted).values).toEqual({});
    expect(otherStore.getSnapshot().values).toEqual({ density: "compact" });

    const unseen = getPreferenceStore({ ...host, widgetSlot: "not-mounted-yet" }, "vault", [
      { migrationId: "old-unseen-widget", values: { density: "legacy" } },
    ]);

    await unseen.refresh();
    expect(unseen.getSnapshot().values).toEqual({});
  });

  it("refreshes child frame caches and cancels stale writes after a family reset event", async () => {
    const server = preferenceServer(http);
    const child = { ...scope, widgetSlot: "chart" };
    server.change(child, { density: "compact" });
    const store = getPreferenceStore(child, "vault");
    await store.refresh();

    const accessor = createPreferenceAccessor(store, "density", {
      defaultValue: "normal",
      validate: isString,
    });

    const delayed = deferredReply();
    http.on("PATCH", "/api/v1/view-preferences", () => delayed.promise);
    const write = accessor.set("dense");
    const rejected = expect(write).rejects.toThrow("Unsaved preferences were reset");
    await resetViewPreferences(scope, server.read(scope).revision, true);
    const host = renderHook(() => useViewPreferences(child, "vault"));
    act(() =>
      publishVaultEvent({
        event: "view_preferences.changed",
        data: JSON.stringify({ scope, vaultKey: "vault", includeSlots: true }),
      }),
    );
    delayed.resolve(
      {
        error: "view preferences changed",
        code: "CONFLICT",
        details: { current: server.read(child) },
      },
      409,
    );
    await rejected;
    await waitFor(() => expect(host.result.current.values).toEqual({}));
    expect(host.result.current.pending).toBe(false);
    expect(http.requests("PATCH", "/api/v1/view-preferences")).toHaveLength(1);
  });

  it("does not cancel its own family reset when its server event arrives before the response", async () => {
    const server = preferenceServer(http);
    const { result } = renderHook(() => useViewPreferences(scope, "vault"));
    await waitFor(() => expect(result.current.loading).toBe(false));
    server.onReset((eventScope, includeSlots) =>
      publishVaultEvent({
        event: "view_preferences.changed",
        data: JSON.stringify({ scope: eventScope, vaultKey: "vault", includeSlots }),
      }),
    );
    await act(async () => {
      await result.current.resetFamily();
    });
    expect(result.current).toMatchObject({ values: {}, pending: false, error: null });
  });

  it("drains immediately consecutive writes made by direct store callers", async () => {
    const server = preferenceServer(http);
    const store = getPreferenceStore(scope, "vault");
    await store.refresh();
    await store.patch({ set: { first: true } });
    await store.patch({ set: { second: true } });
    expect(server.read(scope).values).toEqual({ first: true, second: true });
    expect(store.getSnapshot().pending).toBe(false);
  });
});
