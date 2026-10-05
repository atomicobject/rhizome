import type { UseQueryResult } from "@tanstack/react-query";
import {
  createContext,
  type FocusEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { getNodePreview } from "../../api/client";
import { queryKeys } from "../../api/queryKeys";
import type { NodePreview } from "../../api/types";
import { buildNoteWebHref } from "../../lib/content";
import { useStagedQuery } from "../../staging/stagedQuery";
import { useActiveEditSession } from "../useOntologyEditSession";
import { NotePreviewCard } from "./NotePreviewCard";
import { type HoverIntentSource, useHoverIntent } from "./useHoverIntent";

const VIEWPORT_MARGIN = 8;

const CARD_GAP = 6;

type Anchor = { left: number; top: number; bottom: number };

type Position = { left: number; top: number };

export type PreviewPlacement = "auto" | "left";

export type OpenNote = (target: string, mode: "stack" | "beside") => void;

type ParentPreview = {
  depth: number;
  getRect: () => DOMRect | null;
  retainAncestors: (source: HoverIntentSource) => void;
};

const ParentPreviewContext = createContext<ParentPreview | null>(null);

type OpenCard = { id: string; close: () => void };

const openCards: OpenCard[] = [];

let listeningForEscape = false;

function onEscape(event: KeyboardEvent) {
  if (event.key !== "Escape") return;
  openCards.at(-1)?.close();
}

function registerOpenCard(card: OpenCard) {
  const priorIndex = openCards.findIndex((entry) => entry.id === card.id);

  if (priorIndex >= 0) openCards.splice(priorIndex, 1);
  openCards.push(card);

  if (!listeningForEscape) {
    window.addEventListener("keydown", onEscape);
    listeningForEscape = true;
  }

  return () => {
    const index = openCards.findIndex((entry) => entry.id === card.id);

    if (index >= 0) openCards.splice(index, 1);

    if (openCards.length === 0 && listeningForEscape) {
      window.removeEventListener("keydown", onEscape);
      listeningForEscape = false;
    }
  };
}

function useNodePreviewQuery(target: string, from?: string) {
  return useStagedQuery({
    subject: queryKeys.nodes.preview(target, from),
    session: useActiveEditSession(),
    queryFn: ({ signal, editSession }) => getNodePreview(target, from, { editSession, signal }),
    staleTime: 30_000,
    retry: false,
  });
}

function clamp(value: number, minimum: number, maximum: number) {
  return Math.max(minimum, Math.min(value, maximum));
}

export function previewPosition(
  anchor: Anchor,
  card: { width: number; height: number },
  viewport: { width: number; height: number },
  parent?: DOMRect | null,
  placement: PreviewPlacement = "auto",
): Position {
  const maxLeft = Math.max(VIEWPORT_MARGIN, viewport.width - card.width - VIEWPORT_MARGIN);
  const maxTop = Math.max(VIEWPORT_MARGIN, viewport.height - card.height - VIEWPORT_MARGIN);

  if (parent) {
    const right = parent.right + CARD_GAP;

    if (right + card.width <= viewport.width - VIEWPORT_MARGIN) {
      return { left: right, top: clamp(anchor.top, VIEWPORT_MARGIN, maxTop) };
    }

    const left = parent.left - CARD_GAP - card.width;

    if (left >= VIEWPORT_MARGIN) {
      return { left, top: clamp(anchor.top, VIEWPORT_MARGIN, maxTop) };
    }
  }

  if (placement === "left") {
    return {
      left: clamp(anchor.left - CARD_GAP - card.width, VIEWPORT_MARGIN, maxLeft),
      top: clamp(anchor.top, VIEWPORT_MARGIN, maxTop),
    };
  }

  const below = anchor.bottom + CARD_GAP + card.height <= viewport.height - VIEWPORT_MARGIN;
  const top = below ? anchor.bottom + CARD_GAP : anchor.top - CARD_GAP - card.height;

  return {
    left: clamp(anchor.left, VIEWPORT_MARGIN, maxLeft),
    top: clamp(top, VIEWPORT_MARGIN, maxTop),
  };
}

function anchorFromPointer(event: MouseEvent<HTMLElement>): Anchor {
  const rects = Array.from(event.currentTarget.getClientRects());

  const rect =
    rects.find(
      (candidate) => event.clientY >= candidate.top && event.clientY <= candidate.bottom,
    ) ?? event.currentTarget.getBoundingClientRect();

  return { left: rect.left, top: rect.top, bottom: rect.bottom };
}

function anchorFromElement(element: HTMLElement): Anchor {
  const rect = element.getBoundingClientRect();

  return { left: rect.left, top: rect.top, bottom: rect.bottom };
}

function supportsHover() {
  return window.matchMedia?.("(hover: hover)").matches ?? true;
}

function PreviewPortal({
  anchor,
  cardId,
  target,
  open,
  query,
  retain,
  leave,
  parent,
  placement,
  returnFocus,
}: {
  anchor: Anchor;
  cardId: string;
  target: string;
  open: OpenNote;
  query: UseQueryResult<NodePreview, Error>;
  retain: (source: HoverIntentSource) => void;
  leave: (source: HoverIntentSource) => void;
  parent: ParentPreview | null;
  placement: PreviewPlacement;
  returnFocus: (dismiss: boolean) => void;
}) {
  const cardRef = useRef<HTMLDivElement | null>(null);
  const [position, setPosition] = useState<Position | null>(null);

  const retainTree = useCallback(
    (source: HoverIntentSource) => {
      retain(source);
      parent?.retainAncestors(source);
    },
    [parent, retain],
  );

  useLayoutEffect(() => {
    const card = cardRef.current;

    if (!card) return;

    const update = () => {
      const measured = card.getBoundingClientRect();

      const next = previewPosition(
        anchor,
        { width: measured.width, height: measured.height },
        { width: window.innerWidth, height: window.innerHeight },
        parent?.getRect(),
        placement,
      );

      setPosition((current) =>
        current?.left === next.left && current.top === next.top ? current : next,
      );
    };

    update();
    window.addEventListener("resize", update);
    const observer = new ResizeObserver(update);

    observer.observe(card);

    return () => {
      observer.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [anchor, parent, placement]);

  const context = useMemo<ParentPreview>(
    () => ({
      depth: (parent?.depth ?? 0) + 1,
      getRect: () => cardRef.current?.getBoundingClientRect() ?? null,
      retainAncestors: retainTree,
    }),
    [parent, retainTree],
  );

  return createPortal(
    <div
      ref={cardRef}
      id={cardId}
      className="note-preview"
      role="region"
      tabIndex={-1}
      aria-label={`Preview of ${query.data?.title || target}`}
      onKeyDown={(event) => {
        if (event.key !== "Tab") return;

        const focusable = Array.from(
          event.currentTarget.querySelectorAll<HTMLElement>("a[href], button, [tabindex='0']"),
        );

        const active = document.activeElement;

        const leavingBackward =
          event.shiftKey && (active === event.currentTarget || active === focusable[0]);

        const leavingForward =
          !event.shiftKey && (focusable.length === 0 || active === focusable.at(-1));

        if (!leavingBackward && !leavingForward) return;

        // The card is portaled to the end of the body, so natural tab order
        // would leave the document; hand focus back to the trigger instead.
        event.preventDefault();
        event.stopPropagation();
        returnFocus(leavingForward);
      }}
      style={{
        left: position?.left ?? 0,
        top: position?.top ?? 0,
        maxWidth:
          placement === "left" ? Math.max(0, anchor.left - CARD_GAP - VIEWPORT_MARGIN) : undefined,
        visibility: position ? "visible" : "hidden",
        zIndex: 60 + context.depth,
      }}
      onMouseEnter={supportsHover() ? () => retainTree("hover") : undefined}
      onMouseLeave={supportsHover() ? () => leave("hover") : undefined}
      onFocusCapture={() => retainTree("focus")}
      onBlurCapture={(event: FocusEvent<HTMLDivElement>) => {
        if (
          event.relatedTarget instanceof Node &&
          event.currentTarget.contains(event.relatedTarget)
        ) {
          return;
        }

        leave("focus");
      }}
    >
      <ParentPreviewContext.Provider value={context}>
        <NotePreviewCard
          target={target}
          open={open}
          preview={query.data}
          loading={query.isPending}
          error={query.isError}
        />
      </ParentPreviewContext.Provider>
    </div>,
    document.body,
  );
}

function PreviewQuery({
  target,
  from,
  visible,
  anchor,
  cardId,
  open,
  settled,
  retain,
  leave,
  parent,
  placement,
  returnFocus,
}: {
  target: string;
  from?: string;
  visible: boolean;
  anchor: Anchor | null;
  cardId: string;
  open: OpenNote;
  settled: () => void;
  retain: (source: HoverIntentSource) => void;
  leave: (source: HoverIntentSource) => void;
  parent: ParentPreview | null;
  placement: PreviewPlacement;
  returnFocus: (dismiss: boolean) => void;
}) {
  const query = useNodePreviewQuery(target, from);

  useEffect(() => {
    if (!query.isPending) settled();
  }, [query.isPending, settled]);

  if (!visible || !anchor) return null;

  return (
    <PreviewPortal
      anchor={anchor}
      cardId={cardId}
      target={target}
      open={open}
      query={query}
      retain={retain}
      leave={leave}
      parent={parent}
      placement={placement}
      returnFocus={returnFocus}
    />
  );
}

type TriggerOptions<T extends HTMLElement> = {
  target: string;
  from?: string;
  open: OpenNote;
  placement?: PreviewPlacement;
  placementBoundary?: (trigger: T) => DOMRect | null;
};

export function useNotePreviewTrigger<T extends HTMLElement>({
  target,
  from,
  open,
  placement = "auto",
  placementBoundary,
}: TriggerOptions<T>) {
  const intent = useHoverIntent();
  const parent = useContext(ParentPreviewContext);
  const [anchor, setAnchor] = useState<Anchor | null>(null);
  const reactId = useId();
  const cardId = `note-preview-${reactId.replaceAll(":", "")}`;
  const hoverEnabled = supportsHover();
  const triggerRef = useRef<T | null>(null);
  const suppressFocusOpen = useRef(false);

  useEffect(() => {
    if (!intent.open) return;

    return registerOpenCard({
      id: cardId,
      close: () => {
        const card = document.getElementById(cardId);
        const restoreFocus = Boolean(card?.contains(document.activeElement));

        if (restoreFocus) suppressFocusOpen.current = true;
        intent.close();

        if (restoreFocus) triggerRef.current?.focus();
      },
    });
  }, [cardId, intent.close, intent.open]);

  const beginWithAnchor = (trigger: T, nextAnchor: Anchor, source: HoverIntentSource) => {
    const boundary = placementBoundary?.(trigger);

    setAnchor(boundary ? { ...nextAnchor, left: boundary.left } : nextAnchor);
    intent.begin(source);
  };

  return {
    close: intent.close,
    preview:
      intent.fetchSequence > 0 ? (
        <PreviewQuery
          key={intent.fetchSequence}
          target={target}
          from={from}
          visible={intent.open}
          anchor={anchor}
          cardId={cardId}
          open={open}
          settled={intent.settled}
          retain={intent.retain}
          leave={intent.leave}
          parent={parent}
          placement={placement}
          returnFocus={(dismiss) => {
            // Tabbing forward out of the card dismisses it, so the next Tab
            // continues in document order instead of re-entering the card.
            if (dismiss) {
              suppressFocusOpen.current = true;
              intent.close();
            }

            triggerRef.current?.focus();
          }}
        />
      ) : null,
    triggerProps: {
      ref: triggerRef,
      "aria-describedby": intent.open ? cardId : undefined,
      onMouseEnter: hoverEnabled
        ? (event: MouseEvent<T>) =>
            beginWithAnchor(event.currentTarget, anchorFromPointer(event), "hover")
        : undefined,
      onMouseLeave: hoverEnabled ? () => intent.leave("hover") : undefined,
      onKeyDown: (event: ReactKeyboardEvent<T>) => {
        if (event.key !== "Tab" || event.shiftKey || !intent.open) return;

        const card = document.getElementById(cardId);

        if (!card) return;

        event.preventDefault();
        card.focus();
      },
      onFocus: () => {
        if (suppressFocusOpen.current) {
          suppressFocusOpen.current = false;

          return;
        }

        const element = triggerRef.current;

        if (element) beginWithAnchor(element, anchorFromElement(element), "focus");
      },
      onBlur: (event: FocusEvent<T>) => {
        const next = event.relatedTarget;
        const card = document.getElementById(cardId);

        if (next instanceof Node && card?.contains(next)) {
          intent.retain("focus");

          return;
        }

        intent.leave("focus");
      },
    },
  };
}

type Props = {
  target: string;
  from?: string;
  children: ReactNode;
  open: OpenNote;
};

export function NoteLinkPreview({ target, from, children, open }: Props) {
  const previewTrigger = useNotePreviewTrigger<HTMLAnchorElement>({ target, from, open });

  return (
    <>
      <a
        {...previewTrigger.triggerProps}
        href={buildNoteWebHref(target)}
        className="markdown-link"
        onClick={(event) => {
          if (event.button !== 0 || event.shiftKey || event.altKey) return;
          event.preventDefault();
          previewTrigger.close();
          open(target, event.metaKey || event.ctrlKey ? "beside" : "stack");
        }}
      >
        {children}
      </a>
      {previewTrigger.preview}
    </>
  );
}
