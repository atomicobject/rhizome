import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";

import { queryKeys } from "../../api/queryKeys";
import {
  getTaskRun,
  getTaskRuns,
  getTasksReport,
  getTasksState,
  type TaskRunDetail,
  type TasksLiveState,
} from "../../api/tasks";
import { appendEvents } from "./transcript";

// Page polling (EFF-2026-10-31-14-31): the report on load, on focus, and every
// 60 s; live state every 2 s while a run is going or a launch is awaited; the
// open run's transcript from `next` while it runs. Nothing polls while hidden.

const LIVE_MS = 2_000;

const IDLE_MS = 60_000;

const LAUNCH_WAIT_MS = 30_000;

function subscribeVisibility(onChange: () => void) {
  document.addEventListener("visibilitychange", onChange);

  return () => document.removeEventListener("visibilitychange", onChange);
}

export function usePageVisible(): boolean {
  return useSyncExternalStore(
    subscribeVisibility,
    () => document.visibilityState !== "hidden",
    () => true,
  );
}

export function useTasksReport() {
  return useQuery({
    queryKey: queryKeys.tasks.report(),
    queryFn: ({ signal }) => getTasksReport({ signal }),
    refetchInterval: IDLE_MS,
    refetchOnWindowFocus: true,
  });
}

/** A Run the page launched, awaited until it shows up or fails. */
export type LaunchWatch = { task: string; item: string; launchedAt: string; until: number };

function launchResolved(watch: LaunchWatch, state: TasksLiveState): boolean {
  const since = new Date(watch.launchedAt).getTime();

  const startedSince = (task: string, startedAt?: string) =>
    task === watch.task && Boolean(startedAt) && new Date(startedAt!).getTime() >= since;

  return (
    (state.running ? startedSince(state.running.task, state.running.startedAt) : false) ||
    state.recent.some((record) => startedSince(record.task, record.startedAt)) ||
    state.items.some((item) => item.id === watch.item && Boolean(item.launchError))
  );
}

export function useTasksLiveState() {
  const queryClient = useQueryClient();
  const [watch, setWatch] = useState<LaunchWatch | null>(null);

  const query = useQuery({
    queryKey: queryKeys.tasks.state(),
    queryFn: ({ signal }) => getTasksState({ signal }),
    refetchInterval: (current) => (current.state.data?.running || watch ? LIVE_MS : IDLE_MS),
    refetchOnWindowFocus: true,
  });

  const state = query.data;

  useEffect(() => {
    if (!watch) return;

    if (state && launchResolved(watch, state)) {
      setWatch(null);

      return;
    }

    const timer = window.setTimeout(() => setWatch(null), Math.max(0, watch.until - Date.now()));

    return () => window.clearTimeout(timer);
  }, [state, watch]);

  // A run that starts or finishes changes the report and that task's history.
  const runningID = state?.running?.id ?? "";
  const latestID = state?.recent[0]?.id ?? "";
  const seen = useRef<string | null>(null);

  useEffect(() => {
    const signature = `${runningID}|${latestID}`;

    if (seen.current !== null && seen.current !== signature) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.tasks.report() });
      void queryClient.invalidateQueries({ queryKey: [...queryKeys.tasks.all(), "runs"] });
    }

    seen.current = signature;
  }, [latestID, queryClient, runningID]);

  const awaitLaunch = useCallback((task: string, item: string, launchedAt: string) => {
    setWatch({ task, item, launchedAt, until: Date.now() + LAUNCH_WAIT_MS });
  }, []);

  return { query, state, watch, awaitLaunch };
}

export function useTaskRuns(task: string | undefined) {
  return useQuery({
    queryKey: queryKeys.tasks.runs(task ?? ""),
    queryFn: ({ signal }) => getTaskRuns(task ?? "", { signal }),
    enabled: Boolean(task),
    select: (data) => data.runs ?? [],
  });
}

type TranscriptState = { id: string; detail?: TaskRunDetail; error?: unknown };

/**
 * Reads a run's detail, then, while `live`, resumes from `next` every 2 s.
 * One more read after it stops being live picks up the tail and the record.
 */
export function useRunTranscript(runID: string | undefined, live: boolean) {
  const visible = usePageVisible();
  const [data, setData] = useState<TranscriptState | null>(null);
  const [attempt, setAttempt] = useState(0);
  const dataRef = useRef(data);
  dataRef.current = data;
  // The live flag as of this run's last read, so a run that finished while
  // the page was hidden still gets its tail read.
  const readLive = useRef<{ id?: string; live: boolean }>({ live: false });

  useEffect(() => {
    if (!runID) return;
    const held = dataRef.current?.id === runID ? dataRef.current.detail : undefined;
    const wasLive = readLive.current.id === runID && readLive.current.live;

    if (held && (!visible || (!live && !wasLive))) return;
    readLive.current = { id: runID, live };
    let offset = held?.next ?? 0;
    let timer = 0;
    let cancelled = false;
    const controller = new AbortController();

    const pull = async () => {
      try {
        const detail = await getTaskRun(runID, offset, { signal: controller.signal });

        if (cancelled) return;
        offset = detail.next;
        setData((current) => {
          const previous = current?.id === runID ? current.detail : undefined;

          return {
            id: runID,
            detail: previous
              ? {
                  ...detail,
                  events: appendEvents(previous.events, detail.events),
                  checkLog: previous.checkLog ?? detail.checkLog,
                }
              : { ...detail, events: appendEvents([], detail.events) },
          };
        });
      } catch (error) {
        if (cancelled) return;
        setData((current) => ({
          id: runID,
          detail: current?.id === runID ? current.detail : undefined,
          error,
        }));
      }

      if (!cancelled && live && visible) timer = window.setTimeout(pull, LIVE_MS);
    };

    void pull();

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [attempt, live, runID, visible]);

  const current = data?.id === runID ? data : null;

  const retry = useCallback(() => {
    setData(null);
    setAttempt((value) => value + 1);
  }, []);

  return {
    detail: current?.detail,
    error: current?.detail ? undefined : current?.error,
    loading: Boolean(runID) && !current,
    retry,
  };
}
