// The host's experience frames (SPEC-0136 R2, R3): one long-lived frame per
// experience in a window, moved between tab pages with location messages,
// suspended while none of its pages shows, and evicted beyond K. An
// experience's sidebar panel (R6) is a frame of its own here, with one page
// (tab PANEL_TAB_ID), and counts toward the same K. The frame layer and the
// panel slot render what this store holds; the store owns every message.
import {
  recordExperienceCount,
  recordExperiencePhase,
  recordPagesRetained,
  type ExperienceMark,
} from "../diagnostics/experienceMetrics";
import {
  CLOSE_PAGE_MESSAGE,
  EDIT_SESSION_MESSAGE,
  GENERATION_MESSAGE,
  LOCATION_ACCEPT_TIMEOUT_MS,
  LOCATION_MESSAGE,
  RESUME_MESSAGE,
  SUSPEND_MESSAGE,
  VAULT_EVENT_MESSAGE,
  type ExperienceLocation,
  type HostMessage,
} from "../lib/customViewMessages";
import {
  STREAM_RECONNECTED_EVENT,
  VIEWS_CHANGED_EVENT,
  type VaultEvent,
} from "../query/vaultEvents";
import {
  isServiceRequest,
  messageStamp,
  pushViewParams,
  runServiceRequest,
  staleStageResult,
  viewRequest,
} from "../views/viewBridge";
import {
  changedFolder,
  checkFolder,
  eventGeneration,
  frameKey,
  frameView,
  nested,
  newFrame,
  now,
  pageKey,
  sessionKey,
  type ExperienceShowing,
  type Frame,
  type FrameView,
  type Invocation,
  type PhaseClock,
  type Session,
} from "./experienceFrameRecords";

/**
 * K: how many suspended frames the window keeps; the least recently shown beyond it are destroyed.
 * Set from measurement under SPEC-0136 R3's 150 MB retained-heap budget on 2026-11-09, in Chromium
 * through Playwright (the desktop webview could not be driven from the harness, and WebKit's heap
 * was not measured): the largest real AO KB (1,195 records), five
 * Knowledge-sized experiences each holding the KB overview, a concept, the Assertions list, and
 * eight assertion pages. Retained JS heap after GC: 8 MB with no experience, 38 MB with one frame,
 * about 28 MB per further frame, and 100 to 103 MB steady at K = 3 as frames are evicted; K = 4
 * measured 128 MB before eviction. Three leaves room for WebKit's unmeasured overhead.
 */
export const RETAINED_FRAMES = 3;

type Options = {
  fetchInvocation: (showing: ExperienceShowing) => Promise<Invocation>;
  fetchGeneration: () => Promise<string | undefined>;
};

export class ExperienceFrames {
  private frames = new Map<string, Frame>();
  private listeners = new Set<() => void>();
  private snapshot: FrameView[] = [];
  private nextId = 1;
  private clock = 0;
  private navigations = 0;
  private session: Session = { session: null };
  private generation: string | undefined;
  private invocations = new Map<string, Promise<Invocation> & { generation?: string }>();
  /** The latest location request per tab; an older one that settles later is dropped. */
  private requests = new Map<string, object>();
  private errors = new Map<string, string>();
  /** The active placeholder of the workbench (false) and of the panel slot (true). */
  private placeholders = new Map<boolean, HTMLElement | null>();

  constructor(private options: Options) {}

