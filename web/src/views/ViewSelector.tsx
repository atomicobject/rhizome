import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import type { ViewChoice, ViewTarget } from "../api/types";
import { usePopoverDismiss } from "../components/focusManagement";

const MORE = "more";

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

function Chevron() {
  return (
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
  );
}

/**
 * How many of the choices, `widths` wide in visual order, fit inline in
 * `available`. When some do not, the trigger takes room after the inline ones:
 * `selectedMore` wide when it names the selected choice (at `selected`), else
 * `more` wide.
 */
function inlineCount(
  widths: number[],
  { gap, available, more, selectedMore, selected }: Record<string, number>,
) {
  // Half a pixel absorbs rounding in widths that sum to the space exactly.
  const room = available + 0.5;
  let used = -gap;
  const ends = widths.map((width) => (used += gap + width));

  if ((ends.at(-1) ?? 0) <= room) return widths.length;

  for (let count = widths.length - 1; count > 0; count--) {
    if (ends[count - 1] + gap + (selected >= count ? selectedMore : more) <= room) return count;
  }

  return 0;
}

const px = (value: string) => Number.parseFloat(value) || 0;

const width = (element: Element | null) => element?.getBoundingClientRect().width ?? 0;

/** An element's width with its horizontal margins. */
function outerWidth(element: HTMLElement | null) {
  if (!element) return 0;
  const style = getComputedStyle(element);

  return width(element) + px(style.marginLeft) + px(style.marginRight);
}

/** Persistence state of the remembered selection, from `useViewSelection`. */
export type ViewSelectionStatus = {
  pending: boolean;
  error: Error | null;
  retry: () => Promise<void>;
};

/**
 * Switches a workspace between its views: custom choices first, then standard
 * ones, as many inline as the measured width holds, and the rest behind
 * "More". The selected view is always visible, inline or named on the
 * trigger. The default view says so in its tooltip and, in the menu, with a
 * quiet label; tabs carry no unexplained marks.
 */
