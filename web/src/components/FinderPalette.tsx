import { FileCode2, FileText, Search } from "lucide-react";
import { type KeyboardEvent, type ReactNode, useEffect, useId, useRef, useState } from "react";

/** One row. Match ranges are [start, end) offsets into `title` and `path`. */
export type FinderPaletteItem = {
  id: string;
  kind: "note" | "code";
  title: string;
  path: string;
  /** Muted label beside the title, such as a note's type. */
  detail?: string;
  titleMatches: number[][];
  pathMatches: number[][];
};

export type FinderPaletteProps<T extends FinderPaletteItem> = {
  open: boolean;
  label: string;
  placeholder: string;
  /** Resolves the rows for a query. The signal aborts when the query changes or the palette closes. */
  search: (query: string, signal: AbortSignal) => Promise<T[]>;
  onOpen: (item: T, options: { beside: boolean }) => void;
  onClose: () => void;
  emptyText: (query: string) => string;
  /** Heading over the rows of an empty query. */
  recentHeading?: string;
};

type Results<T> = { query: string; items: T[] } | null;

const PATH_CHARS = 72;

/** True on macOS, where ⌘ is the command modifier; Ctrl elsewhere. */
export const IS_MAC =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

const MOD_KEY = IS_MAC ? "⌘" : "Ctrl+";

/**
 * A keyboard-first finder: a modal combobox over a listbox. Typing never
 * waits on the network; each keystroke aborts the previous request and only
 * the latest response is shown. Closing returns focus to where it was.
 */
