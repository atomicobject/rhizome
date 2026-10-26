import { createContext, type ReactNode, useContext, useMemo, useState } from "react";

/**
 * Where a page's view header takes the view's own controls. A type
 * collection or saved-view page draws the header (title, presentations), and
 * the configured view below it portals Find, Filter, and View into `tools`
 * and its row count into `count`, so the header stays one title row.
 */
export type ViewHeaderSlots = {
  tools: HTMLElement | null;
  count: HTMLElement | null;
};

const SlotsContext = createContext<ViewHeaderSlots | null>(null);

export const ViewHeaderSlotsProvider = SlotsContext.Provider;

/** The enclosing page header's slots, or null when the view draws its own controls. */
export function useViewHeaderSlots() {
  return useContext(SlotsContext);
}

/** Slot elements for a page that draws a `ViewHeader` above an embedded view. */
export function useViewHeaderSlotRefs() {
  const [tools, setTools] = useState<HTMLElement | null>(null);
  const [count, setCount] = useState<HTMLElement | null>(null);
  const slots = useMemo(() => ({ tools, count }), [tools, count]);

  return { slots, toolsRef: setTools, countRef: setCount };
}

type Props = {
  className?: string;
  title: ReactNode;
  titleId?: string;
  /** Hover text for the title, such as the kind and schema name. */
  titleHint?: string;
  /** A count the page knows; without one the header offers a slot the view fills. */
  count?: ReactNode;
  countRef?: (element: HTMLElement | null) => void;
  description?: string | null;
  /** Quiet title-row facts after the description, such as a validation badge. */
  facts?: ReactNode;
  toolsRef?: (element: HTMLElement | null) => void;
  /** The presentations row: text tabs and their trailing controls. */
  tabs?: ReactNode;
};

/**
 * The collection header (SPEC-0126 US3): a quiet title row with the count, a
 * truncating description, and the view's tools, then presentations as tabs.
 */
export function ViewHeader({
  className,
  title,
  titleId,
  titleHint,
  count,
  countRef,
  description,
  facts,
  toolsRef,
  tabs,
}: Props) {
  return (
    <header className={className ? `view-header ${className}` : "view-header"}>
      <div className="view-header__title-row">
        <h2 id={titleId} title={titleHint}>
          {title}
        </h2>
        {count !== undefined && count !== null ? (
          <span className="view-header__count">{count}</span>
        ) : (
          <span className="view-header__count" ref={countRef} />
        )}
        {description && (
          <span className="view-header__about" title={description}>
            {description}
          </span>
        )}
        {facts}
        <div className="view-header__tools" ref={toolsRef} />
      </div>
      {tabs && <div className="view-header__tabs">{tabs}</div>}
    </header>
  );
}
