import { type CSSProperties, type ReactNode, useEffect, useRef } from "react";

import { typingIn } from "./KeyboardShortcuts";
import { PanelResizeHandle } from "./PanelResizeHandle";
import { SIDEBAR_RANGE } from "./shellPanes";
import { setSidebarWidth, toggleSidebar, useWorkbenchSidebar } from "./sidebarState";

const IS_MAC = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

/** The sidebar shortcuts as the toggle's tooltip names them. */
export const SIDEBAR_SHORTCUTS = IS_MAC ? "⌃⌘S or ⌘\\" : "Ctrl+Alt+S or Ctrl+\\";

export function PanelIcon({ side }: { side: "left" | "right" }) {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinejoin="round"
    >
      <rect x="2" y="3" width="12" height="10" rx="1.5" />
      <path d={side === "left" ? "M6 3v10" : "M10 3v10"} />
    </svg>
  );
}

/**
 * ⌃⌘S or ⌘\ (Ctrl+Alt+S or Ctrl+\ elsewhere). In Rhizome Desktop ⌃⌘S is the
 * View menu's, so the page answers only ⌘\ there.
 */
export function isSidebarShortcut(event: KeyboardEvent, desktop = false): boolean {
  if (event.shiftKey || event.isComposing) return false;

  const primary = IS_MAC ? event.metaKey : event.ctrlKey;

  const backslash =
    event.code === "Backslash" && primary && !event.altKey && (IS_MAC ? !event.ctrlKey : true);

  const letterS =
    !desktop &&
    event.code === "KeyS" &&
    (IS_MAC
      ? event.metaKey && event.ctrlKey && !event.altKey
      : event.ctrlKey && event.altKey && !event.metaKey);

  return backslash || letterS;
}

/** Toggles the sidebar from the keyboard, outside text fields, unless `disabled`. */
export function useSidebarShortcut({ desktop, disabled }: { desktop: boolean; disabled: boolean }) {
  useEffect(() => {
    if (disabled) return;

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || typingIn(event.target) || !isSidebarShortcut(event, desktop))
        return;
      event.preventDefault();
      toggleSidebar();
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, [desktop, disabled]);
}

/** The top bar's sidebar toggle. Disabled where a section has no sidebar. */
export function SidebarToggle({ disabled = false }: { disabled?: boolean }) {
  const { collapsed } = useWorkbenchSidebar();
  const label = collapsed ? "Show sidebar" : "Hide sidebar";

  return (
    <button
      type="button"
      className="app-shell__sidebar-toggle"
      aria-label={label}
      aria-expanded={disabled ? undefined : !collapsed}
      aria-keyshortcuts={
        IS_MAC ? "Control+Meta+S Meta+Backslash" : "Control+Alt+S Control+Backslash"
      }
      title={disabled ? "This section has no sidebar" : `${label} (${SIDEBAR_SHORTCUTS})`}
      disabled={disabled}
      onClick={toggleSidebar}
    >
      <PanelIcon side="left" />
    </button>
  );
}

type SidebarStyle = CSSProperties & { "--sidebar-width": string };

/**
 * The one sidebar frame every section shares (SPEC-0128): the themed column,
 * its header, and its resize handle. Each section passes its own contents.
 * While collapsed it stays mounted, hidden and inert, so section state such
 * as expanded groups survives.
 */
export function WorkbenchSidebar({
  title,
  titleHint,
  children,
}: {
  /** Names the vault or repository; Desktop's toolbar names it instead. */
  title?: string | null;
  titleHint?: string | null;
  children: ReactNode;
}) {
  const { collapsed, width } = useWorkbenchSidebar();
  const ref = useRef<HTMLDivElement>(null);
  const style: SidebarStyle = { "--sidebar-width": `${width}px` };

  return (
    <div
      ref={ref}
      className="workbench-sidebar"
      style={style}
      hidden={collapsed}
      inert={collapsed ? true : undefined}
    >
      {title ? (
        <div className="workbench-sidebar__header" title={titleHint ?? undefined}>
          {title}
        </div>
      ) : null}
      <div className="workbench-sidebar__body">{children}</div>
      <PanelResizeHandle
        label="Resize sidebar"
        range={SIDEBAR_RANGE}
        value={width}
        edge="end"
        cssVar="--sidebar-width"
        target={ref}
        onResize={setSidebarWidth}
      />
    </div>
  );
}
