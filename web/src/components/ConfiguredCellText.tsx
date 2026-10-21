import { useQuery } from "@tanstack/react-query";
import { queryKeys } from "../api/queryKeys";
import { splitWikilinks } from "../lib/content";
import { createRhizomeLinkAdapter } from "./markdownEditor/rhizomeLinks";
import { NoteLinkPreview, type OpenNote } from "./notePreview/NoteLinkPreview";

export function hasCellLinks(value: string) {
  return splitWikilinks(value).some((part) => part.kind === "link");
}

export function ConfiguredCellText({
  value,
  from,
  onOpen,
}: {
  value: string;
  from?: string;
  onOpen: OpenNote;
}) {
  return (
    <>
      {splitWikilinks(value).map((part, index) =>
        part.kind === "text" ? (
          part.text
        ) : (
          <CellLink
            key={`${from}:${part.target}:${index}`}
            target={part.target}
            label={part.label}
            from={from}
            onOpen={onOpen}
          />
        ),
      )}
    </>
  );
}

function CellLink({
  target,
  label,
  from,
  onOpen,
}: {
  target: string;
  label: string;
  from?: string;
  onOpen: OpenNote;
}) {
  // Named targets share a lookup across rows; relative targets need the source path.
  const sourcePath =
    target.startsWith(".") || target.startsWith("#") || target.startsWith("^") ? from : undefined;

  const query = useQuery({
    queryKey: queryKeys.nodes.linkTarget(target, sourcePath),
    queryFn: ({ signal }) =>
      createRhizomeLinkAdapter({ currentPath: sourcePath || "", signal }).resolveTarget(target),
    staleTime: 30_000,
    retry: false,
  });

  if (!query.data)
    return <span title={query.isPending ? undefined : `Could not resolve ${target}`}>{label}</span>;

  return (
    <span
      data-configured-relation-link
      onClick={(event) => event.stopPropagation()}
      onFocus={(event) => event.stopPropagation()}
      onDragStart={(event) => {
        event.preventDefault();
        event.stopPropagation();
      }}
    >
      <NoteLinkPreview target={query.data} open={onOpen}>
        {label}
      </NoteLinkPreview>
    </span>
  );
}
