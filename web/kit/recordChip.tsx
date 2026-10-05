import {
  createContext,
  useContext,
  useState,
  useSyncExternalStore,
  type MouseEvent,
  type ReactNode,
} from "react";

import { cn } from "./lib/utils";

type HighlightStore = {
  current: () => string | null;
  set: (key: string | null) => void;
  subscribe: (listener: () => void) => () => void;
};

function createHighlightStore(): HighlightStore {
  let current: string | null = null;
  const listeners = new Set<() => void>();

  return {
    current: () => current,
    set: (key) => {
      if (key === current) return;
      current = key;

      for (const listener of listeners) listener();
    },
    subscribe: (listener) => {
      listeners.add(listener);

      return () => listeners.delete(listener);
    },
  };
}

const HighlightContext = createContext<HighlightStore | null>(null);

/**
 * Shares hover highlighting among the record chips inside it: pointing at or
 * focusing one chip lights every chip with the same record key. Only the chips
 * whose state changes re-render, so a matrix of thousands stays responsive.
 */
export function HighlightProvider({ children }: { children: ReactNode }) {
  const [store] = useState(createHighlightStore);

  return <HighlightContext.Provider value={store}>{children}</HighlightContext.Provider>;
}

const noSubscription = () => () => undefined;

type RecordChipProps = {
  /** Identifies the record across the page, usually its note path or node ref. */
  recordKey: string;
  children: ReactNode;
  /** Shown before the label, usually a `StatusMark` with `hideLabel`. */
  mark?: ReactNode;
  /**
   * The record was reached through another record rather than linked directly.
   * The chip is italic and fainter, with `data-indirect` for styling.
   */
  indirect?: boolean;
  title?: string;
  /** Makes the chip a button, usually opening the record with `openNode`. */
  onOpen?: (event: MouseEvent<HTMLButtonElement>) => void;
  /** Wrap a long label onto a second line instead of truncating it, as in narrow table cells. */
  wrap?: boolean;
  className?: string;
};

/** A compact record reference that highlights with its other occurrences. */
export function RecordChip({
  recordKey,
  children,
  mark,
  indirect = false,
  title,
  onOpen,
  wrap = false,
  className,
}: RecordChipProps) {
  const store = useContext(HighlightContext);

  const highlighted = useSyncExternalStore(
    store?.subscribe ?? noSubscription,
    () => store?.current() === recordKey,
  );

  const enter = () => store?.set(recordKey);

  const leave = () => {
    if (store?.current() === recordKey) store.set(null);
  };

  const props = {
    "data-record-key": recordKey,
    "data-highlighted": highlighted || undefined,
    "data-indirect": indirect || undefined,
    title: indirect ? `${title ?? ""} (reached through another record)`.trim() : title,
    onPointerEnter: enter,
    onPointerLeave: leave,
    onFocus: enter,
    onBlur: leave,
    className: cn(
      "inline-flex max-w-full min-w-0 gap-1.5 rounded-sm px-1.5 py-px text-left text-[12px] leading-snug text-[var(--ao-ink-2)]",
      wrap ? "items-start" : "items-center",
      onOpen &&
        "cursor-pointer border-0 bg-transparent focus-visible:outline-2 focus-visible:outline-[var(--ring)]",
      indirect && "text-[var(--text-faint)] italic",
      highlighted &&
        "bg-[var(--ao-teal-soft)] text-[var(--ao-teal-ink)] shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--ao-teal)_55%,transparent)]",
      className,
    ),
  };

  const content = (
    <>
      {mark}
      <span className={wrap ? "line-clamp-2" : "truncate"}>{children}</span>
      {indirect && <span className="sr-only">(indirect)</span>}
    </>
  );

  return onOpen ? (
    <button type="button" {...props} onClick={onOpen}>
      {content}
    </button>
  ) : (
    <span {...props}>{content}</span>
  );
}
