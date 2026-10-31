import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { TaskRunDetail, TasksLiveState } from "../../api/tasks";
import { jsonReply, withFakeFetch } from "../../test/fakeFetch";
import { renderWithQueryClient as render } from "../../test/renderWithQueryClient";
import { useDesktopRequest } from "../desktopHost";
import {
  CODEX_RUN_ID,
  codexTranscript,
  reportFixture,
  RUN_ID,
  runDetailFixture,
  runsFixture,
  stateFixture,
} from "./taskFixtures";
import { TasksWorkspace } from "./TasksWorkspace";

const NOW = Date.parse("2026-10-31T17:00:00Z");

function open(path: string) {
  window.history.replaceState({}, "", path);
}

/** Shows the request the page's next title report hands Rhizome Desktop. */
function RequestProbe() {
  const request = useDesktopRequest();

  return <output aria-label="Desktop request">{JSON.stringify(request ?? null)}</output>;
}

function summaryRow(name: RegExp | string) {
  const label = within(screen.getByRole("region", { name: "Timeline" })).getByText(name);

  return label.closest("summary")!;
}

describe("TasksWorkspace", () => {
  const http = withFakeFetch();

  beforeEach(() => {
    http
      .json("GET", "/api/tasks", reportFixture(NOW))
      .json("GET", "/api/tasks/state", stateFixture(NOW))
      .json("GET", "/api/tasks/runs", { runs: runsFixture(NOW) })
      .json("GET", `/api/tasks/runs/${RUN_ID}`, runDetailFixture(NOW));
  });

  afterEach(() => {
    vi.useRealTimers();
    open("/");
  });

  it("lists every registered task with its mode, cadence, last outcome, due time, and inbox state", async () => {
    open("/tasks");
    render(<TasksWorkspace />);

    const table = await screen.findByRole("table");
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((row) => within(row).getByRole("rowheader").textContent)).toEqual([
      "Docs drift checkdocs-drift",
      "Dependency updatesdependency-updates",
      "Flaky test triageflaky-tests",
      "Changelog draftchangelog",
    ]);

    const docs = within(rows[0]!);
    expect(docs.getByText("Auto")).toBeInTheDocument();
    expect(docs.getByText("1d")).toBeInTheDocument();
    expect(docs.getByText("Ready")).toBeInTheDocument();
    expect(docs.getByText("Ready for review")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Due now")).toBeInTheDocument();
    expect(within(rows[2]!).getByText("Needs attention")).toBeInTheDocument();
    expect(within(rows[3]!).getByText("Not needed")).toBeInTheDocument();

    const rail = screen.getByRole("navigation", { name: "Tasks" });
    expect(within(rail).getByRole("button", { name: /All tasks/ })).toHaveAttribute(
      "aria-current",
      "page",
    );
    fireEvent.click(within(rail).getByRole("button", { name: /Flaky test triage/ }));
    expect(window.location.pathname).toBe("/tasks/flaky-tests");
  });

  it("shows a new-task announcement and dismisses it", async () => {
    const state = stateFixture(NOW);

    const announcement = {
      ...state.items[0]!,
      id: "12",
      task: "changelog",
      kind: "announcement",
      state: "due",
      summary: 'New task "Changelog draft" runs every 1d in auto mode.',
    };

    http.json("GET", "/api/tasks/state", { ...state, items: [...state.items, announcement] });
    http.json("POST", "/api/tasks/items/12/done", {});
    open("/tasks");
    render(<TasksWorkspace />);

    expect(await screen.findByText(/New task "Changelog draft" runs every 1d/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));

    await waitFor(() => expect(http.count("POST", "/api/tasks/items/12/done")).toBe(1));
  });

  it("pauses and resumes all background work", async () => {
    http.json("GET", "/api/tasks", { ...reportFixture(NOW), paused: true });
    http.json("GET", "/api/tasks/state", { ...stateFixture(NOW), paused: true });
    http.json("POST", "/api/tasks/resume", {});
    open("/tasks");
    render(<TasksWorkspace />);

    expect(await screen.findByText(/Background work is paused/)).toBeVisible();
    fireEvent.click(screen.getAllByRole("button", { name: "Resume all" })[0]!);
    await waitFor(() => expect(http.count("POST", "/api/tasks/resume")).toBe(1));
  });

  it("shows run history newest first, with not-needed checks as quiet single lines", async () => {
    open("/tasks/docs-drift");
    render(<TasksWorkspace />);

    const history = await screen.findByRole("region", { name: "Run history" });
    const rows = await within(history).findAllByRole("button");
    expect(
      rows.map(
        (row) =>
          within(row).getByText(/Ready|Not needed|No changes|Failed|Interrupted/).textContent,
      ),
    ).toEqual(["Ready", "Not needed", "Not needed", "No changes", "Failed", "Interrupted"]);
    expect(rows[1]).toHaveClass("is-quiet");
    expect(within(rows[1]!).queryByText(/tok/)).not.toBeInTheDocument();
    expect(within(rows[0]!).getByText("48,210 tok")).toBeInTheDocument();
    expect(http.requests("GET", "/api/tasks/runs")[0]?.query.get("task")).toBe("docs-drift");

    // The newest run with work opens beside the history.
    expect(rows[0]).toHaveAttribute("aria-current", "true");
    http.json("GET", "/api/tasks/runs/20261028T140004Z-9b2d7e10", {
      run: runsFixture(NOW)[3]!,
      events: [],
      next: 0,
    } satisfies TaskRunDetail);
    fireEvent.click(rows[3]!);
    expect(window.location.pathname).toBe("/tasks/docs-drift/runs/20261028T140004Z-9b2d7e10");
    expect(await screen.findByText("This run recorded nothing.")).toBeVisible();
  });

  it("pins a Claude run's result and keeps every block collapsed until opened", async () => {
    open(`/tasks/docs-drift/runs/${RUN_ID}`);
    render(<TasksWorkspace />);

    const result = await screen.findByRole("region", { name: "Result" });
    expect(within(result).getAllByText("docs/reference/indexing.md").length).toBeGreaterThan(0);
    expect(within(result).getByText(/Replaced the removed/)).toBeInTheDocument();
    expect(within(result).getByText("rzm/docs-drift/2026-10-31")).toBeInTheDocument();
    expect(within(result).getByText("48,210")).toBeInTheDocument();
    expect(within(result).getByRole("button", { name: "Copy worktree path" })).toBeInTheDocument();
    expect(
      screen.getByText("Ready", { selector: ".task-run__head .status-mark__label" }),
    ).toBeVisible();
    expect(screen.getByText(/· 4m 28s/)).toBeInTheDocument();

    const timeline = screen.getByRole("region", { name: "Timeline" });
    // The final message is pinned, not repeated in the timeline.
    expect(within(timeline).queryByText(/Replaced the removed/)).not.toBeInTheDocument();
    expect(within(timeline).getAllByRole("article", { name: "Agent message" })).toHaveLength(2);
    expect(within(timeline).getByText("Retrying: Overloaded (529); retrying in 4s")).toBeVisible();

    // Collapsed: summaries only.
    for (const details of timeline.querySelectorAll("details"))
      expect(details).not.toHaveAttribute("open");
    expect(
      within(timeline).queryByText(/hub_test|docs\/reference\/indexing.md:44/),
    ).not.toBeInTheDocument();
    expect(within(timeline).queryByText(/I should confirm the flag/)).not.toBeInTheDocument();

    const rg = summaryRow("rg -n -- '--legacy-index' cmd pkg docs README.md");
    expect(within(rg).getByText("exit 0")).toBeInTheDocument();
    expect(within(rg).getByText("1.4s")).toBeInTheDocument();
    fireEvent.click(rg);
    expect(await within(timeline).findByText(/docs\/reference\/indexing.md:44/)).toBeVisible();

    const validate = summaryRow("rzm validate docs/reference");
    expect(within(validate).getByText("exit 1")).toBeInTheDocument();

    fireEvent.click(summaryRow("Thinking"));
    expect(
      await within(timeline).findByText(/I should confirm the flag is really gone/),
    ).toBeVisible();

    fireEvent.click(summaryRow("Read"));
    expect(
      await within(timeline).findByText(/"file_path": "docs\/reference\/indexing.md"/),
    ).toBeVisible();

    fireEvent.click(summaryRow("Changed"));
    expect((await within(timeline).findAllByText(/rzm index --rebuild/))[0]).toBeVisible();

    fireEvent.click(summaryRow("Check stderr"));
    expect(await within(timeline).findByText(/found --legacy-index in 3 notes/)).toBeVisible();
  });

  it("renders a Codex transcript: output in result.output, string tool input, failed turn, truncation", async () => {
    const start = NOW - 60_000;
    const run = { ...runsFixture(NOW)[4]!, id: CODEX_RUN_ID, task: "flaky-tests" };
    http.json("GET", "/api/tasks/runs", { runs: [run] });
    http.json("GET", `/api/tasks/runs/${CODEX_RUN_ID}`, {
      run,
      events: codexTranscript(start),
      next: 4096,
    } satisfies TaskRunDetail);
    open(`/tasks/flaky-tests/runs/${CODEX_RUN_ID}`);
    render(<TasksWorkspace />);

    const timeline = await screen.findByRole("region", { name: "Timeline" });
    const command = await within(timeline).findByText(/go test \.\/pkg\/vault\/watchhub/);
    const summary = command.closest("summary")!;
    expect(within(summary).getByText("exit 1")).toBeInTheDocument();
    expect(within(summary).getByText("38s")).toBeInTheDocument();
    fireEvent.click(summary);
    expect(await within(timeline).findByText(/got 2 events, want 1/)).toBeVisible();
    expect(within(timeline).getByText(/Output truncated at 16 KB/)).toBeVisible();
    expect(within(timeline).getByText("in /tmp/wt/flaky-tests")).toBeVisible();

    const tool = summaryRow("rhizome/search");
    expect(within(tool).getByText("watch hub debounce")).toBeInTheDocument();
    fireEvent.click(tool);
    expect(await within(timeline).findByText(/"limit": 5/)).toBeVisible();

    fireEvent.click(summaryRow("Changed"));
    expect(await within(timeline).findByText(/hub\.Debounce\(\)/)).toBeVisible();

    expect(within(timeline).getByText("Turn failed")).toBeVisible();
    expect(within(timeline).getByText(/reached its 10 MB limit/)).toBeVisible();
    expect(screen.getByText("Could not stabilise the test; see the failure above.")).toBeVisible();
  });

  it("follows a live run from `next`, keeps opened blocks open, and stops when it finishes", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    const events = runDetailFixture(NOW).events;
    const running = { ...runsFixture(NOW)[0]!, outcome: "", at: "", stage: "agent" as const };
    let live: TasksLiveState = { ...stateFixture(NOW), running };
    http.on("GET", "/api/tasks/state", () => jsonReply(live));

    const finished = { run: runsFixture(NOW)[0]!, events: events.slice(8), next: 1200 };

    const pages = new Map<string, TaskRunDetail>([
      ["0", { run: running, events: events.slice(0, 4), next: 400, checkLog: "check ok" }],
      ["400", { run: running, events: events.slice(4, 8), next: 800 }],
      ["800", finished],
    ]);

    http.on("GET", `/api/tasks/runs/${RUN_ID}`, (request) =>
      jsonReply(pages.get(request.query.get("after") ?? "") ?? { ...finished, events: [] }),
    );
    open(`/tasks/docs-drift/runs/${RUN_ID}`);
    render(<TasksWorkspace />);

    const timeline = await screen.findByRole("region", { name: "Timeline" });
    expect(
      screen.getByText("Running", { selector: ".task-run__head .status-mark__label" }),
    ).toBeVisible();
    fireEvent.click(summaryRow("Thinking"));
    expect(await within(timeline).findByText(/I should confirm/)).toBeVisible();

    await act(() => vi.advanceTimersByTimeAsync(2_000));
    expect(await within(timeline).findByText("Read")).toBeInTheDocument();
    // The block opened before the poll is still open.
    expect(within(timeline).getByText(/I should confirm/)).toBeVisible();

    live = stateFixture(NOW);
    await act(() => vi.advanceTimersByTimeAsync(2_000));
    await waitFor(() =>
      expect(
        screen.getByText("Ready", { selector: ".task-run__head .status-mark__label" }),
      ).toBeVisible(),
    );
    expect(within(timeline).getByText(/rzm validate docs\/reference/)).toBeInTheDocument();
    expect(
      http.requests("GET", `/api/tasks/runs/${RUN_ID}`).map((r) => r.query.get("after")),
    ).toEqual(["0", "400", "800"]);

    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(http.count("GET", `/api/tasks/runs/${RUN_ID}`)).toBe(3);
  });

  it("finishes a run that started and ended between live-state polls", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    const finished = runsFixture(NOW)[0]!;
    const unfinished = { ...finished, outcome: "", at: "" };
    let detail: TaskRunDetail = { run: unfinished, events: [], next: 0 };
    http.on("GET", `/api/tasks/runs/${RUN_ID}`, () => jsonReply(detail));
    open(`/tasks/docs-drift/runs/${RUN_ID}`);
    render(<TasksWorkspace />);

    expect(
      await screen.findByText("Running", { selector: ".task-run__head .status-mark__label" }),
    ).toBeVisible();
    detail = runDetailFixture(NOW);
    await act(() => vi.advanceTimersByTimeAsync(2_100));

    expect(
      await screen.findByText("Ready", { selector: ".task-run__head .status-mark__label" }),
    ).toBeVisible();
  });

  it("keeps the transcript and offers Retry when a later read fails", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    const running = { ...runsFixture(NOW)[0]!, outcome: "", at: "" };
    let fail = false;
    http.on("GET", `/api/tasks/runs/${RUN_ID}`, () =>
      fail
        ? jsonReply({ error: "runtime restarting" }, 503)
        : jsonReply({ run: running, events: runDetailFixture(NOW).events.slice(0, 4), next: 400 }),
    );
    open(`/tasks/docs-drift/runs/${RUN_ID}`);
    render(<TasksWorkspace />);

    const timeline = await screen.findByRole("region", { name: "Timeline" });
    await waitFor(() => expect(summaryRow("Thinking")).toBeInTheDocument());
    fail = true;
    await act(() => vi.advanceTimersByTimeAsync(2_100));

    expect(await within(timeline).findByText(/Could not refresh this run/)).toBeVisible();
    expect(summaryRow("Thinking")).toBeInTheDocument();
  });

  it("offers an earlier attempt's own item actions when its run is selected", async () => {
    const failedID = "20261027T140003Z-5e5e1f00";
    const state = stateFixture(NOW);

    const earlier = {
      ...state.items.find((item) => item.id === "7")!,
      id: "5",
      state: "needs-attention",
      reason: "agent run failed",
    };

    http.json("GET", "/api/tasks/state", { ...state, items: [...state.items, earlier] });
    http.json("GET", "/api/tasks/runs", {
      runs: runsFixture(NOW).map((run) => (run.id === failedID ? { ...run, item: "5" } : run)),
    });
    http.json("GET", `/api/tasks/runs/${failedID}`, { ...runDetailFixture(NOW), events: [] });
    open(`/tasks/docs-drift/runs/${failedID}`);
    render(<TasksWorkspace />);

    // Item 7 is ready (no Run); item 5 needs attention, so Run is offered.
    expect(await screen.findByRole("button", { name: "Run" })).toBeVisible();
  });

  it("polls live state after Run until the launched run appears", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    const launchedAt = new Date(NOW).toISOString();
    let live = stateFixture(NOW);
    http.on("GET", "/api/tasks/state", () => jsonReply(live));
    http.json("POST", "/api/tasks/items/9/run", { launchedAt });
    open("/tasks/dependency-updates");
    render(<TasksWorkspace />);

    fireEvent.click(await screen.findByRole("button", { name: "Run" }));
    await waitFor(() => expect(http.count("POST", "/api/tasks/items/9/run")).toBe(1));
    expect(await screen.findByText("Starting the run…")).toBeVisible();
    const afterLaunch = http.count("GET", "/api/tasks/state");

    await act(() => vi.advanceTimersByTimeAsync(4_100));
    expect(http.count("GET", "/api/tasks/state")).toBeGreaterThanOrEqual(afterLaunch + 2);

    // The run shows up, then finishes: polling falls back to the idle rate.
    live = {
      ...live,
      recent: [
        {
          id: "20261031T170001Z-00000001",
          task: "dependency-updates",
          startedAt: launchedAt,
          at: launchedAt,
          outcome: "notified",
        },
        ...live.recent,
      ],
    };
    await act(() => vi.advanceTimersByTimeAsync(2_100));
    await waitFor(() => expect(screen.queryByText("Starting the run…")).not.toBeInTheDocument());
    const settled = http.count("GET", "/api/tasks/state");
    await act(() => vi.advanceTimersByTimeAsync(20_000));
    expect(http.count("GET", "/api/tasks/state")).toBe(settled);
  });

  it("stops awaiting a launch after 30 seconds", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    http.json("POST", "/api/tasks/items/9/run", { launchedAt: new Date(NOW).toISOString() });
    open("/tasks/dependency-updates");
    render(<TasksWorkspace />);

    fireEvent.click(await screen.findByRole("button", { name: "Run" }));
    expect(await screen.findByText("Starting the run…")).toBeVisible();
    await act(() => vi.advanceTimersByTimeAsync(31_000));
    expect(screen.queryByText("Starting the run…")).not.toBeInTheDocument();
    const stopped = http.count("GET", "/api/tasks/state");
    await act(() => vi.advanceTimersByTimeAsync(20_000));
    expect(http.count("GET", "/api/tasks/state")).toBe(stopped);
  });

  it("shows a refused Run and a launch error on the item", async () => {
    http.json(
      "POST",
      "/api/tasks/items/9/run",
      { error: "a task run is in progress in this repository; try again when it finishes" },
      409,
    );
    open("/tasks/dependency-updates");
    const { unmount } = render(<TasksWorkspace />);

    fireEvent.click(await screen.findByRole("button", { name: "Run" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "a task run is in progress in this repository; try again when it finishes",
    );
    unmount();

    const state = stateFixture(NOW);
    http.json("GET", "/api/tasks/state", {
      ...state,
      items: state.items.map((item) =>
        item.id === "9" ? { ...item, launchError: "rzm: executable not found" } : item,
      ),
    });
    render(<TasksWorkspace />);
    expect(
      await screen.findByText("The run could not start: rzm: executable not found"),
    ).toBeVisible();
  });

  it("offers only the actions an item's state allows, and sets the mode", async () => {
    http.json("POST", "/api/tasks/items/8/discard", {});
    http.json("POST", "/api/tasks/modes/flaky-tests", {});
    open("/tasks/flaky-tests");
    render(<TasksWorkspace />);

    const item = await screen.findByRole("region", { name: "Inbox item" });
    expect(
      within(item)
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual(["Run", "Snooze…", "Discard", "Done"]);
    fireEvent.click(within(item).getByRole("button", { name: "Discard" }));
    await waitFor(() => expect(http.count("POST", "/api/tasks/items/8/discard")).toBe(1));

    const mode = screen.getByRole("group", { name: "Mode" });
    expect(within(mode).getByRole("button", { name: "Auto" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.click(within(mode).getByRole("button", { name: /Default/ }));
    await waitFor(() => expect(http.count("POST", "/api/tasks/modes/flaky-tests")).toBe(1));
    expect(http.requests("POST", "/api/tasks/modes/flaky-tests")[0]?.body).toBe(
      '{"mode":"default"}',
    );

    http.json(
      "POST",
      "/api/tasks/modes/flaky-tests",
      { error: "mode must be auto, notify, or off" },
      400,
    );
    fireEvent.click(within(mode).getByRole("button", { name: "Off" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not change the mode: mode must be auto, notify, or off",
    );
  });

  it("offers Start and Open worktree only in Rhizome Desktop, and hands them to the app", async () => {
    const worktree = "/repo/.rhizome/task-state/worktrees/flaky-tests-2026-10-31";

    const withWorktree = <T extends { id: string; worktree?: string }>(item: T) =>
      item.id === "8" ? { ...item, worktree } : item;

    const report = reportFixture(NOW);

    http
      .json("GET", "/api/tasks", {
        ...report,
        tasks: report.tasks.map((task) =>
          task.item ? { ...task, item: withWorktree(task.item) } : task,
        ),
      })
      .json("GET", "/api/tasks/state", {
        ...stateFixture(NOW),
        items: stateFixture(NOW).items.map(withWorktree),
      });

    const buttons = async (path: string) => {
      open(path);

      const view = render(
        <>
          <TasksWorkspace />
          <RequestProbe />
        </>,
      );

      const item = await screen.findByRole("region", { name: "Inbox item" });

      const labels = within(item)
        .getAllByRole("button")
        .map((button) => button.textContent);

      return { item, labels, view };
    };

    // A browser keeps only the worktree path and its copy button.
    const browser = await buttons("/tasks/flaky-tests");

    expect(browser.labels).toEqual(["Run", "Snooze…", "Discard", "Done"]);
    browser.view.unmount();

    window.__RHIZOME_DESKTOP__ = true;

    try {
      const attention = await buttons("/tasks/flaky-tests");

      expect(attention.labels).toEqual([
        "Run",
        "Start",
        "Open worktree",
        "Snooze…",
        "Discard",
        "Done",
      ]);

      const request = () =>
        JSON.parse(screen.getByRole("status", { name: "Desktop request" }).textContent!);

      fireEvent.click(within(attention.item).getByRole("button", { name: "Start" }));

      const started = request();

      expect(started).toEqual({ action: "startTask", item: "8", n: started.n });
      fireEvent.click(within(attention.item).getByRole("button", { name: "Open worktree" }));
      expect(request()).toEqual({ action: "openTaskWorktree", path: worktree, n: started.n + 1 });
      attention.view.unmount();

      // A ready item has nothing to start; a due one has no worktree yet.
      const ready = await buttons("/tasks/docs-drift");

      expect(ready.labels).toEqual(["Open worktree", "Snooze…", "Discard", "Done"]);
      ready.view.unmount();

      const due = await buttons("/tasks/dependency-updates");

      expect(due.labels).toEqual(["Run", "Start", "Snooze…", "Skip", "Done"]);
    } finally {
      delete window.__RHIZOME_DESKTOP__;
    }
  });

  it("reports an interrupted run once the run lock holder is gone", async () => {
    const running = { ...runsFixture(NOW)[0]!, outcome: "", stage: "agent" as const };
    http.json("GET", "/api/tasks/state", { ...stateFixture(NOW), running, stale: true });
    open(`/tasks/docs-drift/runs/${RUN_ID}`);
    render(<TasksWorkspace />);

    expect(
      await screen.findByText("Interrupted", { selector: ".task-run__head .status-mark__label" }),
    ).toBeVisible();
  });

  it("labels the run in progress in history when the runs list already has it", async () => {
    const running = { ...runsFixture(NOW)[0]!, outcome: "", stage: "agent" as const };
    http.json("GET", "/api/tasks/state", { ...stateFixture(NOW), running, stale: true });
    http.json("GET", "/api/tasks/runs", { runs: [running, ...runsFixture(NOW).slice(1)] });
    open(`/tasks/docs-drift/runs/${RUN_ID}`);
    render(<TasksWorkspace />);

    const history = await screen.findByRole("region", { name: "Run history" });
    const row = await waitFor(() => history.querySelector<HTMLElement>(".is-selected")!);

    expect(within(row).getByText("Interrupted")).toBeVisible();
  });

  it("shows check stderr written after the run view opened", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    const running = { ...runsFixture(NOW)[0]!, outcome: "", stage: "check" as const };
    http.json("GET", "/api/tasks/state", { ...stateFixture(NOW), running });
    let checkLog = "scanning";
    http.on("GET", `/api/tasks/runs/${RUN_ID}`, () =>
      jsonReply({ run: running, events: [], next: 0, checkLog }),
    );
    open(`/tasks/docs-drift/runs/${RUN_ID}`);
    render(<TasksWorkspace />);

    fireEvent.click(await waitFor(() => summaryRow("Check stderr")));
    checkLog = "scanning\ncheck failed: no such file";
    await act(() => vi.advanceTimersByTimeAsync(2_100));

    expect(await screen.findByText(/check failed: no such file/)).toBeVisible();
  });

  it("keeps awaiting a retried launch while the item shows an earlier launch error", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    const state = stateFixture(NOW);
    // Same second as the retry: only sub-second precision tells them apart.
    const earlier = new Date(NOW + 100).toISOString();
    http.json("GET", "/api/tasks/state", {
      ...state,
      items: state.items.map((item) =>
        item.id === "9" ? { ...item, launchError: "busy", updated: earlier } : item,
      ),
    });
    http.json("POST", "/api/tasks/items/9/run", {
      launchedAt: new Date(NOW + 700).toISOString(),
    });
    open("/tasks/dependency-updates");
    render(<TasksWorkspace />);

    fireEvent.click(await screen.findByRole("button", { name: "Run" }));
    expect(await screen.findByText("Starting the run…")).toBeVisible();
    await act(() => vi.advanceTimersByTimeAsync(4_100));

    expect(screen.getByText("Starting the run…")).toBeVisible();
  });

  it("explains an empty registry and a load failure", async () => {
    http.json("GET", "/api/tasks", { ...reportFixture(NOW), tasks: [] });
    open("/tasks");
    const { unmount } = render(<TasksWorkspace />);
    expect(await screen.findByRole("heading", { name: "No tasks are registered" })).toBeVisible();
    unmount();

    http.json("GET", "/api/tasks", { error: "state.json is unreadable" }, 500);
    render(<TasksWorkspace />);
    expect(await screen.findByRole("heading", { name: "Could not load tasks" })).toBeVisible();
    expect(screen.getByText("state.json is unreadable")).toBeVisible();
  });
});
