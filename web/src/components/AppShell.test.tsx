import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { deferredReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
import type { OntologyEditSessionResponse } from "../api/types";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { AppShell } from "./AppShell";
import type { DesktopCommand } from "./desktopHost";
import { resetSidebarForTests, SIDEBAR_KEY } from "./sidebarState";

type StatusStub = { vaultName: string; vaultPath: string; indexState?: "initializing" | "ready" };

const originalTitle = document.title;

const emptyGraph = { nodes: [], edges: [], truncated: false };

describe("AppShell", () => {
  const http = withFakeFetch();

  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    resetSidebarForTests();
    // Workspaces render for real; they only need empty, well-formed reads.
    http
      .json("GET", "/api/v1/files/tree", { path: "", entries: [] })
      .json("GET", "/api/v1/graphs/global", emptyGraph)
      .json("GET", "/api/v1/ontology/atlas", { schemaPresent: false, types: [] })
      .json("GET", "/api/v1/ontology/summary", { schemaPresent: false, totalNotes: 0 })
      .json("GET", "/api/v1/ontology/types/__all__", { count: 0, notes: [] })
      .json("GET", "/api/v1/views", { views: [] })
      .json("GET", "/api/v1/search", { matches: [], warnings: [], lanes: [] })
      .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 });
  });

  afterEach(() => {
    document.title = originalTitle;
    window.history.replaceState({}, "", "/notes");
  });

  function statusReplies(...statuses: StatusStub[]) {
    http.on("GET", "/api/v1/status", () => {
      const next = statuses.length > 1 ? statuses.shift() : statuses[0];

      if (!next) throw new Error("offline");

      return jsonReply(next);
    });
  }

  it("renders a bare HTML note without workspace chrome when the route carries bare=1", async () => {
    statusReplies({ vaultName: "testvault", vaultPath: "/tmp/x" });

    const session = {
      id: "viewer-1",
      url: "https://viewer-1.content.example.test/reports/prototype.html",
      nonce: "nonce-1",
      expiresAt: "2030-01-01T00:00:00Z",
    };

    http
      .on("POST", "/api/v1/html-viewers", () => jsonReply(session, 201))
      .on("DELETE", "/api/v1/html-viewers/viewer-1", () => new Response(null, { status: 204 }));
    window.history.replaceState({}, "", "/notes?note=reports%2Fprototype.html&bare=1");

    render(<AppShell />);

    expect(await screen.findByLabelText("HTML note viewer")).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Primary" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open in Rhizome" })).toHaveAttribute(
      "href",
      "/notes?note=reports%2Fprototype.html",
    );
  });

  it("fetches /api/v1/status once, names the vault, and sets document.title", async () => {
    statusReplies({ vaultName: "/Users/someone/testvault", vaultPath: "/Users/someone/testvault" });

    render(<AppShell />);

    // The sidebar names the vault; a path shows as its folder name with the path as a tooltip.
    await screen.findByRole("complementary", { name: "Notes navigation" });
    await waitFor(() =>
      expect(document.querySelector(".workbench-sidebar__header")).toHaveTextContent(/^testvault$/),
    );
    expect(document.querySelector(".workbench-sidebar__header")).toHaveAttribute(
      "title",
      "/Users/someone/testvault",
    );
    expect(document.querySelector(".app-shell__brand small")).toBeNull();

    await waitFor(() => {
      expect(document.title).toBe("Rhizome · testvault");
    });

    // Explorer's sidebar names the vault too, and its root row shows the name AppShell handed it.
    fireEvent.click(screen.getByRole("button", { name: "Explorer" }));
    expect(await screen.findByTitle("View full graph")).toHaveTextContent("testvault");
    expect(
      screen.getByRole("complementary", { name: "Vault files" }).closest(".workbench-sidebar"),
    ).toHaveTextContent(/^testvault/);
    expect(document.querySelector(".app-shell__brand small")).toBeNull();
    expect(http.count("GET", "/api/v1/status")).toBe(1);
  });

  it("names no vault before the status fetch resolves", () => {
    const status = deferredReply<StatusStub>();
    http.on("GET", "/api/v1/status", () => status.promise);

    render(<AppShell />);

    expect(document.querySelector(".app-shell__brand small")).toBeNull();
    expect(document.querySelector(".workbench-sidebar__header")).toBeNull();

    // Cleanup the pending promise.
    status.resolve({ vaultName: "after", vaultPath: "/tmp/x" });
  });

  it("preserves a Changes deep link while vault and stored edits restore", async () => {
    window.history.replaceState({}, "", "/notes/modified");
    sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "changes-restore");
    localStorage.setItem(
      "rhizome:ontology-edit-session:%2Ftmp%2Fx:changes-restore",
      JSON.stringify({
        version: 3,
        sessionId: "edit-changes",
        ops: [{ kind: "setField", path: "notes/a.md", field: "title", value: "Draft" }],
        baseFingerprints: {},
        baseDocuments: [],
      }),
    );
    const status = deferredReply<StatusStub>();
    const restore = deferredReply<OntologyEditSessionResponse>();
    http.on("GET", "/api/v1/status", () => status.promise);
    http.on("POST", "/api/v1/edit-sessions/edit-changes/preview", () => restore.promise);
    http.json("POST", "/api/v1/edit-sessions/edit-changes/diff", {
      sessionId: "edit-changes",
      totals: { notes: 1, ops: 1, setField: 1 },
      notes: [],
    });

    render(<AppShell />);
    expect(window.location.pathname).toBe("/notes/modified");

    await act(async () => status.resolve({ vaultName: "testvault", vaultPath: "/tmp/x" }));
    await waitFor(() =>
      expect(http.count("POST", "/api/v1/edit-sessions/edit-changes/preview")).toBe(1),
    );
    expect(window.location.pathname).toBe("/notes/modified");

    await act(async () =>
      restore.resolve({
        sessionId: "edit-changes",
        status: "dirty",
        ops: [{ kind: "setField", path: "notes/a.md", field: "title", value: "Draft" }],
        hasUncommittedChanges: true,
        createdAt: "2026-09-14T00:00:00Z",
        updatedAt: "2026-09-14T00:00:00Z",
      }),
    );
    await waitFor(() => expect(screen.getByRole("tab", { name: "Changes" })).toBeVisible());
    expect(window.location.pathname).toBe("/notes/modified");
  });

  it("surfaces a failed workspace connection and retries it", async () => {
    let attempts = 0;
    http.on("GET", "/api/v1/status", () => {
      attempts += 1;

      if (attempts === 1) throw new Error("offline");

      return jsonReply({ vaultName: "reconnected", vaultPath: "/tmp/x" });
    });

    render(<AppShell />);

    expect(await screen.findByRole("alert")).toHaveTextContent("Workspace unavailable");
    fireEvent.click(screen.getByRole("button", { name: "Retry connection" }));

    await waitFor(() => expect(http.count("GET", "/api/v1/status")).toBe(2));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  });

  it("shows index preparation after the workspace status connects", async () => {
    statusReplies({ vaultName: "testvault", vaultPath: "/tmp/x", indexState: "initializing" });

    render(<AppShell />);

    const banner = await screen.findByTestId("index-ready-banner");
    expect(banner).toHaveAttribute("role", "status");
    expect(banner).toHaveTextContent("Preparing index");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(http.count("GET", "/api/v1/status")).toBe(1);
  });

  it("keeps one shared navigation shell while switching workspaces", async () => {
    statusReplies({ vaultName: "testvault", vaultPath: "/tmp/x" });

    render(<AppShell />);

    await waitFor(() => expect(http.count("GET", "/api/v1/status")).toBe(1));
    const shell = document.querySelector(".app-shell");
    const navigation = document.querySelector(".app-shell__nav");
    expect(shell).toHaveClass("app-shell");
    expect(shell).not.toHaveClass("app-shell--notes");
    expect(screen.getByRole("searchbox", { name: "Search this project" })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Ontology" }));
    expect(document.querySelector(".app-shell__nav")).toBe(navigation);
    expect(screen.getByRole("button", { name: "Ontology" })).toHaveClass("is-active");
    expect(screen.getByRole("searchbox", { name: "Search this project" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Explorer" }));
    fireEvent.click(screen.getByRole("button", { name: "Notes" }));
    expect(screen.getByRole("button", { name: "Notes" })).toHaveClass("is-active");
    expect(screen.getByRole("searchbox", { name: "Search this project" })).toBeVisible();

    // Wait one microtask cycle to let any leaked effects run.
    await Promise.resolve();
    expect(http.count("GET", "/api/v1/status")).toBe(1);
  });

  it("keeps the sidebar where the user puts it across searches and sections", async () => {
    statusReplies({ vaultName: "testvault", vaultPath: "/tmp/x" });
    localStorage.setItem("rhizome:notes:panes:v1", JSON.stringify({ sidebar: 300 }));

    render(<AppShell />);

    const sidebar = () => document.querySelector<HTMLElement>(".workbench-sidebar");
    await screen.findByRole("complementary", { name: "Notes navigation" });
    // The Notes shell's stored width carries over.
    expect(sidebar()?.style.getPropertyValue("--sidebar-width")).toBe("300px");

    // A search tab keeps the sidebar.
    fireEvent.change(screen.getByRole("searchbox", { name: "Search this project" }), {
      target: { value: "meetings" },
    });
    fireEvent.submit(screen.getByRole("searchbox", { name: "Search this project" }));
    await waitFor(() => expect(window.location.search).toContain("search=meetings"));
    expect(sidebar()).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Hide sidebar" }));
    expect(sidebar()).not.toBeVisible();
    expect(JSON.parse(localStorage.getItem(SIDEBAR_KEY) ?? "{}")).toEqual({
      collapsed: true,
      width: 300,
    });

    // ⌘\ or ⌃⌘S (Ctrl+\ or Ctrl+Alt+S here) shows it again, but not from a text field.
    const search = screen.getByRole("searchbox", { name: "Search this project" });
    fireEvent.keyDown(search, { code: "Backslash", key: "\\", ctrlKey: true, metaKey: true });
    expect(sidebar()).not.toBeVisible();
    fireEvent.keyDown(window, { code: "Backslash", key: "\\", ctrlKey: true, metaKey: true });
    expect(sidebar()).toBeVisible();

    // GraphQL has no sidebar, and its toggle and shortcuts do nothing there.
    fireEvent.click(screen.getByRole("button", { name: "GraphQL" }));
    const toggle = await screen.findByRole("button", { name: "Hide sidebar" });
    expect(toggle).toBeDisabled();
    expect(toggle).toHaveAttribute("title", "This section has no sidebar");
    fireEvent.keyDown(window, { code: "Backslash", key: "\\", ctrlKey: true, metaKey: true });
    expect(JSON.parse(localStorage.getItem(SIDEBAR_KEY) ?? "{}").collapsed).toBe(false);
  });

  it("follows the sidebar choice another tab makes", async () => {
    statusReplies({ vaultName: "testvault", vaultPath: "/tmp/x" });
    render(<AppShell />);
    await screen.findByRole("complementary", { name: "Notes navigation" });

    localStorage.setItem(SIDEBAR_KEY, JSON.stringify({ collapsed: true, width: 264 }));
    act(() => {
      window.dispatchEvent(new StorageEvent("storage", { key: SIDEBAR_KEY }));
    });
    expect(screen.getByRole("button", { name: "Show sidebar" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
  });

  it("labels the header's validation count with what it counts", async () => {
    statusReplies({ vaultName: "testvault", vaultPath: "/tmp/x" });
    http.json("GET", "/api/v2/validate", {
      status: "ok",
      generation: 1,
      health: "current_issues",
      snapshot: { issueCount: 862 },
    });
    window.history.replaceState({}, "", "/notes");

    render(<AppShell />);

    const badge = await screen.findByRole("button", { name: "Open 862 validation issues" });
    expect(badge).toHaveTextContent("862 issues");
  });

  it("submits trimmed project search from another route and ignores empty input", async () => {
    statusReplies({ vaultName: "testvault", vaultPath: "/tmp/x" });
    window.history.replaceState({}, "", "/explorer");

    render(<AppShell />);

    const search = screen.getByRole<HTMLInputElement>("searchbox", {
      name: "Search this project",
    });

    fireEvent.change(search, { target: { value: "   " } });
    fireEvent.submit(search.closest("form")!);
    expect(window.location.pathname).toBe("/explorer");

    fireEvent.change(search, { target: { value: "  embedded node identifiers  " } });
    fireEvent.submit(search.closest("form")!);

    await waitFor(() => expect(window.location.pathname).toBe("/notes"));
    expect(new URLSearchParams(window.location.search).get("search")).toBe(
      "embedded node identifiers",
    );
    expect(screen.getByRole("tab", { name: /embedded node identifiers/i })).toBeVisible();
  });

  it("focuses and selects project search with the platform shortcut", async () => {
    statusReplies({ vaultName: "testvault", vaultPath: "/tmp/x" });
    render(<AppShell />);

    const search = screen.getByRole<HTMLInputElement>("searchbox", {
      name: "Search this project",
    });

    fireEvent.change(search, { target: { value: "existing query" } });
    fireEvent.keyDown(window, { key: "k", metaKey: true });

    expect(search).toHaveFocus();
    expect(search.selectionStart).toBe(0);
    expect(search.selectionEnd).toBe("existing query".length);
  });

  describe("inside Rhizome Desktop", () => {
    const send = (
      detail:
        | DesktopCommand
        | { command: "section"; section: string }
        | { command: "theme"; theme: string },
    ) =>
      act(() => {
        window.dispatchEvent(new CustomEvent("rhizome:desktop", { detail }));
      });

    const report = () => JSON.parse(document.title.replace(/^rhizome-desktop:/, ""));

    beforeEach(() => {
      window.__RHIZOME_DESKTOP__ = true;
      statusReplies({ vaultName: "testvault", vaultPath: "/tmp/x" });
    });

    afterEach(() => {
      delete window.__RHIZOME_DESKTOP__;
    });

    it("hides the header and reports the section, search, and issues in the title", async () => {
      http.json("GET", "/api/v2/validate", {
        status: "ok",
        generation: 1,
        health: "current_issues",
        snapshot: { issueCount: 37 },
      });
      window.history.replaceState({}, "", "/notes?search=meetings");

      render(<AppShell />);

      expect(screen.queryByRole("navigation", { name: "Primary" })).not.toBeInTheDocument();
      expect(screen.queryByRole("searchbox", { name: "Search this project" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Keyboard shortcuts" })).toBeNull();
      await waitFor(() =>
        expect(report()).toEqual({
          v: 1,
          section: "notes",
          search: "meetings",
          issues: 37,
          health: "current_issues",
        }),
      );
      // The vault title never replaces the report.
      await waitFor(() => expect(http.count("GET", "/api/v1/status")).toBe(1));
      expect(document.title).toMatch(/^rhizome-desktop:/);
    });

    it("takes the sidebar choice the app sends and reports changes the page makes", async () => {
      render(<AppShell />);
      await screen.findByRole("complementary", { name: "Notes navigation" });
      const sidebar = () => document.querySelector<HTMLElement>(".workbench-sidebar");

      // Until the app sends its choice, the page reports none.
      await waitFor(() => expect(report().section).toBe("notes"));
      expect(report()).not.toHaveProperty("sidebarCollapsed");

      await send({ command: "sidebar", collapsed: true });
      expect(sidebar()).not.toBeVisible();
      await waitFor(() => expect(report().sidebarCollapsed).toBe(true));
      // A reply to the app's command is not a toggle the page made.
      expect(report().sidebarToggles).toBe(0);

      // ⌘\ in the page toggles it, and the report carries the change to the app.
      fireEvent.keyDown(window, { code: "Backslash", key: "\\", ctrlKey: true, metaKey: true });
      expect(sidebar()).toBeVisible();
      await waitFor(() => expect(report().sidebarCollapsed).toBe(false));
      expect(report().sidebarToggles).toBe(1);
      // The app owns the collapsed state; the page keeps only the width.
      expect(JSON.parse(localStorage.getItem(SIDEBAR_KEY) ?? "{}")).toEqual({ width: 264 });

      // Another window of this repository resizes it; only the width follows.
      localStorage.setItem(SIDEBAR_KEY, JSON.stringify({ collapsed: true, width: 320 }));
      act(() => {
        window.dispatchEvent(new StorageEvent("storage", { key: SIDEBAR_KEY }));
      });
      expect(sidebar()?.style.getPropertyValue("--sidebar-width")).toBe("320px");
      expect(sidebar()).toBeVisible();

      // The View menu owns ⌃⌘S in Desktop.
      fireEvent.keyDown(window, {
        code: "KeyS",
        key: "s",
        ctrlKey: true,
        metaKey: true,
        altKey: true,
      });
      expect(sidebar()).toBeVisible();
    });

    it("takes the chrome theme the app sends", async () => {
      render(<AppShell />);

      await send({ command: "theme", theme: "spruce" });
      expect(document.documentElement.dataset.theme).toBe("spruce");
      // Remembered, so a reload paints it before the app sends it again.
      expect(JSON.parse(localStorage.getItem("rhizome:desktop-theme") ?? "{}")).toMatchObject({
        theme: "spruce",
      });
      await send({ command: "theme", theme: "teal" });
      expect(document.documentElement.dataset.theme).toBe("spruce");
      delete document.documentElement.dataset.theme;
      localStorage.removeItem("rhizome:desktop-theme");
    });

    it("switches sections, searches, and opens issues on app commands", async () => {
      render(<AppShell />);

      await send({ command: "section", section: "ontology" });
      expect(window.location.pathname).toBe("/ontology");
      await waitFor(() => expect(report()).toEqual({ v: 1, section: "ontology", search: "" }));

      // The report follows history the page moves through on its own.
      await act(async () => {
        window.history.back();
        await new Promise((resolve) =>
          window.addEventListener("popstate", resolve, { once: true }),
        );
      });
      await waitFor(() => expect(report().section).toBe("notes"));
      await send({ command: "section", section: "ontology" });

      await send({ command: "section", section: "settings" });
      expect(window.location.pathname).toBe("/ontology");

      await send({ command: "search", query: "  embedded node identifiers  " });
      expect(window.location.pathname).toBe("/notes");
      expect(new URLSearchParams(window.location.search).get("search")).toBe(
        "embedded node identifiers",
      );
      await waitFor(() => expect(report().search).toBe("embedded node identifiers"));

      await send({ command: "issues" });
      expect(window.location.pathname).toBe("/notes/issues");
    });

    it("leaves ⌘K to the app", () => {
      render(<AppShell />);
      const focused = document.activeElement;

      fireEvent.keyDown(window, { key: "k", metaKey: true });

      expect(document.activeElement).toBe(focused);
    });
  });
});
