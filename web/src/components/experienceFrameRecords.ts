// The records the host's experience frames keep (SPEC-0136 R2, R3, R6): one
// per frame and one per page it holds, how a frame starts and how the layer
// sees it, and the parsing of what an invocation or a vault event says about
// them. The store in experienceFrames.ts owns their changes.
import { ApiError } from "../api/client";
import type { JsonObject } from "../api/parse";
import type { OntologyEditSessionResponse } from "../api/types";
import { isJsonObject, isString } from "../api/parse";
import type { ExperienceMark } from "../diagnostics/experienceMetrics";
import {
  EXPERIENCE_FRAME_PARAM,
  EXPERIENCE_FRAME_VALUE,
  type ExperienceLocation,
} from "../lib/customViewMessages";
import { editSessionFingerprint, type EditReadLifecycle } from "../staging/stagedQuery";
import { customViewHref, type ViewContext } from "../views/context";
import { viewParamsIdentity, type ViewParamDeclaration, type ViewParams } from "../views/params";
import type { BridgeServices } from "../views/viewBridge";

/** What the active tab of one experience shows. */
export type ExperienceShowing = {
  tabId: string;
  experienceKey: string;
  viewId: string;
  viewName: string;
  declarations: readonly ViewParamDeclaration[];
  context: ViewContext;
  preferenceSlot?: string;
  params: ViewParams;
  services: Omit<BridgeServices, "session" | "readLifecycle">;
  /** Whether the shell may write the page's params to the workspace URL. */
  ownsLocation: boolean;
  /** The experience's sidebar panel frame, not its page frame. */
  panel?: boolean;
};

/** Where phase times start, and the phases already recorded from there. */
export type PhaseClock = { startedAt: number; marked: Set<ExperienceMark> };

export type Page = PhaseClock & {
  navigationId: string;
  /** View, context, and identity params: a change replaces the page. */
  key: string;
  declarations: readonly ViewParamDeclaration[];
  services: ExperienceShowing["services"];
  /** The last location posted, so an unchanged one is not posted twice. */
  posted: string;
};

/** A frame's clock starts at its creation, for its frame-wide phases. */
export type Frame = PhaseClock & {
  id: number;
  /** The experience key, prefixed for a panel frame. */
  key: string;
  panel: boolean;
  experienceKey: string;
  src: string;
  title: string;
  window: Window | null;
  ready: boolean;
  /** The page the frame last painted; the frame shows only while that is the active tab's. */
  rendered: { tabId: string; navigationId: string } | null;
  suspended: boolean;
  shownAt: number;
  activeTab: string | null;
  showing: ExperienceShowing | null;
  pages: Map<string, Page>;
  pending: ExperienceLocation | null;
  /** The tab of the last location posted: the page the frame shows. */
  locatedTab: string | null;
  /** The tab of the last location the frame accepted: by then it has swapped to that page. */
  acceptedTab: string | null;
  awaiting: { navigationId: string; timer: ReturnType<typeof setTimeout> } | null;
  generationSent: boolean;
  /** The edit session last posted, compared by fingerprint and read revision. */
  sessionSent: string | null;
  /** View definition folders the frame has loaded, as views.changed names them. */
  folders: Set<string>;
  bundled: boolean;
};

/** One frame as the layer renders it. */
export type FrameView = {
  id: number;
  experienceKey: string;
  panel: boolean;
  src: string;
  title: string;
  /** Shown over the active tab's placeholder. */
  visible: boolean;
  /** Placed over the active placeholder but not yet painted, so hidden. */
  starting: boolean;
  /** The frame accepted its active tab's location, so it holds that tab's page. */
  onActivePage: boolean;
};

export const frameKey = ({
  experienceKey,
  panel,
}: Pick<ExperienceShowing, "experienceKey" | "panel">) =>
  panel ? `panel:${experienceKey}` : experienceKey;

// Epoch milliseconds, as the kit stamps its phase marks.
export const now = () => performance.timeOrigin + performance.now();

/**
 * The views.changed folder of a view, from its invocation's folder check URL
 * (`/views/_check/<folder>` or `/views/_module/<module>/_check/<folder>`).
 * Bundled views never change, so they name none.
 */
