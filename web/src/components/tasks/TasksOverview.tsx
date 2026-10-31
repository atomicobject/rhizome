import type { ReactNode } from "react";

import type { TaskItem, TasksReport, TaskStatus } from "../../api/tasks";
import { StatusMark } from "../StatusMark";
import { PauseIcon, PlayIcon } from "./TaskIcons";
import {
  formatDue,
  formatRelative,
  itemStatePresentation,
  MODE_LABELS,
  outcomePresentation,
  shortCommit,
} from "./taskFormat";

export function PauseButton({
  paused,
  pending,
  onToggle,
  className = "task-button",
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
        <div className="task-notice task-notice--risk" role="alert">
          <StatusMark tone="risk" label="Not trusted" />
          {/* The reason already says what to run; it starts lowercase. */}
          <p className="task-notice__sentence">{report.untrusted}</p>
        </div>
      ) : null}
      {report.refError ? (
        <div className="task-notice task-notice--risk" role="alert">
          <StatusMark tone="risk" label="Primary ref unavailable" />
          <p>No task runs until the primary ref resolves. {report.refError}</p>
        </div>
      ) : null}
      {report.paused ? (
        <div className="task-notice" role="status">
          <StatusMark tone="muted" label="Paused" />
          <p>Background work is paused. Due tasks wait until you resume.</p>
          <button type="button" className="task-button" disabled={pausePending} onClick={onResume}>
            <PlayIcon />
            Resume all
          </button>
        </div>
      ) : null}
      {report.lastPassError && !brief ? (
        <div className="task-notice" role="status">
          <StatusMark tone="warning" label="Last pass failed" />
          <p>
            {report.lastPassError.message}{" "}
            <span className="task-notice__aside">{formatRelative(report.lastPassError.at)}</span>
          </p>
        </div>
      ) : null}
      {report.mergeWarning && !brief ? (
        <div className="task-notice" role="status">
          <StatusMark tone="info" label="Merge setting" />
          <p>{report.mergeWarning}</p>
        </div>
      ) : null}
    </>
  );
}

function TaskRow({ task, onOpen }: { task: TaskStatus; onOpen: () => void }) {
  const last = task.lastRun ? outcomePresentation(task.lastRun.outcome) : null;
  const item = task.item ? itemStatePresentation(task.item.state) : null;

  return (
    <tr>
      <th scope="row">
        <button type="button" className="task-table__open" onClick={onOpen}>
          <span>{task.name || task.id}</span>
          <code>{task.id}</code>
        </button>
      </th>
      <td>
        {MODE_LABELS[task.mode] ?? task.mode}
        {task.modeSource === "override" ? null : (
          <span className="task-table__aside"> · default</span>
        )}
      </td>
      <td className="task-table__mono is-optional">{task.every}</td>
      <td>
        {last ? (
          <span className="task-table__last">
            <StatusMark tone={last.tone} label={last.label} />
            <span className="task-table__aside">{formatRelative(task.lastRun?.at)}</span>
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
}: {
  report: TasksReport;
  onOpen: (task: string) => void;
  pausePending: boolean;
  onTogglePause: () => void;
  onResume: () => void;
  error?: ReactNode;
  /** New-task notices; they count toward the desktop badge until dismissed. */
  announcements?: TaskItem[];
  onDismiss?: (item: TaskItem) => void;
}) {
  return (
    <div className="tasks-overview">
      <header className="task-head">
        <div className="task-head__title">
          <h2>
            Tasks <span className="task-head__count">{report.tasks.length}</span>
          </h2>
          <p>
            {report.ref ? (
              <code title={report.commit}>
                {report.ref}
                {report.commit ? ` @ ${shortCommit(report.commit)}` : ""}
              </code>
            ) : null}
            {report.fetchedAt ? <span>Fetched {formatRelative(report.fetchedAt)}</span> : null}
          </p>
        </div>
        <PauseButton paused={report.paused} pending={pausePending} onToggle={onTogglePause} />
      </header>
      <div className="tasks-overview__body">
        {error}
        <TasksNotices report={report} pausePending={pausePending} onResume={onResume} />
        {announcements.map((item) => (
          <div key={item.id} className="task-notice" role="status">
            <StatusMark tone="info" label="New task" />
            <p>{item.summary}</p>
            {onDismiss ? (
              <button type="button" className="task-button" onClick={() => onDismiss(item)}>
                Dismiss
              </button>
            ) : null}
          </div>
        ))}
        {report.problems?.length ? (
          <div className="task-notice task-notice--risk" role="alert">
            <StatusMark tone="risk" label={`${report.problems.length} task files did not load`} />
            <ul>
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
              A task is a YAML file in <code>.rhizome/tasks/</code>. Rhizome reads them from the
              primary ref
              {report.ref ? (
                <>
                  {" "}
                  (<code>{report.ref}</code>)
                </>
              ) : null}
              , so a new task appears here once it is merged and fetched.
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
