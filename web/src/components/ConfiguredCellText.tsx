import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { splitWikilinks } from "../lib/content";
import { getVaultIndexRevision, subscribeVaultIndexRevision } from "../query/vaultIndexRevision";
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
  const indexRevision = useSyncExternalStore(subscribeVaultIndexRevision, getVaultIndexRevision);

  const resolver = useMemo(
    () => createRhizomeLinkAdapter({ currentPath: from || "" }),
    [from, indexRevision],
  );

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
            resolver={resolver}
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
  resolver,
  onOpen,
}: {
  target: string;
  label: string;
  resolver: ReturnType<typeof createRhizomeLinkAdapter>;
  onOpen: OpenNote;
}) {
  const [resolved, setResolved] = useState<string | null | undefined>(undefined);

  useEffect(() => {
    let active = true;
    void resolver.resolveTarget(target).then(
      (path) => {
        if (active) setResolved(path);
      },
      () => {
        if (active) setResolved(null);
      },
    );

    return () => {
      active = false;
    };
  }, [resolver, target]);

  if (!resolved)
    return (
      <span title={resolved === null ? `Could not resolve ${target}` : undefined}>{label}</span>
    );

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
      <NoteLinkPreview target={resolved} open={onOpen}>
        {label}
      </NoteLinkPreview>
    </span>
  );
}
