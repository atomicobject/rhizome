import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { RenderedFile } from "../../api/types";
import { jsonReply, withFakeFetch } from "../../test/fakeFetch";
import { useRhizomeMarkdownLinks } from "./useRhizomeMarkdownLinks";

const rendered: RenderedFile = {
  path: "docs/current.md",
  title: "Current",
  links: [
    { text: "Target", target: "docs/target.md", kind: "wikilink" },
    { text: "Ada", target: "People/Ada.md", kind: "wikilink" },
    { text: "Plan#Next", target: "docs/plan.md#Next", kind: "wikilink" },
  ],
};

describe("useRhizomeMarkdownLinks", () => {
  const http = withFakeFetch();

  function searchMatches(matches: Array<{ path: string; title: string }>) {
    http.on("GET", "/api/v1/search/notes", (request) =>
      jsonReply({
        query: request.query.get("q"),
        offset: 0,
        limit: 12,
        count: matches.length,
        total: matches.length,
        matches,
      }),
    );
  }

  it("resolves rendered aliases, local headings and block targets", async () => {
    const { result } = renderHook(() =>
      useRhizomeMarkdownLinks({ currentPath: rendered.path, rendered }),
    );

    await expect(result.current.resolveWikiLink?.("Ada")).resolves.toMatchObject({
      target: "People/Ada.md",
      status: "resolved",
    });
    await expect(result.current.resolveWikiLink?.("#Heading")).resolves.toMatchObject({
      target: "docs/current.md#Heading",
      status: "resolved",
    });
    await expect(result.current.resolveWikiLink?.("^block-id")).resolves.toMatchObject({
      target: "docs/current.md#^block-id",
      status: "resolved",
    });
    expect(http.count("GET", "/api/v1/search/notes")).toBe(0);
  });

  it("resolves a newly typed heading link through note search", async () => {
    searchMatches([{ path: "projects/new-note.md", title: "New Note" }]);
    const onOpen = vi.fn();

    const { result } = renderHook(() =>
      useRhizomeMarkdownLinks({ currentPath: rendered.path, rendered, onOpen }),
    );

    act(() => result.current.onOpenWikiLink?.("New Note#Details"));
    await waitFor(() =>
      expect(onOpen).toHaveBeenCalledWith("projects/new-note.md#Details", "stack"),
    );
    const request = http.requests("GET", "/api/v1/search/notes")[0];
    expect(request.query.get("q")).toBe("New Note");
    expect(request.query.get("limit")).toBe("12");
    expect(request.query.get("offset")).toBe("0");
  });

  it("leaves an ambiguous alias unresolved", async () => {
    searchMatches([
      { path: "one.md", title: "One" },
      { path: "two.md", title: "Two" },
    ]);
    const onOpen = vi.fn();

    const { result } = renderHook(() =>
      useRhizomeMarkdownLinks({ currentPath: rendered.path, rendered, onOpen }),
    );

    await expect(result.current.resolveWikiLink?.("Shared")).resolves.toMatchObject({
      status: "missing",
    });
    act(() => result.current.onOpenWikiLink?.("Shared"));
    await waitFor(() => expect(http.count("GET", "/api/v1/search/notes")).toBe(1));
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("opens explicit vault paths while marking unindexed links missing", async () => {
    searchMatches([]);
    const onOpen = vi.fn();

    const { result } = renderHook(() =>
      useRhizomeMarkdownLinks({ currentPath: rendered.path, rendered, onOpen }),
    );

    await expect(result.current.resolveWikiLink?.("docs/spec")).resolves.toMatchObject({
      target: "docs/spec.md",
      status: "missing",
    });
    await expect(result.current.resolveWikiLink?.("docs/spec.md#Goals")).resolves.toMatchObject({
      target: "docs/spec.md#Goals",
      status: "missing",
    });
    act(() => result.current.onOpenWikiLink?.("docs/spec"));
    await waitFor(() => expect(onOpen).toHaveBeenCalledWith("docs/spec.md", "stack"));
  });

  it("prefers the normalized relative path over a same-named search result", async () => {
    searchMatches([
      { path: "plans/plan.md", title: "Plan" },
      { path: "archive/plan.md", title: "Plan" },
    ]);
    const onOpen = vi.fn();

    const { result } = renderHook(() =>
      useRhizomeMarkdownLinks({ currentPath: rendered.path, rendered, onOpen }),
    );

    act(() => result.current.onOpenWikiLink?.("../plans/plan.md#Next"));
    await waitFor(() => expect(onOpen).toHaveBeenCalledWith("plans/plan.md#Next", "stack"));
    expect(http.requests("GET", "/api/v1/search/notes")[0].query.get("q")).toBe("plan");
  });

  it("resolves local links from the workspace path without rendered metadata", async () => {
    const { result } = renderHook(() =>
      useRhizomeMarkdownLinks({ currentPath: rendered.path, rendered: null }),
    );

    await expect(result.current.resolveWikiLink?.("#Heading")).resolves.toMatchObject({
      target: "docs/current.md#Heading",
      status: "resolved",
    });
    await expect(result.current.resolveWikiLink?.("^block-id")).resolves.toMatchObject({
      target: "docs/current.md#^block-id",
      status: "resolved",
    });
    expect(http.count("GET", "/api/v1/search/notes")).toBe(0);
  });
  it.each(["https://example.com", "mailto:ada@example.com"])(
    "opens the safe external link %s",
    (url) => {
      const open = vi.spyOn(window, "open").mockImplementation(() => null);

      const { result } = renderHook(() =>
        useRhizomeMarkdownLinks({ currentPath: rendered.path, rendered }),
      );

      act(() => result.current.onLinkClick?.(url));

      expect(open).toHaveBeenCalledWith(url, "_blank", "noopener,noreferrer");
      open.mockRestore();
    },
  );

  it("rejects custom schemes", () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    const onOpen = vi.fn();

    const { result } = renderHook(() =>
      useRhizomeMarkdownLinks({ currentPath: rendered.path, rendered, onOpen }),
    );

    act(() => result.current.onLinkClick?.("obsidian://open?vault=notes"));

    expect(open).not.toHaveBeenCalled();
    expect(onOpen).not.toHaveBeenCalled();
    open.mockRestore();
  });

  it("resolves an internal link once through the current open callback", async () => {
    const firstOpen = vi.fn();
    const secondOpen = vi.fn();

    const { result, rerender } = renderHook(
      ({ onOpen }) => useRhizomeMarkdownLinks({ currentPath: rendered.path, rendered, onOpen }),
      { initialProps: { onOpen: firstOpen } },
    );

    rerender({ onOpen: secondOpen });

    await act(async () => result.current.onOpenWikiLink?.("Target"));

    expect(firstOpen).not.toHaveBeenCalled();
    expect(secondOpen).toHaveBeenCalledTimes(1);
    expect(secondOpen).toHaveBeenCalledWith("docs/target.md", "stack");
  });
});
