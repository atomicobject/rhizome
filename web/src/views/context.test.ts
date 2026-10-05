import { describe, expect, it } from "vitest";

import {
  customViewHref,
  isViewContext,
  nodeHref,
  workspaceViewHref,
  type ViewContext,
} from "./context";

describe("view context URLs", () => {
  it("round trips concrete subjects and preserves canonical embedded identity", () => {
    const context: ViewContext = {
      kind: "node",
      type: "Story",
      ref: { notePath: "notes/a %.md", fragment: "^story-1", nodeId: "story-id", kind: "EMBEDDED" },
    };

    const url = new URL(customViewHref("story detail", context), "https://example.test");

    expect(url.pathname).toBe("/views/story%20detail");
    expect(JSON.parse(url.searchParams.get("context") ?? "")).toEqual(context);
    const node = new URL(nodeHref(context.ref, { view: "story-detail", beside: true }), url);

    expect(node.searchParams.get("note")).toBe(context.ref.notePath);
    expect(node.searchParams.get("nodeId")).toBe("story-id");
    expect(node.searchParams.get("kind")).toBe("EMBEDDED");
    expect(node.searchParams.get("presentation")).toBe("story-detail");
    expect(decodeURIComponent(node.hash)).toBe("#^story-1");
    expect(node.searchParams.has("view")).toBe(false);
    expect(customViewHref("legacy")).toBe("/views/legacy");
  });

  it("rejects incomplete subjects and session state", () => {
    for (const value of [
      { kind: "group", group: "*" },
      { kind: "node", type: "Story" },
      { kind: "type", type: "Story", sessionId: "private" },
    ]) {
      expect(isViewContext(value)).toBe(false);
    }
  });
});

it("routes registered native/custom IDs and normalized builtin choices to their workspace subjects", () => {
  expect(workspaceViewHref("native.stories", { kind: "type", type: "UserStory" })).toBe(
    "/notes/user-story?presentation=native.stories",
  );
  expect(
    workspaceViewHref("builtin:overview", { kind: "group", group: "Delivery & Planning" }),
  ).toBe("/notes/group/Delivery%20%26%20Planning?presentation=builtin%3Aoverview");
  expect(workspaceViewHref("standalone.board")).toBe("/notes?view=standalone.board");
});
