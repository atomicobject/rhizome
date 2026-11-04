import {
  createContext,
  type ReactNode,
  useContext,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

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
  /** Shown in the count slot until a view below fills it, such as a type's record count on its Briefing. */
  countFallback?: string;
  description?: string | null;
  /** Quiet title-row facts after the description, such as a validation badge. */
  facts?: ReactNode;
  info?: string;
  infoLink?: { href: string; label: string };
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
  countFallback,
  description,
  facts,
  info,
  infoLink,
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
          <span className="view-header__count" ref={countRef} data-fallback={countFallback} />
        )}
        {(description || info) && (
          <HeaderDescription description={description ?? ""} info={info} infoLink={infoLink} />
        )}
        {facts}
        <div className="view-header__tools" ref={toolsRef} />
      </div>
      {tabs && <div className="view-header__tabs">{tabs}</div>}
    </header>
  );
}

/** The first sentence of a description; null when it is the whole of it. */
export function leadSentence(text: string) {
  const match = /^.+?[.!?](?=\s+\S)/s.exec(text.trim());

  return match ? match[0] : null;
}

/** Below this much room the description is a fragment, so only its info mark shows. */
const DESCRIPTION_MIN_WIDTH = 160;

/**
 * The room the title row leaves the description: the row's width less every
 * other item and the gaps between them.
 */
function descriptionRoom(about: HTMLElement) {
  const row = about.parentElement;

  if (!row) return Infinity;
  const items = [...row.children].filter((child) => child !== about);
  const style = getComputedStyle(row);
  const px = (value: string) => Number.parseFloat(value) || 0;
  const others = items.reduce((sum, item) => sum + item.getBoundingClientRect().width, 0);
  const padding = px(style.paddingLeft) + px(style.paddingRight);

  return row.clientWidth - padding - others - px(style.columnGap) * items.length;
}

/**
 * One line of description: its first sentence, with the rest behind an info
 * mark. When the title row leaves it under 160px, the line hides and the mark
 * alone carries the description.
 */
function HeaderDescription({
  description,
  info,
  infoLink,
}: {
  description: string;
  info?: string;
  infoLink?: Props["infoLink"];
}) {
  const tooltip = [description, info].filter(Boolean).join("\n");
  const Info = infoLink ? "a" : "button";
  const lead = leadSentence(description);
  const aboutRef = useRef<HTMLSpanElement>(null);
  const [narrow, setNarrow] = useState(false);

  useLayoutEffect(() => {
    const about = aboutRef.current;
    const row = about?.parentElement;

    if (!about || !row || typeof ResizeObserver === "undefined") return;
    const measure = () => setNarrow(descriptionRoom(about) < DESCRIPTION_MIN_WIDTH);
    const observer = new ResizeObserver(measure);

    // The row, and each item in it: the tools fill in after the header paints.
    for (const element of [row, ...row.children]) observer.observe(element);
    measure();

    return () => observer.disconnect();
  }, []);

  return (
    <>
      <span
        ref={aboutRef}
        className="view-header__about"
        title={description}
        hidden={narrow || !description}
      >
        {lead ?? description}
      </span>
      {(lead || narrow || info) && (
        <Info
          type={infoLink ? undefined : "button"}
          href={infoLink?.href}
          className="view-header__about-info"
          title={tooltip}
          aria-label={infoLink?.label ?? `Full description: ${tooltip}`}
        >
          <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <circle cx="8" cy="8" r="6.25" fill="none" stroke="currentColor" strokeWidth="1.25" />
            <path
              d="M8 7.2v3.8M8 5.1v.1"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
            />
          </svg>
        </Info>
      )}
    </>
  );
}
