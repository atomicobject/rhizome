import { noteTabID } from "./noteTabIdentity";
import { parseStoredTabs, serializeTabs } from "./noteTabStorage";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { type NoteTab, type Tab, useNoteTabs } from "./useNoteTabs";
import type { ViewCatalogEntry } from "../api/types";

type ChangesAvailabilityProps = { changesAvailable: boolean | null };

const storedKey = (vaultName: string) => `rhizome:notes:tabs:v3:${vaultName}`;

const legacyStoredKey = (vaultName: string) => `rhizome:notes:tabs:v2:${vaultName}`;

const note = (path: string): NoteTab => ({
  id: `note:${path}`,
  kind: "note",
  path,
  dirty: false,
});

function notePaths(tabs: Tab[]): string[] {
  return tabs.flatMap((tab) => (tab.kind === "note" ? [tab.path] : []));
}

describe("parseStoredTabs", () => {
  it.each([2, 3])("accepts v%d entries and ignores obsolete preview state", (version) => {
    expect(
      parseStoredTabs(
        JSON.stringify({
          v: version,
          tabs: [
            { path: "a.md", preview: true, title: "A" },
            { path: "a.md", preview: false },
            { path: "b.md", preview: true },
            { path: "c.md", preview: false, dirty: true },
            { path: "" },
            { preview: true },
          ],
        }),
      ),
    ).toEqual([{ ...note("a.md"), title: "A" }, note("b.md"), note("c.md")]);
  });

  it("restores explicit HTML note paths without rewriting their extension", () => {
    expect(
      parseStoredTabs(
        JSON.stringify({
          v: 2,
          tabs: [{ path: "reports/status.html", title: "Status" }],
        }),
      ),
    ).toEqual([{ ...note("reports/status.html"), title: "Status" }]);
  });

  it("returns an empty list for malformed or unsupported payloads", () => {
    expect(parseStoredTabs(null)).toEqual([]);
    expect(parseStoredTabs("not json")).toEqual([]);
    expect(parseStoredTabs(JSON.stringify({ v: 2, tabs: [] }))).toEqual([]);
    expect(parseStoredTabs(JSON.stringify({ v: 1, tabs: [{ path: "a.md" }] }))).toEqual([]);
    expect(parseStoredTabs(JSON.stringify({ v: 3, tabs: "wrong" }))).toEqual([]);
  });

  it("restores search tabs from the version 2 payload", () => {
    expect(
      parseStoredTabs(
        JSON.stringify({
          v: 2,
          tabs: [
            {
              kind: "search",
              query: "architecture review",
              scope: "code",
              folder: "src/search",
              scrollTop: 180,
            },
          ],
        }),
      ),
    ).toMatchObject([
      {
        kind: "search",
        query: "architecture review",
        filters: { scope: "code", noteType: null, folder: "src/search" },
        scrollTop: 180,
      },
    ]);
  });

  it("restores a folder listing but drops an empty search without a folder", () => {
    expect(
      parseStoredTabs(
        JSON.stringify({
          v: 3,
          tabs: [
            { kind: "search", id: "search-tab:1", query: "", folder: "Notes" },
            { kind: "search", id: "search-tab:2", query: "" },
          ],
        }),
      ),
    ).toMatchObject([{ id: "search-tab:1", query: "", filters: { folder: "Notes" } }]);
  });

  it("writes search and view tabs with the version 3 payload", () => {
    expect(
      JSON.parse(
        serializeTabs([
          { id: "home", kind: "home" },
          {
            id: "search-tab:1",
            kind: "search",
            query: "architecture",
            filters: { scope: "all", noteType: null, folder: null },
            scrollTop: 0,
          },
          {
            id: "view:planning.backlog",
            kind: "view",
            viewId: "planning.backlog",
            title: "Planning backlog",
          },
        ]),
      ),
    ).toEqual({
      v: 3,
      tabs: [
        {
          kind: "search",
          id: "search-tab:1",
          query: "architecture",
          scope: "all",
          noteType: null,
          folder: null,
          scrollTop: 0,
        },
        { kind: "view", viewId: "planning.backlog", title: "Planning backlog" },
      ],
    });
  });

  it("restores view tabs and drops duplicate view ids", () => {
    expect(
      parseStoredTabs(
        JSON.stringify({
          v: 3,
          tabs: [
            { kind: "view", viewId: "planning.backlog", title: "Planning backlog" },
            { kind: "view", viewId: "planning.backlog", title: "Duplicate" },
            { kind: "view", title: "No id" },
          ],
        }),
      ),
    ).toEqual([
      {
        id: "view:planning.backlog",
        kind: "view",
        viewId: "planning.backlog",
        title: "Planning backlog",
      },
    ]);
  });
});

