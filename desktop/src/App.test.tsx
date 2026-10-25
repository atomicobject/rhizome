import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
  fireEvent.click(await screen.findByRole("button", { name }));
  await waitFor(() => expect(finderModel()).not.toBeNull());
  return finderModel()!;
}
function choose(choice: FinderChoice) {
  act(() => emit({ type: "finder", ...choice }));
}
const openRepository = (id: string) =>
  choose({ choice: "open", repository: id, worktree: null, newWindow: false });
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
function present(runtimes: Record<string, { state: "running" | "starting" | "stopped" }> = {}) {
  act(() =>
    emit({
      type: "presence",
      repositories: { [repository.id]: { info: discovery } },
      runtimes: Object.fromEntries(
        Object.entries(runtimes).map(([path, { state }]) => [path, { state, mode: null }]),
      ),
    }),
  );
}
beforeEach(() => vi.resetAllMocks());

describe("desktop shell", () => {
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

  it("opens What Gets Indexed from the repository menu over the workspace", async () => {
    start(undefined, (operation) =>
      operation === "scope"
        ? { builtInApplies: false, rules: [], keepIndexed: [], root: main }
        : undefined,
    );
    await waitFor(() => expect(opened()).toHaveLength(1));
    present();
    const item = (await repositoryMenu()).find((entry) => entry.label === "What Gets Indexed…")!;
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

    act(() => emit({ type: "page", state: null }));
    expect(screen.queryByRole("navigation", { name: "Sections" })).toBeNull();
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
    fireEvent.click(await screen.findByRole("button", { name: "Rhizome v0.51.0 available" }));
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
      expect(screen.queryByRole("button", { name: "Rhizome v0.51.0 available" })).toBeNull(),
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
    openRepository(other.id);
    openRepository(repository.id);
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
    openRepository(other.id);
    act(() =>
      emit({ type: "select", library: both, repository: repository.id, worktree: feature }),
    );
    await waitFor(() => expect(opened()).toHaveLength(2));
    await act(async () => release());
    expect(opened()).toHaveLength(2);
    expect(opened()[1]).toEqual({ id: repository.id, worktree: feature, skipSeed: false });
    expect(screen.getByRole("button", { name: "Repository project" })).toBeVisible();
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
    openRepository(other.id);
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
    const recent: Library = {
      ...both,
      repositories: [
        { ...repository, lastOpened: 1 },
        { ...other, lastOpened: 5 },
      ],
    };
    start(undefined, (operation, args) => {
      if (operation === "list") return recent;
      if (operation === "open") return { generation: ++generation, library: recent };
      if (operation === "discover" && (args as { id: string }).id === other.id)
        return otherDiscovery;
    });
    await waitFor(() => expect(opened()).toHaveLength(1));
    choose({ choice: "open", repository: repository.id, worktree: feature, newWindow: false });
    await waitFor(() => expect(opened()).toHaveLength(2));
    act(() => emit({ type: "command", command: "repositories" }));
    await waitFor(() => expect(finderModel()?.mode).toBe("repositories"));
    expect(finderModel()!.rows.map((r) => [r.title, r.current])).toEqual([
      ["other", false],
      ["project", true],
    ]);
    // The current repository, shown on a feature worktree, switches to its primary.
    openRepository(repository.id);
    await waitFor(() =>
      expect(opened().at(-1)).toEqual({ id: repository.id, worktree: main, skipSeed: false }),
    );
    // On the primary already, choosing it again changes nothing.
    const shown = opened().length;
    openRepository(repository.id);
    await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
    expect(opened()).toHaveLength(shown);
    openRepository(other.id);
    await waitFor(() => expect(opened().at(-1)).toMatchObject({ id: other.id }));
    // A repository opens on its primary worktree, not the one last used here.
    openRepository(repository.id);
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
    openRepository(repository.id);
    await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
    expect(opened()).toHaveLength(1);
    report({ step: "sleeping" });
    openRepository(repository.id);
    await waitFor(() => expect(opened()).toHaveLength(2));
    report({ step: "error" });
    choose({ choice: "open", repository: repository.id, worktree: main, newWindow: false });
    await waitFor(() => expect(opened()).toHaveLength(3));
  });

  it("keeps the worktree label quiet on the primary worktree only", async () => {
    start();
    expect(await screen.findByRole("button", { name: "Worktree main" })).not.toHaveClass("other");
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
      "What Gets Indexed…",
      "Stop Rhizome for main",
      "Restart Rhizome for main",
      "Primary worktree",
      "Rhizome executable for main",
      "Remove from Library",
    ]);
    act(() => emit({ type: "menu", id: items.find((i) => i.label === "Reveal in Finder")!.id! }));
    // The close names the opening it closes, so a late show for it stays hidden.
    expect(call).toHaveBeenCalledWith("finder", { model: null, opened: model.opened });
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
    act(() => emit({ type: "command", command: "repositories" }));
    await waitFor(() => expect(finderModel()?.mode).toBe("repositories"));
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

  it("takes the app's theme on its own root", async () => {
    start();
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe("graphite"));
    act(() => emit({ type: "settings", settings: { theme: "oxblood", appearance: "dark" } }));
    expect(document.documentElement.dataset.theme).toBe("oxblood");
  });
});