export function checkFolder({ check }: JsonObject): string | null {
  if (!isString(check)) return null;
  const match = /^\/views\/(?:_module\/([^/]+)\/)?_check\/(.*)$/.exec(check);

  if (!match || check.startsWith("/views/_bundled/")) return null;
  const folder = decodeURIComponent(match[2] ?? "");

  if (!match[1]) return folder || ".";
  const module = `/module/${decodeURIComponent(match[1])}`;

  return folder ? `${module}/${folder}` : module;
}

/** Whether one folder holds the other; "." is the flat views folder, which holds every flat one. */
export const nested = (a: string, b: string) =>
  a === b ||
  (a === "." && !b.startsWith("/")) ||
  (b === "." && !a.startsWith("/")) ||
  a.startsWith(`${b}/`) ||
  b.startsWith(`${a}/`);

export function changedFolder(data: string | undefined) {
  try {
    const payload: unknown = data ? JSON.parse(data) : null;
    const inner = isJsonObject(payload) ? payload.data : null;

    return isJsonObject(inner) && isString(inner.folder) ? inner.folder.replace(/\/+$/, "") : null;
  } catch {
    return null;
  }
}

export function eventGeneration(data: string | undefined) {
  try {
    const payload: unknown = data ? JSON.parse(data) : null;

    return isJsonObject(payload) && isString(payload.generation) ? payload.generation : undefined;
  } catch {
    return undefined;
  }
}

/** Why a view could not open, and whether the server refused it because the repository is no longer trusted. */
export type Refusal = { message: string; untrusted: boolean };

// oxlint-disable-next-line anti-slop/no-unknown-parameters -- A rejected request's reason is parsed here, once.
export const refusal = (error: unknown): Refusal => ({
  message: error instanceof Error ? error.message : String(error),
  untrusted: error instanceof ApiError && error.code === "VIEWS_UNTRUSTED",
});

/** A view invocation and the generation it was read at. */
export type Invocation = { invocation: JsonObject; generation?: string };

/** The workspace edit session the frames hear of. */
export type Session = {
  session: OntologyEditSessionResponse | null;
  readLifecycle?: EditReadLifecycle;
};

/** A session as a frame last heard it, compared by fingerprint and read revision. */
export const sessionKey = ({ session, readLifecycle }: Session) =>
  `${editSessionFingerprint(session)}\n${readLifecycle?.revision ?? ""}`;

/** A page's view, context, and identity params: a change replaces the page. */
export const pageKey = (showing: ExperienceShowing) =>
  JSON.stringify([
    showing.viewId,
    showing.context,
    viewParamsIdentity(showing.declarations, showing.params),
  ]);

/** A frame for an experience's first shown tab, not yet attached or located. */
export function newFrame(showing: ExperienceShowing, id: number): Frame {
  const href = customViewHref(showing.viewId, showing.context, { hosted: true });
  const url = new URL(href, "http://rhizome.invalid");
  url.searchParams.set(EXPERIENCE_FRAME_PARAM, EXPERIENCE_FRAME_VALUE);

  if (showing.preferenceSlot) url.searchParams.set("preferenceSlot", showing.preferenceSlot);

  return {
    startedAt: now(),
    marked: new Set(),
    id,
    key: frameKey(showing),
    panel: showing.panel === true,
    experienceKey: showing.experienceKey,
    src: `${url.pathname}${url.search}`,
    title: showing.viewName,
    window: null,
    ready: false,
    rendered: null,
    suspended: false,
    shownAt: 0,
    activeTab: null,
    showing: null,
    pages: new Map(),
    pending: null,
    locatedTab: null,
    acceptedTab: null,
    awaiting: null,
    generationSent: false,
    sessionSent: null,
    folders: new Set(),
    bundled: false,
  };
}

/** How the layer renders a frame: visible once it painted its active tab's page. */
export function frameView(frame: Frame): FrameView {
  const active = frame.activeTab !== null && !frame.suspended;
  const page = frame.activeTab === null ? undefined : frame.pages.get(frame.activeTab);

  const painted =
    frame.rendered !== null &&
    frame.rendered.tabId === frame.activeTab &&
    frame.rendered.navigationId === page?.navigationId;

  return {
    id: frame.id,
    experienceKey: frame.experienceKey,
    panel: frame.panel,
    src: frame.src,
    title: frame.title,
    visible: active && painted,
    starting: active && !painted,
    onActivePage: active && frame.acceptedTab === frame.activeTab,
  };
}
