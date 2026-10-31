import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { queryKeys } from "../../api/queryKeys";
import {
  actOnTaskItem,
  runTaskItem,
  setTaskMode,
  setTasksPaused,
  type TaskItem,
  type TaskModeChoice,
  type TaskRunView,
  type TasksLiveState,
  type TaskStatus,
} from "../../api/tasks";
import { useLocationSnapshot } from "../locationStore";
import { WorkbenchSidebar } from "../WorkbenchSidebar";
import { TaskHeader, TaskHistory, TaskItemStrip, type ItemCommand } from "./TaskDetail";
import { formatDue, itemStatePresentation } from "./taskFormat";
import { TaskRunView as RunView } from "./TaskRunView";
import { PauseButton, TasksNotices, TasksOverview } from "./TasksOverview";
import { navigateTasks, parseTasksLocation } from "./tasksRoute";
import { useTaskRuns, useTasksLiveState, useTasksReport } from "./useTasksData";

/** The task's open item, preferring live state, which carries launch errors. */
function currentItem(task: TaskStatus, state?: TasksLiveState): TaskItem | undefined {
  if (!state) return task.item;

  const open = state.items.filter(
    (item) =>
      item.task === task.id &&
      item.kind === "task" &&
      item.state !== "done" &&
      item.state !== "dismissed",
  );

  return open.find((item) => item.state !== "needs-attention") ?? open[0];
}

function withRunning(runs: TaskRunView[], task: string, state?: TasksLiveState): TaskRunView[] {
  const running = state?.running;

  if (!running?.id || running.task !== task || runs.some((run) => run.id === running.id))
    return runs;

  return [
    {
      ...running,
      outcome: state?.stale ? "interrupted" : "running",
      hasTranscript: running.stage === "agent",
      hasCheckLog: true,
    },
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
    >
      <span className="status-mark__shape" />
    </span>
  );
}

function TasksRail({
  tasks,
  selected,
  paused,
  pausePending,
  onTogglePause,
}: {
  tasks: TaskStatus[];
  selected?: string;
  paused: boolean;
  pausePending: boolean;
  onTogglePause: () => void;
}) {
  return (
    <nav className="tasks-rail" aria-label="Tasks">
      <div className="tasks-rail__head">
        <h1>Tasks</h1>
        <PauseButton
          className="tasks-rail__pause"
          paused={paused}
          pending={pausePending}
          onToggle={onTogglePause}
        />
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
              <RailMark item={task.item} />
              <span className="tasks-rail__aside">{formatDue(task.nextDue, task.due)}</span>
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
  const [pending, setPending] = useState<"" | "item" | "mode" | "pause">("");
  // Which action failed: an item ID, "mode", or "pause".
  const [actionError, setActionError] = useState<{ scope: string; text: string } | null>(null);

  const report = reportQuery.data;
  const state = live.state;
  const paused = state?.paused ?? report?.paused ?? false;
  const task = report?.tasks.find((candidate) => candidate.id === location.task);

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.tasks.report() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.tasks.state() });
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
        live.awaitLaunch(item.task, item.id, launchedAt);
      } else {
        await actOnTaskItem(item.id, command.kind, command.until);
      }
    });

  const pageError =
    actionError?.scope === "mode" || actionError?.scope === "pause" ? (
      <p className="task-inline-error task-page-error" role="alert">
        {actionError.scope === "mode"
          ? "Could not change the mode: "
          : "Could not change pausing: "}
        {actionError.text}
      </p>
    ) : null;

  const rail = (
    <WorkbenchSidebar title={sidebarTitle} titleHint={sidebarHint}>
      <TasksRail
        tasks={report?.tasks ?? []}
        selected={location.task}
        paused={paused}
        pausePending={pending === "pause"}
        onTogglePause={togglePause}
      />
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
      />
    );
  } else if (!task) {
    body = (
      <div className="task-empty task-empty--page">
        <h3>No task named “{location.task}”</h3>
        <p>It is not registered on the primary ref. It may have been renamed or removed.</p>
        <button type="button" className="task-button" onClick={() => navigateTasks({})}>
          All tasks
        </button>
      </div>
    );
  } else {
    const item = currentItem(task, state);
    const runs = withRunning(runsQuery.data ?? [], task.id, state);
    const fallbackRun = runs.find((run) => run.outcome !== "not-needed") ?? runs[0];
    const selectedID = location.run ?? fallbackRun?.id;
    const selectedRun = runs.find((run) => run.id === selectedID);
    const running = state?.running?.id === selectedID ? state?.running : undefined;

    body = (
      <>
        <TaskHeader
          task={task}
          modePending={pending === "mode"}
          onMode={changeMode}
          onBack={() => navigateTasks({})}
        />
        {pageError}
        <TasksNotices
          report={{ ...report, paused }}
          brief
          pausePending={pending === "pause"}
          onResume={resume}
        />
        {item ? (
          <TaskItemStrip
            item={item}
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
