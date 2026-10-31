import { notifyLocationChange } from "../locationStore";

// /tasks, /tasks/<task>, and /tasks/<task>/runs/<run id>.

export const TASKS_ROOT_PATH = "/tasks";

export type TasksLocation = { task?: string; run?: string };

export function isTasksPath(pathname: string): boolean {
  return pathname === TASKS_ROOT_PATH || pathname.startsWith(`${TASKS_ROOT_PATH}/`);
}

export function parseTasksLocation(pathname: string): TasksLocation {
  const [task, runs, run] = pathname
    .slice(TASKS_ROOT_PATH.length + 1)
    .split("/")
    .map((part) => {
      try {
        return decodeURIComponent(part);
      } catch {
        return part;
      }
    });

  return { task: task || undefined, run: runs === "runs" && run ? run : undefined };
}

export function tasksHref({ task, run }: TasksLocation): string {
  if (!task) return TASKS_ROOT_PATH;
  const base = `${TASKS_ROOT_PATH}/${encodeURIComponent(task)}`;

  return run ? `${base}/runs/${encodeURIComponent(run)}` : base;
}

export function navigateTasks(location: TasksLocation) {
  window.history.pushState({}, "", tasksHref(location));
  notifyLocationChange();
}
