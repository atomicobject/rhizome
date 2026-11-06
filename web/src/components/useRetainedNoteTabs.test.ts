import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Tab } from "./useNoteTabs";
import { useRetainedNoteTabs } from "./useRetainedNoteTabs";

const tabs: Tab[] = [
  { id: "home", kind: "home" },
  ...["a", "b", "c", "d", "e", "f", "g"].map((id): Tab => ({
    id,
    kind: "note",
    path: `${id}.md`,
    dirty: false,
  })),
];

describe("useRetainedNoteTabs", () => {
  it("keeps five recently visited bodies without loading background tabs", () => {
    const { result, rerender } = renderHook(useRetainedNoteTabs, {
      initialProps: { tabs, activeId: "a", protectEditors: false },
    });

    expect([...result.current]).toEqual(["a"]);

    for (const activeId of ["b", "c", "d", "e", "f", "g"])
      rerender({ tabs, activeId, protectEditors: false });

    expect([...result.current]).toEqual(["g", "f", "e", "d", "c"]);

    rerender({ tabs, activeId: "a", protectEditors: false });
    expect([...result.current]).toEqual(["a", "g", "f", "e", "d"]);
    expect(tabs).toHaveLength(8);
  });

  it("protects resident and visited editors without awakening suspended tabs", () => {
    const { result, rerender } = renderHook(useRetainedNoteTabs, {
      initialProps: { tabs, activeId: "a", protectEditors: false },
    });

    for (const activeId of ["b", "c", "d", "e", "f", "g"])
      rerender({ tabs, activeId, protectEditors: false });

    rerender({ tabs, activeId: "g", protectEditors: true });
    expect([...result.current]).toEqual(["g", "f", "e", "d", "c"]);

    rerender({ tabs, activeId: "a", protectEditors: true });
    rerender({ tabs, activeId: "b", protectEditors: true });
    expect(result.current).toEqual(new Set(["a", "b", "c", "d", "e", "f", "g"]));

    rerender({ tabs, activeId: "b", protectEditors: false });
    expect(result.current.size).toBe(7);
    rerender({ tabs, activeId: "home", protectEditors: false });
    expect([...result.current]).toEqual(["b", "a", "g", "f", "e"]);
  });

  it("forgets closed bodies without warming an unvisited replacement", () => {
    const { result, rerender } = renderHook(useRetainedNoteTabs, {
      initialProps: { tabs, activeId: "a", protectEditors: false },
    });

    for (const activeId of ["b", "c", "d", "e", "f", "g"])
      rerender({ tabs, activeId, protectEditors: false });

    const withoutC = tabs.filter((tab) => tab.id !== "c");

    rerender({ tabs: withoutC, activeId: "g", protectEditors: false });
    expect([...result.current]).toEqual(["g", "f", "e", "d"]);
    rerender({ tabs, activeId: "g", protectEditors: false });
    expect([...result.current]).toEqual(["g", "f", "e", "d"]);
    rerender({ tabs, activeId: "c", protectEditors: false });
    expect([...result.current]).toEqual(["c", "g", "f", "e", "d"]);
  });
});
