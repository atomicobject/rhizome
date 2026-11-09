import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { JsonObject } from "../api/parse";
import type { ExperienceShowing } from "../components/experienceFrameRecords";
import { ExperienceFrames, RETAINED_FRAMES } from "../components/experienceFrames";
import {
  FRAME_READY_MESSAGE,
  LOCATION_ACCEPTED_MESSAGE,
  LOCATION_MESSAGE,
  LOCATION_RENDERED_MESSAGE,
  PHASE_MESSAGE,
} from "../lib/customViewMessages";
import { takeBatch } from "./clientMetrics";
import {
  recordExperienceCount,
  recordExperiencePhase,
  recordPagesRetained,
  resetExperienceMetrics,
  takeExperiences,
} from "./experienceMetrics";

const identity = {
  session: "0123456789abcdef",
  build: "dev",
  surface: "browser",
  route: "notes",
} as const;

const origin = window.location.origin;

// The host's clock is frozen here, so a phase's time is its offset from it.
const T0 = 1_000;

const at = (ms: number) => performance.timeOrigin + T0 + ms;

function harness() {
  const frames = new ExperienceFrames({
    fetchInvocation: async (showing) => ({ invocation: { view: { id: showing.viewId } } }),
    fetchGeneration: async () => undefined,
  });

  const windows = new Map<number, { window: Window; sent: JsonObject[] }>();

  frames.subscribe(() => {
    for (const view of frames.views()) {
      if (windows.has(view.id)) continue;
      const iframe = document.createElement("iframe");
      document.body.append(iframe);
      const sent: JsonObject[] = [];
      vi.spyOn(iframe.contentWindow!, "postMessage").mockImplementation((m: JsonObject) =>
        sent.push(m),
      );
      windows.set(view.id, { window: iframe.contentWindow!, sent });
      frames.attach(view.id, iframe.contentWindow);
    }
  });

  const frame = (key: string) => {
    const view = frames.views().find((each) => each.experienceKey === key);

    return view ? windows.get(view.id)! : null;
  };

  const from = (key: string, data: JsonObject) =>
    frames.receive(new MessageEvent("message", { origin, source: frame(key)!.window, data }));

  /** Shows a tab and lets its frame accept the location; returns the navigation ID. */
  const open = async (show: ExperienceShowing) => {
    frames.show(show, null);
    await vi.advanceTimersByTimeAsync(0);

    if (!frame(show.experienceKey)!.sent.length)
      from(show.experienceKey, { type: FRAME_READY_MESSAGE });

    const location = frame(show.experienceKey)!
      .sent.filter((each) => each.type === LOCATION_MESSAGE)
      .at(-1)!;

    const navigationId = String(location.navigationId);
    from(show.experienceKey, { type: LOCATION_ACCEPTED_MESSAGE, tabId: show.tabId, navigationId });

    return navigationId;
  };

  return { frames, from, open };
}

const showing = (overrides: Partial<ExperienceShowing> = {}): ExperienceShowing => ({
  tabId: "home",
  experienceKey: "Views/PRIVATE_PATH/kb",
  viewId: "kb.record",
  viewName: "PRIVATE_TITLE",
  declarations: [{ name: "q", kind: "string", identity: true }],
  context: { kind: "node", type: "Note", ref: { notePath: "PRIVATE_PATH.md", kind: "NOTE" } },
  params: { q: "PRIVATE_QUERY" },
  services: { onOpenNote: vi.fn(), onStageOps: vi.fn(async () => undefined) },
  ownsLocation: false,
  ...overrides,
});

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  vi.spyOn(performance, "now").mockReturnValue(T0);
});

afterEach(() => {
  resetExperienceMetrics();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  delete window.__RHIZOME_RENDER_PROFILING__;
  document.body.replaceChildren();
});