export function ViewSelector({
  target,
  selectedId,
  onSelect,
  status,
  trailing,
}: {
  target: Pick<ViewTarget, "choices" | "defaultChoiceId">;
  selectedId: string | null;
  onSelect: (id: string) => void;
  status?: ViewSelectionStatus;
  /** Controls for the selected view, such as "Reset to shared", in the selector's row. */
  trailing?: ReactNode;
}) {
  // Fixed placement escapes toolbars that clip their overflow.
  const [menuPlace, setMenuPlace] = useState<{ top: number; right: number } | null>(null);
  const [focusKey, setFocusKey] = useState<string | null>(null);
  // How many choices fit inline, for the choice list it was measured with: a
  // switcher sized by its content (the note bar's) never measures itself
  // smaller than what it shows, so a new list starts with every choice inline.
  const [fit, setFit] = useState<{ ids: string; count: number } | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const toolbarRef = useRef<HTMLDivElement>(null);
  const rulerRef = useRef<HTMLDivElement>(null);
  const trailingRef = useRef<HTMLSpanElement>(null);
  const statusRef = useRef<HTMLSpanElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const refocus = useRef(false);
  const measureRef = useRef(() => {});
  const menuId = useId();
  const defaultNoteId = useId();
  const close = useCallback(() => setMenuPlace(null), []);

  const { choices, defaultChoiceId } = target;
  const switcher = choices.length > 1;

  // Rank by the custom flag, not the renderer: bundled standard views render as custom.
  const ordered = [
    ...choices.filter((choice) => choice.custom),
    ...choices.filter((choice) => !choice.custom),
  ];

  const ids = ordered.map((choice) => choice.id).join("\n");
  const count = fit?.ids === ids ? fit.count : ordered.length;
  const inline = ordered.slice(0, count);
  const overflow = ordered.slice(count);
  const open = menuPlace !== null && overflow.length > 0;
  usePopoverDismiss(open, rootRef, close);

  measureRef.current = () => {
    const ruler = rulerRef.current;

    if (!rootRef.current || !ruler) return;
    const style = getComputedStyle(ruler);
    const [more, selectedMore, ...widths] = [...ruler.children].map(width);

    const next = inlineCount(widths, {
      gap: px(style.columnGap),
      available:
        width(rootRef.current) -
        outerWidth(trailingRef.current) -
        outerWidth(statusRef.current) -
        px(style.paddingLeft) -
        px(style.paddingRight) -
        px(style.borderLeftWidth) -
        px(style.borderRightWidth),
      more,
      selectedMore,
      selected: ordered.findIndex((choice) => choice.id === selectedId),
    });

    if (next === count) return;
    // Removing the focused choice drops focus; the next render restores it.
    refocus.current = rootRef.current.contains(document.activeElement);
    setFit({ ids, count: next });
  };

  useLayoutEffect(() => {
    if (refocus.current) {
      refocus.current = false;

      if (!rootRef.current?.contains(document.activeElement))
        toolbarRef.current?.querySelector<HTMLElement>(':scope > [tabindex="0"]')?.focus();
    }

    measureRef.current();
  });

  useEffect(() => {
    const observer = new ResizeObserver(() => measureRef.current());

    for (const element of [rootRef.current, rulerRef.current, trailingRef.current])
      if (element) observer.observe(element);

    return () => observer.disconnect();
  }, [switcher, Boolean(trailing), Boolean(status?.error)]);

  useEffect(() => {
    if (!open) return;

    const items = [
      ...(menuRef.current?.querySelectorAll<HTMLElement>("[role=menuitemradio]") ?? []),
    ];

    (items.find((item) => item.getAttribute("aria-checked") === "true") ?? items[0])?.focus();
  }, [open]);

  // One choice needs no switcher, but its controls (Reset to shared) remain.
  if (!switcher)
    return trailing ? (
      <div className="view-selector">
        <span className="view-selector__trailing">{trailing}</span>
      </div>
    ) : null;

  const selected = ordered.find((choice) => choice.id === selectedId);
  const selectedInMenu = overflow.find((choice) => choice.id === selectedId);
  const iconColumn = overflow.some((choice) => choice.custom);
  const keys = [...inline.map((choice) => choice.id), ...(overflow.length ? [MORE] : [])];

  const stopFor = (id: string | null) =>
    id === null
      ? null
      : keys.includes(id)
        ? id
        : overflow.some((choice) => choice.id === id)
          ? MORE
          : null;

  // One Tab stop: the segment last focused, else the selected one; a choice
  // that moved into the menu hands it to the trigger.
  const tabStop = stopFor(focusKey) ?? stopFor(selectedId) ?? keys[0];

  const defaultProps = (choice: ViewChoice) =>
    choice.id === defaultChoiceId
      ? { title: `${choice.name} (default view)`, "aria-describedby": defaultNoteId }
      : { title: choice.name };

  const content = (choice: ViewChoice) => (
    <>
      {choice.custom && <CustomViewIcon />}
      <span className="view-selector__label">{choice.name}</span>
    </>
  );

  const triggerContent = (choice: ViewChoice | undefined) => (
    <>
      {choice ? content(choice) : <span className="view-selector__label">More</span>}
      <Chevron />
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
        ref={toolbarRef}
        className="view-tabs"
        role="toolbar"
        aria-label="Workspace view"
        onKeyDown={(event) => {
          const buttons = [...event.currentTarget.querySelectorAll<HTMLElement>(":scope > button")];
          moveFocus(event, buttons, "ArrowLeft", "ArrowRight");
        }}
      >
        {inline.map((choice) => (
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
        {overflow.length > 0 && (
          <button
            ref={triggerRef}
            type="button"
            className="view-selector__trigger"
            aria-pressed={selectedInMenu !== undefined}
            aria-haspopup="menu"
            aria-expanded={open}
            aria-controls={open ? menuId : undefined}
            tabIndex={tabStop === MORE ? 0 : -1}
            onFocus={() => setFocusKey(MORE)}
            onClick={() => (open ? close() : openMenu())}
            onKeyDown={(event) => {
              if (event.key !== "ArrowDown") return;
              event.preventDefault();
              openMenu();
            }}
            {...(selectedInMenu ? defaultProps(selectedInMenu) : { title: "More views" })}
          >
            {triggerContent(selectedInMenu)}
          </button>
        )}
      </div>
      {/* Every choice and both trigger labels at their natural widths, to measure what fits. */}
      <div className="view-selector__ruler" aria-hidden="true" inert>
        <div ref={rulerRef} className="view-tabs">
          <button type="button" tabIndex={-1}>
            {triggerContent(undefined)}
          </button>
          <button type="button" tabIndex={-1} aria-pressed={selected !== undefined}>
            {triggerContent(selected)}
          </button>
          {ordered.map((choice) => (
            <button
              key={choice.id}
              type="button"
              tabIndex={-1}
              aria-pressed={choice.id === selectedId}
            >
              {content(choice)}
            </button>
          ))}
        </div>
      </div>
      {open && (
        <div
          ref={menuRef}
          id={menuId}
          className="view-selector__menu"
          style={menuPlace}
          role="menu"
          aria-label="More views"
          onKeyDown={(event) => {
            const items = [
              ...event.currentTarget.querySelectorAll<HTMLElement>("[role=menuitemradio]"),
            ];

            moveFocus(event, items, "ArrowUp", "ArrowDown");
          }}
        >
          {overflow.map((choice) => (
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
              {/* Labels share one column when custom and standard choices mix. */}
              {!choice.custom && iconColumn && <span className="view-selector__icon" />}
              {content(choice)}
              {choice.id === defaultChoiceId && (
                <span className="view-selector__default" aria-hidden="true">
                  Default
                </span>
              )}
            </button>
          ))}
        </div>
      )}
      {status?.error && (
        <span ref={statusRef} className="view-preferences view-preferences--error" role="alert">
          {status.pending ? "View choice not saved." : "Saved view choice could not load."}
          <button type="button" onClick={() => void status.retry().catch(() => {})}>
            Retry
          </button>
        </span>
      )}
      {trailing && (
        <span ref={trailingRef} className="view-selector__trailing">
          {trailing}
        </span>
      )}
      <span id={defaultNoteId} hidden>
        Default view
      </span>
    </div>
  );
}
