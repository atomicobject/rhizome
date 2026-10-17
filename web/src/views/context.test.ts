import { describe, expect, it } from "vitest";

import {
  customViewHref,
  isViewContext,
  nodeHref,
  parseViewContext,
  parseViewNodeRef,
  workspaceViewHref,
  type ViewContext,
} from "./context";

describe("view context URLs", () => {
  it("accepts GraphQL nulls for navigation and direct node view launches", () => {
    const ref = {
      notePath: "notes/a %.md",
      kind: "NOTE",
      fragment: null,
      nodeId: null,
      typeName: null,
      structuralFingerprint: null,
      parentId: null,
      startByte: null,
      endByte: null,
    };

    const context = { kind: "node" as const, type: "Story", ref };
    const canonical = { notePath: ref.notePath, kind: "NOTE" };
    expect(parseViewNodeRef(ref)).toEqual(canonical);
    expect(nodeHref(ref)).toBe("/notes?note=notes%2Fa+%25.md&kind=NOTE");
    expect(workspaceViewHref("detail", context)).toBe(`${nodeHref(ref)}&presentation=detail`);
    const url = new URL(customViewHref("detail", context), "https://example.test");
    expect(JSON.parse(url.searchParams.get("context") ?? "")).toEqual({
      ...context,
      ref: canonical,
    });
    expect(parseViewContext(context)).toEqual({ ...context, ref: canonical });
    expect(isViewContext(context)).toBe(false);
    const optional = { ...ref, fragment: undefined, nodeId: undefined };
    expect(parseViewNodeRef(optional)).toEqual(canonical);
    expect(customViewHref("detail", { ...context, ref: optional })).toBe(url.pathname + url.search);
  });

  it("preserves embedded identity and rejects invalid required and optional ref fields", () => {
    const ref = {
      notePath: "a.md",
      kind: "EMBEDDED",
      fragment: "^task",
      nodeId: "task",
      structuralFingerprint: "fingerprint",
      startByte: 0,
      endByte: 10,
    };

    expect(parseViewNodeRef(ref)).toEqual(ref);
    expect(nodeHref(ref)).toBe(
      "/notes?note=a.md&kind=EMBEDDED&nodeId=task&structural=fingerprint#%5Etask",
    );

    for (const invalid of [
      null,
      [],
      { ...ref, notePath: null },
      { ...ref, notePath: " " },
      { ...ref, kind: null },
      { ...ref, kind: " " },
      ...["fragment", "nodeId", "typeName", "parentId", "structuralFingerprint"].map((key) => ({
        ...ref,
        [key]: 7,
      })),
      { ...ref, startByte: "0" },
      { ...ref, endByte: Infinity },
    ]) {
      expect(parseViewNodeRef(invalid)).toBeNull();
      expect(parseViewContext({ kind: "node", type: "Story", ref: invalid })).toBeNull();
    }

    expect(parseViewContext({ kind: "node", type: "Story", ref, extra: null })).toBeNull();
  });

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

  it("accepts the workspace subject with no other keys", () => {
    expect(isViewContext({ kind: "workspace" })).toBe(true);
    const url = new URL(customViewHref("workspace.briefing", { kind: "workspace" }), "https://x");
    expect(JSON.parse(url.searchParams.get("context") ?? "")).toEqual({ kind: "workspace" });
  });

  it("rejects incomplete subjects and session state", () => {
    for (const value of [
      { kind: "group", group: "*" },
      { kind: "node", type: "Story" },
      { kind: "type", type: "Story", sessionId: "private" },
      { kind: "workspace", group: "Delivery" },
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
  expect(workspaceViewHref("workspace.overview", { kind: "workspace" })).toBe(
    "/notes?presentation=workspace.overview",
  );
});
