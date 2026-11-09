import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { resetSidebarForTests, useWorkbenchSidebar } from "./sidebarState";
import { bridgeSidebarShortcut } from "./WorkbenchSidebar";

// The sidebar shortcut from inside a same-origin frame, such as a sidebar
// panel's (SPEC-0136 R6).

const shortcut = { code: "Backslash", key: "\\", ctrlKey: true, metaKey: true, bubbles: true };

describe("bridgeSidebarShortcut", () => {
  beforeEach(() => {
    window.localStorage.clear();
    resetSidebarForTests();
  });

  afterEach(() => document.body.replaceChildren());

  it("toggles the sidebar from the frame's document, except in its text fields", () => {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    bridgeSidebarShortcut(frame);
    const sidebar = renderHook(() => useWorkbenchSidebar());
    const inner = frame.contentDocument!;
    const realm = inner.defaultView!;
    expect(sidebar.result.current.collapsed).toBe(false);

    inner.body.dispatchEvent(new realm.KeyboardEvent("keydown", shortcut));
    sidebar.rerender();
    expect(sidebar.result.current.collapsed).toBe(true);

    // The frame's own elements belong to its window, not this one.
    const field = inner.createElement("input");
    inner.body.append(field);
    field.dispatchEvent(new realm.KeyboardEvent("keydown", shortcut));
    sidebar.rerender();
    expect(sidebar.result.current.collapsed).toBe(true);

    inner.body.dispatchEvent(new realm.KeyboardEvent("keydown", shortcut));
    sidebar.rerender();
    expect(sidebar.result.current.collapsed).toBe(false);
  });
});