export function FinderPalette<T extends FinderPaletteItem>({
  open,
  label,
  placeholder,
  search,
  onOpen,
  onClose,
  emptyText,
  recentHeading,
}: FinderPaletteProps<T>) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const returnFocus = useRef<Element | null>(null);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Results<T>>(null);
  const [failed, setFailed] = useState(false);
  const [active, setActive] = useState(0);
  // Enter pressed before its query's results arrived; it opens the first of
  // those results instead of a stale row. Editing the query cancels it.
  const pendingOpen = useRef<{ query: string; beside: boolean } | null>(null);
  const id = useId();
  const listId = `${id}-list`;

  useEffect(() => {
    const dialog = dialogRef.current;

    if (!open || !dialog) return;
    returnFocus.current = document.activeElement;

    if (!dialog.open) dialog.showModal();
    inputRef.current?.focus();

    return () => {
      if (dialog.open) dialog.close();
      const previous = returnFocus.current;

      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
      setQuery("");
      setResults(null);
      pendingOpen.current = null;
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();

    setFailed(false);
    search(query, controller.signal).then(
      (items) => {
        if (controller.signal.aborted) return;
        setResults({ query, items });
        setFailed(false);
        setActive(0);
      },
      () => {
        if (controller.signal.aborted) return;
        setFailed(true);
        pendingOpen.current = null;
      },
    );

    return () => controller.abort();
  }, [open, query, search]);

  const items = results?.items ?? [];
  const current = items[Math.min(active, items.length - 1)];
  const stale = results === null || results.query !== query;

  useEffect(() => {
    if (!current) return;
    const option = document.getElementById(`${id}-${current.id}`);

    option?.scrollIntoView?.({ block: "nearest" });
  }, [current, id]);

  const choose = (item: T | undefined, beside: boolean) => {
    if (!item) return;
    onOpen(item, { beside });
    onClose();
  };

  useEffect(() => {
    const pending = pendingOpen.current;

    if (!pending || stale || results?.query !== pending.query) return;
    pendingOpen.current = null;
    choose(results?.items[0], pending.beside);
  });

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    // Keys that confirm an input method's candidate belong to the input method.
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;

    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();

      if (!items.length) return;
      const step = event.key === "ArrowDown" ? 1 : -1;

      setActive(
        (index) => (Math.min(index, items.length - 1) + step + items.length) % items.length,
      );
    } else if (event.key === "Enter") {
      event.preventDefault();
      const beside = event.metaKey || event.ctrlKey;

      // The rows on screen may be for an earlier query; wait for this one's.
      if (stale) pendingOpen.current = { query, beside };
      else choose(current, beside);
    } else if (event.key === "Escape") {
      event.preventDefault();
      onClose();
    } else if (event.key === "Tab") {
      // The input is the only stop; options are reached with the arrows.
      event.preventDefault();
    }
  };

  if (!open) return null;

  let status: ReactNode = null;

  if (failed) status = "Search is unavailable right now. Try again in a moment.";
  else if (results === null) status = "Searching…";
  else if (!items.length && !stale)
    status = query.trim() ? emptyText(query.trim()) : "Nothing opened yet.";

  return (
    <dialog
      ref={dialogRef}
      className="finder"
      aria-label={label}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="finder__panel">
        <div className="finder__input">
          <Search aria-hidden="true" size={18} strokeWidth={1.75} />
          <input
            ref={inputRef}
            role="combobox"
            aria-label={label}
            aria-expanded={items.length > 0}
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={current ? `${id}-${current.id}` : undefined}
            placeholder={placeholder}
            spellCheck={false}
            autoComplete="off"
            value={query}
            onChange={(event) => {
              pendingOpen.current = null;
              setQuery(event.target.value);
            }}
            onKeyDown={onKeyDown}
          />
          <kbd>esc</kbd>
        </div>
        {!query.trim() && recentHeading && items.length > 0 && (
          <div className="finder__heading" aria-hidden="true">
            {recentHeading}
          </div>
        )}
        <ul
          id={listId}
          role="listbox"
          aria-label={label}
          className="finder__list"
          aria-busy={stale}
        >
          {items.map((item, index) => {
            const selected = item === current;
            const Icon = item.kind === "code" ? FileCode2 : FileText;

            return (
              <li
                key={item.id}
                id={`${id}-${item.id}`}
                role="option"
                aria-selected={selected}
                className="finder__row"
                onMouseMove={() => setActive(index)}
                onMouseDown={(event) => event.preventDefault()}
                onClick={(event) => {
                  // Rows for an earlier query are on their way out; ignore them.
                  if (!stale) choose(item, event.metaKey || event.ctrlKey);
                }}
              >
                <Icon aria-hidden="true" size={16} strokeWidth={1.75} className="finder__icon" />
                <span className="finder__title">
                  <Highlighted text={item.title} ranges={item.titleMatches} />
                </span>
                {item.detail && <span className="finder__detail">{item.detail}</span>}
                <span className="finder__path" title={item.path}>
                  <Highlighted text={item.path} ranges={item.pathMatches} max={PATH_CHARS} />
                </span>
              </li>
            );
          })}
        </ul>
        {status && (
          <p className="finder__status" role="status">
            {status}
          </p>
        )}
        <div className="finder__foot" aria-hidden="true">
          <span>
            <kbd>↑</kbd>
            <kbd>↓</kbd> navigate
          </span>
          <span>
            <kbd>↵</kbd> open
          </span>
          <span>
            <kbd>{MOD_KEY}↵</kbd> open beside
          </span>
          <span>
            <kbd>esc</kbd> close
          </span>
        </div>
      </div>
    </dialog>
  );
}

type Part = { text: string; mark: boolean };

/**
 * Splits text into marked and unmarked parts. Over `max` characters the
 * middle is replaced with an ellipsis so both ends of a path stay readable.
 */
export function highlightParts(text: string, ranges: number[][], max = Infinity): Part[] {
  let spans: Array<[number, number]> = [[0, text.length]];

  if (text.length > max) {
    const tail = Math.ceil((max - 1) * 0.6);
    const head = max - 1 - tail;

    spans = [
      [0, head],
      [text.length - tail, text.length],
    ];
  }

  const parts: Part[] = [];

  const push = (value: string, mark: boolean) => {
    if (!value) return;
    const last = parts.at(-1);

    if (last && last.mark === mark) last.text += value;
    else parts.push({ text: value, mark });
  };

  spans.forEach(([start, end], spanIndex) => {
    if (spanIndex > 0) push("…", false);
    let cursor = start;

    for (const [from, to] of ranges) {
      const a = Math.max(from, cursor);
      const b = Math.min(to, end);

      if (a >= b) continue;
      push(text.slice(cursor, a), false);
      push(text.slice(a, b), true);
      cursor = b;
    }

    push(text.slice(cursor, end), false);
  });

  return parts;
}

function Highlighted({ text, ranges, max }: { text: string; ranges: number[][]; max?: number }) {
  return highlightParts(text, ranges, max).map((part, index) =>
    part.mark ? <mark key={index}>{part.text}</mark> : part.text,
  );
}
