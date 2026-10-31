import { useState } from "react";

import type {
  TaskItem,
  TaskItemAction,
  TaskModeChoice,
  TaskRunView,
  TaskStatus,
} from "../../api/tasks";
import { StatusMark } from "../StatusMark";
import { BackIcon } from "./TaskIcons";
import {
  formatDateTime,
  formatDue,
  formatDuration,
  formatTokens,
  itemStatePresentation,
  MODE_LABELS,
  outcomePresentation,
} from "./taskFormat";

const MODE_CHOICES: TaskModeChoice[] = ["default", "auto", "notify", "off"];

const SNOOZES = [
  ["1 hour", 60 * 60_000],
  ["1 day", 24 * 60 * 60_000],
  ["1 week", 7 * 24 * 60 * 60_000],
] as const;

export type ItemCommand = { kind: "run" } | { kind: TaskItemAction; until?: string };

function ModeSelector({
  task,
  pending,
  onChange,
}: {
  task: TaskStatus;
  pending: boolean;
  onChange: (mode: TaskModeChoice) => void;
}) {
  const chosen: TaskModeChoice = task.modeSource === "override" ? task.mode : "default";

  return (
    <div className="segmented" role="group" aria-label="Mode">
      {MODE_CHOICES.map((mode) => (
        <button
          key={mode}
          type="button"
          aria-pressed={chosen === mode}
          disabled={pending}
          title={
            mode === "default"
              ? "Use the repository's mode for this task"
              : `Override this task's mode with ${MODE_LABELS[mode]}`
          }
          onClick={() => chosen !== mode && onChange(mode)}
        >
          {mode === "default"
            ? `Default${chosen === "default" ? ` · ${MODE_LABELS[task.mode]}` : ""}`
            : MODE_LABELS[mode]}
        </button>
      ))}
    </div>
  );
}

function ItemActions({
  item,
  busy,
  onCommand,
}: {
  item: TaskItem;
  busy: boolean;
  onCommand: (command: ItemCommand) => void;
}) {
  const [snoozing, setSnoozing] = useState(false);
  const task = item.kind === "task";
  const canRun = task && (item.state === "due" || item.state === "needs-attention");
  const active = item.state !== "done" && item.state !== "dismissed";

  if (snoozing) {
    return (
      <div className="task-actions" role="group" aria-label="Snooze for">
        {SNOOZES.map(([label, ms]) => (
          <button
            key={label}
            type="button"
            className="task-button"
            disabled={busy}
            onClick={() => {
              setSnoozing(false);
              onCommand({ kind: "snooze", until: new Date(Date.now() + ms).toISOString() });
            }}
          >
            {label}
          </button>
        ))}
        <button type="button" className="task-ghost-button" onClick={() => setSnoozing(false)}>
          Cancel
        </button>
      </div>
    );
  }

  return (
    <div className="task-actions">
      {canRun ? (
        <button
          type="button"
          className="task-button task-button--primary"
          disabled={busy}
          onClick={() => onCommand({ kind: "run" })}
        >
          Run
        </button>
      ) : null}
      {active ? (
        <button
          type="button"
          className="task-button"
          disabled={busy}
          onClick={() => setSnoozing(true)}
        >
          Snooze…
        </button>
      ) : null}
      {item.state === "due" ? (
        <button
          type="button"
          className="task-button"
          disabled={busy}
          onClick={() => onCommand({ kind: "skip" })}
        >
          Skip
        </button>
      ) : null}
      {item.state === "ready" || item.state === "needs-attention" ? (
        <button
          type="button"
          className="task-button"
          disabled={busy}
          title="Remove the worktree and local branch, then dismiss the item"
          onClick={() => onCommand({ kind: "discard" })}
        >
          Discard
        </button>
      ) : null}
      {active ? (
        <button
          type="button"
          className="task-button"
          disabled={busy}
          onClick={() => onCommand({ kind: "done" })}
        >
          Done
        </button>
      ) : null}
    </div>
  );
}

