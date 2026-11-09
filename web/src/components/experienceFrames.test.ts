import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { JsonObject } from "../api/parse";
import {
  CLOSE_PAGE_MESSAGE,
  EDIT_SESSION_MESSAGE,
  FRAME_READY_MESSAGE,
  GENERATION_MESSAGE,
  LOCATION_ACCEPT_TIMEOUT_MS,
  LOCATION_ACCEPTED_MESSAGE,
  LOCATION_MESSAGE,
  LOCATION_RENDERED_MESSAGE,
  OPEN_NOTE_MESSAGE,
  RESUME_MESSAGE,
  STAGE_OPS_MESSAGE,
  STAGE_RESULT_MESSAGE,
  SUSPEND_MESSAGE,
  VAULT_EVENT_MESSAGE,
} from "../lib/customViewMessages";
import type { EditReadLifecycle } from "../staging/stagedQuery";
import { STREAM_RECONNECTED_EVENT, VIEWS_CHANGED_EVENT } from "../query/vaultEvents";
import {
  ExperienceFrames,
  RETAINED_FRAMES,
  checkFolder,
  type ExperienceShowing,
} from "./experienceFrames";

type Sent = { type: string } & JsonObject;

const origin = window.location.origin;

/** A real frame window per experience frame, with what the host posted to it. */
function harness(generation = "g1") {
  const fetchInvocation = vi.fn(async (showing: ExperienceShowing) => ({
    invocation: {
      view: { id: showing.viewId, name: showing.viewName },
      context: showing.context,
      check: `/views/_check/${showing.experienceKey}/${showing.viewId}`,
    },
    generation,
  }));

  const fetchGeneration = vi.fn(async () => "g9");
  const frames = new ExperienceFrames({ fetchInvocation, fetchGeneration });
  const windows = new Map<number, { window: Window; sent: Sent[] }>();

  // The layer attaches each frame's window as it renders.
  frames.subscribe(() => {
    for (const view of frames.views()) {
      if (windows.has(view.id)) continue;
      const iframe = document.createElement("iframe");
      document.body.append(iframe);
      const target = iframe.contentWindow!;
      const sent: Sent[] = [];
      vi.spyOn(target, "postMessage").mockImplementation((message: Sent) => sent.push(message));
      windows.set(view.id, { window: target, sent });
      frames.attach(view.id, target);
    }
  });

  // `panel` picks the experience's sidebar panel frame.
  const frame = (experienceKey: string, panel = false) => {
    const view = frames
      .views()
      .find((each) => each.experienceKey === experienceKey && each.panel === panel);

    return view ? { ...windows.get(view.id)!, view } : null;
  };

  const from = (experienceKey: string, data: JsonObject, panel = false) =>
    frames.receive(
      new MessageEvent("message", { origin, source: frame(experienceKey, panel)!.window, data }),
    );

  const lastLocation = (experienceKey: string, panel = false) =>
    frame(experienceKey, panel)!
      .sent.filter((each) => each.type === LOCATION_MESSAGE)
      .at(-1)!;

  return { frames, fetchInvocation, fetchGeneration, frame, from, lastLocation };
}

const services = { onOpenNote: vi.fn(), onStageOps: vi.fn(async () => undefined) };

const showing = (overrides: Partial<ExperienceShowing> = {}): ExperienceShowing => ({
  tabId: "home",
  experienceKey: "exp_kb",
  viewId: "kb.home",
  viewName: "Knowledge",
  declarations: [
    { name: "concept", kind: "string", identity: true },
    { name: "sort", kind: "string" },
  ],
  context: { kind: "group", group: "Knowledge" },
  params: {},
  services,
  ownsLocation: true,
  ...overrides,
});

const panelShowing = (overrides: Partial<ExperienceShowing> = {}) =>
  showing({
    tabId: "panel",
    viewId: "kb.panel",
    viewName: "Knowledge panel",
    declarations: [],
    ownsLocation: false,
    panel: true,
    ...overrides,
  });

