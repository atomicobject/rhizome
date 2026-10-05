import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { ViewCatalogEntry } from "../api/types";
import { useViewState, useStoredViewChoice } from "./useViewState";
import { isString } from "../api/parse";
import { withFakeFetch } from "../test/fakeFetch";
import { preferenceServer } from "../viewPreferences/testServer";
import { clearPreferenceStores } from "../viewPreferences/store";
import { nativePreferenceScope, viewStorageKey } from "../viewPreferences/native";
import { useViewLayout } from "./useViewLayout";

const http = withFakeFetch();

let server: ReturnType<typeof preferenceServer>;

beforeEach(() => {
  clearPreferenceStores();
  server = preferenceServer(http);
});

function view(id: string): ViewCatalogEntry {
  return {
    id,
    name: id,
    source: { kind: "ontology_type", type: "Meeting" },
    mount: { kind: "type", type: "Meeting" },
    defaults: { variant: "table" },
    variants: { table: {}, card: {} },
    availableVariants: ["table", "card"],
    definition: {
      apiVersion: "rhizome.view.v1",
      id,
      name: id,
      source: { kind: "ontology_type", type: "Meeting" },
      mount: { kind: "type", type: "Meeting" },
      variants: { table: {}, card: {} },
    },
  };
}

afterEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  clearPreferenceStores();
});

