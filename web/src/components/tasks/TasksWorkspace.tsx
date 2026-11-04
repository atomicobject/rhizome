import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { queryKeys } from "../../api/queryKeys";
import {
  actOnTaskItem,
  runTask,
  runTaskItem,
  setTaskMode,
  setTasksPaused,
  testTask,
  type TaskItem,
  type TaskModeChoice,
  type TaskRunView,
  type TasksLiveState,
  type TaskStatus,
} from "../../api/tasks";
import { useLocationSnapshot } from "../locationStore";
import { StatusMark } from "../StatusMark";
import { WorkbenchSidebar } from "../WorkbenchSidebar";
import {
  TaskHeader,
  TaskHistory,
  TaskItemStrip,
  TaskLocalStrip,
  type ItemCommand,
} from "./TaskDetail";
import { formatDue, itemStatePresentation } from "./taskFormat";
import { TaskRunView as RunView } from "./TaskRunView";
import { TasksNotices, TasksOverview } from "./TasksOverview";
import { navigateTasks, parseTasksLocation } from "./tasksRoute";
import { useTaskRuns, useTasksLiveState, useTasksReport } from "./useTasksData";

function openItems(task: TaskStatus, state?: TasksLiveState): TaskItem[] {
  return (state?.items ?? []).filter(
    (item) =>
      item.task === task.id &&
      item.kind === "task" &&
      item.state !== "done" &&
      item.state !== "dismissed",
  );
}

/** The task's open item, preferring live state, which carries launch errors. */
function currentItem(task: TaskStatus, state?: TasksLiveState): TaskItem | undefined {
  if (!state) return task.item;

  const open = openItems(task, state);

  return open.find((item) => item.state !== "needs-attention") ?? open[0];
}

function withRunning(runs: TaskRunView[], task: string, state?: TasksLiveState): TaskRunView[] {
  const running = state?.running;

  if (!running?.id || running.task !== task) return runs;
  const outcome = state?.stale ? "interrupted" : "running";

  // The runs endpoint lists the run in progress too, with no outcome yet.
  if (runs.some((run) => run.id === running.id))
    return runs.map((run) => (run.id === running.id && !run.outcome ? { ...run, outcome } : run));

  return [
    { ...running, outcome, hasTranscript: running.stage === "agent", hasCheckLog: true },
    ...runs,
  ];
}

function RailMark({ item }: { item?: TaskItem }) {
  if (!item) return null;
  const { label, tone } = itemStatePresentation(item.state);

  return (
    <span
      className={`status-mark status-mark--${tone} tasks-rail__mark`}
      role="img"
      aria-label={label}
      title={label}
    >
      <span className="status-mark__shape" />
    </span>
  );
}

function TasksRail({ tasks, selected }: { tasks: TaskStatus[]; selected?: string }) {
  return (
    <nav className="tasks-rail" aria-label="Tasks">
      {/* Pause all lives in the overview header only. */}
      <div className="tasks-rail__head">
        <h1>Tasks</h1>
      </div>
      <ul className="tasks-rail__list">
        <li>
          <button
            type="button"
            className="tasks-rail__row"
            aria-current={selected ? undefined : "page"}
            onClick={() => navigateTasks({})}
          >
            <span className="tasks-rail__name">All tasks</span>
            <span className="tasks-rail__aside">{tasks.length}</span>
          </button>
        </li>
        {tasks.map((task) => (
          <li key={task.id}>
            <button
              type="button"
              className="tasks-rail__row"
              aria-current={selected === task.id ? "page" : undefined}
              onClick={() => navigateTasks({ task: task.id })}
            >
              <span className="tasks-rail__name">{task.name || task.id}</span>
              {/* A failure is named in words, not only by the red mark; the
                  due time it displaces is on the task's row and page. */}
              {task.lastRun?.outcome === "failed" ? (
                <span className="tasks-rail__failed">
                  <StatusMark tone="risk" label="Failed" />
                </span>
              ) : (
                <>
                  <RailMark item={task.item} />
                  <span className="tasks-rail__aside">
                    {task.scheduled ? formatDue(task.nextDue, task.due) : "Local"}
                  </span>
                </>
              )}
            </button>
          </li>
        ))}
      </ul>
    </nav>
  );
}