  // ---- what the layer renders --------------------------------------------

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);

    return () => this.listeners.delete(listener);
  };

  views = () => this.snapshot;

  /** The active placeholder, which the visible frame covers: the workbench's, or the panel slot's. */
  activePlaceholder = (panel = false) => this.placeholders.get(panel) ?? null;

  /** Why a tab's page could not be shown, if it could not. */
  error = (tabId: string) => this.errors.get(tabId) ?? null;

  private publish() {
    this.snapshot = [...this.frames.values()].sort((a, b) => a.id - b.id).map(frameView);

    for (const listener of this.listeners) listener();
  }

  /** The layer reports each frame's window once its iframe is in the document. */
  attach(id: number, target: Window | null) {
    for (const frame of this.frames.values()) if (frame.id === id) frame.window = target;
  }

  // ---- tabs ----------------------------------------------------------------

  /** A tab of an experience is on screen, at `placeholder`. */
  show(showing: ExperienceShowing, placeholder: HTMLElement | null) {
    const panel = showing.panel === true;
    this.placeholders.set(panel, placeholder);
    let frame = this.frames.get(frameKey(showing));

    if (!frame) frame = this.create(showing);

    // The workbench shows one page and the slot one panel, side by side.
    for (const other of this.frames.values())
      if (other !== frame && other.panel === panel && other.activeTab !== null) this.leave(other);

    const resuming = frame.suspended;
    frame.activeTab = showing.tabId;
    frame.showing = showing;
    this.clock += 1;
    frame.shownAt = this.clock;
    frame.suspended = false;

    if (resuming) recordExperienceCount(frame.experienceKey, "resumes");

    if (resuming && frame.ready) {
      this.post(frame, { type: RESUME_MESSAGE, generation: this.generation });
      this.postSession(frame);
    }

    this.publish();
    void this.locate(frame, showing);
  }

  /** A tab's page left the screen: its frame suspends unless another of its tabs shows. */
  hide(tabId: string) {
    const frame = [...this.frames.values()].find((each) => each.activeTab === tabId);

    if (!frame) return;
    frame.activeTab = null;
    this.requests.delete(tabId);

    // Another tab of the same experience may show in this same commit.
    queueMicrotask(() => {
      if (frame.activeTab === null && this.frames.get(frame.key) === frame) this.leave(frame);
    });
  }

  /** Drop the pages of tabs that closed. */
  retainTabs(open: ReadonlySet<string>) {
    for (const frame of this.frames.values()) {
      // The panel's page is not a workbench tab's.
      if (frame.panel) continue;

      for (const tabId of frame.pages.keys()) {
        if (open.has(tabId)) continue;
        frame.pages.delete(tabId);
        this.errors.delete(tabId);
        this.post(frame, { type: CLOSE_PAGE_MESSAGE, tabId });
      }
    }
  }

  private leave(frame: Frame) {
    frame.activeTab = null;

    if (!frame.suspended) {
      frame.suspended = true;
      this.post(frame, { type: SUSPEND_MESSAGE });
    }

    const suspended = [...this.frames.values()]
      .filter((each) => each.suspended)
      .sort((a, b) => b.shownAt - a.shownAt);

    for (const evicted of suspended.slice(RETAINED_FRAMES)) {
      recordExperienceCount(evicted.experienceKey, "evictions");
      this.destroy(evicted);
    }

    this.publish();
  }

  // ---- frames ----------------------------------------------------------------

  private create(showing: ExperienceShowing): Frame {
    const frame = newFrame(showing, this.nextId++);

    this.frames.set(frame.key, frame);
    recordExperienceCount(showing.experienceKey, "frames_created");

    return frame;
  }

  private destroy(frame: Frame) {
    if (frame.awaiting) clearTimeout(frame.awaiting.timer);
    frame.awaiting = null;

    if (this.frames.get(frame.key) === frame) this.frames.delete(frame.key);
  }

  /** Replace a frame with a fresh one at the location its active tab shows. */
  private rebuild(frame: Frame) {
    const showing = frame.activeTab === null ? null : frame.showing;
    this.destroy(frame);

    if (showing) this.show(showing, this.activePlaceholder(frame.panel));
    else this.publish();
  }

  /** Destroy every frame, as on a trust or vault change. */
  clear() {
    for (const frame of Array.from(this.frames.values())) this.destroy(frame);
    this.publish();
  }

  // ---- locations -------------------------------------------------------------

  private invocation(showing: ExperienceShowing) {
    const key = JSON.stringify([showing.viewId, showing.context, showing.preferenceSlot ?? ""]);
    const cached = this.invocations.get(key);

    if (cached && (this.generation === undefined || cached.generation === this.generation))
      return cached;

    const request: Promise<Invocation> & { generation?: string } = this.options
      .fetchInvocation(showing)
      .then((result) => {
        request.generation = result.generation;

        if (result.generation !== undefined) this.generation = result.generation;

        return result;
      });

    request.catch(() => this.invocations.delete(key));
    this.invocations.set(key, request);

    return request;
  }

  private async locate(frame: Frame, showing: ExperienceShowing) {
    const { tabId } = showing;
    const startedAt = now();
    const token = {};
    this.requests.set(tabId, token);

    const current = () =>
      this.requests.get(tabId) === token && this.frames.get(frame.key) === frame;

    let result: Invocation;

    try {
      result = await this.invocation(showing);
    } catch (error) {
      if (!current()) return;
      this.errors.set(tabId, error instanceof Error ? error.message : String(error));

      // A refused invocation, such as revoked trust, ends the frame.
      if (frame.pages.size === 0) this.destroy(frame);
      this.publish();

      return;
    }

    // A newer location for this tab, or another frame for the experience, wins.
    if (!current()) return;
    this.errors.delete(tabId);

    const key = pageKey(showing);

    const existing = frame.pages.get(tabId);
    const reused = existing?.key === key ? existing : undefined;
    let navigationId = reused?.navigationId;

    if (navigationId) recordExperienceCount(frame.experienceKey, "locations_reused");
    else {
      this.navigations += 1;
      navigationId = `n${this.navigations}`;
    }

    const location: ExperienceLocation = {
      navigationId,
      tabId,
      invocation: result.invocation,
      params: showing.params,
    };

    const folder = checkFolder(result.invocation);

    if (folder) frame.folders.add(folder);
    const posted = JSON.stringify([navigationId, showing.params, frame.id]);

    // An unchanged location for the page the frame already shows needs no message.
    if (existing?.posted === posted && frame.locatedTab === tabId) {
      frame.pages.set(tabId, { ...existing, services: showing.services });

      return;
    }

    frame.pages.set(tabId, {
      // A page updated in place keeps its navigation's clock.
      startedAt: reused?.startedAt ?? startedAt,
      marked: reused?.marked ?? new Set(),
      navigationId,
      key,
      declarations: showing.declarations,
      services: showing.services,
      posted,
    });
    recordPagesRetained(frame.experienceKey, frame.pages.size);

    if (!frame.generationSent && result.generation !== undefined)
      location.generation = result.generation;

    if (frame.ready) this.postLocation(frame, location);
    else frame.pending = location;
    this.publish();
  }

  private postLocation(frame: Frame, location: ExperienceLocation) {
    if (location.generation !== undefined) frame.generationSent = true;
    frame.locatedTab = location.tabId;
    this.post(frame, { type: LOCATION_MESSAGE, ...location });

    if (frame.awaiting) clearTimeout(frame.awaiting.timer);

    const timer = setTimeout(() => {
      if (frame.awaiting?.navigationId === location.navigationId) this.rebuild(frame);
    }, LOCATION_ACCEPT_TIMEOUT_MS);

    frame.awaiting = { navigationId: location.navigationId, timer };
  }

  // ---- messages --------------------------------------------------------------

  private post(frame: Frame, message: HostMessage) {
    if (frame.ready) frame.window?.postMessage(message, window.location.origin);
  }

  private postSession(frame: Frame) {
    const key = sessionKey(this.session);

    if (!frame.ready || frame.sessionSent === key) return;
    frame.sessionSent = key;
    this.post(frame, { type: EDIT_SESSION_MESSAGE, ...this.session });
  }

  /** The workspace edit session changed; suspended frames hear of it when they resume. */
  setSession(session: Session) {
    this.session = session;

    for (const frame of this.frames.values()) if (!frame.suspended) this.postSession(frame);
  }

  /** An event from the host's vault stream, for every frame that is not suspended. */
  vaultEvent({ event, data }: VaultEvent) {
    const generation = eventGeneration(data);

    if (generation !== undefined) this.generation = generation;

    if (event === VIEWS_CHANGED_EVENT) {
      const folder = changedFolder(data);

      for (const frame of Array.from(this.frames.values())) {
        if (folder !== null && [...frame.folders].some((each) => nested(each, folder)))
          this.rebuild(frame);
      }
    }

    for (const frame of this.frames.values()) {
      if (frame.suspended) continue;
      const message: HostMessage = { type: VAULT_EVENT_MESSAGE, event };

      if (data !== undefined) message.data = data;

      if (generation !== undefined) message.generation = generation;
      this.post(frame, message);
    }

    if (event === STREAM_RECONNECTED_EVENT) void this.reconnected();
  }

  // Events missed during the outage are not replayed, so every frame learns
  // the current generation from the status API.
  private async reconnected() {
    const generation = await this.options.fetchGeneration().catch(() => undefined);

    if (generation === undefined) return;
    this.generation = generation;

    for (const frame of this.frames.values())
      if (!frame.suspended) this.post(frame, { type: GENERATION_MESSAGE, generation });
  }

  /** Records a phase once per frame or navigation, as time since its clock started. */
  private mark(frame: Frame, clock: PhaseClock, mark: ExperienceMark, at: number) {
    if (clock.marked.has(mark)) return;
    clock.marked.add(mark);
    recordExperiencePhase(frame.experienceKey, mark, at - clock.startedAt);
  }

  private painted(frame: Frame, tabId: string, navigationId: string, at: number) {
    const page = frame.pages.get(tabId);
    const current = page?.navigationId === navigationId;

    if (page && current) this.mark(frame, page, "location-painted", at);

    if (
      !current ||
      (frame.rendered?.tabId === tabId && frame.rendered.navigationId === navigationId)
    )
      return;
    frame.rendered = { tabId, navigationId };
    this.publish();
  }

  /** A message on the host window; ignored unless one of these frames sent it. */
  receive(event: MessageEvent) {
    if (event.origin !== window.location.origin) return;
    const frame = [...this.frames.values()].find((each) => each.window === event.source);

    if (!frame) return;
    const stamp = messageStamp(event.data);
    const page = stamp.tabId === undefined ? undefined : frame.pages.get(stamp.tabId);
    const request = viewRequest(event.data, page?.declarations ?? []);

    if (!request) return;

    switch (request.kind) {
      case "frame-ready":
        frame.ready = true;

        // A frame suspended while it started waits for its next location.
        if (frame.suspended) this.post(frame, { type: SUSPEND_MESSAGE });
        else if (frame.pending) this.postLocation(frame, frame.pending);
        frame.pending = null;

        if (!frame.suspended) this.postSession(frame);

        return;
      case "location-accepted":
        if (frame.awaiting?.navigationId === request.navigationId) {
          clearTimeout(frame.awaiting.timer);
          frame.awaiting = null;
        }

        return;
      case "location-rendered":
        this.painted(frame, request.tabId, request.navigationId, request.at);

        return;
      case "phase":
        // A page's mark counts only while its navigation is current.
        if (stamp.tabId === undefined) this.mark(frame, frame, request.phase, request.at);
        else if (page && page.navigationId === stamp.navigationId)
          this.mark(frame, page, request.phase, request.at);

        return;
      case "rendered":
        return;
      default:
        break;
    }

    const reply = (message: HostMessage) => this.post(frame, message);

    // Only the page whose navigation is current for its tab may act.
    if (!page || page.navigationId !== stamp.navigationId) {
      if (request.kind === "hello") reply({ type: EDIT_SESSION_MESSAGE, ...this.session });
      else if (request.kind === "stage") reply(staleStageResult(request.requestId));
      else console.warn(`[Rhizome experience] Rejected ${request.kind} from a stale page`);

      return;
    }

    if (request.kind === "params") {
      if (frame.activeTab === stamp.tabId && frame.showing?.ownsLocation)
        pushViewParams(page.declarations, request.patch);

      return;
    }

    if (isServiceRequest(request))
      runServiceRequest(request, { ...page.services, ...this.session }, reply);
  }
}