describe("experience frames measurement", () => {
  it("aggregates counts and nearest-rank percentiles per experience per window", () => {
    for (let ms = 20; ms >= 1; ms--) recordExperiencePhase("a", "first-render", ms);
    recordExperiencePhase("a", "data-ready", 7);
    recordExperienceCount("a", "frames_created");
    recordExperienceCount("a", "frames_created");
    recordExperienceCount("a", "evictions");
    recordPagesRetained("a", 2);
    recordPagesRetained("a", 5);
    recordPagesRetained("a", 3);
    recordExperienceCount("b", "resumes");

    const experiences = takeExperiences();
    expect(Object.keys(experiences)).toHaveLength(2);
    const [a, b] = Object.values(experiences);
    expect(a).toEqual({
      frames_created: 2,
      locations_reused: 0,
      pages_retained: 5,
      resumes: 0,
      evictions: 1,
      phases: {
        "first-render": { count: 20, p50_ms: 10, p95_ms: 19 },
        "data-ready": { count: 1, p50_ms: 7, p95_ms: 7 },
      },
    });
    expect(b).toMatchObject({ resumes: 1, phases: {} });

    // The window closed: the next one starts empty.
    expect(takeExperiences()).toEqual({});
  });

  it("records nothing without the profiling opt-in", () => {
    vi.stubEnv("DEV", false);
    recordExperiencePhase("a", "first-render", 5);
    recordExperienceCount("a", "frames_created");
    recordPagesRetained("a", 1);
    expect(takeExperiences()).toEqual({});
    expect(takeBatch(identity, takeExperiences())).toBeNull();

    window.__RHIZOME_RENDER_PROFILING__ = true;
    recordExperienceCount("a", "frames_created");
    expect(Object.values(takeExperiences())).toMatchObject([{ frames_created: 1 }]);
  });

  it("times phases from the frame and the navigation, without titles, paths, or query text", async () => {
    const { from, open } = harness();
    const key = showing().experienceKey;
    const first = await open(showing());

    from(key, { type: PHASE_MESSAGE, phase: "shell-start", at: at(10) });
    from(key, { type: PHASE_MESSAGE, phase: "module-loaded", at: at(25) });
    from(key, {
      type: PHASE_MESSAGE,
      phase: "first-render",
      at: at(40),
      tabId: "home",
      navigationId: first,
    });
    from(key, { type: LOCATION_RENDERED_MESSAGE, tabId: "home", navigationId: first, at: at(50) });
    // A repeated mark for the same navigation counts once.
    from(key, { type: LOCATION_RENDERED_MESSAGE, tabId: "home", navigationId: first, at: at(90) });

    // The tab moves to another subject: a new navigation replaces the page.
    const second = await open(showing({ params: { q: "PRIVATE_OTHER" } }));
    expect(second).not.toBe(first);
    // The replaced navigation's late mark is stale and ignored.
    from(key, {
      type: PHASE_MESSAGE,
      phase: "data-ready",
      at: at(999),
      tabId: "home",
      navigationId: first,
    });
    from(key, {
      type: PHASE_MESSAGE,
      phase: "data-ready",
      at: at(30),
      tabId: "home",
      navigationId: second,
    });

    const batch = takeBatch(identity, takeExperiences());
    const text = JSON.stringify(batch);
    expect(text).not.toMatch(/PRIVATE|kb\.record|Views/);

    const [hash] = Object.keys(batch!.experiences);
    expect(hash).toMatch(/^[0-9a-f]{14}$/);
    expect(batch!.experiences[hash!]).toEqual({
      frames_created: 1,
      locations_reused: 0,
      pages_retained: 1,
      resumes: 0,
      evictions: 0,
      phases: {
        "shell-start": { count: 1, p50_ms: 10, p95_ms: 10 },
        "module-loaded": { count: 1, p50_ms: 25, p95_ms: 25 },
        "first-render": { count: 1, p50_ms: 40, p95_ms: 40 },
        "location-painted": { count: 1, p50_ms: 50, p95_ms: 50 },
        "data-ready": { count: 1, p50_ms: 30, p95_ms: 30 },
      },
    });
  });

  it("counts reused locations, retained pages, resumes, and evictions", async () => {
    const { frames, open } = harness();
    const kb = showing({ experienceKey: "kb" });

    const leave = async (tabId: string) => {
      frames.hide(tabId);
      await vi.advanceTimersByTimeAsync(0);
    };

    await open(kb);
    frames.hide("home");
    await open({ ...kb, tabId: "second" });
    // Another experience shows, so kb's frame suspends with both pages.
    await leave("second");
    await open(showing({ experienceKey: "other0", tabId: "other0" }));
    // Back to kb's first tab: the frame resumes and the page is reused.
    await leave("other0");
    await open(kb);

    // Newer experiences push kb out of the window's retained frames.
    let previous = "home";

    for (let index = 1; index <= RETAINED_FRAMES + 1; index++) {
      await leave(previous);
      previous = `other${index}`;
      await open(showing({ experienceKey: previous, tabId: previous }));
    }

    const counts = Object.values(takeExperiences()).map(({ phases: _phases, ...rest }) => rest);
    expect(counts[0]).toEqual({
      frames_created: 1,
      locations_reused: 1,
      pages_retained: 2,
      resumes: 1,
      evictions: 1,
    });
  });
});