export function TaskItemStrip({
  item,
  busy,
  actionError,
  launching,
  onCommand,
}: {
  item: TaskItem;
  busy: boolean;
  actionError?: string;
  launching: boolean;
  onCommand: (command: ItemCommand) => void;
}) {
  const { label, tone } = itemStatePresentation(item.state);
  const snoozed = item.snoozedUntil && new Date(item.snoozedUntil).getTime() > Date.now();

  return (
    <section className="task-item" aria-label="Inbox item">
      <div className="task-item__line">
        <StatusMark tone={tone} label={label} />
        {snoozed ? (
          <span className="task-item__aside">
            Snoozed until {formatDateTime(item.snoozedUntil)}
          </span>
        ) : null}
        <span className="task-item__summary">{item.summary || item.message || ""}</span>
        <ItemActions item={item} busy={busy || launching} onCommand={onCommand} />
      </div>
      {launching ? (
        <p className="task-item__note" role="status">
          Starting the run…
        </p>
      ) : null}
      {item.launchError ? (
        <p className="task-inline-error" role="alert">
          The run could not start: {item.launchError}
        </p>
      ) : null}
      {actionError ? (
        <p className="task-inline-error" role="alert">
          {actionError}
        </p>
      ) : null}
      {item.reason ? <p className="task-item__note">{item.reason}</p> : null}
    </section>
  );
}

function HistoryRow({
  run,
  selected,
  onSelect,
}: {
  run: TaskRunView;
  selected: boolean;
  onSelect: () => void;
}) {
  const { label, tone } = outcomePresentation(run.outcome);
  const quiet = run.outcome === "not-needed";
  const duration = run.outcome === "running" ? "" : formatDuration(run.startedAt, run.at);

  return (
    <li>
      <button
        type="button"
        className={`task-history__row${quiet ? " is-quiet" : ""}${selected ? " is-selected" : ""}`}
        aria-current={selected ? "true" : undefined}
        onClick={onSelect}
      >
        <StatusMark tone={tone} label={label} />
        <time className="task-history__time" dateTime={run.startedAt || run.at}>
          {formatDateTime(run.startedAt || run.at)}
        </time>
        {!quiet ? (
          <span className="task-history__facts">
            {duration ? <span>{duration}</span> : null}
            {run.tokens ? <span>{formatTokens(run.tokens)} tok</span> : null}
            {run.changed?.length ? (
              <span>
                {run.changed.length} {run.changed.length === 1 ? "file" : "files"}
              </span>
            ) : null}
          </span>
        ) : null}
      </button>
    </li>
  );
}

export function TaskHeader({
  task,
  modePending,
  onMode,
  onBack,
}: {
  task: TaskStatus;
  modePending: boolean;
  onMode: (mode: TaskModeChoice) => void;
  onBack: () => void;
}) {
  return (
    <header className="task-head">
      <button type="button" className="task-back task-back--tasks" onClick={onBack}>
        <BackIcon />
        Tasks
      </button>
      <div className="task-head__title">
        <h2>{task.name || task.id}</h2>
        <p>
          <code>{task.id}</code>
          <span>Every {task.every}</span>
          <span>{task.scope === "team" ? "Team clock" : "Personal clock"}</span>
          <span>{formatDue(task.nextDue, task.due)}</span>
        </p>
      </div>
      <ModeSelector task={task} pending={modePending} onChange={onMode} />
    </header>
  );
}

export function TaskHistory({
  runs,
  loading,
  error,
  selectedID,
  onSelect,
  onRetry,
  task,
}: {
  runs: TaskRunView[];
  loading: boolean;
  error: unknown;
  selectedID?: string;
  onSelect: (id: string) => void;
  onRetry: () => void;
  task: TaskStatus;
}) {
  return (
    <section className="task-history" aria-label="Run history">
      <h3 className="task-history__head">
        History <span>{runs.length || ""}</span>
      </h3>
      {loading ? (
        <div className="task-skeleton" role="status" aria-label="Loading runs">
          <span />
          <span />
          <span />
        </div>
      ) : error ? (
        <p className="task-inline-error" role="alert">
          Could not load runs. {error instanceof Error ? error.message : String(error)}{" "}
          <button type="button" className="task-ghost-button" onClick={onRetry}>
            Retry
          </button>
        </p>
      ) : runs.length === 0 ? (
        <p className="task-empty">
          No runs yet. The first run is recorded when this task is next due (
          {formatDue(task.nextDue, task.due).toLowerCase()}).
        </p>
      ) : (
        <ol className="task-history__list">
          {runs.map((run, index) => (
            <HistoryRow
              key={run.id || `${run.at}-${index}`}
              run={run}
              selected={Boolean(run.id) && run.id === selectedID}
              onSelect={() => run.id && onSelect(run.id)}
            />
          ))}
        </ol>
      )}
    </section>
  );
}
