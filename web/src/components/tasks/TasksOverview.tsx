import { type ReactNode, useState } from "react";

import type { TaskItem, TasksReport, TaskStatus } from "../../api/tasks";
import { StatusMark } from "../StatusMark";
import { PauseIcon, PlayIcon } from "./TaskIcons";
import { navigateTasks, tasksHref } from "./tasksRoute";
import {
  formatDue,
  formatEvery,
  formatRelative,
  itemStatePresentation,
  localStatePresentation,
  MODE_LABELS,
  outcomePresentation,
  shortCommit,
} from "./taskFormat";

export function PauseButton({
  paused,
  pending,
  onToggle,
  className = "button",
}: {
  paused: boolean;
  pending: boolean;
  onToggle: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      className={className}
      disabled={pending}
      aria-pressed={paused}
      title={
        paused ? "Resume background work for every task" : "Pause background work for every task"
      }
      onClick={onToggle}
    >
      {paused ? <PlayIcon /> : <PauseIcon />}
      {paused ? "Resume all" : "Pause all"}
    </button>
  );
}

/** Repository-wide conditions that stop or limit runs. */
export function TasksNotices({
  report,
  brief = false,
  pausePending,
  onResume,
}: {
  report: TasksReport;
  /** Only the conditions that stop runs, as on a task's page. */
  brief?: boolean;
  pausePending: boolean;
  onResume: () => void;
}) {
  return (
    <>
      {report.untrusted ? (
        <div className="banner banner--risk task-notice" role="alert">
          <StatusMark tone="risk" label="Not trusted" />
          {/* The reason already says what to run; it starts lowercase. */}
          <p className="banner__text task-notice__sentence">{report.untrusted}</p>
        </div>
      ) : null}
      {report.refError ? (
        <div className="banner banner--risk task-notice" role="alert">
          <StatusMark tone="risk" label="Primary ref unavailable" />
          <p className="banner__text">
            No task runs until the primary ref resolves. {report.refError}
          </p>
        </div>
      ) : null}
      {report.paused ? (
        <div className="banner task-notice" role="status">
          <StatusMark tone="muted" label="Paused" />
          <p className="banner__text">
            Background work is paused. Due tasks wait until you resume.
          </p>
          <button type="button" className="button" disabled={pausePending} onClick={onResume}>
            <PlayIcon />
            Resume all
          </button>
        </div>
      ) : null}
      {report.lastPassError && !brief ? (
        <div className="banner task-notice" role="status">
          <StatusMark tone="warning" label="Last pass failed" />
          <p className="banner__text">
            {report.lastPassError.message}{" "}
            <span className="banner__aside">{formatRelative(report.lastPassError.at)}</span>
          </p>
        </div>
      ) : null}
      {report.mergeWarning && !brief ? (
        <div className="banner task-notice" role="status">
          <StatusMark tone="info" label="Merge setting" />
          <p className="banner__text">{report.mergeWarning}</p>
        </div>
      ) : null}
    </>
  );
}

function RunLink({ task }: { task: TaskStatus }) {
  if (!task.lastRun) return null;

  return (
    <a
      className="task-table__run-link"
      href={tasksHref({ task: task.id, run: task.lastRun.id })}
      onClick={(event) => {
        event.preventDefault();
        navigateTasks({ task: task.id, run: task.lastRun?.id });
      }}
    >
      View run
    </a>
  );
}

/** A failed last run leads the page, above any notice about the task. */
function FailureBanner({ task }: { task: TaskStatus }) {
  return (
    <div className="banner banner--risk task-notice" role="status">
      <StatusMark tone="risk" label="Failed" />
      <p className="banner__text">
        {task.name || task.id}{" "}
        <span className="banner__aside">{formatRelative(task.lastRun?.at)}</span>
      </p>
      <RunLink task={task} />
    </div>
  );
}

// The runner writes 'New task "Name" runs every 7d in notify mode. <purpose>'.
const ANNOUNCEMENT_PREFIX = /^New task ".*?" runs every \S+ in \S+ mode\.\s*/;

/** "New: Name. Runs every 7 days in Notify mode.", with the purpose on request. */
function AnnouncementText({ item, task }: { item: TaskItem; task?: TaskStatus }) {
  const [open, setOpen] = useState(false);
  const summary = item.summary || item.reason || item.message || "";

  if (!task || !ANNOUNCEMENT_PREFIX.test(summary)) return <p className="banner__text">{summary}</p>;

  const purpose = summary.replace(ANNOUNCEMENT_PREFIX, "");

  return (
    <div className="banner__text">
      {task.name || task.id}. Runs every {formatEvery(task.every)} in{" "}
      {MODE_LABELS[task.mode] ?? task.mode} mode.
      {purpose ? (
        <>
          {" "}
          <button
            type="button"
            className="task-notice__more"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
          >
            {open ? "Less" : "What it does"}
          </button>
          {open ? <p className="task-notice__purpose">{purpose}</p> : null}
        </>
      ) : null}
    </div>
  );
}

