import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { TaskRunDetail, TasksLiveState } from "../../api/tasks";
import { jsonReply, withFakeFetch } from "../../test/fakeFetch";
import { renderWithQueryClient as render } from "../../test/renderWithQueryClient";
import {
  localDraftsReportFixture,
  RUN_ID,
  runDetailFixture,
  runsFixture,
  SOURCE_CHECKOUT,
  stateFixture,
  TEST_RUN_ID,
  testRunFixture,
} from "./taskFixtures";
import { TasksWorkspace } from "./TasksWorkspace";

const NOW = Date.parse("2026-10-31T17:00:00Z");

function open(path: string) {
  window.history.replaceState({}, "", path);
}

function localStrip() {
  return screen.findByRole("region", { name: "Local version" });
}

describe("TasksWorkspace local task drafts", () => {
  const http = withFakeFetch();

  beforeEach(() => {
    http
      .json("GET", "/api/tasks", localDraftsReportFixture(NOW))
      .json("GET", "/api/tasks/state", stateFixture(NOW))
      .json("GET", "/api/tasks/runs", { runs: runsFixture(NOW) })
      .json("GET", `/api/tasks/runs/${RUN_ID}`, runDetailFixture(NOW));
  });

  afterEach(() => {
    vi.useRealTimers();
    open("/");
  });

  it("marks each task's local version on one row, and gives a local-only task no schedule", async () => {
    open("/tasks");
    render(<TasksWorkspace />);

    const table = await screen.findByRole("table");
    const rows = within(table).getAllByRole("row").slice(1);
    const row = (id: string) => rows.find((candidate) => within(candidate).queryByText(id))!;

    expect(rows).toHaveLength(5);
    expect(within(row("docs-drift")).getByText("Local changes")).toBeVisible();
    expect(within(row("flaky-tests")).getByText("Invalid locally")).toBeVisible();
    // Unchanged and locally deleted tasks read as plain scheduled rows.
    expect(within(row("changelog")).queryByText(/locally|Local/)).not.toBeInTheDocument();
    expect(within(row("dependency-updates")).queryByText(/locally|Local/)).not.toBeInTheDocument();
    expect(within(row("dependency-updates")).getByText("Due now")).toBeVisible();

    const draft = within(row("session-mining"));
    expect(draft.getByText("Local only")).toBeVisible();
    expect(draft.getByText("Not scheduled")).toBeVisible();
    expect(draft.queryByText(/Off|Auto|Notify/)).not.toBeInTheDocument();

    const rail = screen.getByRole("navigation", { name: "Tasks" });
    expect(within(rail).getByRole("button", { name: /Session mining/ })).toHaveTextContent("Local");
  });

  it("tests a local-only task once and opens the test when it starts", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    const launchedAt = new Date(NOW).toISOString();
    let live: TasksLiveState = stateFixture(NOW);
    http.on("GET", "/api/tasks/state", () => jsonReply(live));
    http.json("GET", "/api/tasks/runs", { runs: [] });
    http.json("POST", "/api/tasks/tests/session-mining", {
      launchedAt,
      sourceCheckout: SOURCE_CHECKOUT,
    });
    open("/tasks/session-mining");
    render(<TasksWorkspace />);

    const strip = await localStrip();
    expect(within(strip).getByText("Local only")).toBeVisible();
    expect(within(strip).getByText(".rhizome/tasks/session-mining.yaml")).toBeVisible();
    expect(screen.queryByRole("group", { name: "Mode" })).not.toBeInTheDocument();
    expect(screen.getByText(/No tests yet/)).toBeVisible();

    fireEvent.click(within(strip).getByRole("button", { name: "Test once" }));
    await waitFor(() => expect(http.count("POST", "/api/tasks/tests/session-mining")).toBe(1));
    expect(await within(strip).findByText("Starting the test…")).toBeVisible();
    expect(within(strip).getByRole("button", { name: "Test once" })).toBeDisabled();

    // A scheduled run of the same task is not the launched test.
    const started = { task: "session-mining", startedAt: launchedAt, at: launchedAt };
    live = { ...live, recent: [{ ...started, id: "scheduled-1", outcome: "ready" }] };
    await act(() => vi.advanceTimersByTimeAsync(2_100));
    expect(within(strip).getByText("Starting the test…")).toBeVisible();

    const testID = "20261031T170001Z-7e570001";

    const run = {
      ...started,
      id: testID,
      outcome: "",
      stage: "check" as const,
      test: true,
      sourceCheckout: SOURCE_CHECKOUT,
    };

    live = { ...live, running: run };
    http.json("GET", `/api/tasks/runs/${testID}`, {
      run: { ...run, hasTranscript: false, hasCheckLog: true },
      events: [],
      next: 0,
    } satisfies TaskRunDetail);
    await act(() => vi.advanceTimersByTimeAsync(2_100));

    await waitFor(() =>
      expect(window.location.pathname).toBe(`/tasks/session-mining/runs/${testID}`),
    );
    expect(within(strip).queryByText("Starting the test…")).not.toBeInTheDocument();
    // A run in progress blocks another test.
    expect(within(strip).getByRole("button", { name: "Test once" })).toBeDisabled();
  });

  it("opens this checkout's failed test, not another checkout's test of the same task", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: NOW });
    const launchedAt = new Date(NOW).toISOString();
    let live: TasksLiveState = stateFixture(NOW);
    http.on("GET", "/api/tasks/state", () => jsonReply(live));
    http.json("GET", "/api/tasks/runs", { runs: [] });
    http.json("POST", "/api/tasks/tests/session-mining", {
      launchedAt,
      sourceCheckout: SOURCE_CHECKOUT,
    });
    open("/tasks/session-mining");
    render(<TasksWorkspace />);

    const strip = await localStrip();
    fireEvent.click(within(strip).getByRole("button", { name: "Test once" }));
    await waitFor(() => expect(http.count("POST", "/api/tasks/tests/session-mining")).toBe(1));

    // Another checkout's test of the same task holds the running slot, so this
    // checkout's test failed fast as busy.
    const started = { task: "session-mining", startedAt: launchedAt, at: launchedAt, test: true };

    const foreign = {
      ...started,
      id: "foreign-1",
      outcome: "",
      sourceCheckout: "/Users/dev/other",
    };

    const ownID = "20261031T170001Z-7e570002";
    const own = { ...started, id: ownID, outcome: "failed", sourceCheckout: SOURCE_CHECKOUT };
    live = { ...live, running: foreign, recent: [own, ...live.recent] };
    http.json("GET", "/api/tasks/runs/foreign-1", {
      run: { ...foreign, hasTranscript: false, hasCheckLog: false },
      events: [],
      next: 0,
    } satisfies TaskRunDetail);
    http.json("GET", `/api/tasks/runs/${ownID}`, {
      run: { ...own, hasTranscript: false, hasCheckLog: false },
      events: [],
      next: 0,
    } satisfies TaskRunDetail);
    await act(() => vi.advanceTimersByTimeAsync(2_100));

    await waitFor(() =>
      expect(window.location.pathname).toBe(`/tasks/session-mining/runs/${ownID}`),
    );
  });

  it("keeps a scheduled task on its scheduled run and labels tests with their provenance", async () => {
    http.json("GET", "/api/tasks/runs", { runs: [testRunFixture(NOW), ...runsFixture(NOW)] });
    http.json("GET", `/api/tasks/runs/${TEST_RUN_ID}`, {
      run: testRunFixture(NOW),
      events: [],
      next: 0,
    } satisfies TaskRunDetail);
    open("/tasks/docs-drift");
    render(<TasksWorkspace />);

    const strip = await localStrip();
    expect(within(strip).getByText("Local changes")).toBeVisible();
    expect(within(strip).getByRole("button", { name: "Test once" })).toBeEnabled();
    expect(screen.getByRole("group", { name: "Mode" })).toBeVisible();

    const history = screen.getByRole("region", { name: "Run history" });
    const rows = await within(history).findAllByRole("button");
    expect(within(rows[0]!).getByText("Test")).toBeVisible();
    expect(within(rows[1]!).queryByText("Test")).not.toBeInTheDocument();
    // The newer test does not displace the latest scheduled run.
    expect(rows[1]).toHaveAttribute("aria-current", "true");

    fireEvent.click(rows[0]!);
    const result = await screen.findByRole("region", { name: "Result" });
    expect(within(result).getByText("Snapshot")).toBeVisible();
    expect(within(result).getByText("8c41f0e2")).toBeVisible();
    expect(within(result).getByText(SOURCE_CHECKOUT)).toBeVisible();
    expect(within(result).getByRole("button", { name: "Copy source checkout path" })).toBeVisible();
    expect(within(result).queryByText("Base commit")).not.toBeInTheDocument();
    expect(within(screen.getByRole("article")).getAllByText("Test").length).toBeGreaterThan(0);
  });

  it("explains an invalid local version, blocks its test, and keeps the scheduled details", async () => {
    open("/tasks/flaky-tests");
    render(<TasksWorkspace />);

    const strip = await localStrip();
    expect(within(strip).getByText("Invalid locally")).toBeVisible();
    expect(within(strip).getByText("every: expected a duration such as 12h")).toBeVisible();
    expect(within(strip).getByText(/Scheduled runs still use the primary ref/)).toBeVisible();
    expect(within(strip).getByRole("button", { name: "Test once" })).toBeDisabled();

    expect(screen.getByRole("group", { name: "Mode" })).toBeVisible();
    expect(screen.getByRole("region", { name: "Inbox item" })).toBeVisible();
    expect(screen.getByText("Every 12h")).toBeVisible();
  });

  it("offers Test once for an unchanged local copy, and shows a refused launch", async () => {
    http.json(
      "POST",
      "/api/tasks/tests/changelog",
      { error: "a task run is in progress in this repository; try again when it finishes" },
      409,
    );
    open("/tasks/changelog");
    render(<TasksWorkspace />);

    const strip = await localStrip();
    expect(within(strip).getByText("Unchanged locally")).toBeVisible();
    fireEvent.click(within(strip).getByRole("button", { name: "Test once" }));

    expect(await within(strip).findByRole("alert")).toHaveTextContent(
      "The test could not start: a task run is in progress in this repository",
    );
  });

  it("disables Test once while any task runs", async () => {
    const running = { ...runsFixture(NOW)[0]!, task: "dependency-updates", outcome: "" };
    http.json("GET", "/api/tasks/state", { ...stateFixture(NOW), running });
    open("/tasks/docs-drift");
    render(<TasksWorkspace />);

    const strip = await localStrip();
    await waitFor(() =>
      expect(within(strip).getByRole("button", { name: "Test once" })).toBeDisabled(),
    );
  });

  it("keeps a scheduled task deleted locally on its schedule, with no local strip", async () => {
    open("/tasks/dependency-updates");
    render(<TasksWorkspace />);

    expect(await screen.findByRole("group", { name: "Mode" })).toBeVisible();
    expect(screen.queryByRole("region", { name: "Local version" })).not.toBeInTheDocument();
  });

  it("keeps open work of a task removed from the primary ref reachable though a local copy remains", async () => {
    const state = stateFixture(NOW);

    const orphan = {
      ...state.items[0]!,
      id: "13",
      task: "session-mining",
      state: "ready" as const,
      summary: "Session mining left a branch",
    };

    http.json("GET", "/api/tasks/state", { ...state, items: [...state.items, orphan] });
    open("/tasks");
    render(<TasksWorkspace />);

    expect(await screen.findByText("Session mining left a branch")).toBeVisible();
  });
});