describe("ExperienceFrames", () => {
  beforeEach(() => vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] }));

  afterEach(() => {
    vi.useRealTimers();
    document.body.replaceChildren();
  });

  it("posts nothing before frame-ready, then the first location with the generation", async () => {
    const { frames, frame, from, lastLocation } = harness();
    frames.show(showing(), null);
    await vi.waitFor(() => expect(frame("exp_kb")).not.toBeNull());
    await vi.advanceTimersByTimeAsync(0);

    expect(frame("exp_kb")!.sent).toEqual([]);
    expect(frame("exp_kb")!.view.starting).toBe(true);

    from("exp_kb", { type: FRAME_READY_MESSAGE });
    expect(lastLocation("exp_kb")).toMatchObject({ tabId: "home", generation: "g1" });

    const { navigationId } = lastLocation("exp_kb");
    from("exp_kb", { type: LOCATION_ACCEPTED_MESSAGE, tabId: "home", navigationId });
    from("exp_kb", { type: LOCATION_RENDERED_MESSAGE, tabId: "home", navigationId, at: 1 });
    expect(frame("exp_kb")!.view.visible).toBe(true);

    // The accepted location never times out.
    await vi.advanceTimersByTimeAsync(LOCATION_ACCEPT_TIMEOUT_MS * 2);
    expect(frames.views()).toHaveLength(1);
  });

  it("shows tabs of one experience in one frame, each with its own location", async () => {
    const { frames, frame, from, lastLocation } = harness();
    frames.show(showing(), null);
    await vi.advanceTimersByTimeAsync(0);
    from("exp_kb", { type: FRAME_READY_MESSAGE });

    frames.hide("home");
    frames.show(
      showing({
        tabId: "note:a.md",
        viewId: "kb.assertion",
        context: { kind: "node", type: "Assertion", ref: { notePath: "a.md", kind: "NOTE" } },
      }),
      null,
    );
    await vi.advanceTimersByTimeAsync(0);

    expect(frames.views()).toHaveLength(1);
    const locations = frame("exp_kb")!.sent.filter((each) => each.type === LOCATION_MESSAGE);
    expect(locations.map((each) => each.tabId)).toEqual(["home", "note:a.md"]);
    expect(locations[1]?.generation).toBeUndefined();
    expect(locations[0]?.navigationId).not.toBe(locations[1]?.navigationId);
    // Switching tabs within the frame never suspends it.
    expect(frame("exp_kb")!.sent.some((each) => each.type === SUSPEND_MESSAGE)).toBe(false);

    // A UI param keeps the page's navigation; an identity param replaces it.
    const before = lastLocation("exp_kb").navigationId;
    frames.show(
      showing({
        tabId: "note:a.md",
        viewId: "kb.assertion",
        params: { sort: "x" },
        context: { kind: "node", type: "Assertion", ref: { notePath: "a.md", kind: "NOTE" } },
      }),
      null,
    );
    await vi.advanceTimersByTimeAsync(0);
    expect(lastLocation("exp_kb").navigationId).toBe(before);
    frames.show(
      showing({
        tabId: "note:a.md",
        viewId: "kb.assertion",
        params: { concept: "c" },
        context: { kind: "node", type: "Assertion", ref: { notePath: "a.md", kind: "NOTE" } },
      }),
      null,
    );
    await vi.advanceTimersByTimeAsync(0);
    expect(lastLocation("exp_kb").navigationId).not.toBe(before);

    // Returning to the first tab moves the frame back to its page.
    frames.hide("note:a.md");
    frames.show(showing(), null);
    await vi.advanceTimersByTimeAsync(0);
    expect(lastLocation("exp_kb")).toMatchObject({
      tabId: "home",
      navigationId: locations[0]?.navigationId,
    });

    frames.retainTabs(new Set(["home"]));
    expect(frame("exp_kb")!.sent.at(-1)).toEqual({ type: CLOSE_PAGE_MESSAGE, tabId: "note:a.md" });
  });

  it("replaces a frame that does not accept its location in time", async () => {
    const { frames, frame, from } = harness();
    frames.show(showing(), null);
    await vi.advanceTimersByTimeAsync(0);
    from("exp_kb", { type: FRAME_READY_MESSAGE });
    const first = frame("exp_kb")!.view.id;

    await vi.advanceTimersByTimeAsync(LOCATION_ACCEPT_TIMEOUT_MS - 1);
    expect(frame("exp_kb")!.view.id).toBe(first);
    await vi.advanceTimersByTimeAsync(1);
    await vi.advanceTimersByTimeAsync(0);

    expect(frames.views()).toHaveLength(1);
    expect(frame("exp_kb")!.view.id).not.toBe(first);
    expect(frame("exp_kb")!.view.starting).toBe(true);
  });

  it("suspends a hidden frame, forwards it no events, and resumes it with what changed", async () => {
    const { frames, frame, from } = harness();
    frames.setSession({ session: null });
    frames.show(showing(), null);
    await vi.advanceTimersByTimeAsync(0);
    from("exp_kb", { type: FRAME_READY_MESSAGE });

    frames.hide("home");
    await vi.advanceTimersByTimeAsync(0);
    const sent = frame("exp_kb")!.sent;
    expect(sent.at(-1)).toEqual({ type: SUSPEND_MESSAGE });
    expect(frame("exp_kb")!.view.visible || frame("exp_kb")!.view.starting).toBe(false);

    const count = sent.length;
    frames.vaultEvent({ event: "index.changed", data: JSON.stringify({ generation: "g2" }) });
    expect(sent).toHaveLength(count);

    // The same session is not sent again on resume.
    frames.show(showing(), null);
    expect(sent.slice(count).map((each) => each.type)).toEqual([RESUME_MESSAGE]);
    expect(sent.at(-1)).toEqual({ type: RESUME_MESSAGE, generation: "g2" });

    frames.hide("home");
    await vi.advanceTimersByTimeAsync(0);
    // SAFETY: the store compares only the read lifecycle's revision.
    frames.setSession({ session: null, readLifecycle: { revision: 3 } as EditReadLifecycle });
    const before = sent.length;
    frames.show(showing(), null);
    expect(sent.slice(before).map((each) => each.type)).toEqual([
      RESUME_MESSAGE,
      EDIT_SESSION_MESSAGE,
    ]);

    frames.vaultEvent({ event: "index.changed" });
    expect(sent.at(-1)).toMatchObject({ type: VAULT_EVENT_MESSAGE, event: "index.changed" });
  });

  it(`keeps at most ${RETAINED_FRAMES} suspended frames, evicting the least recently shown`, async () => {
    const { frames } = harness();

    for (let index = 0; index < RETAINED_FRAMES + 2; index += 1) {
      frames.show(showing({ experienceKey: `exp_${index}`, tabId: `view:${index}` }), null);
      await vi.advanceTimersByTimeAsync(0);
    }

    expect(frames.views().map((each) => each.experienceKey)).toEqual([
      "exp_1",
      "exp_2",
      "exp_3",
      "exp_4",
      "exp_5",
    ]);
  });

  it("runs an experience's panel in its own frame beside its page, updating one page per subject", async () => {
    const { frames, frame, from, lastLocation } = harness();
    frames.show(showing(), null);
    frames.show(panelShowing(), null);
    await vi.advanceTimersByTimeAsync(0);
    expect(frames.views().map((each) => each.panel)).toEqual([false, true]);
    expect(frame("exp_kb", true)!.view.src).toMatch(/[?&]frame=experience(&|$)/);
    expect(frame("exp_kb", true)!.view.src).toMatch(/[?&]hosted=1(&|$)/);

    from("exp_kb", { type: FRAME_READY_MESSAGE });
    from("exp_kb", { type: FRAME_READY_MESSAGE }, true);
    // The panel and the page both show: neither suspends the other.
    expect(frame("exp_kb")!.sent.map((each) => each.type)).not.toContain(SUSPEND_MESSAGE);
    const first = lastLocation("exp_kb", true);
    expect(first).toMatchObject({ tabId: "panel", generation: "g1" });

    frames.show(
      panelShowing({
        context: { kind: "node", type: "Assertion", ref: { notePath: "a.md", kind: "NOTE" } },
      }),
      null,
    );
    await vi.advanceTimersByTimeAsync(0);
    const next = lastLocation("exp_kb", true);
    expect(frames.views()).toHaveLength(2);
    expect(next).toMatchObject({ tabId: "panel", invocation: { context: { kind: "node" } } });
    expect(next.navigationId).not.toBe(first.navigationId);
    expect(next.generation).toBeUndefined();

    // Closing workbench tabs never closes the panel's page.
    frames.retainTabs(new Set());
    expect(frame("exp_kb", true)!.sent.map((each) => each.type)).not.toContain(CLOSE_PAGE_MESSAGE);
  });

  it(`suspends a hidden panel and counts it among the ${RETAINED_FRAMES} retained frames`, async () => {
    const { frames, frame, from } = harness();
    frames.show(panelShowing(), null);
    await vi.advanceTimersByTimeAsync(0);
    from("exp_kb", { type: FRAME_READY_MESSAGE }, true);
    frames.hide("panel");
    await vi.advanceTimersByTimeAsync(0);
    expect(frame("exp_kb", true)!.sent.at(-1)).toEqual({ type: SUSPEND_MESSAGE });

    frames.show(panelShowing(), null);
    expect(frame("exp_kb", true)!.sent.at(-1)).toMatchObject({ type: RESUME_MESSAGE });
    frames.hide("panel");
    await vi.advanceTimersByTimeAsync(0);

    // K pages shown after it leave the panel the least recently shown of K + 1.
    for (let index = 0; index < RETAINED_FRAMES; index += 1) {
      frames.show(showing({ experienceKey: `exp_${index}`, tabId: `view:${index}` }), null);
      await vi.advanceTimersByTimeAsync(0);
    }

    expect(frame("exp_kb", true)).not.toBeNull();
    frames.hide(`view:${RETAINED_FRAMES - 1}`);
    await vi.advanceTimersByTimeAsync(0);
    expect(frame("exp_kb", true)).toBeNull();
    expect(frames.views()).toHaveLength(RETAINED_FRAMES);
  });

  it("answers a stale page's stage with an error and acts only for the current page", async () => {
    const { frames, from, frame, lastLocation } = harness();
    frames.show(showing(), null);
    await vi.advanceTimersByTimeAsync(0);
    from("exp_kb", { type: FRAME_READY_MESSAGE });
    const old = lastLocation("exp_kb").navigationId;

    frames.show(showing({ context: { kind: "group", group: "Other" } }), null);
    await vi.advanceTimersByTimeAsync(0);
    const current = lastLocation("exp_kb").navigationId;
    expect(current).not.toBe(old);

    vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const stage = { type: STAGE_OPS_MESSAGE, requestId: "r1", ops: [{ op: "x" }], tabId: "home" };
    from("exp_kb", { ...stage, navigationId: old });
    from("exp_kb", { type: OPEN_NOTE_MESSAGE, path: "a.md", tabId: "home", navigationId: old });

    expect(frame("exp_kb")!.sent.at(-1)).toMatchObject({
      type: STAGE_RESULT_MESSAGE,
      requestId: "r1",
      error: expect.stringContaining("not staged"),
    });
    expect(services.onOpenNote).not.toHaveBeenCalled();

    from("exp_kb", { type: OPEN_NOTE_MESSAGE, path: "a.md", tabId: "home", navigationId: current });
    expect(services.onOpenNote).toHaveBeenCalledExactlyOnceWith("a.md", "activate");
  });

  it("posts the status generation after a reconnect and rebuilds a frame whose views changed", async () => {
    const { frames, frame, from } = harness();
    frames.show(showing(), null);
    await vi.advanceTimersByTimeAsync(0);
    from("exp_kb", { type: FRAME_READY_MESSAGE });

    frames.vaultEvent({ event: STREAM_RECONNECTED_EVENT });
    await vi.waitFor(() =>
      expect(frame("exp_kb")!.sent.at(-1)).toEqual({ type: GENERATION_MESSAGE, generation: "g9" }),
    );

    const id = frame("exp_kb")!.view.id;
    frames.vaultEvent({
      event: VIEWS_CHANGED_EVENT,
      data: JSON.stringify({ data: { folder: "elsewhere" } }),
    });
    expect(frame("exp_kb")!.view.id).toBe(id);

    frames.vaultEvent({
      event: VIEWS_CHANGED_EVENT,
      data: JSON.stringify({ data: { folder: "exp_kb/kb.home" } }),
    });
    expect(frame("exp_kb")!.view.id).not.toBe(id);
  });
});

describe("checkFolder", () => {
  it("names a repository or module view's folder as views.changed does", () => {
    expect(checkFolder({ check: "/views/_check/kb/home" })).toBe("kb/home");
    expect(checkFolder({ check: "/views/_module/ao-kb/_check/site%20views" })).toBe(
      "/module/ao-kb/site views",
    );
    expect(checkFolder({ check: "/views/_module/ao-kb/_check/" })).toBe("/module/ao-kb");
    expect(checkFolder({ check: "/views/_check/" })).toBe(".");
    expect(checkFolder({ check: "/views/_bundled/_check/kb" })).toBeNull();
    expect(checkFolder({})).toBeNull();
  });
});
