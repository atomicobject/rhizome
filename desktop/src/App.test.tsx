import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, onTestFinished, vi } from "vitest";
import { App } from "./App";
import {
  attach,
  pickPath,
  request,
  type Discovery,
  type FinderChoice,
  type FinderModel,
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
const library: Library = { version: 3, repositories: [repository], revision: 1 };
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
const both: Library = { version: 3, repositories: [repository, other], revision: 2 };

type Result = Awaited<ReturnType<typeof request>>;
let emit: (message: Message) => void;
let generation = 0;
function start(
  session: WindowSession = { repository: repository.id, worktree: main },
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
    if (operation === "update-status") return { state: "idle" };
    if (operation === "cli-link-status")
      return { linkPath: "/usr/local/bin/rzm", state: "absent", shell: { isApp: false } };
    if (operation === "settings") return { theme: "graphite", appearance: "auto" };
    if (["session", "layout", "menu", "trust", "deselect", "finder"].includes(operation))
      return null;
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
/** The model the shell last showed the finder, or null once it closed it. */
function finderModel(): FinderModel | null {
  const calls = call.mock.calls.filter(([operation]) => operation === "finder");
  return (calls.at(-1)?.[1] as { model: FinderModel | null } | undefined)?.model ?? null;
}
async function showFinder(name: string | RegExp) {
  const button = await screen.findByRole("button", { name });
  const before = call.mock.calls.filter(([operation]) => operation === "finder").length;
  fireEvent.click(button);
  await waitFor(() => {
    expect(call.mock.calls.filter(([operation]) => operation === "finder").length).toBeGreaterThan(
      before,
    );
    expect(finderModel()).not.toBeNull();
  });
  return finderModel()!;
}
function choose(choice: FinderChoice) {
  act(() => emit({ type: "finder", ...choice }));
}
async function openRepository(id: string) {
  if (finderModel()?.mode !== "repositories") await showFinder(/^Repository /);
  choose({ choice: "open", repository: id, worktree: null, newWindow: false });
}
/** The popup menu the shell last asked for. */
function lastMenu() {
  const [, menu] = call.mock.calls.filter(([operation]) => operation === "menu").at(-1)!;
  return (menu as { items: MenuEntry[] }).items;
}
/** The current repository's actions, from its name in the toolbar. */
async function repositoryMenu() {
  const before = call.mock.calls.filter(([operation]) => operation === "menu").length;
  fireEvent.contextMenu(await screen.findByRole("button", { name: /^Repository / }));
  await waitFor(() =>
    expect(call.mock.calls.filter(([operation]) => operation === "menu").length).toBe(before + 1),
  );
  return lastMenu();
}
function present(
  runtimes: Record<string, { state: "running" | "starting" | "stopped" }> = {},
  info = discovery,
) {
  act(() =>
    emit({
      type: "presence",
      repositories: { [repository.id]: { info } },
      runtimes: Object.fromEntries(
        Object.entries(runtimes).map(([path, { state }]) => [path, { state, mode: null }]),
      ),
    }),
  );
}
beforeEach(() => vi.resetAllMocks());

describe("desktop shell", () => {
  it.each([
    {
      macOS: true,
      repositoryTitle: "/work/project/.git\nSwitch repository (⌘O)",
      worktreeTitle: "/work/project\nSwitch worktree (⇧⌘O)",
      repositoryShortcut: "Meta+O",
      worktreeShortcut: "Meta+Shift+O",
      searchShortcut: "Meta+K",
      searchHint: "⌘K",
      reloadTitle: "Reload (⌘R)",
      sidebarTitle: "Hide sidebar (⌃⌘S or ⌘\\)",
    },
    {
      macOS: false,
      repositoryTitle: "/work/project/.git\nSwitch repository (Ctrl+O)",
      worktreeTitle: "/work/project\nSwitch worktree (Ctrl+Shift+O)",
      repositoryShortcut: "Control+O",
      worktreeShortcut: "Control+Shift+O",
      searchShortcut: "Control+K",
      searchHint: "Ctrl+K",
      reloadTitle: "Reload (Ctrl+R)",
      sidebarTitle: "Hide sidebar (Ctrl+Alt+S or Ctrl+\\)",
    },
  ])("shows native shortcut hints when macOS is $macOS", async (expected) => {
    window.__RHIZOME_MACOS__ = expected.macOS;
    onTestFinished(() => {
      delete window.__RHIZOME_MACOS__;
    });
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "ready" });
    act(() =>
      emit({ type: "page", state: { section: "notes", search: "", sidebarCollapsed: false } }),
    );
    const repositoryButton = screen.getByRole("button", { name: "Repository project" });
    expect(repositoryButton).toHaveAttribute("title", expected.repositoryTitle);
    expect(repositoryButton).toHaveAttribute("aria-keyshortcuts", expected.repositoryShortcut);
    const worktreeButton = screen.getByRole("button", { name: "Worktree main" });
    expect(worktreeButton).toHaveAttribute("title", expected.worktreeTitle);
    expect(worktreeButton).toHaveAttribute("aria-keyshortcuts", expected.worktreeShortcut);
    expect(screen.getByRole("searchbox", { name: "Search this project" })).toHaveAttribute(
      "aria-keyshortcuts",
      expected.searchShortcut,
    );
    expect(screen.getByText(expected.searchHint)).toBeVisible();
    expect(screen.getByRole("button", { name: "Reload" })).toHaveAttribute(
      "title",
      expected.reloadTitle,
    );
    expect(screen.getByRole("button", { name: "Hide sidebar" })).toHaveAttribute(
      "title",
      expected.sidebarTitle,
    );
  });

  it("restores the window's selected worktree", async () => {
    start({ repository: repository.id, worktree: feature });
    await waitFor(() =>
      expect(opened()).toEqual([{ id: repository.id, worktree: feature, skipSeed: false }]),
    );
    expect(screen.queryByRole("navigation", { name: "Repositories" })).toBeNull();
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("session", {
        repository: repository.id,
        worktree: feature,
      }),
    );
  });

  it("lists the worktree finder's rows with the primary first and new ones marked", async () => {
    start({ repository: repository.id, worktree: feature });
    expect(await screen.findByTitle("New worktrees")).toBeVisible();
    const model = await showFinder(/^Worktree /);
    expect(model.mode).toBe("worktrees");
    expect(model.rows.map((r) => [r.title, r.primary, r.fresh, r.current])).toEqual([
      ["main", true, false, false],
      ["feature", false, false, true],
      ["spike", false, true, false],
    ]);
  });
  it("shows each open step and ignores reports from a replaced open", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "seeding", primary: feature });
    expect(screen.getByRole("status")).toHaveTextContent("Copying the index from project-feature");
    choose({ choice: "open", repository: repository.id, worktree: feature, newWindow: false });
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

  it("opens Repository Settings from the repository menu on the repository tab", async () => {
    start(undefined, (operation) => {
      if (operation === "scope")
        return { builtInApplies: false, rules: [], keepIndexed: [], root: main };
      if (operation === "settings-report") throw { code: "setup_unsupported", message: "Too old." };
      if (operation === "global-status") return null;
      return undefined;
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    present();
    const item = (await repositoryMenu()).find((entry) => entry.label === "Repository Settings…")!;
    expect(item.disabled).toBe(false);
    act(() => emit({ type: "menu", id: item.id! }));
    expect(await screen.findByRole("heading", { name: "Settings" })).toBeVisible();
    expect(screen.getByRole("tab", { name: /project/ })).toHaveAttribute("aria-selected", "true");
    expect(
      await screen.findByRole("heading", {
        name: "This repository's Rhizome is too old to change settings here",
      }),
    ).toBeVisible();
    expect(call).toHaveBeenCalledWith("settings-report", { id: repository.id, worktree: main });
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("layout", expect.objectContaining({ covered: true })),
    );

    fireEvent.click(screen.getByRole("tab", { name: "Rhizome on this Mac" }));
    expect(screen.getByRole("tab", { name: "Rhizome on this Mac" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("heading", { name: "Settings" })).toBeNull();
    // The gear opens Settings on the tab last chosen.
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByRole("tab", { name: "Rhizome on this Mac" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("returns from the rules page to the repository tab that opened it", async () => {
    const plan = {
      root: main,
      name: "project",
      configured: true,
      findings: { docs: "all Markdown", code: "Go" },
      workflow: "none",
      workflows: [
        { id: "none", label: "Search and agent guidance only", description: "", recommended: true },
      ],
      addons: [],
      agents: [{ id: "claude", label: "Claude Code", enabled: false }],
      search: {
        provider: "off",
        ready: false,
        providers: [{ id: "off", label: "Off", ready: true }],
      },
      ignoredRepositories: [],
      pin: "",
      writes: { summary: [], files: [] },
    };
    start(undefined, (operation) => {
      if (operation === "scope")
        return { builtInApplies: false, rules: [], keepIndexed: [], root: main };
      if (operation === "settings-report") return plan;
      if (operation === "global-status") return null;
      return undefined;
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    present();
    const item = (await repositoryMenu()).find((entry) => entry.label === "Repository Settings…")!;
    act(() => emit({ type: "menu", id: item.id! }));
    fireEvent.click(await screen.findByRole("checkbox", { name: /Claude Code/ }));
    fireEvent.click(screen.getByRole("button", { name: "Edit rules…" }));
    expect(
      await screen.findByRole("heading", { name: "What gets indexed in project" }),
    ).toBeVisible();
    expect(call).toHaveBeenCalledWith("scope", { id: repository.id, worktree: main });
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.getByRole("tab", { name: /project/ })).toHaveAttribute("aria-selected", "true");
    // The staged change survived the rules page.
    expect(screen.getByRole("checkbox", { name: /Claude Code/ })).toBeChecked();
    expect(screen.getByText("1 change not applied")).toBeVisible();
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
    start({}, (operation) => {
      if (operation === "list") return { version: 3, repositories: [], revision: 0 };
      if (operation === "add") return { library, id: repository.id };
    });
    pick.mockResolvedValue(feature);
    fireEvent.click(await screen.findByRole("button", { name: "Add Repository…" }));
    await waitFor(() => expect(opened()).toHaveLength(1));
    expect(call).toHaveBeenCalledWith("add", { path: feature });
    expect(call.mock.calls.map(([operation]) => operation)).not.toContain("initialize");
    expect(screen.getByRole("button", { name: "Repository project" })).toBeVisible();
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
    expect(screen.queryByRole("button", { name: /^Repository / })).toBeNull();
  });

  it("shows which worktrees and repositories run Rhizome in the finders", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    present({
      [main]: { state: "running" },
      [feature]: { state: "stopped" },
      [added]: { state: "starting" },
    });
    const worktrees = await showFinder(/^Worktree /);
    expect(worktrees.rows.map((r) => [r.title, r.runtime])).toEqual([
      ["main", "running"],
      ["feature", "stopped"],
      ["spike", "starting"],
    ]);
    // ⌘O while the worktree finder is open switches to the repository finder.
    act(() => emit({ type: "command", command: "repositories" }));
    await waitFor(() => expect(finderModel()?.mode).toBe("repositories"));
    expect(finderModel()!.rows).toEqual([
      expect.objectContaining({ title: "project", runtime: "running", current: true, fresh: true }),
    ]);
    present({ [added]: { state: "starting" } });
    await waitFor(() => expect(finderModel()!.rows[0].runtime).toBe("starting"));
    // Each live update is numbered, so the app drops one that arrives late.
    const revisions = call.mock.calls
      .filter(([operation]) => operation === "finder")
      .map(([, args]) => args as { model: FinderModel | null; revision?: number })
      .filter((args) => args.model)
      .map((args) => args.revision);
    expect(revisions).toEqual(revisions.map((_, at) => at + 1));
  });
  it("restarts a running worktree's Rhizome from the repository name's menu", async () => {
    start(undefined, (operation) => (operation === "restart" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    present({ [main]: { state: "running" } });
    const restart = (await repositoryMenu()).find((item) => item.label?.startsWith("Restart"))!;
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
    present({ [main]: { state: "running" } });
    const items = await repositoryMenu();
    expect(items.slice(0, 2).map((item) => [item.label, item.disabled])).toEqual([
      ["Open in Browser", false],
      ["Reveal in Finder", false],
    ]);
    const stop = items.find((item) => item.label === "Stop Rhizome for main")!;
    act(() => emit({ type: "menu", id: stop.id! }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("stop", { worktree: main }));
    report({ generation: 2, step: "sleeping" });
    present({ [main]: { state: "stopped" } });
    expect((await showFinder(/^Worktree /)).rows[0].runtime).toBe("sleeping");
    choose({ choice: "close" });
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
    present({ [main]: { state: "running" } });
    act(() => emit({ type: "select", library, repository: repository.id, worktree: main }));
    await waitFor(() => expect(discoveries).toBe(2));
    const stop = (await repositoryMenu()).find((item) => item.label?.startsWith("Stop Rhizome"))!;
    act(() => emit({ type: "menu", id: stop.id! }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("stop", { worktree: main }));
    await act(async () => finishDiscovery(discovery));
    expect(opened()).toHaveLength(1);
  });

  it("draws the workspace page's controls in the toolbar once the page reports", async () => {
    start(undefined, (operation) => (operation === "page" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "ready" });
    expect(screen.queryByRole("navigation", { name: "Sections" })).toBeNull();

    act(() =>
      emit({
        type: "page",
        state: { section: "notes", search: "meetings", issues: 37, health: "current_issues" },
      }),
    );
    expect(screen.getByRole("button", { name: "Notes" })).toHaveAttribute("aria-current", "page");
    const search = screen.getByRole<HTMLInputElement>("searchbox", { name: "Search this project" });
    expect(search.value).toBe("meetings");

    fireEvent.click(screen.getByRole("button", { name: "Ontology" }));
    expect(call).toHaveBeenCalledWith("page", { command: "section", section: "ontology" });
    fireEvent.click(screen.getByRole("button", { name: "Open 37 validation issues" }));
    expect(call).toHaveBeenCalledWith("page", { command: "issues" });
    fireEvent.click(screen.getByRole("button", { name: "Keyboard shortcuts" }));
    expect(call).toHaveBeenCalledWith("page", { command: "shortcuts" });
    fireEvent.change(search, { target: { value: "decisions" } });
    fireEvent.submit(search.closest("form")!);
    expect(call).toHaveBeenCalledWith("page", { command: "search", query: "decisions" });

    act(() => emit({ type: "command", command: "focus-search" }));
    expect(search).toHaveFocus();

    // The active tab follows the page, and the badge belongs to Notes.
    act(() => emit({ type: "page", state: { section: "ontology", search: "" } }));
    expect(screen.getByRole("button", { name: "Ontology" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.queryByRole("button", { name: /validation/ })).toBeNull();
    expect(search.value).toBe("");

    // The Tasks tab counts the items that need the person, from any section.
    expect(screen.getByRole("button", { name: "Tasks" })).toHaveTextContent(/^Tasks$/);
    act(() =>
      emit({ type: "page", state: { section: "ontology", search: "", tasks: { needs: 3 } } }),
    );
    expect(screen.getByRole("button", { name: "Tasks, 3 need you" })).toHaveTextContent("Tasks3");
    fireEvent.click(screen.getByRole("button", { name: "Tasks, 3 need you" }));
    expect(call).toHaveBeenCalledWith("page", { command: "section", section: "tasks" });
    act(() =>
      emit({ type: "page", state: { section: "ontology", search: "", tasks: { needs: 0 } } }),
    );
    expect(screen.getByRole("button", { name: "Tasks" })).toHaveTextContent(/^Tasks$/);

    act(() => emit({ type: "page", state: null }));
    expect(screen.queryByRole("navigation", { name: "Sections" })).toBeNull();
  });

  it("toggles the sidebar from the toolbar while the page has one", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    const toggle = () => screen.getByRole("button", { name: /sidebar$/ });
    expect(toggle()).toBeDisabled();
    report({ step: "ready" });
    // A page that has not taken the app's sidebar state cannot toggle it.
    act(() => emit({ type: "page", state: { section: "notes", search: "" } }));
    expect(toggle()).toBeDisabled();

    act(() =>
      emit({ type: "page", state: { section: "notes", search: "", sidebarCollapsed: false } }),
    );
    expect(toggle()).toBeEnabled();
    expect(toggle()).toHaveAccessibleName("Hide sidebar");
    expect(toggle()).toHaveAttribute("aria-pressed", "true");
    expect(toggle()).toHaveAttribute("title", "Hide sidebar (⌃⌘S or ⌘\\)");
    fireEvent.click(toggle());
    expect(call).toHaveBeenCalledWith("page", { command: "sidebar", collapsed: true });

    act(() =>
      emit({ type: "page", state: { section: "agent", search: "", sidebarCollapsed: true } }),
    );
    expect(toggle()).toHaveAccessibleName("Show sidebar");
    expect(toggle()).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(toggle());
    expect(call).toHaveBeenCalledWith("page", { command: "sidebar", collapsed: false });

    act(() =>
      emit({ type: "page", state: { section: "graphql", search: "", sidebarCollapsed: true } }),
    );
    expect(toggle()).toBeDisabled();
    act(() => emit({ type: "page", state: null }));
    expect(toggle()).toBeDisabled();
  });

  it("drops a search draft when the page moves to another section", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "ready" });
    act(() => emit({ type: "page", state: { section: "notes", search: "" } }));
    const search = screen.getByRole<HTMLInputElement>("searchbox", { name: "Search this project" });
    fireEvent.change(search, { target: { value: "half typed" } });

    act(() => emit({ type: "page", state: { section: "ontology", search: "" } }));
    expect(search.value).toBe("");
  });

  it("hides the page controls while the shell covers the page", async () => {
    start(undefined, (operation) => (operation === "global-status" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "ready" });
    act(() => emit({ type: "page", state: { section: "agent", search: "" } }));
    expect(screen.getByRole("navigation", { name: "Sections" })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.queryByRole("navigation", { name: "Sections" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByRole("navigation", { name: "Sections" })).toBeVisible();

    report({ step: "starting" });
    expect(screen.queryByRole("navigation", { name: "Sections" })).toBeNull();
  });

  it("offers to repair a command-line link left by a moved app", async () => {
    start(undefined, (operation) =>
      operation === "cli-link-status"
        ? {
            linkPath: "/usr/local/bin/rzm",
            state: "dangling",
            target: "/Applications/Old/Rhizome.app/Contents/MacOS/rzm",
            shell: { isApp: false },
          }
        : operation === "global-status"
          ? {
              path: "/Applications/Rhizome.app/Contents/MacOS/rzm",
              version: "0.51.0",
              bundled: true,
            }
          : undefined,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Repair rzm Command" }));
    expect(await screen.findByRole("button", { name: "Repair Command-Line Tool" })).toBeVisible();
  });

  it("keeps a ready update when an older status response arrives later", async () => {
    let statusCalls = 0;
    let answer: (status: Result) => void = () => {};
    start(undefined, (operation) => {
      if (operation === "update-status") {
        statusCalls += 1;
        if (statusCalls === 1) return { state: "idle" } as Result;
        return new Promise<Result>((resolve) => (answer = resolve));
      }
      if (operation === "global-status")
        return {
          path: "/Applications/Rhizome.app/Contents/MacOS/rzm",
          version: "0.51.0",
          bundled: true,
        } as Result;
      return undefined;
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    await waitFor(() => expect(statusCalls).toBe(2));
    act(() => emit({ type: "update", status: { state: "ready", version: "0.52.0" } }));
    await act(async () => answer({ state: "checking" } as Result));
    expect(screen.getAllByRole("button", { name: "Restart to Update" }).length).toBeGreaterThan(0);
  });

  it("offers a ready update in the toolbar and installs it on request", async () => {
    start(undefined, (operation) => (operation === "install-update" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    expect(screen.queryByRole("button", { name: "Restart to Update" })).toBeNull();
    act(() => emit({ type: "update", status: { state: "ready", version: "0.51.0" } }));
    fireEvent.click(screen.getByRole("button", { name: "Restart to Update" }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("install-update", {}));
  });

  const offered: Discovery = {
    ...discovery,
    worktrees: discovery.worktrees.map((w) =>
      w.path === main
        ? { ...w, pinOffer: { state: "advance", pinned: "v0.50.5", available: "v0.51.0" } }
        : w,
    ),
  };
  async function pinMenu() {
    fireEvent.click(await screen.findByRole("button", { name: "Update to v0.51.0" }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const [, menu] = call.mock.calls.find(([operation]) => operation === "menu")!;
    return (menu as { items: MenuEntry[] }).items;
  }

  it("offers to advance an older pin, then reopens the worktree on it", async () => {
    start(undefined, (operation) =>
      operation === "discover" ? offered : operation === "pin-advance" ? null : undefined,
    );
    await waitFor(() => expect(opened()).toHaveLength(1));
    const items = await pinMenu();
    expect(items.map((item) => item.label)).toContain("Changes .rhizome/config.yml from v0.50.5");
    const advance = items.find((item) => item.label === "Update Pin to v0.51.0")!;
    act(() => emit({ type: "menu", id: advance.id! }));
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("pin-advance", { id: repository.id, worktree: main }),
    );
    await waitFor(() => expect(opened()).toHaveLength(2));
  });

  it("does not pull the user back after a pin advance finishes elsewhere", async () => {
    let finish: () => void = () => {};
    start(undefined, (operation) =>
      operation === "discover"
        ? offered
        : operation === "pin-advance"
          ? new Promise<Result>((resolve) => (finish = () => resolve(null as Result)))
          : undefined,
    );
    await waitFor(() => expect(opened()).toHaveLength(1));
    const advance = (await pinMenu()).find((item) => item.label === "Update Pin to v0.51.0")!;
    act(() => emit({ type: "menu", id: advance.id! }));
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("pin-advance", { id: repository.id, worktree: main }),
    );
    choose({ choice: "open", repository: repository.id, worktree: feature, newWindow: false });
    await waitFor(() => expect(opened().at(-1)).toMatchObject({ worktree: feature }));
    const count = opened().length;
    await act(async () => finish());
    await waitFor(() =>
      expect(call.mock.calls.filter(([op]) => op === "discover").length).toBeGreaterThan(1),
    );
    expect(opened()).toHaveLength(count);
  });

  it("leads with updating the pin when its version is no longer published", async () => {
    start(undefined, (operation) =>
      operation === "discover" ? offered : operation === "pin-advance" ? null : undefined,
    );
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({
      step: "error",
      code: "pinned_version_unavailable",
      message: "This worktree pins Rhizome v0.50.5, which is no longer published.",
    });
    const actions = within(screen.getByRole("alert")).getAllByRole("button");
    expect(actions.map((button) => button.textContent)).toEqual([
      "Update to Rhizome v0.51.0",
      "Try again",
    ]);
    expect(actions[0]).toHaveClass("primary");
    fireEvent.click(actions[0]);
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("pin-advance", { id: repository.id, worktree: main }),
    );
    await waitFor(() => expect(opened()).toHaveLength(2));
  });

  it("leads with updating the pin when its version is too old for the app", async () => {
    start(undefined, (operation) => (operation === "discover" ? offered : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({
      step: "error",
      code: "pinned_version_unsupported",
      message: "This worktree pins Rhizome v0.50.5, which is too old to run in the desktop app.",
    });
    const actions = within(screen.getByRole("alert")).getAllByRole("button");
    expect(actions.map((button) => button.textContent)).toEqual([
      "Update to Rhizome v0.51.0",
      "Try again",
    ]);
    expect(actions[0]).toHaveClass("primary");
  });

  it("offers only app updates when the app cannot move a pin that is too old", async () => {
    start(undefined, () => undefined);
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "error", code: "pinned_version_unsupported", message: "too old" });
    const actions = within(screen.getByRole("alert")).getAllByRole("button");
    expect(actions.map((button) => button.textContent)).toEqual([
      "Check for App Updates",
      "Try again",
    ]);
    expect(actions[0]).toHaveClass("primary");
  });

  it("offers app updates and a worktree executable when the app cannot move a missing pin", async () => {
    start(undefined, () => undefined);
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({
      step: "error",
      code: "pinned_version_unavailable",
      message: "This worktree pins Rhizome v0.52.0, which is no longer published.",
    });
    const actions = within(screen.getByRole("alert")).getAllByRole("button");
    expect(actions.map((button) => button.textContent)).toEqual([
      "Check for App Updates",
      "Try again",
      "Choose executable…",
    ]);
    expect(actions[0]).toHaveClass("primary");
    fireEvent.click(actions[0]);
    await waitFor(() => expect(call).toHaveBeenCalledWith("check-for-updates", {}));
  });

  it("shows what the app could not check beside the error page's pin update", async () => {
    const caution =
      "The app could not check that Rhizome v0.51.0 can read this worktree's index and sign in.";
    const cautioned: Discovery = {
      ...discovery,
      worktrees: discovery.worktrees.map((w) =>
        w.path === main
          ? {
              ...w,
              pinOffer: { state: "advance", pinned: "v0.50.5", available: "v0.51.0", caution },
            }
          : w,
      ),
    };
    start(undefined, (operation) => (operation === "discover" ? cautioned : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "error", code: "pinned_version_unavailable", message: "gone" });
    await waitFor(() => expect(within(screen.getByRole("alert")).getByText(caution)).toBeVisible());
  });

  it("keeps retry first when a pinned install fails for another reason", async () => {
    start(undefined, (operation) => (operation === "discover" ? offered : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "error", code: "install_error", message: "connection refused" });
    const actions = within(screen.getByRole("alert")).getAllByRole("button");
    expect(actions.map((button) => button.textContent)).toEqual([
      "Try again",
      "Update to Rhizome v0.51.0",
    ]);
    expect(actions[0]).toHaveClass("primary");
  });

  it("hides a pin offer after Not Now for the app's version", async () => {
    const dismissed: Library = {
      ...library,
      repositories: [{ ...repository, pinOfferDismissed: "v0.51.0" }],
      revision: 2,
    };
    start(undefined, (operation) =>
      operation === "discover"
        ? offered
        : operation === "dismiss-pin-offer"
          ? dismissed
          : undefined,
    );
    const later = (await pinMenu()).find((item) => item.label === "Not Now")!;
    act(() => emit({ type: "menu", id: later.id! }));
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("dismiss-pin-offer", {
        id: repository.id,
        version: "v0.51.0",
      }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Update to v0.51.0" })).toBeNull(),
    );
  });

  it("never offers a pin the app's Rhizome cannot read, and points to app updates", async () => {
    const reason = "Rhizome v0.51.0 can't read this worktree's index, which a newer Rhizome wrote.";
    const incompatible: Discovery = {
      ...discovery,
      worktrees: discovery.worktrees.map((w) =>
        w.path === main
          ? {
              ...w,
              pinOffer: { state: "incompatible", pinned: "v0.50.5", available: "v0.51.0", reason },
            }
          : w,
      ),
    };
    start(undefined, (operation) => (operation === "discover" ? incompatible : undefined));
    fireEvent.click(await screen.findByRole("button", { name: "Needs newer app" }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const labels = lastMenu().map((item) => item.label);
    expect(labels).toContain(reason);
    expect(labels).toContain("Check for App Updates");
    expect(labels.some((label) => label?.startsWith("Update Pin"))).toBe(false);
    report({ step: "error", code: "pinned_version_unavailable", message: "gone" });
    expect(
      within(screen.getByRole("alert"))
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).not.toContain("Update to Rhizome v0.51.0");
  });

  it("names the rzm serving an externally managed worktree", async () => {
    const external: Discovery = {
      ...discovery,
      worktrees: discovery.worktrees.map((w) =>
        w.path === main ? { ...w, authority: "external" } : w,
      ),
    };
    start(undefined, (operation) =>
      operation === "discover"
        ? external
        : operation === "set-executable"
          ? library
          : operation === "restart"
            ? null
            : undefined,
    );
    act(() =>
      emit({
        type: "presence",
        repositories: { [repository.id]: { info: external } },
        runtimes: { [main]: { state: "running", mode: "headless", executable: "/tools/dev/rzm" } },
      }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "External rzm" }));
    await waitFor(() => expect(call).toHaveBeenCalledWith("menu", expect.anything()));
    const labels = lastMenu().map((item) => item.label);
    expect(labels).toContain("Running /tools/dev/rzm");
    pick.mockResolvedValue("/tools/other/rzm");
    const choose = lastMenu().find((item) => item.label === "Choose Executable…")!;
    act(() => emit({ type: "menu", id: choose.id! }));
    // The running runtime is replaced, or Open would reuse it.
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("restart", { id: repository.id, worktree: main }),
    );
  });

  it("confirms a copied page URL briefly in the toolbar", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    act(() => emit({ type: "notice", message: "Copied the page URL" }));
    expect(screen.getByText("Copied the page URL")).toBeVisible();
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
      if (operation === "discover" && id === other.id && slow)
        return new Promise<Result>((resolve) => {
          release = () => resolve(otherDiscovery);
        });
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    slow = true;
    await openRepository(other.id);
    await openRepository(repository.id);
    await waitFor(() => expect(opened()).toHaveLength(2));
    await act(async () => release());
    expect(opened()).toHaveLength(2);
    expect(opened()[1]).toEqual({ id: repository.id, worktree: main, skipSeed: false });
    const [first, second] = call.mock.calls
      .filter(([operation]) => operation === "open")
      .map(([, args]) => (args as { selection: number }).selection);
    expect(second).toBeGreaterThan(first);
    expect(screen.getByRole("button", { name: "Repository project" })).toBeVisible();
  });

  it("lets a terminal selection outrank a pending earlier one", async () => {
    let release = () => {};
    let slow = false;
    start(undefined, (operation, args) => {
      if (operation === "list") return both;
      if (operation === "open") return { generation: ++generation, library: both };
      const id = (args as { id?: string }).id;
      if (operation === "discover" && id === other.id && slow)
        return new Promise<Result>((resolve) => {
          release = () => resolve(otherDiscovery);
        });
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    slow = true;
    await openRepository(other.id);
    act(() =>
      emit({ type: "select", library: both, repository: repository.id, worktree: feature }),
    );
    await waitFor(() => expect(opened()).toHaveLength(2));
    await act(async () => release());
    expect(opened()).toHaveLength(2);
    expect(opened()[1]).toEqual({ id: repository.id, worktree: feature, skipSeed: false });
    expect(screen.getByRole("button", { name: "Repository project" })).toBeVisible();
  });

  it("ignores a terminal selection begun before the user chose another", async () => {
    start(undefined, (operation, args) => {
      if (operation === "list") return both;
      if (operation === "open") return { generation: ++generation, library: both };
      if (operation === "discover" && (args as { id: string }).id === other.id)
        return otherDiscovery;
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    const begun = Date.now() - 1000;
    openRepository(other.id);
    await waitFor(() => expect(opened()).toHaveLength(2));
    act(() =>
      emit({
        type: "select",
        library: both,
        repository: repository.id,
        worktree: feature,
        started: begun,
      }),
    );
    await act(async () => {});
    expect(opened()).toHaveLength(2);
    expect(opened()[1]).toMatchObject({ id: other.id });
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
    expect(screen.getByRole("button", { name: "Repository project" })).toBeVisible();
  });

  it("trusts and restarts a rebuilt worktree from its alert", async () => {
    start(undefined, (operation) => (operation === "restart" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    act(() =>
      emit({
        type: "alert",
        code: "trust_required",
        message: "wt has a new Rhizome build. Trust this worktree to restart Rhizome on it.",
        repository: repository.id,
        worktree: "/repo/wt",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Trust and restart" }));
    const target = { id: repository.id, worktree: "/repo/wt" };
    await waitFor(() =>
      expect(
        call.mock.calls.filter(([operation]) => ["trust", "restart"].includes(operation)),
      ).toEqual([
        ["trust", target],
        ["restart", target],
      ]),
    );
    expect(screen.queryByRole("alert")).toBeNull();
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
    await openRepository(other.id);
    expect(await screen.findByText("other is unavailable")).toBeVisible();
    expect(call).toHaveBeenCalledWith("deselect", { selection: expect.any(Number) });
  });

  it("waits for the repository finder model before choosing during startup", async () => {
    let selecting = false;
    const selection = new Promise<void>((resolve, reject) => {
      start(undefined, (operation, args) => {
        if (operation === "list") return both;
        if (operation === "open") {
          if (!selecting) {
            selecting = true;
            const receive = emit;
            emit = (message) => {
              if (message.type === "finder" && message.choice === "open") {
                expect(finderModel()?.mode).toBe("repositories");
                expect(finderModel()?.rows).toEqual(
                  expect.arrayContaining([expect.objectContaining({ repository: other.id })]),
                );
              }
              receive(message);
            };
            queueMicrotask(() => {
              Promise.resolve()
                .then(() => openRepository(other.id))
                .then(resolve, reject);
            });
          }
          return { generation: ++generation, library: both };
        }
        if (
          operation === "discover" &&
          typeof args === "object" &&
          args !== null &&
          "id" in args &&
          args.id === other.id
        )
          return Promise.reject({
            code: "folder_missing",
            message: "This repository is unavailable.",
          });
      });
    });
    await selection;
    expect(await screen.findByText("other is unavailable")).toBeVisible();
    expect(call).toHaveBeenCalledWith("discover", expect.objectContaining({ id: other.id }));
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
    await showFinder(/^Repository /);
    saved = both;
    act(() => emit({ type: "library", library: saved }));
    await waitFor(() =>
      expect(finderModel()!.rows.map((r) => r.title)).toEqual(["project", "other"]),
    );
    saved = { version: 3, repositories: [other], revision: 3 };
    act(() => emit({ type: "library", library: saved }));
    await waitFor(() =>
      expect(opened().at(-1)).toEqual({ id: other.id, worktree: "/work/other", skipSeed: false }),
    );
    expect(await screen.findByRole("button", { name: "Repository other" })).toBeVisible();
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
    const titles = () => finderModel()!.rows.map((r) => r.title);
    await showFinder(/^Repository /);
    expect(titles()).toEqual(["project", "other"]);
    act(() => emit({ type: "library", library: { ...library, revision: 3 } }));
    await waitFor(() => expect(titles()).toEqual(["project"]));
    await act(async () => release());
    expect(titles()).toEqual(["project"]);
  });

  it("opens a repository's primary worktree from the repository finder, newest first", async () => {
    let initialOpen = () => {};
    const restoring = new Promise<void>((resolve) => {
      initialOpen = resolve;
    });
    const recent: Library = {
      ...both,
      repositories: [
        { ...repository, lastOpened: 1 },
        { ...other, lastOpened: 5 },
      ],
    };
    start(undefined, (operation, args) => {
      if (operation === "list") return recent;
      if (operation === "open") {
        initialOpen();
        return { generation: ++generation, library: recent };
      }
      if (operation === "discover" && (args as { id: string }).id === other.id)
        return otherDiscovery;
    });
    await restoring;
    // An open IPC call starts before React commits the restored selection.
    await showFinder(/^Worktree /);
    choose({ choice: "open", repository: repository.id, worktree: feature, newWindow: false });
    await waitFor(() => expect(opened()).toHaveLength(2));
    act(() => emit({ type: "command", command: "repositories" }));
    await waitFor(() => expect(finderModel()?.mode).toBe("repositories"));
    expect(finderModel()!.rows.map((r) => [r.title, r.current])).toEqual([
      ["other", false],
      ["project", true],
    ]);
    // The current repository, shown on a feature worktree, switches to its primary.
    await openRepository(repository.id);
    await waitFor(() =>
      expect(opened().at(-1)).toEqual({ id: repository.id, worktree: main, skipSeed: false }),
    );
    // On the primary already, choosing it again changes nothing.
    const shown = opened().length;
    await openRepository(repository.id);
    await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
    expect(opened()).toHaveLength(shown);
    await openRepository(other.id);
    await waitFor(() => expect(opened().at(-1)).toMatchObject({ id: other.id }));
    // A repository opens on its primary worktree, not the one last used here.
    await openRepository(repository.id);
    await waitFor(() =>
      expect(opened().at(-1)).toEqual({ id: repository.id, worktree: main, skipSeed: false }),
    );
    // A new window is the app's to open; this window keeps its selection.
    const count = opened().length;
    choose({ choice: "open", repository: other.id, worktree: null, newWindow: true });
    expect(opened()).toHaveLength(count);
  });

  it("opens a stopped or failed worktree again from the finders", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "ready" });
    // Shown already, choosing it changes nothing.
    choose({ choice: "open", repository: repository.id, worktree: main, newWindow: false });
    await openRepository(repository.id);
    await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
    expect(opened()).toHaveLength(1);
    report({ step: "sleeping" });
    await openRepository(repository.id);
    await waitFor(() => expect(opened()).toHaveLength(2));
    report({ step: "error" });
    choose({ choice: "open", repository: repository.id, worktree: main, newWindow: false });
    await waitFor(() => expect(opened()).toHaveLength(3));
  });

  it("keeps the worktree label quiet on the primary worktree only", async () => {
    start();
    expect(await screen.findByRole("button", { name: "Worktree main" })).not.toHaveClass("other");
    await showFinder(/^Worktree /);
    choose({ choice: "open", repository: repository.id, worktree: feature, newWindow: false });
    expect(await screen.findByRole("button", { name: "Worktree feature" })).toHaveClass("other");
  });

  it("runs a finder row's repository actions on the window behind the finder", async () => {
    start(undefined, (operation) => (operation === "reveal" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    present();
    const model = await showFinder(/^Repository /);
    choose({ choice: "menu", repository: repository.id, x: 120, y: 240 });
    await waitFor(() =>
      expect(call).toHaveBeenCalledWith("menu", expect.objectContaining({ x: 120, y: 240 })),
    );
    const items = lastMenu();
    expect(items.filter((item) => item.label).map((item) => item.label)).toEqual([
      "Open in Browser",
      "Reveal in Finder",
      "Repository Settings…",
      "Stop Rhizome for main",
      "Restart Rhizome for main",
      "Primary worktree",
      "Rhizome executable for main",
      "Remove from Library",
    ]);
    act(() => emit({ type: "menu", id: items.find((i) => i.label === "Reveal in Finder")!.id! }));
    // The close names the opening it closes, so a late show for it stays hidden,
    // and keeps focus in the shell, which runs the action.
    expect(call).toHaveBeenCalledWith("finder", {
      model: null,
      opened: model.opened,
      toShell: true,
    });
    await waitFor(() => expect(call).toHaveBeenCalledWith("reveal", { worktree: main }));
  });

  it("adds a repository from the finder's foot", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    pick.mockResolvedValue(null);
    choose({ choice: "add" });
    await waitFor(() => expect(pick).toHaveBeenCalledWith(true));
  });

  it("lets the app's settings messages, not a save's reply, set the settings", async () => {
    let reply: (settings: Result) => void = () => {};
    start(undefined, (operation) => {
      if (operation === "global-status") return null;
      if (operation === "set-settings") return new Promise<Result>((resolve) => (reply = resolve));
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    fireEvent.click(screen.getByRole("radio", { name: "Slate" }));
    // Another window's later change arrives before this save's reply.
    act(() => emit({ type: "settings", settings: { theme: "oxblood", appearance: "auto" } }));
    await act(async () => reply({ theme: "slate", appearance: "auto" }));
    expect(document.documentElement.dataset.theme).toBe("oxblood");
  });

  it("closes a finder for any other menu command, such as Settings or Add Repository", async () => {
    start(undefined, (operation) => (operation === "global-status" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    await showFinder(/^Repository /);
    act(() => emit({ type: "command", command: "settings" }));
    expect(finderModel()).toBeNull();
    expect(screen.getByRole("radiogroup", { name: "Theme" })).toBeVisible();

    pick.mockResolvedValue(null);
    act(() => emit({ type: "command", command: "worktrees" }));
    await waitFor(() => expect(finderModel()?.mode).toBe("worktrees"));
    act(() => emit({ type: "command", command: "add-repository" }));
    expect(finderModel()).toBeNull();
    await waitFor(() => expect(pick).toHaveBeenCalledWith(true));
  });

  it("uncovers the workspace when a finder picks the one Settings covers", async () => {
    start(undefined, (operation) => (operation === "global-status" ? null : undefined));
    await waitFor(() => expect(opened()).toHaveLength(1));
    report({ step: "ready" });
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByRole("radiogroup", { name: "Theme" })).toBeVisible();
    choose({ choice: "open", repository: repository.id, worktree: main, newWindow: false });
    expect(screen.queryByRole("radiogroup", { name: "Theme" })).toBeNull();
    expect(opened()).toHaveLength(1);
  });

  it("closes a finder when a terminal opens a folder in this window", async () => {
    start();
    await waitFor(() => expect(opened()).toHaveLength(1));
    await showFinder(/^Repository /);
    act(() => emit({ type: "select", library, repository: repository.id, worktree: feature }));
    expect(finderModel()).toBeNull();
    await waitFor(() => expect(opened().at(-1)).toMatchObject({ worktree: feature }));
  });

  it("keeps a settings message over a startup reply that arrives after it", async () => {
    let reply: (settings: Result) => void = () => {};
    start(undefined, (operation) =>
      operation === "settings" ? new Promise<Result>((resolve) => (reply = resolve)) : undefined,
    );
    await waitFor(() => expect(opened()).toHaveLength(1));
    act(() => emit({ type: "settings", settings: { theme: "indigo", appearance: "auto" } }));
    await act(async () => reply({ theme: "graphite", appearance: "auto" }));
    expect(document.documentElement.dataset.theme).toBe("indigo");
  });

  it("paints the theme saved when its window opened, before asking the app", () => {
    window.__RHIZOME_SHELL_SETTINGS__ = { theme: "spruce", appearance: "auto" };
    onTestFinished(() => {
      delete window.__RHIZOME_SHELL_SETTINGS__;
    });
    start();
    expect(document.documentElement.dataset.theme).toBe("spruce");
  });

  it("takes the app's theme on its own root", async () => {
    start();
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe("graphite"));
    act(() => emit({ type: "settings", settings: { theme: "oxblood", appearance: "dark" } }));
    expect(document.documentElement.dataset.theme).toBe("oxblood");
  });
});

it("shows runtime provider reports in Settings and restarts that worktree through the app", async () => {
  start(undefined, (operation) => {
    if (operation === "credential-status")
      return {
        available: true,
        ready: [],
        providers: [{ provider: "voyage", preference: "server", source: "team", saved: false }],
      };
    if (operation === "team-status") return { signed_in: true, email: "ada@example.test" };
    if (operation === "global-status") return { path: "/app/rzm", bundled: true, version: "0.1.0" };
    if (operation === "render-profiling") return { enabled: false, always: false };
    if (operation === "restart") return null;
    return undefined;
  });
  await waitFor(() => expect(opened()).toHaveLength(1));
  act(() =>
    emit({
      type: "presence",
      repositories: { [repository.id]: { info: discovery } },
      runtimes: {
        [main]: {
          state: "running",
          mode: "headless",
          version: "0.1.0",
          providerAccess: [
            {
              provider: "voyage",
              active: {
                provider: "voyage",
                source: "direct",
                key: "environment",
                envName: "VOYAGE_API_KEY",
                saved: false,
              },
              next: { provider: "voyage", source: "team", preference: "server", saved: false },
              restartRequired: true,
            },
          ],
        },
        [feature]: { state: "running", mode: "attached" },
      },
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Settings" }));
  expect(
    await screen.findByText("Active connection: VOYAGE_API_KEY from the environment"),
  ).toBeVisible();
  expect(screen.getByText("Next start: Atomic Object server")).toBeVisible();
  expect(screen.getByText(/Unknown: this runtime does not report provider sources/)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Restart project / main" }));
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith("restart", { id: repository.id, worktree: main }),
  );
});

it("selects the bundled Rhizome, restarts the runtime, and reloads repository settings", async () => {
  const managedInfo: Discovery = {
    ...discovery,
    worktrees: discovery.worktrees.map((w) => ({
      ...w,
      authority: "managed",
      pinnedVersion: "v0.51.0",
    })),
  };
  let picked = false;
  let reads = 0;
  const bundled = "/Applications/Test Rhizome.app/Contents/MacOS/rzm";
  const updated = {
    ...library,
    revision: 2,
    repositories: [{ ...repository, executables: { [main]: bundled } }],
  };
  start(undefined, (operation) => {
    if (operation === "discover") return managedInfo;
    if (operation === "global-status") return { path: bundled, version: "v0.51.0", bundled: true };
    if (operation === "settings-report") {
      reads++;
      throw {
        code: picked ? "settings_failed" : "setup_unsupported",
        message: picked ? "Report reached the new executable" : "Too old",
      };
    }
    if (operation === "set-executable") {
      picked = !picked;
      return picked ? updated : library;
    }
    if (operation === "restart") return null;
    if (operation === "open" && picked) return { generation: ++generation, library: updated };
    return undefined;
  });
  await waitFor(() => expect(opened()).toHaveLength(1));
  present({ [main]: { state: "running" } }, managedInfo);
  const item = (await repositoryMenu()).find((entry) => entry.label === "Repository Settings…")!;
  act(() => emit({ type: "menu", id: item.id! }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Use the app's Rhizome for this repository" }),
  );
  expect(await screen.findByText("Report reached the new executable")).toBeVisible();
  expect(reads).toBe(2);
  expect(call).toHaveBeenCalledWith("set-executable", {
    id: repository.id,
    worktree: main,
    executable: bundled,
  });
  expect(call).toHaveBeenCalledWith("restart", { id: repository.id, worktree: main });
  expect(screen.getByRole("radio", { name: /The app's Rhizome/ })).toBeChecked();
  fireEvent.click(screen.getByRole("radio", { name: /Pinned version/ }));
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith("set-executable", {
      id: repository.id,
      worktree: main,
      executable: null,
    }),
  );
  expect(
    await screen.findByRole("heading", {
      name: "This repository's Rhizome is too old to change settings here",
    }),
  ).toBeVisible();
  expect(call.mock.calls.filter(([op]) => op === "restart")).toHaveLength(2);
});

it("restarts another repository's running worktree after updating its pin from Settings", async () => {
  const pinned: Discovery = {
    ...otherDiscovery,
    worktrees: otherDiscovery.worktrees.map((w) => ({
      ...w,
      authority: "managed",
      pinnedVersion: "v0.51.0",
      pinOffer: { state: "advance", pinned: "v0.51.0", available: "v0.52.0" },
    })),
  };
  start(undefined, (operation, args) => {
    if (operation === "list") return both;
    if (operation === "discover")
      return (args as { id: string }).id === other.id ? pinned : discovery;
    if (operation === "global-status") return null;
    if (operation === "settings-report") throw { code: "setup_unsupported", message: "Too old" };
    if (["pin-advance", "restart"].includes(operation)) return null;
    return undefined;
  });
  await waitFor(() => expect(opened()).toHaveLength(1));
  act(() =>
    emit({
      type: "presence",
      repositories: { [repository.id]: { info: discovery }, [other.id]: { info: pinned } },
      runtimes: { "/work/other": { state: "running", mode: null } },
    }),
  );
  await showFinder(/^Repository /);
  choose({ choice: "menu", repository: other.id, x: 120, y: 240 });
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith("menu", expect.objectContaining({ x: 120, y: 240 })),
  );
  const item = lastMenu().find((entry) => entry.label === "Repository Settings…")!;
  act(() => emit({ type: "menu", id: item.id! }));
  const update = () => screen.getByRole("button", { name: "Update other to v0.52.0" });
  await waitFor(() => expect(update()).toBeEnabled());
  fireEvent.click(update());
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith("restart", { id: other.id, worktree: "/work/other" }),
  );
  expect(call).toHaveBeenCalledWith("pin-advance", { id: other.id, worktree: "/work/other" });
});
