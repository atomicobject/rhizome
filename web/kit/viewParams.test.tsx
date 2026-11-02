import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, onTestFinished, vi } from "vitest";

import { PARAMS_CHANGED_MESSAGE, VIEW_PARAMS_MESSAGE } from "../src/lib/customViewMessages";
import { coerceViewParam, readViewParams } from "../src/views/params";

const params = [
  { name: "tab", kind: "string", default: "recommendations" },
  { name: "page", kind: "int", default: 1 },
  { name: "wide", kind: "bool" },
  { name: "item", kind: "string", identity: true },
] as const;

const parentDescriptor = Object.getOwnPropertyDescriptor(window, "parent")!;

beforeEach(() => {
  const invocation = document.createElement("script");
  invocation.id = "rhizome-view-invocation";
  invocation.type = "application/json";
  invocation.textContent = JSON.stringify({
    view: { id: "compounding.workspace", name: "Workspace" },
    context: { kind: "standalone" },
    params,
  });
  document.head.append(invocation);
});

afterEach(() => {
  document.getElementById("rhizome-view-invocation")?.remove();
  Object.defineProperty(window, "parent", parentDescriptor);
  window.history.replaceState({}, "", "/");
  vi.restoreAllMocks();
});

describe("view param coercion", () => {
  it("reads each kind, applies defaults, and ignores undeclared or malformed values", () => {
    expect(coerceViewParam("int", "12")).toBe(12);
    expect(coerceViewParam("int", "1.5")).toBeUndefined();
    expect(coerceViewParam("bool", "1")).toBe(true);
    expect(coerceViewParam("bool", "false")).toBe(false);
    expect(coerceViewParam("bool", "yes")).toBeUndefined();

    expect(readViewParams([...params], "?tab=health&page=x&wide=true&other=1")).toEqual({
      tab: "health",
      page: 1,
      wide: true,
    });
    expect(readViewParams([...params], "")).toEqual({ tab: "recommendations", page: 1 });
  });
});

describe("standalone view params", () => {
  async function kit(url: string) {
    window.history.replaceState({}, "", url);
    vi.resetModules();

    return import("./index");
  }

  it("follows the page's own URL and pushes UI params into its history", async () => {
    const { useViewParams, setViewParams } = await kit("/views/compounding.workspace?page=3");
    const { result } = renderHook(() => useViewParams());
    expect(result.current).toEqual({ tab: "recommendations", page: 3 });
    const before = window.history.length;

    act(() => setViewParams({ tab: "health", page: null }));
    expect(window.location.search).toBe("?tab=health");
    expect(window.history.length).toBe(before + 1);
    expect(result.current).toEqual({ tab: "health", page: 1 });

    // Back to the earlier entry, with the same identity: no reload.
    act(() => {
      window.history.replaceState({}, "", "/views/compounding.workspace?page=3");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(result.current).toEqual({ tab: "recommendations", page: 3 });

    expect(() => setViewParams({ other: "x" })).toThrow(/not declared/);
    expect(() => setViewParams({ page: "2" })).toThrow(/must be a int value/);
  });

  it("navigates to a new invocation when an identity param changes", async () => {
    const { setViewParams, getViewParams } = await kit("/views/compounding.workspace?item=a");
    expect(getViewParams().item).toBe("a");
    const assign = vi.fn();
    const location = Object.getOwnPropertyDescriptor(window, "location");
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...window.location, href: window.location.href, assign },
    });
    onTestFinished(() => {
      if (location) Object.defineProperty(window, "location", location);
    });

    setViewParams({ item: "b" });
    expect(assign).toHaveBeenCalledExactlyOnceWith(
      "http://localhost:3000/views/compounding.workspace?item=b",
    );
  });

  it("opens another view with params on the workspace URL", async () => {
    const { openView } = await kit("/views/compounding.workspace");
    const assign = vi.fn();
    const location = Object.getOwnPropertyDescriptor(window, "location");
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...window.location, assign },
    });
    onTestFinished(() => {
      if (location) Object.defineProperty(window, "location", location);
    });

    openView("compounding.workspace", { kind: "standalone" }, { tab: "health" });
    openView("compounding.workspace", { kind: "workspace" }, { item: "finding:a.md" });
    expect(assign.mock.calls).toEqual([
      ["/notes?view=compounding.workspace&tab=health"],
      ["/notes?presentation=compounding.workspace&item=finding%3Aa.md"],
    ]);
    // Views are untyped at runtime, so a list can reach openView.
    const list = JSON.parse('{"tab":[]}');
    expect(() => openView("x", { kind: "standalone" }, list)).toThrow(/params/);
  });
});

describe("hosted view params", () => {
  async function hostedKit(url: string) {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    onTestFinished(() => frame.remove());
    const parent = frame.contentWindow!;
    Object.defineProperty(window, "parent", { configurable: true, value: parent });
    window.history.replaceState({}, "", url);
    vi.resetModules();

    return { kit: await import("./index"), parent };
  }

  it("asks the host to change params and updates when the host answers", async () => {
    const { kit, parent } = await hostedKit("/views/compounding.workspace?hosted=1&tab=health");
    const posted = vi.spyOn(parent, "postMessage");
    const { result } = renderHook(() => kit.useViewParams());
    expect(result.current).toEqual({ tab: "health", page: 1 });

    kit.setViewParams({ tab: "memory" });
    expect(posted).toHaveBeenLastCalledWith(
      { type: PARAMS_CHANGED_MESSAGE, params: { tab: "memory" } },
      window.location.origin,
    );
    // The host owns the URL; nothing changes until it answers.
    expect(result.current.tab).toBe("health");

    // oxlint-disable-next-line anti-slop/no-unknown-parameters -- Tests send malformed host payloads.
    const answer = (source: MessageEventSource, data: unknown) =>
      act(() => {
        window.dispatchEvent(
          new MessageEvent("message", { source, origin: window.location.origin, data }),
        );
      });

    answer(window, { type: VIEW_PARAMS_MESSAGE, params: { tab: "ignored" } });
    expect(result.current.tab).toBe("health");

    answer(parent, { type: VIEW_PARAMS_MESSAGE, params: { tab: "memory", page: "x", junk: 1 } });
    expect(result.current).toEqual({ tab: "memory", page: 1 });
  });

  it("sends openView params to the host", async () => {
    const { kit, parent } = await hostedKit("/views/compounding.workspace?hosted=1");
    const posted = vi.spyOn(parent, "postMessage");

    kit.openView("compounding.item", { kind: "standalone" }, { item: "proposal:c1" });
    expect(posted).toHaveBeenLastCalledWith(
      {
        type: "rhizome:open-view",
        id: "compounding.item",
        context: { kind: "standalone" },
        params: { item: "proposal:c1" },
      },
      window.location.origin,
    );
  });
});
