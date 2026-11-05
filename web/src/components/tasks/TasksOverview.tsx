import { RelativeTime } from "../../../kit/format";
import { type ReactNode, useState } from "react";

import type { TaskItem, TasksReport, TaskStatus } from "../../api/tasks";
import { DisclosureChevron } from "../DisclosureChevron";
import { StatusMark } from "../StatusMark";
import { PauseIcon, PlayIcon } from "./TaskIcons";
import { navigateTasks, tasksHref } from "./tasksRoute";
import {
  formatAgo,
  formatDue,
  formatEvery,
  formatRelative,
  itemStatePresentation,
  localStatePresentation,
  MODE_LABELS,
  outcomePresentation,
  shortCommit,
} from "./taskFormat";

/** The header's one plain line: when work next happens. */
function scheduleSummary(report: TasksReport): string {
  if (report.paused) return "Paused";
  const scheduled = report.tasks.filter((task) => task.scheduled);

  if (scheduled.some((task) => task.due)) return "A task is due now";

  const next = scheduled
    .flatMap((task) => (task.nextDue ? [task.nextDue] : []))
    .sort((a, b) => new Date(a).getTime() - new Date(b).getTime())[0];

  return next ? `Next run ${formatRelative(next)}` : "";
}

/** Where tasks are read from, behind Details: diagnostics, not the page's message. */
function SourceDetails({ report }: { report: TasksReport }) {
  if (!report.ref && !report.fetchedAt) return null;

  // Where each scheduled task's mode comes from: its file's default, or set here.
  const modes = report.tasks
    .filter((task) => task.scheduled)
    .map(
      (task) =>
        `${task.name || task.id}: ${task.modeSource === "override" ? "set here" : "task default"}`,
    );

  return (
    <details className="header-details">
      <summary className="button button--ghost">
        Details <DisclosureChevron />
      </summary>
      <dl>
        {report.ref ? (
          <>
            <dt>Primary ref</dt>
            <dd>
              <code>{report.ref}</code>
            </dd>
          </>
        ) : null}
        {report.commit ? (
          <>
            <dt>Commit</dt>
            <dd>
              <code title={report.commit}>{shortCommit(report.commit)}</code>
            </dd>
          </>
        ) : null}
        {report.fetchedAt ? (
          <>
            <dt>Fetched</dt>
            <dd className="num">{formatAgo(report.fetchedAt)}</dd>
          </>
        ) : null}
        {modes.length ? (
          <>
            <dt>Mode</dt>
            <dd>{modes.join(", ")}</dd>
          </>
        ) : null}
      </dl>
    </details>
  );
}

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

/** A quiet link to a task's page, or to one of its runs. */
function TaskLink({ task, run, label }: { task: TaskStatus; run?: string; label: ReactNode }) {
  return (
    <a
      className="link-quiet task-table__run-link"
      href={tasksHref({ task: task.id, run })}
      onClick={(event) => {
        event.preventDefault();
        navigateTasks({ task: task.id, run });
      }}
    >
      {label}
    </a>
  );
}

/**
 * Two or more failed last runs lead the page as one aggregate alert; a single
 * failure is told by its row, which carries the "Review failed run" action.
 */
function FailureBanner({ tasks }: { tasks: TaskStatus[] }) {
  if (tasks.length < 2) return null;

  return (
    <div className="banner banner--risk task-notice" role="status">
      <StatusMark tone="risk" label={`${tasks.length} tasks failed`} />
      <p className="banner__text">{tasks.map((task) => task.name || task.id).join(", ")}</p>
    </div>
  );
}

// The runner writes 'New task "Name" runs every 7d in notify mode. <purpose>'.
const ANNOUNCEMENT_PREFIX = /^New task ".*?" runs every \S+ in \S+ mode\.\s*/;

function announcementText(item: TaskItem) {
  return item.summary || item.reason || item.message || "";
}

/** A new task's notice belongs on its row when the row exists and the notice has the runner's shape. */
function rowAnnouncement(item: TaskItem, tasks: TaskStatus[]) {
  return (
    item.kind === "announcement" &&
    tasks.some((task) => task.id === item.task) &&
    ANNOUNCEMENT_PREFIX.test(announcementText(item))
  );
}

/** The Inbox cell: an open item that needs a person is one action, not another red state. */
function InboxCell({ task }: { task: TaskStatus }) {
  const item = task.item ? itemStatePresentation(task.item.state) : null;
  const failed = task.lastRun?.outcome === "failed";

  if (!item) {
    return failed && task.lastRun?.id ? (
      <TaskLink task={task} run={task.lastRun.id} label="Review failed run" />
    ) : (
      <span className="task-table__none">—</span>
    );
  }

  if (task.item?.state === "needs-attention") {
    // Without a run id the task's page, which lists its runs, is the place to review.
    const run = task.item.run ?? task.lastRun?.id;

    return <TaskLink task={task} run={run} label={failed ? "Review failed run" : "Review"} />;
  }

  return <StatusMark tone={item.tone} label={item.label} />;
}

