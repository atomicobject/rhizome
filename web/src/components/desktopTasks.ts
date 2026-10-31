import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { queryKeys } from "../api/queryKeys";
import { getTasksReport, getTasksState, type TaskItem, type TasksLiveState } from "../api/tasks";
import type { DesktopTaskItem, DesktopTasks } from "./desktopHost";

// Rhizome Desktop badges its Dock icon and notifies for task items that need
// the person (EFF-2026-10-31-14-31, phase 3). The page reads the live state
// every 60 s and on focus, even while hidden, and reports it in its title.

const POLL_MS = 60_000;

/** Task names change rarely; the report only titles notifications. */
const NAMES_MS = 10 * 60_000;

/** Rhizome Desktop rejects longer reports. */
const ITEM_LIMIT = 50;

const TEXT_LIMIT = 200;

type NeedsPerson = TaskItem & { state: DesktopTaskItem["state"] };

function needsPerson(item: TaskItem, now: number): item is NeedsPerson {
  if (item.snoozedUntil && new Date(item.snoozedUntil).getTime() > now) return false;

  return item.state === "due" || item.state === "ready" || item.state === "needs-attention";
}

function shorten(text: string): string {
  const chars = [...text];

  return chars.length > TEXT_LIMIT ? `${chars.slice(0, TEXT_LIMIT - 1).join("")}…` : text;
}

export function desktopTasks(
  state: TasksLiveState,
  names: ReadonlyMap<string, string>,
  now: number,
): DesktopTasks {
  const items = state.items.filter((item) => needsPerson(item, now));

  return {
    needs: items.length,
    items: items.slice(0, ITEM_LIMIT).map((item) => ({
      id: item.id,
      task: item.task,
      name: names.get(item.task) || undefined,
      state: item.state,
      text: shorten(item.summary || item.reason || ""),
    })),
  };
}

/** The report's `tasks`, once the live state has loaded. */
export function useDesktopTasks(): DesktopTasks | undefined {
  const state = useQuery({
    queryKey: queryKeys.tasks.state(),
    queryFn: ({ signal }) => getTasksState({ signal }),
    refetchInterval: POLL_MS,
    refetchIntervalInBackground: true,
    refetchOnWindowFocus: true,
  });

  const report = useQuery({
    queryKey: queryKeys.tasks.report(),
    queryFn: ({ signal }) => getTasksReport({ signal }),
    staleTime: NAMES_MS,
  });

  const { data, dataUpdatedAt } = state;
  const tasks = report.data?.tasks;

  // Each read re-checks snoozes against its own time, so an item whose snooze
  // ended counts again even when the state itself did not change.
  return useMemo(
    () =>
      data
        ? desktopTasks(data, new Map(tasks?.map((task) => [task.id, task.name])), dataUpdatedAt)
        : undefined,
    [data, dataUpdatedAt, tasks],
  );
}
