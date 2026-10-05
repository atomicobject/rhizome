import { type RefObject, useEffect, useRef } from "react";

/**
 * Returns focus to an inline editor's trigger when the editor closes and its
 * focused element unmounts, so Escape or a commit never drops focus on the
 * page. Leaves focus alone when the user moved it somewhere else.
 */
export function useReturnFocusOnClose<T extends HTMLElement>(editing: boolean) {
  const triggerRef = useRef<T>(null);
  const wasEditing = useRef(editing);

  useEffect(() => {
    const lost = !document.activeElement || document.activeElement === document.body;

    if (wasEditing.current && !editing && lost) triggerRef.current?.focus();
    wasEditing.current = editing;
  }, [editing]);

  return triggerRef;
}

const TABBABLE =
  'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, [tabindex="0"]';

// ponytail: checkVisibility is missing in jsdom, where everything counts as visible.
const visible = (element: Element) =>
  !element.closest("[hidden], [inert]") && (element.checkVisibility?.() ?? true);

function firstTabbable(region: Element): HTMLElement | null {
  return (
    [...region.querySelectorAll<HTMLElement>(TABBABLE)].find(
      (element) => element.tabIndex >= 0 && visible(element),
    ) ?? null
  );
}

/**
 * F6 and Shift+F6 move focus between the regions `selectors` name, in order,
 * skipping hidden or inert ones. A region regains the element that last had
 * focus in it, else its first tabbable element.
 */
export function useFocusRegions(
  rootRef: RefObject<HTMLElement | null>,
  selectors: string,
  enabled: boolean,
) {
  useEffect(() => {
    const root = rootRef.current;

    if (!root || !enabled) return;

    const remembered = new WeakMap<Element, HTMLElement>();

    const regions = () => [...root.querySelectorAll(selectors)].filter(visible);

    const onFocusIn = (event: FocusEvent) => {
      const target = event.target;

      if (!(target instanceof HTMLElement)) return;
      const region = regions().find((candidate) => candidate.contains(target));

      if (region) remembered.set(region, target);
    };

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "F6" || event.altKey || event.metaKey || event.ctrlKey) return;
      const list = regions();

      if (list.length === 0) return;
      event.preventDefault();

      const step = event.shiftKey ? -1 : 1;
      const current = list.findIndex((region) => region.contains(document.activeElement));
      // From outside every region, F6 starts at the first and Shift+F6 at the last.
      const start = current >= 0 ? current : step > 0 ? -1 : list.length;

      for (let offset = 1; offset <= list.length; offset += 1) {
        const region = list[(((start + step * offset) % list.length) + list.length) % list.length];
        const last = remembered.get(region);

        const target =
          last && region.contains(last) && visible(last) && last.tabIndex >= 0
            ? last
            : firstTabbable(region);

        if (target) {
          target.focus();

          return;
        }
      }
    };

    root.addEventListener("focusin", onFocusIn);
    window.addEventListener("keydown", onKeyDown);

    return () => {
      root.removeEventListener("focusin", onFocusIn);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [rootRef, selectors, enabled]);
}

/**
 * Closes an open popover on Escape, a press outside it, or focus leaving it.
 * `containerRef` wraps both the trigger (the element with `aria-expanded`)
 * and the popover; Escape returns focus to that trigger.
 */
export function usePopoverDismiss(
  open: boolean,
  containerRef: RefObject<HTMLElement | null>,
  close: () => void,
) {
  useEffect(() => {
    const container = containerRef.current;

    if (!open || !container) return;

    const inside = (target: EventTarget | null) =>
      target instanceof Node && container.contains(target);

    const onPointerDown = (event: MouseEvent) => {
      if (!inside(event.target)) close();
    };

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      close();
      container.querySelector<HTMLElement>("[aria-expanded]")?.focus();
    };

    const onFocusOut = (event: FocusEvent) => {
      if (event.relatedTarget && !inside(event.relatedTarget)) close();
    };

    document.addEventListener("mousedown", onPointerDown);
    container.addEventListener("keydown", onKeyDown);
    container.addEventListener("focusout", onFocusOut);

    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      container.removeEventListener("keydown", onKeyDown);
      container.removeEventListener("focusout", onFocusOut);
    };
  }, [open, containerRef, close]);
}

// A row's selection checkbox is reached with X or a click, not arrow keys.
const CELL_CONTROL =
  "a[href], button:not(:disabled), input:not(:disabled):not([data-row-select]), select, textarea";

/**
 * Arrow keys move between rows and between cells (Up/Down keep the column);
 * Escape leaves a cell for its row. Text fields and selects keep their own
 * arrow keys and Escape. Returns whether the key was handled.
 */
export function moveTableFocus(
  rowElement: HTMLTableRowElement,
  target: EventTarget,
  key: string,
): boolean {
  if (!(target instanceof HTMLElement) || !rowElement.contains(target)) return false;
  const onRow = target === rowElement;

  const control = target.matches("a, button, [type=checkbox]") ? target : null;

  if (!onRow && !control) return false;

  const cellIndex = control?.closest("td")?.cellIndex ?? -1;

  if (key === "Escape" && !onRow) {
    rowElement.focus();

    return true;
  }

  if (key === "ArrowUp" || key === "ArrowDown") {
    const rows = [
      ...(rowElement.parentElement?.querySelectorAll<HTMLTableRowElement>("tr[data-row-key]") ??
        []),
    ];

    const next = rows[rows.indexOf(rowElement) + (key === "ArrowDown" ? 1 : -1)];

    if (next && (onRow || !focusCell(next, cellIndex))) next.focus();

    return true;
  }

  if (key === "ArrowRight" || (key === "ArrowLeft" && !onRow)) {
    const step = key === "ArrowRight" ? 1 : -1;

    for (
      let index = cellIndex + step;
      index >= 0 && index < rowElement.cells.length;
      index += step
    ) {
      if (focusCell(rowElement, index)) return true;
    }

    if (step < 0) rowElement.focus();

    return true;
  }

  return false;
}

function focusCell(rowElement: HTMLTableRowElement, index: number) {
  const control = rowElement.cells[index]?.querySelector<HTMLElement>(CELL_CONTROL);
  control?.focus();

  return Boolean(control);
}

/**
 * ArrowDown and ArrowUp move focus among a menu's items, wrapping at the
 * ends; from outside the items they reach the first or last. Returns whether
 * the key was handled.
 */
export function moveMenuFocus(menu: HTMLElement, key: string) {
  if (key !== "ArrowDown" && key !== "ArrowUp") return false;

  const items = [...menu.querySelectorAll<HTMLElement>("[role^=menuitem]")];
  const index = items.findIndex((item) => item === document.activeElement);
  const step = key === "ArrowDown" ? 1 : -1;
  const next = index < 0 ? (step > 0 ? 0 : items.length - 1) : index + step;
  items[(next + items.length) % items.length]?.focus();

  return true;
}
