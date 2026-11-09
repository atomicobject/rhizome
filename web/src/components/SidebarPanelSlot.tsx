// The Notes sidebar's one panel slot (SPEC-0136 R6): the navigation above,
// one panel below it in split, minimised, or cover state. The slot owns the
// shared panel rules (states, inert regions, focus, Escape, the divider, and
// persistence); a panel's content is the child it hosts.

import "../sidebar-panel.css";

import {
  createContext,
  type CSSProperties,
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import type { ViewTarget } from "../api/types";

type ViewPanel = ViewTarget["panel"];

import { useScopedViewPreference } from "../viewPreferences/hooks";
import type { ViewPreferenceScope } from "../viewPreferences/types";
import { DisclosureChevron } from "./DisclosureChevron";
import { PanelResizeHandle } from "./PanelResizeHandle";
import { clampWidth, type PanelRange } from "./shellPanes";

export const PANEL_HEIGHT_RANGE: PanelRange = { min: 120, max: 640, initial: 280 };

/** The Folder panel's identity, in place of an experience's. */
export const FOLDER_PANEL_IDENTITY = "builtin:folder";

/**
 * The identity a panel's split height and minimised state are kept under: its
 * experience, or the built-in Folder, never its subject or view ID.
 */
export function panelIdentity(panel: ViewPanel): string | null {
  return panel.kind === "view"
    ? `experience:${panel.experienceKey}`
    : panel.kind === "folder"
      ? FOLDER_PANEL_IDENTITY
      : null;
}

/**
 * The slot's own preference scope. A panel page's preferences live in the
 * same view ID without a slot (web/kit/preferences.ts), so the host's chrome
 * keeps to its own slot and the two never overwrite each other.
 */
export function panelPreferenceScope(identity: string): ViewPreferenceScope {
  return { viewId: identity, context: { kind: "standalone" }, slot: "rhizome-panel" };
}

const isHeight = (value: unknown): value is number =>
  typeof value === "number" &&
  Number.isFinite(value) &&
  value === clampWidth(value, PANEL_HEIGHT_RANGE);

const HEIGHT = { defaultValue: PANEL_HEIGHT_RANGE.initial, validate: isHeight };

const isBoolean = (value: unknown): value is boolean => typeof value === "boolean";

const MINIMISED = { defaultValue: false, validate: isBoolean };

const MINIMISED_BY_DEFAULT = { defaultValue: true, validate: isBoolean };

export type SidebarPanel = {
  /** From `panelIdentity`. */
  identity: string;
  title: string;
  /** Folder starts minimised in each vault until someone opens it. */
  minimisedByDefault?: boolean;
  /** The ID of the panel's frame, which lives outside the region so it never reloads. */
  owns?: string;
};

type PanelStyle = CSSProperties & { "--panel-height": string };

const PanelOpen = createContext(true);

/** False while the hosting panel is minimised, so its content can skip reads. */
export const usePanelOpen = () => useContext(PanelOpen);

export type PanelState = "split" | "minimised" | "cover";

/** Moves focus to `to` when `region` holds it, before the region hides. */
function rescueFocus(region: RefObject<HTMLElement | null>, to: RefObject<HTMLElement | null>) {
  if (region.current?.contains(document.activeElement)) to.current?.focus();
}

const errorIgnored = () => {
  // A failed save keeps the state on screen; the preference store reports it.
};

export function SidebarPanelSlot({
  vaultKey,
  panel,
  navigation,
  children,
}: {
  vaultKey: string | null;
  panel: SidebarPanel | null;
  /** The sidebar's normal navigation, hidden and inert while the panel covers it. */
  navigation: ReactNode;
  /** The panel's content. */
  children?: ReactNode;
}) {
  const [cover, setCover] = useState(false);
  const scope = useMemo(() => panelPreferenceScope(panel?.identity ?? "none"), [panel?.identity]);
  const key = panel ? vaultKey : null;
  const height = useScopedViewPreference(scope, key, "height", HEIGHT);

  const minimised = useScopedViewPreference(
    scope,
    key,
    "minimised",
    panel?.minimisedByDefault ? MINIMISED_BY_DEFAULT : MINIMISED,
  );

  const navRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLElement>(null);
  const titleRef = useRef<HTMLButtonElement>(null);
  const coverRef = useRef<HTMLButtonElement>(null);

  // A different panel (or none) starts in split; cover is never remembered.
  useEffect(() => setCover(false), [panel?.identity]);

  const state: PanelState | null = !panel
    ? null
    : minimised.value
      ? "minimised"
      : cover
        ? "cover"
        : "split";

  const covering = state === "cover";
  const panelStyle: PanelStyle = { "--panel-height": `${height.value}px` };

  const setMinimised = (next: boolean) => {
    if (next) {
      rescueFocus(bodyRef, titleRef);
      setCover(false);
    }

    void minimised.set(next).catch(errorIgnored);
  };

  const toggleCover = () => {
    if (!covering) rescueFocus(navRef, coverRef);

    if (minimised.value) setMinimised(false);
    setCover(!covering);
  };

  // Escape returns from cover to split after a child menu has had the chance
  // to consume it (React delivers the bubbling key here after the child).
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== "Escape" || event.defaultPrevented || !covering) return;
    event.preventDefault();
    setCover(false);
  };

  return (
    <div className={`sidebar-panel-slot${state ? ` is-${state}` : ""}`}>
      <div
        ref={navRef}
        className="sidebar-panel-slot__navigation"
        hidden={covering}
        inert={covering ? true : undefined}
      >
        {navigation}
      </div>
      {panel && state ? (
        <section
          ref={panelRef}
          className="sidebar-panel"
          aria-label={`${panel.title} panel`}
          aria-owns={state === "minimised" ? undefined : panel.owns}
          style={panelStyle}
          onKeyDown={onKeyDown}
        >
          {state === "split" ? (
            <PanelResizeHandle
              axis="y"
              label={`Resize ${panel.title} panel`}
              range={PANEL_HEIGHT_RANGE}
              value={height.value}
              edge="start"
              cssVar="--panel-height"
              target={panelRef}
              onResize={(next) => void height.set(next).catch(errorIgnored)}
            />
          ) : null}
          <div className="sidebar-panel__bar">
            <button
              ref={titleRef}
              type="button"
              className="sidebar-panel__title"
              aria-expanded={state !== "minimised"}
              onClick={() => setMinimised(state !== "minimised")}
            >
              <span className="sidebar-panel__caret" aria-hidden="true">
                <DisclosureChevron open={state !== "minimised"} />
              </span>
              {panel.title}
            </button>
            <button
              ref={coverRef}
              type="button"
              className="sidebar-panel__control"
              aria-label={`Fill the sidebar with ${panel.title}`}
              aria-pressed={covering}
              title={covering ? "Show navigation (Esc)" : "Fill the sidebar"}
              onClick={toggleCover}
            >
              <CoverIcon covering={covering} />
            </button>
          </div>
          <div
            ref={bodyRef}
            className="sidebar-panel__body"
            hidden={state === "minimised"}
            inert={state === "minimised" ? true : undefined}
          >
            <PanelOpen.Provider value={state !== "minimised"}>{children}</PanelOpen.Provider>
          </div>
        </section>
      ) : null}
    </div>
  );
}

// Arrows pointing out to fill the sidebar, or in to give it back.
function CoverIcon({ covering }: { covering: boolean }) {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d={covering ? "M5 2.5 8 5.5l3-3M5 13.5l3-3 3 3" : "M5 5.5 8 2.5l3 3M5 10.5l3 3 3-3"} />
    </svg>
  );
}