function TaskRow({ task, onOpen }: { task: TaskStatus; onOpen: () => void }) {
  const last = task.lastRun ? outcomePresentation(task.lastRun.outcome) : null;
  const item = task.item ? itemStatePresentation(task.item.state) : null;

  // A local copy that matches the primary ref needs no mark.
  const local =
    task.local && task.local.state !== "same" ? localStatePresentation(task.local.state) : null;

  return (
    <tr>
      <th scope="row">
        <button type="button" className="task-table__open" onClick={onOpen}>
          <span className="link-quiet">{task.name || task.id}</span>
          {task.name ? <code>{task.id}</code> : null}
        </button>
        {local ? (
          <span className="task-table__local">
            <StatusMark tone={local.tone} label={local.label} />
          </span>
        ) : null}
      </th>
      <td>
        {task.scheduled ? (
          <>
            {MODE_LABELS[task.mode] ?? task.mode}
            {task.modeSource === "override" ? null : (
              <span className="task-table__aside"> · default</span>
            )}
          </>
        ) : (
          <span className="task-table__none">—</span>
        )}
      </td>
      <td className="task-table__time is-optional">{task.every}</td>
      <td>
        {last && task.lastRun ? (
          <span className="task-table__last">
            <StatusMark tone={last.tone} label={last.label} />
            <span className="task-table__aside">{formatRelative(task.lastRun.at)}</span>
            {last.tone === "risk" ? <RunLink task={task} /> : null}
          </span>
        ) : (
          <span className="task-table__none">Never run</span>
        )}
      </td>
      <td className={task.due ? "task-table__due is-due" : "task-table__due"}>
        <time dateTime={task.nextDue}>{formatDue(task.nextDue, task.due)}</time>
      </td>
      <td>
        {item ? (
          <StatusMark tone={item.tone} label={item.label} />
        ) : (
          <span className="task-table__none">—</span>
        )}
      </td>
    </tr>
  );
}

export function TasksOverview({
  report,
  onOpen,
  pausePending,
  onTogglePause,
  onResume,
  error,
  announcements = [],
  onDismiss,
  onDiscard,
  itemError,
}: {
  report: TasksReport;
  onOpen: (task: string) => void;
  pausePending: boolean;
  onTogglePause: () => void;
  onResume: () => void;
  error?: ReactNode;
  /**
   * New-task notices and open items of tasks no longer registered; both count
   * toward the desktop badge until closed.
   */
  announcements?: TaskItem[];
  onDismiss?: (item: TaskItem) => void;
  onDiscard?: (item: TaskItem) => void;
  /** The last failed action; shown beside the item it names. */
  itemError?: { scope: string; text: string } | null;
}) {
  return (
    <div className="tasks-overview">
      <header className="view-header">
        <div className="view-header__title-row">
          <h2>Tasks</h2>
          <span className="view-header__count">{report.tasks.length}</span>
          <span className="view-header__about">
            {report.ref ? (
              <code title={report.commit}>
                {report.ref}
                {report.commit ? ` @ ${shortCommit(report.commit)}` : ""}
              </code>
            ) : null}
            {report.ref && report.fetchedAt ? " · " : null}
            {report.fetchedAt ? <>Fetched {formatRelative(report.fetchedAt)}</> : null}
          </span>
          <div className="view-header__actions">
            <PauseButton paused={report.paused} pending={pausePending} onToggle={onTogglePause} />
          </div>
        </div>
      </header>
      <div className="tasks-overview__body">
        {error}
        {report.tasks
          .filter((task) => task.lastRun?.outcome === "failed")
          .map((task) => (
            <FailureBanner key={task.id} task={task} />
          ))}
        <TasksNotices report={report} pausePending={pausePending} onResume={onResume} />
        {announcements.map((item) => (
          <div key={item.id} className="banner task-notice" role="status">
            {item.kind === "announcement" ? (
              <StatusMark tone="info" label="New:" />
            ) : (
              <StatusMark tone="warning" label={`Removed task ${item.task}`} />
            )}
            {item.kind === "announcement" ? (
              <AnnouncementText
                item={item}
                task={report.tasks.find((candidate) => candidate.id === item.task)}
              />
            ) : (
              <p className="banner__text">{item.summary || item.reason || item.message}</p>
            )}
            {item.kind === "task" && item.worktree && onDiscard ? (
              <button type="button" className="button" onClick={() => onDiscard(item)}>
                Discard
              </button>
            ) : null}
            {onDismiss ? (
              <button type="button" className="button" onClick={() => onDismiss(item)}>
                {item.kind === "announcement" ? "Dismiss" : "Done"}
              </button>
            ) : null}
            {itemError?.scope === item.id ? (
              <p className="task-inline-error" role="alert">
                {itemError.text}
              </p>
            ) : null}
          </div>
        ))}
        {report.problems?.length ? (
          <div className="banner banner--risk task-notice" role="alert">
            <StatusMark tone="risk" label={`${report.problems.length} task files did not load`} />
            <ul className="banner__text">
              {report.problems.map((problem, index) => (
                <li key={`${problem.path}-${index}`}>
                  <code>{problem.path}</code> {problem.message}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        {report.tasks.length === 0 ? (
          <div className="task-empty task-empty--page">
            <h3>No tasks are registered</h3>
            <p>
              A task is a YAML file in <code>.rhizome/tasks/</code>. Rhizome schedules them from the
              primary ref
              {report.ref ? (
                <>
                  {" "}
                  (<code>{report.ref}</code>)
                </>
              ) : null}
              . A task added in this checkout appears here to test before it is merged.
            </p>
          </div>
        ) : (
          <table className="task-table">
            <thead>
              <tr>
                <th scope="col">Task</th>
                <th scope="col">Mode</th>
                <th scope="col" className="is-optional">
                  Every
                </th>
                <th scope="col">Last run</th>
                <th scope="col">Next due</th>
                <th scope="col">Inbox</th>
              </tr>
            </thead>
            <tbody>
              {report.tasks.map((task) => (
                <TaskRow key={task.id} task={task} onOpen={() => onOpen(task.id)} />
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
