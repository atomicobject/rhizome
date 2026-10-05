import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import type { ViewChoice, ViewTarget } from "../api/types";
import { usePopoverDismiss } from "../components/focusManagement";

const MENU = "menu";

/** Moves focus among `items` for the arrow keys named, Home, and End. */
function moveFocus(event: KeyboardEvent, items: HTMLElement[], back: string, forward: string) {
  const current = items.findIndex((item) => item === event.target);

  const next =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? items.length - 1
        : event.key === back
          ? (current - 1 + items.length) % items.length
          : event.key === forward
            ? (current + 1) % items.length
            : null;

  if (next === null || current < 0) return;
  event.preventDefault();
  items[next]?.focus();
}

function CustomViewIcon() {
  return (
    <svg
      className="view-selector__icon"
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinejoin="round"
    >
      <rect x="2.5" y="2.5" width="4.5" height="6" rx="1" />
      <rect x="9" y="2.5" width="4.5" height="3.5" rx="1" />
      <rect x="9" y="8.5" width="4.5" height="5" rx="1" />
      <rect x="2.5" y="10.5" width="4.5" height="3" rx="1" />
    </svg>
  );
}

/** Persistence state of the remembered selection, from `useViewSelection`. */
export type ViewSelectionStatus = {
  pending: boolean;
  error: Error | null;
  retry: () => Promise<void>;
};

/**
 * Switches a workspace between its views: standard presentations as segments,
 * one custom view as a segment of its own, and several behind a menu. The
 * target's default view carries a dot.
 */
export function ViewSelector({
  target,
  selectedId,
  onSelect,
  status,
}: {
  target: Pick<ViewTarget, "choices" | "defaultChoiceId">;
  selectedId: string | null;
  onSelect: (id: string) => void;
  status?: ViewSelectionStatus;
}) {
  // Fixed placement escapes toolbars that clip their overflow.
  const [menuPlace, setMenuPlace] = useState<{ top: number; right: number } | null>(null);
  const open = menuPlace !== null;
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const defaultNoteId = useId();
  const close = useCallback(() => setMenuPlace(null), []);
  usePopoverDismiss(open, rootRef, close);

  useEffect(() => {
    if (!open) return;

    const items = [
      ...(menuRef.current?.querySelectorAll<HTMLElement>("[role=menuitemradio]") ?? []),
    ];

    (items.find((item) => item.getAttribute("aria-checked") === "true") ?? items[0])?.focus();
  }, [open]);

  const { choices, defaultChoiceId } = target;

  if (choices.length < 2) return null;

  const custom = choices.filter((choice) => choice.custom);
  const useMenu = custom.length > 1;
  const segments = useMenu ? choices.filter((choice) => !choice.custom) : choices;
  const selectedInMenu = useMenu ? custom.find((choice) => choice.id === selectedId) : undefined;
  const keys = [...segments.map((choice) => choice.id), ...(useMenu ? [MENU] : [])];

  // One Tab stop: the segment last focused, else the selected one.
  const tabStop =
    focusKey && keys.includes(focusKey)
      ? focusKey
      : selectedInMenu
        ? MENU
        : keys.includes(selectedId ?? "")
          ? selectedId
          : keys[0];

  const defaultProps = (choice: ViewChoice) =>
    choice.id === defaultChoiceId
      ? { title: `${choice.name} (default view)`, "aria-describedby": defaultNoteId }
      : { title: choice.name };

  const content = (choice: ViewChoice) => (
    <>
      {choice.custom && <CustomViewIcon />}
      <span className="view-selector__label">{choice.name}</span>
      {choice.id === defaultChoiceId && <span className="view-selector__default" />}
    </>
  );

  const openMenu = () => {
    const rect = triggerRef.current?.getBoundingClientRect();

    if (rect) setMenuPlace({ top: rect.bottom + 4, right: window.innerWidth - rect.right });
  };

  // Re-picking the default still reports it, which clears a remembered choice.
  const choose = (id: string) => {
    close();

    if (id !== selectedId || id === target.defaultChoiceId) onSelect(id);
  };

  const pick = (id: string) => {
    choose(id);
    triggerRef.current?.focus();
  };

  return (
    <div className="view-selector" ref={rootRef}>
      <div
        className="segmented"
        role="toolbar"
        aria-label="Workspace view"
        onKeyDown={(event) => {
          const buttons = [...event.currentTarget.querySelectorAll<HTMLElement>(":scope > button")];
          moveFocus(event, buttons, "ArrowLeft", "ArrowRight");
        }}
      >
        {segments.map((choice) => (
          <button
            key={choice.id}
            type="button"
            aria-pressed={choice.id === selectedId}
            tabIndex={tabStop === choice.id ? 0 : -1}
            onFocus={() => setFocusKey(choice.id)}
            onClick={() => choose(choice.id)}
            {...defaultProps(choice)}
          >
            {content(choice)}
          </button>
        ))}
        {useMenu && (
          <button
            ref={triggerRef}
            type="button"
            className="view-selector__trigger"
            aria-pressed={selectedInMenu !== undefined}
            aria-haspopup="menu"
            aria-expanded={open}
            aria-controls={open ? menuId : undefined}
            tabIndex={tabStop === MENU ? 0 : -1}
            onFocus={() => setFocusKey(MENU)}
            onClick={() => (open ? close() : openMenu())}
            onKeyDown={(event) => {
              if (event.key !== "ArrowDown") return;
              event.preventDefault();
              openMenu();
            }}
            {...(selectedInMenu ? defaultProps(selectedInMenu) : { title: "Custom views" })}
          >
            {selectedInMenu ? (
              content(selectedInMenu)
            ) : (
              <>
                <CustomViewIcon />
                <span className="view-selector__label">Views</span>
              </>
            )}
            <svg
              className="view-selector__chevron"
              viewBox="0 0 16 16"
              aria-hidden="true"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M4.5 6.5 8 10l3.5-3.5" />
            </svg>
          </button>
        )}
      </div>
      {menuPlace && (
        <div
          ref={menuRef}
          id={menuId}
          className="view-selector__menu"
          style={menuPlace}
          role="menu"
          aria-label="Custom views"
          onKeyDown={(event) => {
            const items = [
              ...event.currentTarget.querySelectorAll<HTMLElement>("[role=menuitemradio]"),
            ];

            moveFocus(event, items, "ArrowUp", "ArrowDown");
          }}
        >
          {custom.map((choice) => (
            <button
              key={choice.id}
              type="button"
              role="menuitemradio"
              aria-checked={choice.id === selectedId}
              tabIndex={-1}
              onClick={() => pick(choice.id)}
              {...defaultProps(choice)}
            >
              <svg
                className="view-selector__check"
                viewBox="0 0 16 16"
                aria-hidden="true"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.75"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="m3.5 8.5 3 3 6-7" />
              </svg>
              <span className="view-selector__label">{choice.name}</span>
              {choice.id === defaultChoiceId && <span className="view-selector__default" />}
            </button>
          ))}
        </div>
      )}
      {status?.error && (
        <span className="view-preferences view-preferences--error" role="alert">
          {status.pending ? "View choice not saved." : "Saved view choice could not load."}
          <button type="button" onClick={() => void status.retry().catch(() => {})}>
            Retry
          </button>
        </span>
      )}
      <span id={defaultNoteId} hidden>
        Default view
      </span>
    </div>
  );
}
