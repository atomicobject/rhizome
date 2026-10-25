// Browser diagnostics counters (SPEC-0116 "Web UI evidence"). Everything here is
// module state, never React state, so measuring cannot cause a render. A batch
// carries counts, durations, and fixed identifiers only: no titles, paths,
// URLs, query text, error messages, or stacks.

export type RegionName = "app-shell" | "sidebar" | "view-table" | "note-view";

export type RouteKind = "notes" | "ontology" | "explorer" | "agent" | "graphql" | "other";

export type Surface = "desktop" | "browser";

type RegionStats = {
  mount: number;
  update: number;
  nested_update: number;
  actual_total_ms: number;
  actual_max_ms: number;
  base_total_ms: number;
  base_max_ms: number;
  commits_after_refresh: number;
};

export type ClientBatch = {
  session: string;
  build: string;
  surface: Surface;
  route: RouteKind;
  window_start: number;
  window_end: number;
  regions: Partial<Record<RegionName, RegionStats>>;
  frames: { over_50ms: number; over_200ms: number; stalled_ms: number; longest_ms: number };
  refresh: { cycles: number; refetched: number; unchanged: number; churned: number };
  stability: {
    reconnects: number;
    errors: Record<string, number>;
    rejections: Record<string, number>;
  };
};

const REFRESH_COMMIT_WINDOW_MS = 1_000;

// A refetch that never resolves (canceled, or replaced by a later refresh
// before it finished) must not hold its cycle open forever.
const CYCLE_SETTLE_CAP_MS = 10_000;

const STALL_MS = 50;

const LONG_STALL_MS = 200;

// Bounds the batch: further distinct error names count under "Other".
const MAX_ERROR_NAMES = 16;

const ERROR_NAME = /^[A-Za-z][A-Za-z0-9_.$]{0,63}$/;

let regions = new Map<RegionName, RegionStats>();

let frames = { over_50ms: 0, over_200ms: 0, stalled_ms: 0, longest_ms: 0 };

let refresh = { cycles: 0, refetched: 0, unchanged: 0, churned: 0 };

/**
 * One background refresh: the commits in the second after it and how its
 * refetches resolved. Churn is decided per cycle, once its commit window has
 * closed and its refetches have resolved, so a batch boundary cannot split a
 * commit from the refetch it followed.
 */
export type RefreshCycle = {
  pending: number;
  refetched: number;
  unchanged: number;
  commits: number;
  windowOpen: boolean;
  settled: boolean;
  /** A refetch failed or was replaced, so its data is unknown. */
  incomplete: boolean;
  windowTimer?: ReturnType<typeof setTimeout>;
};

let currentCycle: RefreshCycle | undefined;

let stability = {
  reconnects: 0,
  errors: new Map<string, number>(),
  rejections: new Map<string, number>(),
};

let windowStart = Date.now();

export function recordCommit(
  region: RegionName,
  phase: "mount" | "update" | "nested-update",
  actualMs: number,
  baseMs: number,
) {
  let stats = regions.get(region);

  if (!stats) {
    stats = {
      mount: 0,
      update: 0,
      nested_update: 0,
      actual_total_ms: 0,
      actual_max_ms: 0,
      base_total_ms: 0,
      base_max_ms: 0,
      commits_after_refresh: 0,
    };
    regions.set(region, stats);
  }

  if (phase === "mount") stats.mount++;
  else if (phase === "update") stats.update++;
  else stats.nested_update++;
  stats.actual_total_ms += actualMs;
  stats.actual_max_ms = Math.max(stats.actual_max_ms, actualMs);
  stats.base_total_ms += baseMs;
  stats.base_max_ms = Math.max(stats.base_max_ms, baseMs);

  if (currentCycle?.windowOpen) {
    stats.commits_after_refresh++;
    currentCycle.commits++;
  }
}

/** Records one gap between animation frames observed while the page was visible. */
export function recordFrameGap(gapMs: number) {
  if (gapMs <= STALL_MS) return;
  frames.over_50ms++;

  if (gapMs > LONG_STALL_MS) frames.over_200ms++;
  // Total stalled time is the sum of the stalled gaps themselves.
  frames.stalled_ms += gapMs;
  frames.longest_ms = Math.max(frames.longest_ms, gapMs);
}

function settleCycle(cycle: RefreshCycle, force = false) {
  if (cycle.settled || (!force && (cycle.windowOpen || cycle.pending > 0))) return;
  cycle.settled = true;

  // A refetch still outstanding at the cap, failed, or replaced could have
  // returned changed data, so such a cycle is never judged.
  if (cycle.pending > 0 || cycle.incomplete) return;

  // Regions committed right after this refresh, yet every refetch returned
  // identical data: those renders had nothing new to show.
  if (cycle.commits > 0 && cycle.refetched > 0 && cycle.unchanged === cycle.refetched) {
    refresh.churned++;
  }
}

