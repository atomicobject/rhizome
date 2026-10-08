import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import {
  attach,
  pickPath,
  request,
  type Discovery,
  type Library,
  type MenuEntry,
  type Message,
  type OpenState,
  type Repository,
  type WindowSession,
  type Worktree,
} from "./api";
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  request: vi.fn(),
  attach: vi.fn(),
  pickPath: vi.fn(),
}));
const call = vi.mocked(request);
const connect = vi.mocked(attach);
const pick = vi.mocked(pickPath);

const main = "/work/project";
const feature = "/work/project-feature";
const added = "/work/project-new";
const repository: Repository = {
  id: "a".repeat(64),
  root: "/work/project/.git",
  name: "project",
  acknowledged: [main, feature],
};
const library: Library = { version: 2, repositories: [repository], revision: 1 };
const worktree = (path: string, branch: string): Worktree => ({
  path,
  branch,
  configured: true,
  hasDatabase: true,
  trusted: true,
  trustRequired: false,
});
const discovery: Discovery = {
  id: repository.id,
  root: repository.root,
  name: "project",
  git: true,
  defaultPrimary: main,
  primary: main,
  worktrees: [worktree(main, "main"), worktree(feature, "feature"), worktree(added, "spike")],
};

const other: Repository = {
  id: "b".repeat(64),
  root: "/work/other/.git",
  name: "other",
  acknowledged: ["/work/other"],
};
const otherDiscovery: Discovery = {
  ...discovery,
  id: other.id,
  root: other.root,
  name: "other",
  defaultPrimary: "/work/other",
  primary: "/work/other",
  worktrees: [worktree("/work/other", "main")],
};
const both: Library = { version: 2, repositories: [repository, other], revision: 2 };

type Result = Awaited<ReturnType<typeof request>>;
let emit: (message: Message) => void;
let generation = 0;
function start(
  session: WindowSession = { sidebarCollapsed: false, repository: repository.id, worktree: main },
  handle?: (operation: string, args: unknown) => Result | Promise<Result> | undefined,
) {
  generation = 0;
  connect.mockImplementation(async (onMessage) => {
    emit = onMessage;
    return session;
  });
  call.mockImplementation(async (operation, args) => {
    const result = handle?.(operation, args);
    if (result !== undefined) return result;
    if (operation === "list") return library;
    if (operation === "discover") return discovery;
    if (operation === "open") return { generation: ++generation, library };
    if (["session", "layout", "menu", "trust", "deselect"].includes(operation)) return null;
    throw new Error(`Unexpected operation: ${operation}`);
  });
  render(<App />);
}
function report(state: Partial<OpenState>) {
  act(() =>
    emit({
      type: "open",
      generation,
      repository: repository.id,
      worktree: main,
      step: "checking",
      // Past the quiet period, so progress shows at once.
      started: Date.now() - 1000,
      ...state,
    }),
  );
}
/** Open requests without the shell's selection numbers. */
function opened() {
  return call.mock.calls
    .filter(([operation]) => operation === "open")
    .map(([, args]) =>
      Object.fromEntries(Object.entries(args).filter(([key]) => key !== "selection")),
    );
}
beforeEach(() => vi.resetAllMocks());

