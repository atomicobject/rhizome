import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { isString } from "../src/api/parse";
import { withFakeFetch } from "../src/test/fakeFetch";
import { clearPreferenceStores } from "../src/viewPreferences/store";
import { preferenceServer } from "../src/viewPreferences/testServer";
import { getViewPreference, useViewPreference } from "./preferences";
import { getViewConfiguration } from "./viewContext";

const http = withFakeFetch();

const scope = { viewId: "delivery", context: { kind: "group" as const, group: "Delivery" } };

let server: ReturnType<typeof preferenceServer>;

beforeEach(() => {
  server = preferenceServer(http);
  const invocation = document.createElement("script");
  invocation.id = "rhizome-view-invocation";
  invocation.type = "application/json";
  invocation.textContent = JSON.stringify({
    view: { id: "delivery", name: "Delivery" },
    context: scope.context,
    vaultKey: "vault",
    configuration: { density: "comfortable" },
  });
  document.head.append(invocation);
});

afterEach(() => {
  document.getElementById("rhizome-view-invocation")?.remove();
  clearPreferenceStores();
});

describe("kit personal preferences", () => {
  it("uses authored defaults and shares HTML accessor changes with React controls", async () => {
    const definition = {
      defaultValue: String(getViewConfiguration()?.density),
      validate: (value: unknown): value is string =>
        isString(value) && ["comfortable", "compact"].includes(value),
    };

    const html = getViewPreference("density", definition);
    const changed = vi.fn();
    const unsubscribe = html.subscribe(changed);
    const { result } = renderHook(() => useViewPreference("density", definition));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.value).toBe("comfortable");
    expect(server.writes).toEqual([]);
    await act(async () => {
      await html.set("compact");
    });
    expect(result.current.value).toBe("compact");
    expect(html.getSnapshot().value).toBe("compact");
    expect(changed).toHaveBeenCalled();
    await act(async () => {
      await result.current.set("comfortable");
    });
    expect(server.read(scope).values).toEqual({});
    expect(html.getSnapshot().overridden).toBe(false);
    unsubscribe();
  });

  it("rejects invalid writes and falls back from invalid saved values with reset available", async () => {
    server.change(scope, { density: 99 });

    const { result } = renderHook(() =>
      useViewPreference("density", {
        defaultValue: "comfortable",
        validate: (value): value is string =>
          isString(value) && ["comfortable", "compact"].includes(value),
      }),
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.value).toBe("comfortable");
    expect(result.current.error?.message).toBe("Invalid saved preference: density");
    await expect(result.current.set("invalid")).rejects.toThrow("Invalid preference value");
    await act(async () => {
      await result.current.reset();
    });
    expect(result.current.error).toBeNull();
  });

  it("keeps named widgets inside the inherited host slot", async () => {
    const invocation = document.getElementById("rhizome-view-invocation");

    if (!invocation) throw new Error("Missing fixture invocation");
    invocation.textContent = JSON.stringify({
      view: { id: "delivery", name: "Delivery" },
      context: scope.context,
      vaultKey: "vault",
      preferenceSlot: "left",
    });

    const widget = getViewPreference("density", {
      defaultValue: "normal",
      validate: isString,
      slot: "chart",
    });

    await widget.set("compact");
    expect(server.read({ ...scope, slot: "left", widgetSlot: "chart" }).values).toEqual({
      density: "compact",
    });
    expect(server.read({ ...scope, slot: "chart" }).values).toEqual({});
    expect(server.read({ ...scope, slot: "right", widgetSlot: "chart" }).values).toEqual({});
  });
});