describe("useViewState", () => {
  it("leaves native layout ownership with the shared target selector", () => {
    window.localStorage.setItem("rhizome.view.variant.meetings", "card");

    const { result } = renderHook(() => useViewState(view("meetings"), null));

    expect(result.current[0]).toEqual({});
  });

  it("ignores a saved layout the view does not offer", () => {
    window.localStorage.setItem("rhizome.view.variant.meetings", "kanban");

    const { result } = renderHook(() => useViewState(view("meetings"), null));

    expect(result.current[0]).toEqual({});
  });

  it("keeps each view's search across view switches and remounts", () => {
    const { result, rerender, unmount } = renderHook(
      ({ current }: { current: ViewCatalogEntry }) => useViewState(current, null),
      { initialProps: { current: view("meetings") } },
    );

    act(() => result.current[1]({ search: "tech lead", page: { offset: 200 } }));
    rerender({ current: view("people") });
    expect(result.current[0]).toEqual({});

    rerender({ current: view("meetings") });
    expect(result.current[0]).toEqual({ search: "tech lead", page: { offset: 200 } });

    unmount();
    const remounted = renderHook(() => useViewState(view("meetings"), null));
    expect(remounted.result.current[0]).toEqual({ search: "tech lead", page: { offset: 200 } });
  });

  it("keeps one vault's saved view state out of another vault's view", async () => {
    const first = renderHook(() =>
      useViewState(view("generated.type.Meeting.table"), "/vaults/one"),
    );

    act(() => first.result.current[1]({ search: "planning", variant: "card" }));
    await waitFor(() => expect(first.result.current[2].loading).toBe(false));
    first.unmount();

    const second = renderHook(() =>
      useViewState(view("generated.type.Meeting.table"), "/vaults/two"),
    );

    expect(second.result.current[0]).toEqual({});
    await waitFor(() => expect(second.result.current[2].loading).toBe(false));
  });

  it("hydrates durable fields and preserves tab search/page after acknowledged migration", async () => {
    const definition = view("meetings");
    const key = viewStorageKey("state", "vault", definition.id);
    window.sessionStorage.setItem(
      key,
      JSON.stringify({
        sort: [{ field: "title", direction: "desc" }],
        filters: [{ field: "status", op: "eq", value: "active" }],
        search: "planning",
        page: { offset: 20 },
      }),
    );
    const { result, unmount } = renderHook(() => useViewState(definition, "vault"));
    expect(result.current[2].loading).toBe(true);
    await waitFor(() => expect(result.current[2].loading).toBe(false));
    expect(result.current[0]).toEqual({
      sort: [{ field: "title", direction: "desc" }],
      filters: [{ field: "status", op: "eq", value: "active" }],
      search: "planning",
      page: { offset: 20 },
    });
    expect(window.sessionStorage.getItem(key)).toBeNull();
    expect(server.read(nativePreferenceScope(definition)).values).toEqual({
      "native.sort": [{ field: "title", direction: "desc" }],
      "native.filters": [{ field: "status", op: "eq", value: "active" }],
    });
    unmount();
    const remount = renderHook(() => useViewState(definition, "vault"));
    expect(remount.result.current[0].search).toBe("planning");
  });

  it("patches only changed query fields and never persists search/page", async () => {
    const definition = view("meetings");
    const { result } = renderHook(() => useViewState(definition, "vault"));
    await waitFor(() => expect(result.current[2].loading).toBe(false));
    act(() =>
      result.current[1]({
        sort: [{ field: "title", direction: "desc" }],
        search: "needle",
        page: { offset: 10 },
      }),
    );
    await waitFor(() => {
      expect(result.current[2].error).toBeNull();
      expect(result.current[2].pending).toBe(false);
    });
    act(() => result.current[1]({ ...result.current[0], group: { field: "status" } }));
    await waitFor(() => expect(result.current[2].pending).toBe(false));
    expect(server.writes[1].set).toEqual({ "native.group": { field: "status" } });
    expect(server.read(nativePreferenceScope(definition)).values).toEqual({
      "native.sort": [{ field: "title", direction: "desc" }],
      "native.group": { field: "status" },
    });
  });

  it("falls back from malformed query overrides with a resettable error", async () => {
    const definition = view("meetings");
    server.change(nativePreferenceScope(definition), {
      "native.sort": [{ field: "title", direction: "sideways" }],
    });
    const { result } = renderHook(() => useViewState(definition, "vault"));
    await waitFor(() => expect(result.current[2].loading).toBe(false));
    expect(result.current[0]).toEqual({});
    expect(result.current[2].error?.message).toBe("Invalid saved preference: native.sort");
    await act(async () => {
      await result.current[2].resetAll();
    });
    expect(result.current[2].error).toBeNull();
  });

  it("imports query and every local layout source into the same instance", async () => {
    const definition = view("meetings");
    window.sessionStorage.setItem(
      viewStorageKey("state", "vault", definition.id),
      JSON.stringify({ group: { field: "status" } }),
    );
    window.localStorage.setItem(
      viewStorageKey("density", "vault", definition.id),
      JSON.stringify("one-line"),
    );
    window.localStorage.setItem(
      viewStorageKey("columns", "vault", definition.id),
      JSON.stringify(["title"]),
    );
    window.localStorage.setItem(
      viewStorageKey("widths", "vault", definition.id),
      JSON.stringify({ title: 250 }),
    );
    const columns = [{ field: "title" }, { field: "status" }];

    const { result } = renderHook(() => ({
      state: useViewState(definition, "vault"),
      layout: useViewLayout(definition, "vault", null, columns, columns, undefined),
    }));

    await waitFor(() => expect(result.current.state[2].loading).toBe(false));
    expect(server.read(nativePreferenceScope(definition)).values).toEqual({
      "native.group": { field: "status" },
      "native.density": "one-line",
      "native.columns": ["title"],
      "native.widths": { title: 250 },
    });
    expect(result.current.layout.visibleFields).toEqual(["title"]);
    expect(result.current.layout.columnWidths).toEqual({ title: 250 });
    expect(window.localStorage.length).toBe(0);
  });

  it("resets column choices atomically and follows later authored columns", async () => {
    const definition = view("meetings");
    const initialProps = { columns: [{ field: "title" }, { field: "status" }] };

    const { result, rerender } = renderHook(
      ({ columns }) => useViewLayout(definition, "vault", null, columns, columns, undefined),
      { initialProps },
    );

    await waitFor(() => expect(result.current.preferences.loading).toBe(false));
    act(() => result.current.toggleColumn("status"));
    await waitFor(() => expect(result.current.preferences.pending).toBe(false));
    expect(result.current.visibleFields).toEqual(["title"]);
    act(() => result.current.resetColumns());
    await waitFor(() => expect(result.current.preferences.pending).toBe(false));
    rerender({ columns: [{ field: "title" }, { field: "owner" }] });
    expect(result.current.visibleFields).toEqual(["title", "owner"]);
    expect(server.read(nativePreferenceScope(definition)).values).toEqual({});
  });

  it("uses default widths and exposes malformed layout values through both aggregate statuses", async () => {
    const definition = view("meetings");
    const columns = [{ field: "title" }, { field: "status" }];
    server.change(nativePreferenceScope(definition), { "native.widths": { title: -20 } });

    const { result } = renderHook(() => ({
      state: useViewState(definition, "vault"),
      layout: useViewLayout(definition, "vault", null, columns, columns, undefined),
    }));

    await waitFor(() => expect(result.current.layout.preferences.loading).toBe(false));
    expect(result.current.state[0]).toEqual({});
    expect(result.current.state[2].error?.message).toBe("Invalid saved preference: native.widths");
    expect(result.current.layout.preferences.error?.message).toBe(
      "Invalid saved preference: native.widths",
    );
    expect(result.current.layout.columnWidths).toBeNull();
    expect(result.current.layout.visibleFields).toEqual(["title", "status"]);
    act(() => result.current.layout.setColumnWidths({ title: 220 }));
    await waitFor(() => expect(result.current.layout.preferences.error).toBeNull());
    expect(result.current.state[2].error).toBeNull();
    expect(result.current.layout.columnWidths).toEqual({ title: 220 });
  });

  it("keeps controls usable in mount-local state while vault identity is unavailable", () => {
    const definition = view("meetings");
    const columns = [{ field: "title" }, { field: "status" }];

    const { result } = renderHook(() => ({
      layout: useViewLayout(definition, null, null, columns, columns, undefined),
      density: useStoredViewChoice("density", null, definition, isString),
    }));

    act(() => {
      result.current.layout.toggleColumn("status");
      result.current.density[1]("one-line");
    });
    expect(result.current.layout.visibleFields).toEqual(["title"]);
    expect(result.current.density[0]).toBe("one-line");
    act(() => result.current.layout.resetColumns());
    expect(result.current.layout.visibleFields).toEqual(["title", "status"]);
    expect(server.writes).toEqual([]);
    expect(window.localStorage.length).toBe(0);
  });
});
