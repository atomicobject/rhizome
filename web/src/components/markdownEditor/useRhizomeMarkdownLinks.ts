import { useCallback, useEffect, useMemo, useRef } from "react";
import type { RenderedFile } from "../../api/types";
import { createRhizomeLinkAdapter } from "./rhizomeLinks";
import type { MarkdownEditorLinks } from "./types";

type UseRhizomeMarkdownLinksOptions = {
  currentPath: string;
  rendered?: RenderedFile | null;
  onOpen?: (path: string, target?: "current" | "stack" | "beside") => void;
};

export function useRhizomeMarkdownLinks({
  currentPath,
  rendered,
  onOpen,
}: UseRhizomeMarkdownLinksOptions): MarkdownEditorLinks {
  const onOpenRef = useRef(onOpen);
  useEffect(() => {
    onOpenRef.current = onOpen;
  }, [onOpen]);

  const adapter = useMemo(
    () => createRhizomeLinkAdapter({ currentPath, rendered }),
    [currentPath, rendered],
  );

  const openInternal = useCallback(
    (target: string) => {
      void adapter.resolveTarget(target).then((resolvedTarget) => {
        if (resolvedTarget) onOpenRef.current?.(resolvedTarget, "stack");
      });
    },
    [adapter],
  );

  const onLinkClick = useCallback(
    (url: string) => {
      if (/^(https?:|mailto:)/i.test(url)) {
        try {
          window.open(url, "_blank", "noopener,noreferrer");
        } catch {
          // Sandboxed browser contexts can reject window.open.
        }

        return;
      }

      if (/^[a-z][a-z0-9+.-]*:/i.test(url)) return;
      openInternal(url);
    },
    [openInternal],
  );

  return useMemo(
    () => ({
      resolveWikiLink: adapter.resolveWikiLink,
      suggestWikiLinks: adapter.suggestWikiLinks,
      onOpenWikiLink: openInternal,
      onLinkClick,
    }),
    [adapter, onLinkClick, openInternal],
  );
}
