import { getNodePreview } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type { ViewTableRow } from "../api/types";
import { useStagedQuery } from "../staging/stagedQuery";
import { relationTargetRow } from "./ConfiguredTableCellReadOnly";
import { NoteLinkPreview } from "./notePreview/NoteLinkPreview";
import { useActiveEditSession } from "./useOntologyEditSession";
import type { OpenMode } from "./useNoteTabs";

/**
 * The note an embedded record lives in, named as a quiet link; the path stays
 * in the tooltip. It names the note from its file and never fetches: a table
 * can hold hundreds of source notes. Once the hover preview has loaded a note,
 * its title replaces the file name.
 */
export function SourceNoteLink({
  path,
  onOpen,
}: {
  path: string;
  onOpen?: (row: ViewTableRow, mode?: OpenMode) => void;
}) {
  const preview = useStagedQuery({
    subject: queryKeys.nodes.preview(path, undefined),
    session: useActiveEditSession(),
    queryFn: ({ signal, editSession }) => getNodePreview(path, undefined, { editSession, signal }),
    staleTime: 30_000,
    retry: false,
    // Read only what the hover preview cached; one request per row would flood the server.
    enabled: false,
  });

  if (!path) return null;

  const title = preview.data?.title || fileTitle(path);

  return (
    <span
      className="configured-view__source-link"
      data-configured-relation-link
      title={path}
      onClick={(event) => event.stopPropagation()}
    >
      {onOpen ? (
        <NoteLinkPreview
          target={path}
          open={(target, mode) =>
            onOpen(relationTargetRow(target), mode === "beside" ? "beside" : "activate")
          }
        >
          {title}
        </NoteLinkPreview>
      ) : (
        title
      )}
    </span>
  );
}

/** "docs/2026-10-07-17-15-desktop-unified-toolbar.md" reads as "Desktop unified toolbar". */
export function fileTitle(path: string) {
  const name = path
    .replace(/^.*\//, "")
    .replace(/\.[a-z0-9]+$/i, "")
    .replace(/^\d{4}-\d{2}-\d{2}(?:-\d{2}-\d{2})?-/, "")
    .replace(/[-_]+/g, " ")
    .trim();

  return name ? name.charAt(0).toUpperCase() + name.slice(1) : path;
}
