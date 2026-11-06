import {
  createContext,
  type ComponentPropsWithRef,
  type RefObject,
  useContext,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

type ReadingPosition = {
  top: number;
  anchor?: { id: string; occurrence: number; offset: number };
};

type ReadingState = {
  navigation: string;
  position?: ReadingPosition;
  disclosures: Map<string, boolean>;
  suspended: boolean;
};

export const NoteReadingStateContext = createContext<ReadingState | null>(null);

/** Native disclosure state survives disposal of the note body that owns its DOM. */
export function NoteDisclosure({
  disclosureId,
  open = false,
  onToggle,
  ...props
}: ComponentPropsWithRef<"details"> & { disclosureId: string }) {
  const reading = useContext(NoteReadingStateContext);
  const [expanded, setExpanded] = useState(() => reading?.disclosures.get(disclosureId) ?? open);

  useLayoutEffect(() => {
    setExpanded(reading?.disclosures.get(disclosureId) ?? open);
  }, [disclosureId, open, reading]);

  return (
    <details
      {...props}
      open={expanded}
      onToggle={(event) => {
        const next = event.currentTarget.open;
        reading?.disclosures.set(disclosureId, next);
        setExpanded(next);
        onToggle?.(event);
      }}
    />
  );
}

function position(container: HTMLElement): ReadingPosition {
  const top = container.scrollTop;
  const viewportTop = container.getBoundingClientRect().top;
  let anchor: ReadingPosition["anchor"];
  const occurrences = new Map<string, number>();

  for (const element of container.querySelectorAll<HTMLElement>("[data-reading-anchor]")) {
    const id = element.dataset.readingAnchor;

    if (!id) continue;
    const occurrence = occurrences.get(id) ?? 0;
    occurrences.set(id, occurrence + 1);
    const closed = element.closest("details:not([open])");

    if (closed && closed !== element) continue;
    const bounds = element.getBoundingClientRect();

    if (bounds.height === 0 || bounds.top > viewportTop + 1) continue;
    anchor = { id, occurrence, offset: bounds.top - viewportTop };
  }

  return { top, anchor };
}

function restorePosition(container: HTMLElement, saved: ReadingPosition) {
  const anchor = saved.anchor;

  const element = anchor
    ? Array.from(container.querySelectorAll<HTMLElement>("[data-reading-anchor]")).filter(
        (candidate) => candidate.dataset.readingAnchor === anchor.id,
      )[anchor.occurrence]
    : null;

  container.scrollTop =
    element && anchor
      ? container.scrollTop +
        element.getBoundingClientRect().top -
        container.getBoundingClientRect().top -
        anchor.offset
      : saved.top;
}

export function useNoteReadingState({
  identity,
  navigation,
  active,
  resident,
  containerRef,
}: {
  identity: string;
  navigation: string;
  active: boolean;
  resident: boolean;
  containerRef: RefObject<HTMLDivElement | null>;
}) {
  const memories = useRef(new Map<string, ReadingState>());

  const reading = useMemo(() => {
    let remembered = memories.current.get(identity);

    if (!remembered) {
      remembered = { navigation, disclosures: new Map(), suspended: false };
      memories.current.set(identity, remembered);
    }

    return remembered;
  }, [identity, navigation]);

  const restoring = reading.navigation === navigation && Boolean(reading.position);

  useLayoutEffect(() => {
    if (reading.navigation !== navigation) {
      reading.navigation = navigation;
      reading.position = undefined;
    }

    if (!resident) {
      reading.suspended = true;

      return;
    }

    const container = containerRef.current;

    if (!active || !container) return;
    const saved = reading.suspended ? reading.position : undefined;
    reading.suspended = false;
    let observer: ResizeObserver | undefined;
    let frame: number | undefined;
    let correctionStopped = false;
    let restoredTop = container.scrollTop;

    const stopCorrection = () => {
      correctionStopped = true;
      observer?.disconnect();

      if (frame !== undefined) window.cancelAnimationFrame(frame);
      frame = undefined;
    };

    const onScroll = () => {
      if (Math.abs(container.scrollTop - restoredTop) > 0.5) stopCorrection();
    };

    if (saved) {
      restorePosition(container, saved);
      restoredTop = container.scrollTop;
      const initialHeight = container.scrollHeight;

      // Correct one delayed layout, then leave scrolling entirely to the reader.
      observer = new ResizeObserver(() => {
        if (correctionStopped || container.scrollHeight === initialHeight) return;
        correctionStopped = true;
        observer?.disconnect();
        frame = window.requestAnimationFrame(() => {
          frame = undefined;
          restorePosition(container, saved);
        });
      });

      for (const child of container.children) observer.observe(child);
      container.addEventListener("wheel", stopCorrection, { passive: true });
      container.addEventListener("touchstart", stopCorrection, { passive: true });
      container.addEventListener("pointerdown", stopCorrection);
      container.addEventListener("keydown", stopCorrection);
      container.addEventListener("scroll", onScroll, { passive: true });
    }

    return () => {
      reading.position = position(container);
      stopCorrection();
      container.removeEventListener("wheel", stopCorrection);
      container.removeEventListener("touchstart", stopCorrection);
      container.removeEventListener("pointerdown", stopCorrection);
      container.removeEventListener("keydown", stopCorrection);
      container.removeEventListener("scroll", onScroll);
    };
  }, [active, containerRef, navigation, reading, resident]);

  return { reading, restoring };
}