function TaskRow({
  task,
  announcement,
  onOpen,
  onDismiss,
  itemError,
}: {
  task: TaskStatus;
  /** The new-task notice, folded into this row's "What it does". */
  announcement?: TaskItem;
  onOpen: () => void;
  onDismiss?: (item: TaskItem) => void;
  itemError?: { scope: string; text: string } | null;
}) {
  const [open, setOpen] = useState(false);
  const last = task.lastRun ? outcomePresentation(task.lastRun.outcome) : null;

  const purpose = announcement
    ? announcementText(announcement).replace(ANNOUNCEMENT_PREFIX, "")
    : "";

  const purposeId = `task-purpose-${task.id}`;

  // A local copy that matches the primary ref needs no mark.
  const local =
    task.local && task.local.state !== "same" ? localStatePresentation(task.local.state) : null;

  return (
    <>
      <tr className={open ? "is-expanded" : undefined}>
        <th scope="row">
          <button type="button" className="task-table__open" onClick={onOpen}>
            <span className="link-quiet link-title">{task.name || task.id}</span>
          </button>
          {local ? (
            <span className="task-table__local">
              <StatusMark tone={local.tone} label={local.label} />
            </span>
          ) : null}
          {task.name || announcement ? (
            <span className="task-table__meta">
              {announcement ? (
                <>
                  <span className="task-table__new-tag">New definition</span>
                  <button
                    type="button"
                    className="task-notice__more"
                    aria-expanded={open}
                    aria-controls={purposeId}
                    onClick={() => setOpen(!open)}
                  >
                    What it does
                    <DisclosureChevron open={open} />
                  </button>
                </>
              ) : null}
            </span>
          ) : null}
        </th>
        <td>
          {task.scheduled ? (
            (MODE_LABELS[task.mode] ?? task.mode)
          ) : (
            <span className="task-table__none">—</span>
          )}
        </td>
        <td className="task-table__time is-optional">{formatEvery(task.every)}</td>
        <td>
          {last && task.lastRun ? (
            <span className="task-table__last">
              <StatusMark tone={last.tone} label={last.label} />
              <span className="task-table__aside">
                <RelativeTime value={task.lastRun.at} />
              </span>
            </span>
          ) : (
            <span className="task-table__none">Never run</span>
          )}
        </td>
        <td className={task.due ? "task-table__due is-due" : "task-table__due"}>
          <time dateTime={task.nextDue}>{formatDue(task.nextDue, task.due)}</time>
        </td>
        <td>
          <InboxCell task={task} />
        </td>
      </tr>
      {announcement && open ? (
        <tr className="task-table__purpose" id={purposeId}>
          <td colSpan={6}>
            <code>{task.id}</code>
            <p>
              Runs every {formatEvery(task.every)} in {MODE_LABELS[task.mode] ?? task.mode} mode.
              {purpose ? ` ${purpose}` : ""}
            </p>
            {onDismiss ? (
              <button type="button" className="button" onClick={() => onDismiss(announcement)}>
                Dismiss
              </button>
            ) : null}
            {itemError?.scope === announcement.id ? (
              <p className="task-inline-error" role="alert">
                {itemError.text}
              </p>
            ) : null}
          </td>
        </tr>
      ) : null}
    </>
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
  const bannerItems = announcements.filter((item) => !rowAnnouncement(item, report.tasks));

  const failedRuns = report.tasks.filter((task) => task.lastRun?.outcome === "failed").length;

  return (
    <div className="tasks-overview">
      <header className="view-header">
        <div className="view-header__title-row">
          <h2>Tasks</h2>
          <span className="view-header__count">{report.tasks.length}</span>
          <span className="view-header__about">
            {scheduleSummary(report)}
            {failedRuns > 0 ? (
              <>
                {" "}
                · {failedRuns} failed {failedRuns === 1 ? "run" : "runs"}
              </>
            ) : null}
          </span>
          <div className="view-header__actions">
            <SourceDetails report={report} />
            <PauseButton paused={report.paused} pending={pausePending} onToggle={onTogglePause} />
          </div>
        </div>
      </header>
      <div className="tasks-overview__body">
        {error}
        <FailureBanner tasks={report.tasks.filter((task) => task.lastRun?.outcome === "failed")} />
        <TasksNotices report={report} pausePending={pausePending} onResume={onResume} />
        {bannerItems.map((item) => (
          <div key={item.id} className="banner task-notice" role="status">
            {item.kind === "announcement" ? (
              <StatusMark tone="info" label="New:" />
            ) : (
              <StatusMark tone="warning" label={`Removed task ${item.task}`} />
            )}
            <p className="banner__text">{announcementText(item)}</p>
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
                  Frequency
                </th>
                <th scope="col">Last run</th>
                <th scope="col">Next due</th>
                <th scope="col">Action</th>
              </tr>
            </thead>
            <tbody>
              {report.tasks.map((task) => (
                <TaskRow
                  key={task.id}
                  task={task}
                  announcement={announcements.find(
                    (item) => item.task === task.id && rowAnnouncement(item, report.tasks),
                  )}
                  onOpen={() => onOpen(task.id)}
                  onDismiss={onDismiss}
                  itemError={itemError}
                />
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
