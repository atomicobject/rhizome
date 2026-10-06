import { useEffect, useRef, type KeyboardEvent, type MouseEvent } from "react";

import type { NoteTab, Tab } from "./useNoteTabs";
import { displayTitle } from "../lib/labels";
import { searchTabLabel } from "./searchState";

function tabTitle(tab: Tab): string {
  if (tab.kind === "home") return "Home";

  if (tab.kind === "collection") return tab.collection === "issues" ? "Problems" : "Changes";

  if (tab.kind === "search") return searchTabLabel(tab.query, tab.filters.folder);

  if (tab.kind === "view") return tab.title;

  // Before a note loads its title, show the file name without the Markdown extension.
  return displayTitle(tab.title) || tab.path.split("/").at(-1)?.replace(/\.md$/i, "") || tab.path;
}

function accessibleName(tab: Tab): string {
  const parts = [tabTitle(tab)];

  if (tab.kind === "note" && tab.dirty) parts.push("unsaved changes");

  return parts.join(", ");
}

function noteTabClass(tab: NoteTab, active: boolean): string {
  return ["notes-tab", active ? "is-active" : "", tab.dirty ? "is-dirty" : ""]
    .filter(Boolean)
    .join(" ");
}

function tabClass(tab: Tab, active: boolean): string {
  if (tab.kind === "home") return `notes-tab notes-tab--pinned-home${active ? " is-active" : ""}`;

  if (tab.kind === "collection")
    return `notes-tab notes-tab--collection${active ? " is-active" : ""}`;

  if (tab.kind === "search") return `notes-tab notes-tab--search${active ? " is-active" : ""}`;

  if (tab.kind === "view") return `notes-tab notes-tab--view${active ? " is-active" : ""}`;

  return noteTabClass(tab, active);
}

export function NoteTabStrip({
  tabs,
  activeId,
  onActivate,
  onClose,
}: {
  tabs: Tab[];
  activeId: string;
  onActivate: (id: string) => void;
  onClose: (id: string) => void;
}) {
  const stripRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const strip = stripRef.current;

    if (!strip) return;

    const revealActive = () => {
      const active = strip.querySelector<HTMLElement>('[aria-selected="true"]');

      if (!active) return;
      const viewport = strip.getBoundingClientRect();
      const selected = active.getBoundingClientRect();
      // The pinned Home tab sticks over the strip's left edge.
      const pinned = strip.querySelector<HTMLElement>(".notes-tab--pinned-home");

      const left =
        !pinned || active === pinned ? viewport.left : pinned.getBoundingClientRect().right;

      if (selected.left < left) strip.scrollLeft += selected.left - left;
      else if (selected.right > viewport.right) strip.scrollLeft += selected.right - viewport.right;
    };

    revealActive();
    const observer = new ResizeObserver(revealActive);
    observer.observe(strip);

    return () => observer.disconnect();
  }, [activeId, tabs]);

  const activateAt = (index: number, elements: HTMLElement[]) => {
    const tab = tabs[index];

    if (!tab) return;
    onActivate(tab.id);
    elements[index]?.focus();
  };

  const closeAndRestoreFocus = (id: string) => {
    const index = tabs.findIndex((tab) => tab.id === id);
    const nextFocus = tabs[index + 1]?.id || tabs[index - 1]?.id || "home";
    onClose(id);
    window.requestAnimationFrame(() => {
      if (document.getElementById(`tab-${id}`)) return;
      document.getElementById(`tab-${nextFocus}`)?.focus();
    });
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const target = event.target;

    if (!(target instanceof HTMLElement) || target.getAttribute("role") !== "tab") return;
    const index = tabs.findIndex((tab) => tab.id === target.dataset.tabId);

    if (index < 0) return;
    const elements = Array.from(event.currentTarget.querySelectorAll<HTMLElement>("[role='tab']"));

    if (event.key === "ArrowRight") activateAt((index + 1) % tabs.length, elements);
    else if (event.key === "ArrowLeft")
      activateAt((index - 1 + tabs.length) % tabs.length, elements);
    else if (event.key === "Home") activateAt(0, elements);
    else if (event.key === "End") activateAt(tabs.length - 1, elements);
    else if (
      (event.key === "Delete" || event.key === "Backspace") &&
      tabs[index]?.kind !== "home"
    ) {
      closeAndRestoreFocus(tabs[index].id);
    } else return;
    event.preventDefault();
  };

  const closeTab = (event: MouseEvent<HTMLButtonElement>, id: string) => {
    event.stopPropagation();
    closeAndRestoreFocus(id);
  };

  return (
    <div
      ref={stripRef}
      className="notes-tabs"
      role="tablist"
      aria-label="Open notes"
      onKeyDown={handleKeyDown}
    >
      {tabs.map((tab) => {
        const active = tab.id === activeId;

        return (
          <div
            key={tab.id}
            id={`tab-${tab.id}`}
            role="tab"
            data-tab-id={tab.id}
            aria-label={accessibleName(tab)}
            aria-controls={`panel-${tab.id}`}
            aria-selected={active}
            tabIndex={active ? 0 : -1}
            className={tabClass(tab, active)}
            title={tab.kind === "home" ? "Home" : undefined}
            onClick={() => onActivate(tab.id)}
          >
            {tab.kind === "home" ? (
              <svg
                viewBox="0 0 16 16"
                aria-hidden="true"
                className="notes-tab__home-icon"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M2 7 8 2l6 5v6a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1z" />
                <path d="M6.25 14V9.5h3.5V14" />
              </svg>
            ) : (
              <span className="notes-tab__title">{tabTitle(tab)}</span>
            )}
            {tab.kind === "note" && tab.dirty ? (
              <span className="notes-tab__dirty" aria-hidden="true">
                ●
              </span>
            ) : null}
            {tab.kind !== "home" ? (
              <button
                type="button"
                className="notes-tab__close"
                tabIndex={-1}
                aria-label={`Close ${tabTitle(tab)}`}
                onClick={(event) => closeTab(event, tab.id)}
              >
                ×
              </button>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}
