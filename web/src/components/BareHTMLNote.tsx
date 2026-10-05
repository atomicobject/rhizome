import { useCallback } from "react";

import { buildNoteWebHref } from "../lib/content";
import { HTMLNoteViewer } from "./HTMLNoteViewer";

type Props = {
  path: string;
  query: string | null;
  fragment: string | null;
};

/** The canonical note route with `bare` removed, so links leave bare mode. */
function applicationHrefFor(target: string): string {
  const url = new URL(buildNoteWebHref(target), "http://rhizome.invalid");
  url.searchParams.delete("bare");

  return `${url.pathname}${url.search}${url.hash}`;
}

/**
 * Chrome-free HTML note page: the isolated viewer fills the window. Navigation
 * out of the document goes to the full Rhizome note route, so links and
 * generated downloads keep their parent-controlled behavior.
 */
export function BareHTMLNote({ path, query, fragment }: Props) {
  const target = `${path}${query ? `?${query}` : ""}${fragment ? `#${fragment}` : ""}`;

  const onNavigate = useCallback((nextTarget: string, beside: boolean) => {
    const href = applicationHrefFor(nextTarget);

    if (beside) window.open(href, "_blank", "noopener,noreferrer");
    else window.location.assign(href);
  }, []);

  return (
    <main className="bare-html-note">
      <HTMLNoteViewer
        path={path}
        query={query}
        fragment={fragment}
        visible
        bare
        applicationHref={applicationHrefFor(target)}
        onNavigate={onNavigate}
      />
    </main>
  );
}
