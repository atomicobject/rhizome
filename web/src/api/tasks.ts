import { fetchJSON, type RequestOptions } from "./client";
import type { AgentEvent } from "./types";

// Scheduled tasks (SPEC-0127 US8). The shapes mirror pkg/app/tasks: the
// `rzm tasks status --json` Report, LiveState, RunView, and RunDetail.

export type TaskMode = "auto" | "notify" | "off";

/** A mode choice; `default` removes the person's override. */
export type TaskModeChoice = TaskMode | "default";

export type TaskItemState =
  | "due"
  | "running"
  | "ready"
  | "started"
  | "needs-attention"
  | "done"
  | "dismissed";

export type TaskItem = {
  id: string;
  task: string;
  kind: "task" | "announcement" | string;
  state: TaskItemState;
  created: string;
  updated: string;
  snoozedUntil?: string;
  summary?: string;
  context?: string;
  commit?: string;
  branch?: string;
  worktree?: string;
  message?: string;
  changed?: string[];
  tokens?: number;
  reason?: string;
  /** The attempt that owns the item's worktree. */
  run?: string;
  /** Why a Run from the page failed before the work started. */
  launchError?: string;
};

export type TaskRunRecord = {
  id?: string;
  task: string;
  startedAt?: string;
  /** Finish time. */
  at: string;
  outcome: string;
  /** Only on LiveState.running. */
  stage?: "check" | "agent";
  tokens?: number;
  message?: string;
  item?: string;
  branch?: string;
  /** Base commit. */
  commit?: string;
  worktree?: string;
  changed?: string[];
};

export type TaskTagged = { At: string; Author: string; Commit: string };

export type TaskStatus = {
  id: string;
  name: string;
  scope: string;
  every: string;
  mode: TaskMode;
  modeSource: "override" | "repository" | "default" | string;
  lastRun?: TaskRunRecord;
  lastTagged?: TaskTagged;
  nextDue?: string;
  due: boolean;
  item?: TaskItem;
  tokensLast30Days?: number;
};

export type TaskProblem = { path: string; message: string };

export type TasksReport = {
  ref?: string;
  commit?: string;
  refError?: string;
  fetchedAt?: string;
  paused: boolean;
  untrusted?: string;
  lastPassError?: { at: string; message: string };
  mergeWarning?: string;
  stateDir: string;
  tasks: TaskStatus[];
  problems?: TaskProblem[];
};

export type TasksLiveState = {
  paused: boolean;
  running?: TaskRunRecord;
  /** Running is set but the run.lock holder is gone. */
  stale?: boolean;
  items: TaskItem[];
  /** Newest 50 across tasks. */
  recent: TaskRunRecord[];
};

export type TaskRunView = TaskRunRecord & { hasTranscript: boolean; hasCheckLog: boolean };

/** One transcript line: the agent event shape plus `final-message` and `transcript-truncated`. */
export type TaskTranscriptEvent = AgentEvent;

export type TaskRunDetail = {
  run: TaskRunView;
  events: TaskTranscriptEvent[];
  /** Byte offset to resume from. */
  next: number;
  /** Only when `after` was 0. */
  checkLog?: string;
};

export type TaskItemAction = "snooze" | "skip" | "discard" | "done";

type TaskRequestBody = { until?: string } | { mode: TaskModeChoice };

function post<T>(url: string, body?: TaskRequestBody) {
  return fetchJSON<T>(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
}

const enc = encodeURIComponent;

export function getTasksReport(options: RequestOptions = {}) {
  return fetchJSON<TasksReport>("/api/tasks", { signal: options.signal });
}

export function getTasksState(options: RequestOptions = {}) {
  return fetchJSON<TasksLiveState>("/api/tasks/state", { signal: options.signal });
}

export function getTaskRuns(task: string, options: RequestOptions = {}) {
  return fetchJSON<{ runs: TaskRunView[] | null }>(`/api/tasks/runs?task=${enc(task)}`, {
    signal: options.signal,
  });
}

export function getTaskRun(runID: string, after = 0, options: RequestOptions = {}) {
  return fetchJSON<TaskRunDetail>(`/api/tasks/runs/${enc(runID)}?after=${after}`, {
    signal: options.signal,
  });
}

/** Starts the item's work in a separate process; a failed precheck is a 409. */
export function runTaskItem(itemID: string) {
  return post<{ launchedAt: string }>(`/api/tasks/items/${enc(itemID)}/run`);
}

export function actOnTaskItem(itemID: string, action: TaskItemAction, until?: string) {
  return post<Record<string, never>>(
    `/api/tasks/items/${enc(itemID)}/${action}`,
    action === "snooze" ? { until } : undefined,
  );
}

export function setTaskMode(task: string, mode: TaskModeChoice) {
  return post<Record<string, never>>(`/api/tasks/modes/${enc(task)}`, { mode });
}

export function setTasksPaused(paused: boolean) {
  return post<Record<string, never>>(paused ? "/api/tasks/pause" : "/api/tasks/resume");
}