describe("desktop shell", () => {
  it("restores the window's sidebar state and selected worktree", async () => {
    start({ sidebarCollapsed: true, repository: repository.id, worktree: feature });
    await waitFor(() =>
      expect(opened()).toEqual([{ id: repository.id, worktree: feature, skipSeed: false }]),
    );
    expect(screen.queryByRole("navigation", { name: "Repositories" })).toBeNull();
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("session", {
        repository: repository.id,
        worktree: feature,
        sidebarCollapsed: true,
      }),
    );
  });

  it("marks worktrees that have not been opened", async () => {
    start();
    const sidebar = screen.getByRole("navigation", { name: "Repositories" });
    expect(await within(sidebar).findByTitle("1 new worktree")).toHaveTextContent("1 new");
    fireEvent.click(await screen.findByRole("button", { name: "Worktree" }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const [, menu] = call.mock.calls.find(([operation]) => operation === "menu")!;
    const labels = (menu as { items: MenuEntry[] }).items.map((item) => item.label);
    expect(labels).toEqual([
      "main — project (primary)",
      "feature — project-feature",
      "● spike — project-new",
    ]);
  });

  it("shows each open step and ignores reports from a replaced open", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "seeding", primary: feature });
    expect(screen.getByRole("status")).toHaveTextContent("Copying the index from project-feature");
    fireEvent.click(screen.getByRole("button", { name: "Worktree" }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const [, menu] = call.mock.calls.find(([operation]) => operation === "menu")!;
    const item = (menu as { items: MenuEntry[] }).items[1];
    act(() => emit({ type: "menu", id: item.id! }));
    await waitFor(() => expect(opened()).toHaveLength(2));
    report({ generation: 1, step: "error", message: "stale failure" });
    expect(screen.queryByText("stale failure")).toBeNull();
    report({ generation: 2, worktree: feature, step: "starting" });
    expect(screen.getByRole("status")).toHaveTextContent("Starting Rhizome");
    report({ generation: 2, worktree: feature, step: "ready" });
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("asks for trust in the content area before opening", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "trust" });
    expect(screen.getByRole("heading", { name: "Trust this worktree?" })).toBeVisible();
    expect(call).not.toHaveBeenCalledWith("trust", expect.anything());
    fireEvent.click(screen.getByRole("button", { name: "Trust and open" }));
    await waitFor(() => expect(opened()).toHaveLength(2));
    expect(call).toHaveBeenCalledWith("trust", { id: repository.id, worktree: main });
  });

  it("shows the setup sheet for a worktree that needs setup", async () => {
    start(undefined, (operation) => {
      if (operation === "setup-report") throw { code: "setup_unsupported", message: "Too old" };
      return undefined;
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "setup" });
    expect(
      await screen.findByRole("heading", {
        name: "Rhizome needs an update to set up from the app",
      }),
    ).toBeVisible();
    expect(call).toHaveBeenCalledWith("setup-report", { id: repository.id, worktree: main });
  });

  it("opens What Gets Indexed from the repository menu over the workspace", async () => {
    start(undefined, (operation) =>
      operation === "scope"
        ? { builtInApplies: false, rules: [], keepIndexed: [], root: main }
        : undefined,
    );
    await waitFor(() => expect(opened()).toHaveLength(1));
    act(() =>
      emit({
        type: "presence",
        repositories: { [repository.id]: { info: discovery } },
        runtimes: {},
      }),
    );
    fireEvent.contextMenu(screen.getByRole("tab", { name: /project/ }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const [, menu] = call.mock.calls.find(([operation]) => operation === "menu")!;
    const item = (menu as { items: MenuEntry[] }).items.find(
      (entry) => entry.label === "What Gets Indexed…",
    )!;
    expect(item.disabled).toBe(false);
    act(() => emit({ type: "menu", id: item.id! }));
    expect(
      await screen.findByRole("heading", { name: "What gets indexed in project" }),
    ).toBeVisible();
    expect(call).toHaveBeenCalledWith("scope", { id: repository.id, worktree: main });
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("layout", expect.objectContaining({ covered: true })),
    );
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("heading", { name: "What gets indexed in project" })).toBeNull();
  });

  it("offers retry and starting without copying after a seeding failure", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "seed-error", message: "database is locked" });
    expect(screen.getByRole("alert")).toHaveTextContent("database is locked");
    fireEvent.click(screen.getByRole("button", { name: "Start without copying" }));
    await waitFor(() =>
      expect(opened().at(-1)).toEqual({ id: repository.id, worktree: main, skipSeed: true }),
    );
  });

  it("adds a repository from the empty state without running it", async () => {
    start({ sidebarCollapsed: false }, (operation) => {
      if (operation === "list") return { version: 2, repositories: [], revision: 0 };
      if (operation === "add") return { library, id: repository.id };
    });
    pick.mockResolvedValue(feature);
    fireEvent.click(await screen.findByRole("button", { name: "Add Repository…" }));
    await waitFor(() => expect(opened()).toHaveLength(1));
    expect(call).toHaveBeenCalledWith("add", { path: feature });
    expect(call.mock.calls.map(([operation]) => operation)).not.toContain("initialize");
    expect(screen.getByRole("tab", { name: /project/ })).toHaveAttribute("aria-selected", "true");
  });

  it("preserves a broken library and offers retry", async () => {
    start(undefined, (operation) => {
      if (operation === "list")
        return Promise.reject({
          code: "desktop_error",
          message: "Saved repositories are invalid. Your file has been preserved.",
        });
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("Your file has been preserved.");
    expect(screen.getByRole("button", { name: "Try again" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Add repository" })).toBeDisabled();
  });

  it("shows which worktrees run Rhizome alongside the new marker", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    expect(screen.queryByLabelText("Rhizome running")).toBeNull();
    act(() =>
      emit({
        type: "presence",
        repositories: { [repository.id]: { info: discovery } },
        runtimes: {
          [main]: { state: "running", mode: "headless" },
          [feature]: { state: "stopped", mode: null },
          [added]: { state: "starting", mode: null },
        },
      }),
    );
    expect(screen.getByLabelText("Rhizome running")).toBeVisible();
    const sidebar = screen.getByRole("navigation", { name: "Repositories" });
    expect(within(sidebar).getByTitle("1 new worktree")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Worktree" }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const [, menu] = call.mock.calls.find(([operation]) => operation === "menu")!;
    expect((menu as { items: MenuEntry[] }).items.map((item) => item.label)).toEqual([
      "main — project (primary) · Running",
      "feature — project-feature",
      "● spike — project-new · Starting…",
    ]);
    act(() =>
      emit({
        type: "presence",
        repositories: {},
        runtimes: { [added]: { state: "starting", mode: null } },
      }),
    );
    expect(screen.getByLabelText("Rhizome starting")).toBeVisible();
  });

  it("restarts a running worktree's Rhizome from the sidebar context menu", async () => {
    start(undefined, (operation) => (operation === "restart" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    act(() =>
      emit({
        type: "presence",
        repositories: { [repository.id]: { info: discovery } },
        runtimes: { [main]: { state: "running", mode: "headless" } },
      }),
    );
    fireEvent.contextMenu(screen.getByRole("tab", { name: /project/ }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const [, menu] = call.mock.calls.find(([operation]) => operation === "menu")!;
    const restart = (menu as { items: MenuEntry[] }).items.find((item) =>
      item.label?.startsWith("Restart"),
    )!;
    expect(restart).toMatchObject({ label: "Restart Rhizome for main", disabled: false });
    act(() => emit({ type: "menu", id: restart.id! }));
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("restart", { id: repository.id, worktree: main }),
    );
  });

  it("shows nothing for progress that finishes within the quiet period", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "loading", started: Date.now() });
    expect(screen.queryByRole("status")).toBeNull();
    expect(await screen.findByRole("status", {}, { timeout: 2000 })).toHaveTextContent(
      "Loading the workspace",
    );
  });

  it("stops Rhizome from the context menu and starts it again from the sleeping view", async () => {
    start(undefined, (operation) => (operation === "stop" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    act(() =>
      emit({
        type: "presence",
        repositories: { [repository.id]: { info: discovery } },
        runtimes: { [main]: { state: "running", mode: "headless" } },
      }),
    );
    fireEvent.contextMenu(screen.getByRole("tab", { name: /project/ }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const [, menu] = call.mock.calls.find(([operation]) => operation === "menu")!;
    const items = (menu as { items: MenuEntry[] }).items;
    expect(items.slice(0, 2).map((item) => [item.label, item.disabled])).toEqual([
      ["Open in Browser", false],
      ["Reveal in Finder", false],
    ]);
    const stop = items.find((item) => item.label === "Stop Rhizome for main")!;
    act(() => emit({ type: "menu", id: stop.id! }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("stop", { worktree: main }));
    report({ generation: 2, step: "sleeping" });
    act(() =>
      emit({
        type: "presence",
        repositories: { [repository.id]: { info: discovery } },
        runtimes: { [main]: { state: "stopped", mode: null } },
      }),
    );
    expect(screen.getByLabelText("Rhizome sleeping")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Start Rhizome" }));
    await waitFor(() => expect(opened()).toHaveLength(2));
  });

  it("shows no progress on the first frame of a selection after an earlier slow one", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "loading" });
    expect(screen.getByRole("status")).toHaveTextContent("Loading the workspace");
    report({ generation: 2, step: "checking", started: Date.now() });
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("keeps a selection still discovering from reopening a runtime the user stopped", async () => {
    let finishDiscovery: (found: Discovery) => void = () => {};
    let discoveries = 0;
    start(undefined, (operation) => {
      if (operation === "stop") return null;
      if (operation === "discover" && ++discoveries === 2)
        return new Promise<Discovery>((resolve) => (finishDiscovery = resolve));
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    act(() =>
      emit({
        type: "presence",
        repositories: { [repository.id]: { info: discovery } },
        runtimes: { [main]: { state: "running", mode: "headless" } },
      }),
    );
    fireEvent.click(screen.getByRole("tab", { name: /project/ }));
    await waitFor(() => expect(discoveries).toBe(2));
    fireEvent.contextMenu(screen.getByRole("tab", { name: /project/ }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const [, menu] = call.mock.calls.filter(([operation]) => operation === "menu").at(-1)!;
    const stop = (menu as { items: MenuEntry[] }).items.find((item) =>
      item.label?.startsWith("Stop Rhizome"),
    )!;
    act(() => emit({ type: "menu", id: stop.id! }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("stop", { worktree: main }));
    await act(async () => finishDiscovery(discovery));
    expect(opened()).toHaveLength(1);
  });

  it("confirms a copied page URL briefly in the toolbar", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    act(() => emit({ type: "notice", message: "Copied the page URL" }));
    expect(screen.getByText("Copied the page URL")).toBeVisible();
  });

  it("reorders repositories from the keyboard", async () => {
    start(undefined, (operation, args) => {
      if (operation === "list") return both;
      if (operation === "discover")
        return (args as { id: string }).id === other.id ? otherDiscovery : discovery;
      if (operation === "reorder")
        return { ...both, repositories: [other, repository], revision: 3 };
    });
    const tab = await screen.findByRole("tab", { name: /project/ });
    fireEvent.keyDown(tab, { key: "ArrowDown", altKey: true });
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("reorder", { ids: [other.id, repository.id] }),
    );
    await waitFor(() =>
      expect(screen.getAllByRole("tab").map((t) => t.querySelector(".name")?.textContent)).toEqual([
        "other",
        "project",
      ]),
    );
  });

  it("shows reconnecting while the app restarts a stopped runtime, then offers retry", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "ready" });
    report({ generation: 2, step: "starting", reconnecting: "stopped" });
    expect(screen.getByRole("status")).toHaveTextContent("Rhizome stopped — reconnecting");
    expect(screen.getByRole("status")).toHaveTextContent("Starting Rhizome");
    report({ generation: 2, step: "error", reconnecting: "stopped", message: "port in use" });
    expect(screen.getByRole("status")).toHaveTextContent("The last attempt failed: port in use");
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    report({
      generation: 2,
      step: "error",
      code: "runtime_stopped",
      message: "did not stay running",
    });
    expect(screen.getByRole("alert")).toHaveTextContent("did not stay running");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(opened()).toHaveLength(2));
  });

  it("never lets a slower repository selection replace a newer one", async () => {
    let release = () => {};
    let slow = false;
    start(undefined, (operation, args) => {
      if (operation === "list") return both;
      if (operation === "open") return { generation: ++generation, library: both };
      const id = (args as { id?: string }).id;
      if (operation === "discover" && id === other.id) return otherDiscovery;
      if (operation === "discover" && slow)
        return new Promise<Result>((resolve) => {
          release = () => resolve(discovery);
        });
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    slow = true;
    fireEvent.click(screen.getByRole("tab", { name: /project/ }));
    fireEvent.click(screen.getByRole("tab", { name: /other/ }));
    await waitFor(() => expect(opened()).toHaveLength(2));
    await act(async () => release());
    expect(opened()).toHaveLength(2);
    expect(opened()[1]).toEqual({ id: other.id, worktree: "/work/other", skipSeed: false });
    const [first, second] = call.mock.calls
      .filter(([operation]) => operation === "open")
      .map(([, args]) => (args as { selection: number }).selection);
    expect(second).toBeGreaterThan(first);
    expect(screen.getByRole("tab", { name: /other/ })).toHaveAttribute("aria-selected", "true");
  });

  it("lets a terminal selection outrank a pending earlier one", async () => {
    let release = () => {};
    let slow = false;
    start(undefined, (operation, args) => {
      if (operation === "list") return library;
      if (operation === "open") return { generation: ++generation, library: both };
      const id = (args as { id?: string }).id;
      if (operation === "discover" && id === other.id) return otherDiscovery;
      if (operation === "discover" && slow)
        return new Promise<Result>((resolve) => {
          release = () => resolve(discovery);
        });
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    slow = true;
    fireEvent.click(screen.getByRole("tab", { name: /project/ }));
    act(() =>
      emit({ type: "select", library: both, repository: other.id, worktree: "/work/other" }),
    );
    await waitFor(() => expect(opened()).toHaveLength(2));
    await act(async () => release());
    expect(opened()).toHaveLength(2);
    expect(opened()[1]).toEqual({ id: other.id, worktree: "/work/other", skipSeed: false });
    expect(screen.getByRole("tab", { name: /other/ })).toHaveAttribute("aria-selected", "true");
  });

  it("opens a terminal selection instead of restoring the session at launch", async () => {
    let list = (_: Library) => {};
    start(undefined, (operation, args) => {
      if (operation === "list") return new Promise<Result>((resolve) => (list = resolve));
      if (operation === "open") return { generation: ++generation, library };
      if (operation === "discover" && (args as { id: string }).id === repository.id)
        return discovery;
    });
    await waitFor(() => expect(call).toHaveBeenCalledWith("list", {}));
    act(() => emit({ type: "select", library, repository: repository.id, worktree: feature }));
    await act(async () => list(library));
    await waitFor(() => expect(opened()).toHaveLength(1));
    expect(opened()[0]).toEqual({ id: repository.id, worktree: feature, skipSeed: false });
  });

  it("reports a terminal path it cannot open without changing the selection", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    act(() =>
      emit({ type: "alert", code: "folder_missing", message: "Cannot open /gone: missing" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Cannot open /gone: missing");
    expect(opened()).toHaveLength(1);
    expect(screen.getByRole("tab", { name: /project/ })).toHaveAttribute("aria-selected", "true");
  });

  it("stops showing the previous worktree when the selected repository is unavailable", async () => {
    start(undefined, (operation, args) => {
      if (operation === "list") return both;
      if (operation === "open") return { generation: ++generation, library: both };
      if (operation === "discover" && (args as { id: string }).id === other.id)
        return Promise.reject({
          code: "folder_missing",
          message: "This repository is unavailable.",
        });
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    fireEvent.click(screen.getByRole("tab", { name: /other/ }));
    expect(await screen.findByText("other is unavailable")).toBeVisible();
    expect(call).toHaveBeenCalledWith("deselect", { selection: expect.any(Number) });
  });

  it("applies library changes made in another window", async () => {
    let saved = library;
    start(undefined, (operation, args) => {
      if (operation === "open") return { generation: ++generation, library: saved };
      if (operation === "discover" && (args as { id: string }).id === other.id)
        return otherDiscovery;
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    saved = both;
    act(() => emit({ type: "library", library: saved }));
    expect(screen.getByRole("tab", { name: /other/ })).toBeVisible();
    saved = { version: 2, repositories: [other], revision: 3 };
    act(() => emit({ type: "library", library: saved }));
    await waitFor(() =>
      expect(opened().at(-1)).toEqual({ id: other.id, worktree: "/work/other", skipSeed: false }),
    );
    expect(screen.queryByRole("tab", { name: /project/ })).toBeNull();
  });

  it("ignores a library snapshot older than one it already applied", async () => {
    let release = () => {};
    start(undefined, (operation) => {
      if (operation === "list") return both;
      if (operation === "open")
        return new Promise<Result>((resolve) => {
          release = () => resolve({ generation: ++generation, library: both });
        });
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    expect(screen.getByRole("tab", { name: /other/ })).toBeVisible();
    act(() => emit({ type: "library", library: { ...library, revision: 3 } }));
    expect(screen.queryByRole("tab", { name: /other/ })).toBeNull();
    await act(async () => release());
    expect(screen.queryByRole("tab", { name: /other/ })).toBeNull();
    expect(screen.getByRole("tab", { name: /project/ })).toHaveAttribute("aria-selected", "true");
  });
});
