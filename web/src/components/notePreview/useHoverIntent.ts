import { useCallback, useEffect, useRef, useState } from "react";

const FETCH_DWELL_MS = 100;

const OPEN_DELAY_MS = 300;

const OPEN_MAX_WAIT_MS = 700;

const CLOSE_GRACE_MS = 150;

type Timer = ReturnType<typeof setTimeout>;

export type HoverIntentSource = "hover" | "focus";

type HoverIntentCallbacks = {
  onFetch: () => void;
  onOpenChange: (open: boolean) => void;
};

type HoverIntentController = {
  begin: (source: HoverIntentSource) => void;
  retain: (source: HoverIntentSource) => void;
  leave: (source: HoverIntentSource) => void;
  settled: () => void;
  close: () => void;
  dispose: () => void;
};

function createHoverIntentController({
  onFetch,
  onOpenChange,
}: HoverIntentCallbacks): HoverIntentController {
  const activeSources = new Set<HoverIntentSource>();
  let open = false;
  let requested = false;
  let startedAt = 0;
  let dwellTimer: Timer | undefined;
  let openTimer: Timer | undefined;
  let capTimer: Timer | undefined;
  let closeTimer: Timer | undefined;

  const active = () => activeSources.size > 0;

  const clearOpening = () => {
    clearTimeout(dwellTimer);
    clearTimeout(openTimer);
    clearTimeout(capTimer);
    dwellTimer = undefined;
    openTimer = undefined;
    capTimer = undefined;
  };

  const show = () => {
    if (!active() || open) return;
    clearOpening();
    open = true;
    onOpenChange(true);
  };

  const close = () => {
    activeSources.clear();
    requested = false;
    clearOpening();
    clearTimeout(closeTimer);
    closeTimer = undefined;

    if (!open) return;
    open = false;
    onOpenChange(false);
  };

  return {
    begin(source) {
      clearTimeout(closeTimer);
      closeTimer = undefined;

      if (activeSources.has(source)) return;
      const alreadyActive = active();
      activeSources.add(source);

      if (alreadyActive || open) return;
      requested = false;
      startedAt = Date.now();
      dwellTimer = setTimeout(() => {
        dwellTimer = undefined;

        if (!active()) return;
        requested = true;
        onFetch();
        capTimer = setTimeout(show, Math.max(0, OPEN_MAX_WAIT_MS - (Date.now() - startedAt)));
      }, FETCH_DWELL_MS);
    },
    retain(source) {
      activeSources.add(source);

      if (!open) return;
      clearTimeout(closeTimer);
      closeTimer = undefined;
    },
    leave(source) {
      activeSources.delete(source);

      if (active()) return;

      if (!open) {
        close();

        return;
      }

      clearTimeout(closeTimer);
      closeTimer = setTimeout(close, CLOSE_GRACE_MS);
    },
    settled() {
      if (!active() || !requested || open) return;
      clearTimeout(openTimer);
      clearTimeout(capTimer);
      capTimer = undefined;
      openTimer = setTimeout(show, Math.max(0, OPEN_DELAY_MS - (Date.now() - startedAt)));
    },
    close,
    dispose() {
      activeSources.clear();
      requested = false;
      open = false;
      clearOpening();
      clearTimeout(closeTimer);
      closeTimer = undefined;
    },
  };
}

export function useHoverIntent() {
  const [open, setOpen] = useState(false);
  const [fetchSequence, setFetchSequence] = useState(0);
  const controller = useRef<HoverIntentController | null>(null);

  if (!controller.current) {
    controller.current = createHoverIntentController({
      onFetch: () => setFetchSequence((value) => value + 1),
      onOpenChange: setOpen,
    });
  }

  useEffect(() => () => controller.current?.dispose(), []);

  const begin = useCallback((source: HoverIntentSource) => controller.current?.begin(source), []);
  const retain = useCallback((source: HoverIntentSource) => controller.current?.retain(source), []);
  const leave = useCallback((source: HoverIntentSource) => controller.current?.leave(source), []);
  const settled = useCallback(() => controller.current?.settled(), []);
  const close = useCallback(() => controller.current?.close(), []);

  return {
    open,
    fetchSequence,
    begin,
    retain,
    leave,
    settled,
    close,
  };
}