describe("useNoteTabs", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    window.history.replaceState({}, "", "/notes");
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("keeps note tab URLs free of the view parameter and restores the view tab on close", () => {
    window.history.replaceState({}, "", "/notes?view=planning.backlog");
    const { result, unmount } = renderHook(() => useNoteTabs("vault-a"));
    expect(result.current.activeId).toBe("view:planning.backlog");

    act(() => result.current.open("a.md#section"));
    expect(window.location.search).toBe("?note=a.md");
    expect(result.current.activeId).toBe("note:a.md");
    act(() => result.current.setFocusedTarget("note:a.md", "a.md#struct:section", "a.md#section"));
    expect(window.location.search).toBe("?note=a.md");
    expect(window.location.hash).toBe("#struct%3Asection");

    act(() => result.current.activate("view:planning.backlog"));
    expect(window.location.search).toBe("?view=planning.backlog");
    act(() => result.current.activate("note:a.md"));
    unmount();

    const restored = renderHook(() => useNoteTabs("vault-a"));
    expect(restored.result.current.activeId).toBe("note:a.md");
    act(() => restored.result.current.close("note:a.md"));
    expect(window.location.search).toBe("?view=planning.backlog");
    expect(restored.result.current.activeId).toBe("view:planning.backlog");
  });

  it("opens one view tab per view id, activates it, and closes back to Home", () => {
    const backlog: ViewCatalogEntry = {
      id: "planning.backlog",
      name: "Planning backlog",
      source: { kind: "query_recipe" },
      mount: { kind: "standalone" },
      defaults: { variant: "table" },
      variants: { table: { columns: [{ field: "title", label: "Title" }] } },
      definition: {
        apiVersion: "rhizome.view.v1",
        id: "planning.backlog",
        name: "Planning backlog",
        source: { kind: "query_recipe" },
        mount: { kind: "standalone" },
        variants: { table: { columns: [{ field: "title", label: "Title" }] } },
      },
    };

    window.history.replaceState({}, "", "/notes/plan");
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    act(() => result.current.openView(backlog));
    expect(result.current.activeTab).toEqual({
      id: "view:planning.backlog",
      kind: "view",
      viewId: "planning.backlog",
      title: "Planning backlog",
    });
    expect(`${window.location.pathname}${window.location.search}`).toBe(
      "/notes?view=planning.backlog",
    );

    // A view tab never takes over the Home tab's own collection.
    act(() => result.current.activate("home"));
    expect(`${window.location.pathname}${window.location.search}`).toBe("/notes/plan");
    act(() => result.current.activate("view:planning.backlog"));

    act(() => result.current.open("docs/spec.md"));
    act(() => result.current.openView(backlog));
    expect(result.current.tabs.filter((tab) => tab.kind === "view")).toHaveLength(1);
    expect(result.current.activeId).toBe("view:planning.backlog");

    act(() => result.current.close("view:planning.backlog"));
    expect(result.current.tabs.some((tab) => tab.kind === "view")).toBe(false);
    expect(result.current.activeId).toBe("note:docs/spec.md");
  });

  it("restores a stored view tab and activates it for a back or forward view URL", async () => {
    window.sessionStorage.setItem(
      storedKey("vault-a"),
      JSON.stringify({
        v: 3,
        tabs: [{ kind: "view", viewId: "planning.backlog", title: "Planning backlog" }],
      }),
    );
    window.history.replaceState({}, "", "/notes");
    const { result } = renderHook(() => useNoteTabs("vault-a"));
    await waitFor(() =>
      expect(result.current.tabs.filter((tab) => tab.kind === "view")).toHaveLength(1),
    );
    expect(result.current.activeId).toBe("home");

    act(() => {
      window.history.pushState({}, "", "/notes?view=planning.backlog");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() => expect(result.current.activeId).toBe("view:planning.backlog"));
    expect(result.current.tabs.filter((tab) => tab.kind === "view")).toHaveLength(1);
  });

  it("opens one reusable Problems tab and routes activation through its collection URL", () => {
    window.history.replaceState({}, "", "/notes/plan");
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    act(() => result.current.openCollection("issues"));
    expect(result.current.activeTab).toEqual({
      id: "collection:issues",
      kind: "collection",
      collection: "issues",
      issueScope: null,
      issueKey: null,
    });
    expect(window.location.pathname).toBe("/notes/issues");

    act(() => result.current.open("docs/spec.md"));
    act(() => result.current.openCollection("issues"));
    expect(result.current.tabs.filter((tab) => tab.kind === "collection")).toHaveLength(1);
    expect(result.current.activeId).toBe("collection:issues");

    act(() => result.current.activate("home"));
    expect(`${window.location.pathname}${window.location.search}`).toBe("/notes/plan");
  });

  it("retains Problems scope and selected issue across activation and close", () => {
    window.history.replaceState(
      {},
      "",
      "/notes/issues?issueScopeKind=note&issueScopeKey=docs%2Fspec.md&issue=issue-1",
    );
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    expect(result.current.activeTab).toMatchObject({
      id: "collection:issues",
      issueScope: { kind: "note", key: "docs/spec.md" },
      issueKey: "issue-1",
    });

    act(() => result.current.open("docs/other.md"));
    act(() => result.current.activate("collection:issues"));
    expect(`${window.location.pathname}${window.location.search}`).toBe(
      "/notes/issues?issueScopeKind=note&issueScopeKey=docs%2Fspec.md&issue=issue-1",
    );

    act(() => result.current.activate("note:docs/other.md"));
    act(() => result.current.close("note:docs/other.md"));
    expect(result.current.activeId).toBe("collection:issues");
    expect(`${window.location.pathname}${window.location.search}`).toBe(
      "/notes/issues?issueScopeKind=note&issueScopeKey=docs%2Fspec.md&issue=issue-1",
    );
  });

  it("keeps a Changes deep link while edit-session availability is unresolved", async () => {
    window.history.replaceState({}, "", "/notes/modified");
    const initialProps: ChangesAvailabilityProps = { changesAvailable: null };

    const { result, rerender } = renderHook(
      ({ changesAvailable }: ChangesAvailabilityProps) => useNoteTabs("vault-a", changesAvailable),
      { initialProps },
    );

    expect(result.current.activeId).toBe("collection:modified");
    expect(window.location.pathname).toBe("/notes/modified");

    rerender({ changesAvailable: true });
    expect(window.location.pathname).toBe("/notes/modified");

    rerender({ changesAvailable: false });
    await waitFor(() => expect(window.location.pathname).toBe("/notes"));
  });

  it("removes Changes and its unavailable route when editing becomes clean", async () => {
    const { result, rerender } = renderHook(
      ({ changesAvailable }) => useNoteTabs("vault-a", changesAvailable),
      { initialProps: { changesAvailable: false } },
    );

    act(() => result.current.openCollection("modified"));
    expect(result.current.tabs.some((tab) => tab.id === "collection:modified")).toBe(false);

    rerender({ changesAvailable: true });
    act(() => result.current.openCollection("modified"));
    expect(result.current.activeId).toBe("collection:modified");
    expect(window.location.pathname).toBe("/notes/modified");

    rerender({ changesAvailable: false });
    await waitFor(() => expect(window.location.pathname).toBe("/notes"));
    expect(result.current.tabs.some((tab) => tab.id === "collection:modified")).toBe(false);
  });

  it("retains independent filtered searches and reuses normalized identities", () => {
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    act(() => result.current.openSearch("  Architecture   Review "));
    const firstID = result.current.activeId;
    expect(result.current.activeTab).toMatchObject({
      kind: "search",
      query: "Architecture Review",
      filters: { scope: "all", noteType: null, folder: null },
    });
    expect(new URLSearchParams(window.location.search).get("searchTab")).toBe(firstID);

    act(() => result.current.openSearch("architecture review"));
    expect(result.current.tabs.filter((tab) => tab.kind === "search")).toHaveLength(1);
    expect(result.current.activeId).toBe(firstID);

    act(() => result.current.openSearch("architecture review", { scope: "code" }));
    expect(result.current.tabs.filter((tab) => tab.kind === "search")).toHaveLength(2);
    expect(new URLSearchParams(window.location.search).get("scope")).toBe("code");
  });

  it("opens and reuses a folder listing, then turns it into a ranked search", () => {
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    act(() => result.current.openSearch("", { folder: "" }));
    expect(result.current.tabs.some((tab) => tab.kind === "search")).toBe(false);

    act(() => result.current.openSearch("", { folder: "Notes" }));
    const id = result.current.activeId;
    expect(result.current.activeTab).toMatchObject({
      kind: "search",
      query: "",
      filters: { folder: "Notes" },
    });
    expect(new URLSearchParams(window.location.search).get("search")).toBe("");

    act(() => result.current.openSearch("", { folder: "/Notes/" }));
    expect(result.current.tabs.filter((tab) => tab.kind === "search")).toHaveLength(1);
    expect(result.current.activeId).toBe(id);

    act(() => result.current.refineSearch(id, { folder: "" }));
    expect(result.current.activeTab).toMatchObject({ query: "", filters: { folder: "Notes" } });

    act(() => result.current.refineSearch(id, { folder: "Notes" }, "plan"));
    expect(result.current.activeId).toBe(id);
    expect(result.current.activeTab).toMatchObject({ query: "plan", filters: { folder: "Notes" } });
    expect(new URLSearchParams(window.location.search).get("search")).toBe("plan");
  });

  it("refines one search tab in place and keeps its stable tab identity", () => {
    const { result } = renderHook(() => useNoteTabs("vault-a"));
    act(() => result.current.openSearch("architecture review"));
    const id = result.current.activeId;

    act(() => result.current.refineSearch(id, { scope: "notes", noteType: "Spec" }));

    expect(result.current.activeId).toBe(id);
    expect(result.current.tabs.filter((tab) => tab.kind === "search")).toHaveLength(1);
    expect(result.current.activeTab).toMatchObject({
      id,
      kind: "search",
      filters: { scope: "notes", noteType: "Spec", folder: null },
      scrollTop: 0,
    });
  });

  it("replays search filter history into the same tab", async () => {
    window.history.replaceState({}, "", "/notes?search=architecture&searchTab=search-tab%3A1");
    const { result } = renderHook(() => useNoteTabs("vault-a"));
    await waitFor(() => expect(result.current.activeId).toBe("search-tab:1"));

    act(() => result.current.refineSearch("search-tab:1", { scope: "notes" }));
    expect(window.location.search).toContain("searchTab=search-tab%3A1");

    act(() => {
      window.history.pushState({}, "", "/notes?search=architecture&searchTab=search-tab%3A1");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() => expect(result.current.activeId).toBe("search-tab:1"));
    expect(result.current.tabs.filter((tab) => tab.kind === "search")).toHaveLength(1);
    expect(result.current.activeTab).toMatchObject({
      id: "search-tab:1",
      filters: { scope: "all", noteType: null, folder: null },
    });
  });

  it("does not recycle a closed search id that remains in browser history", async () => {
    const { result } = renderHook(() => useNoteTabs("vault-a"));
    act(() => result.current.openSearch("search a"));
    const firstID = result.current.activeId;
    act(() => result.current.openSearch("search b"));
    const secondID = result.current.activeId;
    act(() => result.current.close(firstID));
    act(() => result.current.openSearch("search c"));
    const thirdID = result.current.activeId;

    expect(new Set([firstID, secondID, thirdID]).size).toBe(3);

    act(() => {
      window.history.pushState(
        {},
        "",
        `/notes?search=search+a&searchTab=${encodeURIComponent(firstID)}`,
      );
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() => expect(result.current.activeId).toBe(firstID));
    expect(result.current.tabs).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ id: firstID, query: "search a" }),
        expect.objectContaining({ id: secondID, query: "search b" }),
        expect.objectContaining({ id: thirdID, query: "search c" }),
      ]),
    );
  });

  it("does not restore tabs closed before vault hydration but restores untouched tabs", () => {
    window.history.replaceState({}, "", "/notes?note=closed.md");
    window.sessionStorage.setItem(
      legacyStoredKey("vault-a"),
      JSON.stringify({
        v: 2,
        tabs: [{ path: "closed.md" }, { path: "untouched.md" }],
      }),
    );

    const { result, rerender } = renderHook<
      ReturnType<typeof useNoteTabs>,
      { vault: string | null }
    >(({ vault }) => useNoteTabs(vault), { initialProps: { vault: null } });

    act(() => result.current.close("note:closed.md"));
    rerender({ vault: "vault-a" });
    expect(notePaths(result.current.tabs)).toEqual(["untouched.md"]);
    expect(result.current.activeId).toBe("home");
    expect(window.sessionStorage.getItem(storedKey("vault-a"))).not.toContain("closed.md");
  });

  it("does not restore search tabs closed before vault hydration", () => {
    window.history.replaceState({}, "", "/notes?search=closed");
    window.sessionStorage.setItem(
      storedKey("vault-a"),
      JSON.stringify({
        v: 2,
        tabs: [
          { kind: "search", id: "search-tab:5", query: "closed", scope: "all" },
          { kind: "search", id: "search-tab:6", query: "untouched", scope: "all" },
        ],
      }),
    );

    const { result, rerender } = renderHook<
      ReturnType<typeof useNoteTabs>,
      { vault: string | null }
    >(({ vault }) => useNoteTabs(vault), { initialProps: { vault: null } });

    const initialSearchID = result.current.activeId;
    expect(initialSearchID).toMatch(/^search-tab:/);
    act(() => result.current.close(initialSearchID));
    rerender({ vault: "vault-a" });

    expect(result.current.tabs.filter((tab) => tab.kind === "search")).toEqual([
      expect.objectContaining({ id: "search-tab:6", query: "untouched" }),
    ]);
    expect(result.current.activeId).toBe("home");
    expect(window.sessionStorage.getItem(storedKey("vault-a"))).not.toContain("search-tab:5");
  });

  it("retains an explicitly reopened tab after an early close", () => {
    window.history.replaceState({}, "", "/notes?note=reopened.md");
    window.sessionStorage.setItem(
      storedKey("vault-a"),
      JSON.stringify({
        v: 2,
        tabs: [{ path: "reopened.md" }],
      }),
    );

    const { result, rerender } = renderHook<
      ReturnType<typeof useNoteTabs>,
      { vault: string | null }
    >(({ vault }) => useNoteTabs(vault), { initialProps: { vault: null } });

    act(() => result.current.close("note:reopened.md"));
    act(() => result.current.open("reopened.md#new"));
    rerender({ vault: "vault-a" });
    expect(notePaths(result.current.tabs)).toEqual(["reopened.md"]);
    expect(result.current.activeId).toBe("note:reopened.md");
    expect(window.location.hash).toBe("#new");
  });

  it("keeps ordinary, beside, activation, and close navigation in URL sync", () => {
    const pushState = vi.spyOn(window.history, "pushState");
    const replaceState = vi.spyOn(window.history, "replaceState");
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    act(() => result.current.open("docs/a.md#section 1"));
    expect(window.location.pathname + window.location.search + window.location.hash).toBe(
      "/notes?note=docs%2Fa.md#section%201",
    );
    expect(result.current.activeId).toBe("note:docs/a.md");

    act(() => result.current.open("docs/b.md", { mode: "beside" }));
    expect(result.current.activeId).toBe("note:docs/a.md");
    expect(window.location.search).toContain("note=docs%2Fa.md");
    expect(pushState).toHaveBeenCalledTimes(1);

    act(() => result.current.activate("note:docs/b.md"));
    expect(window.location.search).toContain("note=docs%2Fb.md");
    expect(pushState).toHaveBeenCalledTimes(2);

    act(() => result.current.close("note:docs/b.md"));
    expect(result.current.activeId).toBe("note:docs/a.md");
    expect(window.location.search).toContain("note=docs%2Fa.md");
    expect(replaceState).toHaveBeenCalledTimes(1);

    act(() => result.current.open("docs/c.md"));
    act(() => result.current.open("docs/a.md"));
    expect(notePaths(result.current.tabs)).toEqual(["docs/a.md", "docs/c.md"]);
    expect(result.current.activeId).toBe("note:docs/a.md");

    act(() => result.current.open("docs/d.md", { mode: "beside" }));
    act(() => result.current.open("docs/c.md", { mode: "beside" }));
    expect(notePaths(result.current.tabs)).toEqual(["docs/a.md", "docs/d.md", "docs/c.md"]);
    expect(result.current.activeId).toBe("note:docs/a.md");

    act(() => result.current.activate("note:docs/d.md"));
    act(() => result.current.close("note:docs/d.md"));
    expect(result.current.activeId).toBe("note:docs/c.md");
    act(() => result.current.close("note:docs/c.md"));
    expect(result.current.activeId).toBe("note:docs/a.md");
    act(() => result.current.close("note:docs/a.md"));
    expect(result.current.activeId).toBe("home");
    expect(notePaths(result.current.tabs)).toEqual([]);
  });

  it("opens and restores retained HTML tabs with fragments and beside behavior", async () => {
    const first = renderHook(() => useNoteTabs("vault-a"));

    act(() => first.result.current.open("reports/one.html?filter=active#results"));
    expect(first.result.current.activeId).toBe("note:reports/one.html");
    expect(window.location.pathname + window.location.search + window.location.hash).toBe(
      "/notes?note=reports%2Fone.html&noteQuery=filter%3Dactive#results",
    );
    expect(new URLSearchParams(window.location.search).get("noteQuery")).toBe("filter=active");

    act(() => first.result.current.open("reports/two.HTM", { mode: "beside" }));
    expect(first.result.current.activeId).toBe("note:reports/one.html");
    expect(notePaths(first.result.current.tabs)).toEqual(["reports/one.html", "reports/two.HTM"]);
    await waitFor(() =>
      expect(window.sessionStorage.getItem(storedKey("vault-a"))).toContain("reports/one.html"),
    );

    first.unmount();
    window.history.replaceState(
      {},
      "",
      "/notes?note=reports%2Fone.html&noteQuery=filter%3Dactive#results",
    );
    const restored = renderHook(() => useNoteTabs("vault-a"));
    expect(notePaths(restored.result.current.tabs)).toEqual([
      "reports/one.html",
      "reports/two.HTM",
    ]);
    expect(restored.result.current.activeId).toBe("note:reports/one.html");
    expect(restored.result.current.findByPath("reports/one.html")?.fragment).toBe("results");
    expect(restored.result.current.findByPath("reports/one.html")?.query).toBe("filter=active");

    act(() => restored.result.current.open("reports/one.md"));
    act(() => restored.result.current.open("reports/one.html?filter=archived#summary"));
    expect(notePaths(restored.result.current.tabs)).toEqual([
      "reports/one.html",
      "reports/two.HTM",
      "reports/one.md",
    ]);
    expect(restored.result.current.activeId).toBe("note:reports/one.html");
    expect(restored.result.current.findByPath("reports/one.html")).toMatchObject({
      id: "note:reports/one.html",
      query: "filter=archived",
      fragment: "summary",
    });
  });

  it("restores query and fragment state through Back and Forward without duplicate tabs", async () => {
    const pushState = vi.spyOn(window.history, "pushState");
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    act(() => result.current.open("reports/status.html?filter=active#results"));
    act(() => result.current.open("reports/status.html?filter=archived#summary"));
    expect(notePaths(result.current.tabs)).toEqual(["reports/status.html"]);
    expect(result.current.findByPath("reports/status.html")).toMatchObject({
      query: "filter=archived",
      fragment: "summary",
    });

    act(() => {
      window.history.back();
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() =>
      expect(result.current.findByPath("reports/status.html")?.query).toBe("filter=active"),
    );
    expect(notePaths(result.current.tabs)).toEqual(["reports/status.html"]);
    expect(pushState).toHaveBeenCalledTimes(2);

    act(() => {
      window.history.replaceState({}, "", "/notes?note=reports%2Fstatus.html");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() => expect(result.current.findByPath("reports/status.html")?.query).toBeNull());
  });

  it("keeps the document query in both tab state and URL for fragment-only navigation", () => {
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    act(() => result.current.open("reports/status.html?filter=active#results"));
    act(() => result.current.open("reports/status.html#summary"));

    expect(result.current.findByPath("reports/status.html")).toMatchObject({
      query: "filter=active",
      fragment: "summary",
    });
    expect(new URLSearchParams(window.location.search).get("noteQuery")).toBe("filter=active");
    expect(window.location.hash).toBe("#summary");
  });

  it("preserves background fragments across activation, reload, and replacing an existing target", () => {
    const first = renderHook(() => useNoteTabs("vault-a"));
    act(() => first.result.current.open("a.md", { mode: "activate" }));
    act(() => first.result.current.open("b.md#section%201", { mode: "beside" }));
    expect(first.result.current.activeId).toBe("note:a.md");
    expect(window.location.hash).toBe("");
    first.unmount();
    const { result } = renderHook(() => useNoteTabs("vault-a"));
    act(() => result.current.activate("note:b.md"));
    expect(window.location.hash).toBe("#section%25201");
    act(() => result.current.activate("note:a.md"));
    act(() => result.current.open("b.md#different", { mode: "beside" }));
    act(() => result.current.activate("note:b.md"));
    expect(window.location.hash).toBe("#different");
    expect(notePaths(result.current.tabs)).toEqual(["a.md", "b.md"]);
  });

  it("rejects an obsolete focused target after newer navigation", () => {
    const { result } = renderHook(() => useNoteTabs("vault-a"));
    act(() => result.current.open("a.md#old"));
    act(() => result.current.open("a.md#new"));
    act(() => result.current.setFocusedTarget("note:a.md", "a.md#struct:obsolete", "a.md#old"));
    expect(window.location.hash).toBe("#new");
    expect(result.current.findByPath("a.md")?.fragment).toBe("new");
    act(() => result.current.setFocusedTarget("note:a.md", "a.md#struct:current", "a.md#new"));
    expect(window.location.hash).toBe("#struct%3Acurrent");
  });

  it("canonicalizes extensionless targets while preserving their fragment", () => {
    const { result } = renderHook(() => useNoteTabs("vault-a"));
    act(() => result.current.open("communities/engineering#overview", { mode: "activate" }));
    expect(window.location.search).toBe("?note=communities%2Fengineering.md");
    expect(window.location.hash).toBe("#overview");
    expect(result.current.findByPath("communities/engineering")).toMatchObject({
      path: "communities/engineering.md",
    });

    act(() => result.current.open("decisions/Design v2.0"));
    expect(window.location.search).toBe("?note=decisions%2FDesign+v2.0.md");
    act(() => result.current.open("notes/README.MD"));
    expect(window.location.search).toBe("?note=notes%2FREADME.MD");
    act(() => result.current.open("communities/engineering.md"));
    expect(result.current.activeId).toBe("note:communities/engineering.md");
    expect(notePaths(result.current.tabs)).toEqual([
      "communities/engineering.md",
      "decisions/Design v2.0.md",
      "notes/README.MD",
    ]);
  });

  it("reconciles popstate without duplicating existing tabs or writing history", async () => {
    const pushState = vi.spyOn(window.history, "pushState");
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    act(() => result.current.open("a.md", { mode: "activate" }));
    const pushesAfterOpen = pushState.mock.calls.length;
    act(() => {
      window.history.pushState({}, "", "/notes?note=a.md");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() => expect(result.current.activeId).toBe("note:a.md"));
    expect(notePaths(result.current.tabs)).toEqual(["a.md"]);
    expect(pushState).toHaveBeenCalledTimes(pushesAfterOpen + 1);

    act(() => {
      window.history.replaceState({}, "", "/notes");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(result.current.activeId).toBe("home");
    expect(notePaths(result.current.tabs)).toEqual(["a.md"]);

    act(() => {
      window.history.pushState({}, "", "/notes?note=b.md");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() => expect(result.current.activeId).toBe("note:b.md"));
    expect(notePaths(result.current.tabs)).toEqual(["a.md", "b.md"]);
    expect(
      result.current.tabs.filter((tab) => tab.kind === "note" && tab.path === "b.md"),
    ).toHaveLength(1);
  });

  it("opens a new retained search when shared chrome changes the location", async () => {
    const { result } = renderHook(() => useNoteTabs("vault-a"));

    act(() => {
      window.history.pushState({}, "", "/notes?search=first");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() => expect(result.current.activeTab).toMatchObject({ query: "first" }));

    act(() => {
      window.history.pushState({}, "", "/notes?search=second&scope=code");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() =>
      expect(result.current.activeTab).toMatchObject({
        kind: "search",
        query: "second",
        filters: { scope: "code" },
      }),
    );
    expect(result.current.tabs.filter((tab) => tab.kind === "search")).toHaveLength(2);
  });

  it.each(["setItem", "removeItem"] as const)(
    "restores legacy tabs when migration %s fails",
    (method) => {
      window.sessionStorage.setItem(
        legacyStoredKey("vault-a"),
        JSON.stringify({ v: 2, tabs: [{ path: "first.md" }, { path: "second.md" }] }),
      );
      vi.spyOn(Storage.prototype, method).mockImplementation(() => {
        throw new DOMException("Storage unavailable", "QuotaExceededError");
      });

      const { result } = renderHook(() => useNoteTabs("vault-a"));

      expect(notePaths(result.current.tabs)).toEqual(["first.md", "second.md"]);
      expect(result.current.activeId).toBe("home");
      act(() => result.current.activate("note:second.md"));
      expect(result.current.activeId).toBe("note:second.md");
    },
  );

  it("persists a vault tab set without dirty or active state", async () => {
    const { result } = renderHook(() => useNoteTabs("vault-a"));
    act(() => result.current.open("a.md", { mode: "activate" }));
    act(() => result.current.markDirty("a.md", true));
    await waitFor(() => expect(window.sessionStorage.getItem(storedKey("vault-a"))).not.toBeNull());

    const persisted = window.sessionStorage.getItem(storedKey("vault-a"));
    expect(persisted).toBe(JSON.stringify({ v: 3, tabs: [{ path: "a.md", kind: "note" }] }));
    expect(persisted).not.toContain("dirty");
    expect(persisted).not.toContain("active");
    expect(persisted).not.toContain("preview");
  });

  it("does not write before hydration and preserves early interactions for the initial vault", async () => {
    window.sessionStorage.setItem(
      storedKey("vault-a"),
      JSON.stringify({ v: 2, tabs: [{ path: "stored.md", preview: false }] }),
    );
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const initialVault: string | null = null;

    const { result, rerender } = renderHook<
      ReturnType<typeof useNoteTabs>,
      { vault: string | null }
    >(({ vault }) => useNoteTabs(vault), {
      initialProps: { vault: initialVault },
    });

    act(() => result.current.open("early.md", { mode: "activate" }));
    expect(setItem).not.toHaveBeenCalled();

    rerender({ vault: "vault-a" });
    await waitFor(() => expect(notePaths(result.current.tabs)).toEqual(["early.md", "stored.md"]));
  });

  it("keeps stored order and live titles when metadata arrives before vault hydration", async () => {
    window.history.replaceState({}, "", "/notes?note=stored.md");
    window.sessionStorage.setItem(
      storedKey("vault-a"),
      JSON.stringify({
        v: 2,
        tabs: [
          { path: "stored.md", preview: false },
          { path: "second.md", preview: false },
        ],
      }),
    );
    const initialVault: string | null = null;

    const { result, rerender } = renderHook<
      ReturnType<typeof useNoteTabs>,
      { vault: string | null }
    >(({ vault }) => useNoteTabs(vault), {
      initialProps: { vault: initialVault },
    });

    act(() => result.current.setTitle("note:stored.md", "Loaded title"));
    rerender({ vault: "vault-a" });
    await waitFor(() => expect(notePaths(result.current.tabs)).toEqual(["stored.md", "second.md"]));
    expect(result.current.tabs[1]).toMatchObject({
      path: "stored.md",
      title: "Loaded title",
    });
    expect(result.current.tabs[2]).toMatchObject({
      path: "second.md",
    });
  });

  it("keeps early interactions while migrating a stored preview", async () => {
    window.history.replaceState({}, "", "/notes?note=early.md");
    window.sessionStorage.setItem(
      storedKey("vault-a"),
      JSON.stringify({ v: 2, tabs: [{ path: "stored.md", preview: true }] }),
    );
    const initialVault: string | null = null;

    const { result, rerender } = renderHook<
      ReturnType<typeof useNoteTabs>,
      { vault: string | null }
    >(({ vault }) => useNoteTabs(vault), {
      initialProps: { vault: initialVault },
    });

    act(() => result.current.open("early-opened.md", { mode: "activate" }));
    rerender({ vault: "vault-a" });
    await waitFor(() =>
      expect(notePaths(result.current.tabs)).toEqual(["early.md", "early-opened.md", "stored.md"]),
    );
    act(() => result.current.open("another.md"));
    expect(notePaths(result.current.tabs)).toEqual([
      "early.md",
      "early-opened.md",
      "stored.md",
      "another.md",
    ]);
  });

  it("resets the tab set when the vault changes", async () => {
    window.sessionStorage.setItem(
      storedKey("vault-a"),
      JSON.stringify({ v: 2, tabs: [{ path: "a.md", preview: false }] }),
    );
    window.sessionStorage.setItem(
      storedKey("vault-b"),
      JSON.stringify({ v: 2, tabs: [{ path: "b.md", preview: false }] }),
    );

    const { result, rerender } = renderHook(({ vault }) => useNoteTabs(vault), {
      initialProps: { vault: "vault-a" },
    });

    await waitFor(() => expect(notePaths(result.current.tabs)).toEqual(["a.md"]));

    act(() => result.current.open("a2.md", { mode: "beside" }));
    rerender({ vault: "vault-b" });
    await waitFor(() => expect(notePaths(result.current.tabs)).toEqual(["b.md"]));
    expect(result.current.tabs.some((tab) => tab.kind === "note" && tab.path === "a.md")).toBe(
      false,
    );
    expect(result.current.tabs.some((tab) => tab.kind === "note" && tab.path === "a2.md")).toBe(
      false,
    );
  });
});

describe("node presentation routing", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    window.history.replaceState({}, "", "/notes/group/Delivery");
  });
  it("distinguishes structural subjects when they have no node ID or fragment", () => {
    const ref = { notePath: "a.md", kind: "SECTION", structuralFingerprint: "first" };
    expect(noteTabID(ref.notePath, ref)).not.toBe(
      noteTabID(ref.notePath, { ...ref, structuralFingerprint: "second" }),
    );
  });

  it("keeps a requested presentation on reopen unless explicitly replaced or cleared", () => {
    const { result } = renderHook(() => useNoteTabs("reopen-presentation-vault"));

    act(() => result.current.open("efforts/a.md", { presentation: "effort.detail" }));
    act(() => result.current.open("efforts/a.md"));
    expect(result.current.activeTab).toMatchObject({ presentation: "effort.detail" });
    expect(new URLSearchParams(window.location.search).get("presentation")).toBe("effort.detail");

    act(() => result.current.open("efforts/a.md", { presentation: "builtin:source" }));
    expect(result.current.activeTab).toMatchObject({ presentation: "builtin:source" });

    act(() => result.current.open("efforts/a.md", { presentation: null }));
    expect(result.current.activeTab).toMatchObject({ presentation: null });
    expect(new URLSearchParams(window.location.search).has("presentation")).toBe(false);

    act(() => result.current.open("efforts/a.md", { presentation: "effort.detail" }));
    act(() => {
      window.history.replaceState({}, "", "/notes?note=efforts%2Fa.md");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(result.current.activeTab).toMatchObject({ presentation: null });
  });

  it("opens a different same-file node beside without changing the active subject or presentation", async () => {
    const { result } = renderHook(() => useNoteTabs("same-file-vault"));
    const root = { notePath: "efforts/a.md", kind: "NOTE", nodeId: "root" };

    const task = {
      notePath: "efforts/a.md",
      kind: "EMBEDDED",
      nodeId: "task-1",
      fragment: "node:task-1",
      structuralFingerprint: "fingerprint",
    };

    act(() => result.current.open(root.notePath, { nodeRef: root, presentation: "effort.detail" }));
    const rootId = result.current.activeId;
    const before = result.current.activeTab;
    act(() =>
      result.current.open(`${task.notePath}#${task.fragment}`, {
        mode: "beside",
        nodeRef: task,
        presentation: "task.detail",
      }),
    );
    expect(result.current.activeId).toBe(rootId);
    expect(result.current.activeTab).toEqual(before);
    expect(new URLSearchParams(window.location.search).get("presentation")).toBe("effort.detail");
    const taskId = noteTabID(task.notePath, task);
    expect(result.current.tabs).toMatchObject([
      { id: "home" },
      { id: rootId, nodeRef: root, presentation: "effort.detail" },
      { id: taskId, nodeRef: task, presentation: "task.detail" },
    ]);
    act(() => result.current.activate(taskId));
    expect(result.current.activeId).toBe(taskId);
    expect(new URLSearchParams(window.location.search).get("nodeId")).toBe("task-1");
    act(() => result.current.activate(rootId));
    expect(result.current.activeTab).toEqual(before);
    expect(window.location.hash).toBe("");
    expect(new URLSearchParams(window.location.search).get("presentation")).toBe("effort.detail");

    act(() => result.current.markDirty(task.notePath, true));
    expect(
      result.current.tabs
        .filter((tab) => tab.kind === "note")
        .every((tab) => tab.kind === "note" && tab.dirty),
    ).toBe(true);
    await waitFor(() =>
      expect(
        parseStoredTabs(window.sessionStorage.getItem(storedKey("same-file-vault"))),
      ).toHaveLength(2),
    );
  });

  it("preserves canonical identity and presentation across beside-open, activation, and storage", async () => {
    const { result } = renderHook(() => useNoteTabs("presentation-vault"));

    const ref = {
      notePath: "efforts/a.md",
      kind: "HEADING",
      nodeId: "node-2",
      fragment: "Outcome",
      structuralFingerprint: "fingerprint",
    };

    act(() =>
      result.current.open("efforts/a.md#Outcome", {
        mode: "beside",
        nodeRef: ref,
        nodeId: ref.nodeId,
        nodeKind: ref.kind,
        structural: ref.structuralFingerprint,
        presentation: "effort.detail",
      }),
    );
    expect(result.current.activeId).toBe("home");
    act(() => result.current.activate(noteTabID(ref.notePath, ref)));
    const params = new URLSearchParams(window.location.search);
    expect(params.get("presentation")).toBe("effort.detail");
    expect(params.get("nodeId")).toBe("node-2");
    expect(params.get("kind")).toBe("HEADING");
    expect(params.get("structural")).toBe("fingerprint");
    await waitFor(() =>
      expect(
        parseStoredTabs(window.sessionStorage.getItem(storedKey("presentation-vault"))),
      ).toMatchObject([{ presentation: "effort.detail", nodeRef: ref }]),
    );
    act(() => result.current.setPresentation(noteTabID(ref.notePath, ref), "builtin:source"));
    expect(new URLSearchParams(window.location.search).get("presentation")).toBe("builtin:source");
    expect(new URLSearchParams(window.location.search).get("nodeId")).toBe("node-2");
  });
});