export function TasksWorkspace({
  sidebarTitle,
  sidebarHint,
}: {
  sidebarTitle?: string | null;
  sidebarHint?: string | null;
}) {
  const snapshot = useLocationSnapshot();
  const location = parseTasksLocation(snapshot.pathname);
  const queryClient = useQueryClient();
  const reportQuery = useTasksReport();
  const live = useTasksLiveState();
  const runsQuery = useTaskRuns(location.task);
  const [pending, setPending] = useState<"" | "item" | "mode" | "pause" | "run" | "test">("");
  // Which action failed: an item ID, "mode", "pause", "run", or "test:<task>".
  const [actionError, setActionError] = useState<{ scope: string; text: string } | null>(null);

  const report = reportQuery.data;
  const state = live.state;
  const paused = state?.paused ?? report?.paused ?? false;
  const task = report?.tasks.find((candidate) => candidate.id === location.task);

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.tasks.report() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.tasks.state() });
    // Skip and Done can add records that carry no run id, which the live
    // state's change check cannot see.
    void queryClient.invalidateQueries({ queryKey: [...queryKeys.tasks.all(), "runs"] });
  };

  const perform = async (kind: typeof pending, scope: string, action: () => Promise<void>) => {
    setPending(kind);
    setActionError(null);

    try {
      await action();
    } catch (error) {
      setActionError({ scope, text: error instanceof Error ? error.message : String(error) });
    } finally {
      setPending("");
      refresh();
    }
  };

  const pauseAll = (next: boolean) =>
    void perform("pause", "pause", async () => {
      await setTasksPaused(next);
    });

  const togglePause = () => pauseAll(!paused);
  const resume = () => pauseAll(false);

  const changeMode = (mode: TaskModeChoice) =>
    task &&
    void perform("mode", "mode", async () => {
      await setTaskMode(task.id, mode);
    });

  const runCommand = (item: TaskItem, command: ItemCommand) =>
    void perform("item", item.id, async () => {
      if (command.kind === "run") {
        const { launchedAt } = await runTaskItem(item.id);
        live.awaitLaunch({ task: item.task, item: item.id, launchedAt });
      } else {
        await actOnTaskItem(item.id, command.kind, command.until);
      }
    });

  const runNow = (id: string) =>
    void perform("run", "run", async () => {
      const { launchedAt, item } = await runTask(id);
      live.awaitLaunch({ task: id, item, launchedAt });
    });

  const runTest = (id: string) =>
    void perform("test", `test:${id}`, async () => {
      const { launchedAt, sourceCheckout } = await testTask(id);
      live.awaitLaunch({
        task: id,
        test: true,
        sourceCheckout,
        launchedAt,
        // Open the test once it starts, unless the person has moved on.
        onStarted: (run) => {
          if (parseTasksLocation(window.location.pathname).task === id)
            navigateTasks({ task: id, run });
        },
      });
    });

  const pageError =
    actionError?.scope === "mode" ||
    actionError?.scope === "pause" ||
    actionError?.scope === "run" ? (
      <p className="task-inline-error task-page-error" role="alert">
        {actionError.scope === "mode"
          ? "Could not change the mode: "
          : actionError.scope === "run"
            ? "Could not run the task: "
            : "Could not change pausing: "}
        {actionError.text}
      </p>
    ) : null;

  const rail = (
    <WorkbenchSidebar title={sidebarTitle} titleHint={sidebarHint}>
      <TasksRail tasks={report?.tasks ?? []} selected={location.task} />
    </WorkbenchSidebar>
  );

  let body;

  if (reportQuery.isPending) {
    body = (
      <div className="task-skeleton task-skeleton--page" role="status" aria-label="Loading tasks">
        <span />
        <span />
        <span />
        <span />
      </div>
    );
  } else if (reportQuery.isError || !report) {
    body = (
      <div className="task-empty task-empty--page" role="alert">
        <h3>Could not load tasks</h3>
        <p>{reportQuery.error?.message}</p>
        <button type="button" className="task-button" onClick={() => void reportQuery.refetch()}>
          Retry
        </button>
      </div>
    );
  } else if (!location.task) {
    body = (
      <TasksOverview
        error={pageError}
        report={{ ...report, paused }}
        onOpen={(id) => navigateTasks({ task: id })}
        pausePending={pending === "pause"}
        onTogglePause={togglePause}
        onResume={resume}
        announcements={(state?.items ?? []).filter(
          (item) =>
            (item.kind === "announcement" && item.state === "due") ||
            // Open work of a task no longer on the primary ref stays reachable.
            (item.kind === "task" &&
              item.state !== "done" &&
              item.state !== "dismissed" &&
              !report.tasks.some(
                (registered) => registered.id === item.task && registered.scheduled,
              )),
        )}
        onDismiss={(item) => runCommand(item, { kind: "done" })}
        onDiscard={(item) => runCommand(item, { kind: "discard" })}
        itemError={actionError}
      />
    );
  } else if (!task) {
    body = (
      <div className="task-empty task-empty--page">
        <h3>No task named “{location.task}”</h3>
        <p>
          It is not on the primary ref or in this checkout. It may have been renamed or removed.
        </p>
        <button type="button" className="task-button" onClick={() => navigateTasks({})}>
          All tasks
        </button>
      </div>
    );
  } else {
    const runs = withRunning(runsQuery.data ?? [], task.id, state);
    // A scheduled task opens on its latest scheduled run; tests stay a click away.
    const scheduledRuns = task.scheduled ? runs.filter((run) => !run.test) : runs;

    const latest = (list: TaskRunView[]) =>
      list.find((run) => run.outcome !== "not-needed") ?? list[0];

    const fallbackRun = latest(scheduledRuns) ?? latest(runs);
    const selectedID = location.run ?? fallbackRun?.id;
    const selectedRun = runs.find((run) => run.id === selectedID);

    // A retry gives the new attempt its own item; the selected run's item,
    // while open, keeps its actions reachable.
    const item =
      openItems(task, state).find((open) => open.id === selectedRun?.item) ??
      currentItem(task, state);

    const running = state?.running?.id === selectedID ? state?.running : undefined;
    const runActive = Boolean(state?.running) && !state?.stale;

    body = (
      <>
        <TaskHeader
          task={task}
          modePending={pending === "mode"}
          onMode={changeMode}
          onBack={() => navigateTasks({})}
          onRun={task.scheduled && !task.checkOnly && !item ? () => runNow(task.id) : undefined}
          runBlocked={
            pending === "run" || (live.watch?.task === task.id && !live.watch.test)
              ? "Starting the run…"
              : runActive
                ? "A task run is in progress; run when it finishes"
                : undefined
          }
        />
        {pageError}
        <TasksNotices
          report={{ ...report, paused }}
          brief
          pausePending={pending === "pause"}
          onResume={resume}
        />
        <TaskLocalStrip
          task={task}
          blocked={runActive ? "A task run is in progress; test when it finishes" : undefined}
          launching={pending === "test" || Boolean(live.watch?.test && live.watch.task === task.id)}
          actionError={actionError?.scope === `test:${task.id}` ? actionError.text : undefined}
          onTest={() => runTest(task.id)}
        />
        {item && task.scheduled ? (
          <TaskItemStrip
            key={item.id}
            item={item}
            checkOnly={task.checkOnly}
            busy={pending === "item"}
            launching={live.watch?.item === item.id}
            actionError={actionError?.scope === item.id ? actionError.text : undefined}
            onCommand={(command) => runCommand(item, command)}
          />
        ) : null}
        <div className="tasks-split" data-run-open={location.run ? "true" : "false"}>
          <TaskHistory
            task={task}
            runs={runs}
            loading={runsQuery.isPending}
            error={runsQuery.error}
            selectedID={selectedID}
            onSelect={(id) => navigateTasks({ task: task.id, run: id })}
            onRetry={() => void runsQuery.refetch()}
          />
          {selectedID ? (
            <RunView
              key={selectedID}
              runID={selectedID}
              fallback={selectedRun}
              live={Boolean(running) && !state?.stale}
              interrupted={Boolean(running) && Boolean(state?.stale)}
              onBack={() => navigateTasks({ task: task.id })}
            />
          ) : (
            <div className="task-run task-run--empty">
              <p className="task-empty">Pick a run to read its result and transcript.</p>
            </div>
          )}
        </div>
      </>
    );
  }

  return (
    <main className="workspace workbench tasks-workspace">
      {rail}
      <section className="workbench__panel tasks-panel" aria-label="Tasks">
        {body}
      </section>
    </main>
  );
}