function newCycle(): RefreshCycle {
  const cycle: RefreshCycle = {
    pending: 0,
    refetched: 0,
    unchanged: 0,
    commits: 0,
    windowOpen: true,
    settled: false,
    incomplete: false,
  };

  setTimeout(() => settleCycle(cycle, true), CYCLE_SETTLE_CAP_MS);

  return cycle;
}

/**
 * Starts a refresh. One that begins inside an earlier refresh's commit window
 * joins it: a commit in the shared window cannot be attributed to either alone,
 * so the group is churn only if every refetch from all of them was unchanged.
 */
export function recordRefreshCycle(): RefreshCycle {
  refresh.cycles++;

  const cycle = currentCycle?.windowOpen && !currentCycle.settled ? currentCycle : newCycle();

  currentCycle = cycle;
  clearTimeout(cycle.windowTimer);
  cycle.windowTimer = setTimeout(() => {
    cycle.windowOpen = false;
    settleCycle(cycle);
  }, REFRESH_COMMIT_WINDOW_MS);

  return cycle;
}

/** Ends one refetch of `cycle`; `unchanged` is null when it produced no data. */
export function finishRefetch(cycle: RefreshCycle, unchanged: boolean | null) {
  cycle.pending--;

  if (unchanged === null) cycle.incomplete = true;
  else {
    refresh.refetched++;
    cycle.refetched++;

    if (unchanged) {
      refresh.unchanged++;
      cycle.unchanged++;
    }
  }

  settleCycle(cycle);
}

export function recordReconnect() {
  stability.reconnects++;
}

/** Counts an error by name only; `null` means the value was not an Error. */
export function recordError(kind: "errors" | "rejections", errorName: string | null) {
  const counts = stability[kind];
  let name = errorName === null ? "NonError" : ERROR_NAME.test(errorName) ? errorName : "Other";

  if (!counts.has(name) && counts.size >= MAX_ERROR_NAMES) name = "Other";
  counts.set(name, (counts.get(name) ?? 0) + 1);
}

/** Clears all counters and the open refresh, for tests that need a clean start. */
export function resetClientMetrics() {
  currentCycle = undefined;
  refresh = { cycles: 0, refetched: 0, unchanged: 0, churned: 0 };
  regions = new Map();
  frames = { over_50ms: 0, over_200ms: 0, stalled_ms: 0, longest_ms: 0 };
  stability = { reconnects: 0, errors: new Map(), rejections: new Map() };
  windowStart = Date.now();
}

const round = (ms: number) => Math.round(ms * 100) / 100;

/**
 * Closes the current window and starts the next. Returns null when every
 * measurement in the closed window is zero, which is never sent.
 */
export function takeBatch(identity: {
  session: string;
  build: string;
  surface: Surface;
  route: RouteKind;
}): ClientBatch | null {
  const windowEnd = Date.now();
  const closed = { regions, frames, refresh, stability, windowStart };

  regions = new Map();
  frames = { over_50ms: 0, over_200ms: 0, stalled_ms: 0, longest_ms: 0 };
  refresh = { cycles: 0, refetched: 0, unchanged: 0, churned: 0 };
  stability = { reconnects: 0, errors: new Map(), rejections: new Map() };
  windowStart = windowEnd;

  const empty =
    closed.regions.size === 0 &&
    closed.frames.over_50ms === 0 &&
    closed.refresh.cycles === 0 &&
    closed.refresh.refetched === 0 &&
    closed.refresh.churned === 0 &&
    closed.stability.reconnects === 0 &&
    closed.stability.errors.size === 0 &&
    closed.stability.rejections.size === 0;

  if (empty) return null;

  return {
    ...identity,
    window_start: closed.windowStart,
    window_end: windowEnd,
    regions: Object.fromEntries(
      [...closed.regions].map(([name, stats]) => [
        name,
        {
          ...stats,
          actual_total_ms: round(stats.actual_total_ms),
          actual_max_ms: round(stats.actual_max_ms),
          base_total_ms: round(stats.base_total_ms),
          base_max_ms: round(stats.base_max_ms),
        },
      ]),
    ),
    frames: {
      ...closed.frames,
      stalled_ms: round(closed.frames.stalled_ms),
      longest_ms: round(closed.frames.longest_ms),
    },
    refresh: closed.refresh,
    stability: {
      reconnects: closed.stability.reconnects,
      errors: Object.fromEntries(closed.stability.errors),
      rejections: Object.fromEntries(closed.stability.rejections),
    },
  };
}
