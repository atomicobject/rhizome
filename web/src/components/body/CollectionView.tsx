import { useMemo, useState } from "react";
import { isContentNode, type NodeRef } from "../../api/types";
import { publicWorkspaceRef } from "../../api/client";
import { formatTypeName } from "../../lib/typeNames";
import { useNotePreviewTrigger } from "../notePreview/NoteLinkPreview";
import { extractChildIdentity, structuralNodeFromWorkspace } from "./childIdentity";
import type { BodyRendererProps } from "./registry";

const SEARCH_THRESHOLD = 3;

/**
 * Collection body block renderer. Groups the list-bound children from the
 * parent's `@contains(list)` field into a scannable table: one row per
 * item with type badge, ID, title, status chip, and summary ellipsis.
 * Rows open the item in a new stacked pane; a search input appears when
 * the collection grows past `SEARCH_THRESHOLD` entries so large lists
 * stay filterable without cluttering small ones.
 *
 * Rows are always compact — inline expansion is a future iteration. The
 * one-click "open pane" is the standard way to see full item content.
 */
export function CollectionView({ block, context }: BodyRendererProps) {
  const [filter, setFilter] = useState("");

  const rows = useMemo(() => {
    const refs = block.childRefs || [];

    return refs.flatMap((ref) => {
      const row = buildRow(ref, context);

      return row ? [row] : [];
    });
  }, [block.childRefs, context]);

  const filtered = useMemo(() => {
    const needle = filter.trim().toLowerCase();

    if (!needle) return rows;

    return rows.filter((row) => row.haystack.includes(needle));
  }, [filter, rows]);

  if (rows.length === 0) return null;

  return (
    <section className="body-collection" data-field={block.fieldName}>
      <header className="body-collection__header">
        <span className="body-collection__label">{block.fieldName || "Included"}</span>
        <span className="body-collection__count">
          {filtered.length}
          {filtered.length !== rows.length ? ` / ${rows.length}` : ""}
        </span>
        {rows.length > SEARCH_THRESHOLD && (
          <input
            type="search"
            className="body-collection__search"
            placeholder={`Filter ${rows.length} items`}
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            aria-label={`Filter ${block.fieldName || "items"}`}
          />
        )}
      </header>
      {filtered.length === 0 ? (
        <div className="body-collection__empty">No matching items.</div>
      ) : (
        <ul className="body-collection__rows">
          {filtered.map((row) => (
            <li key={row.key}>
              <CollectionItem row={row} context={context} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function CollectionItem({
  row,
  context,
}: {
  row: CollectionRow;
  context: BodyRendererProps["context"];
}) {
  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target: row.refKey,
    from: context.workspace.node.notePath,
    open: (path, mode) => context.onOpen?.(path, mode),
  });

  return (
    <>
      <button
        {...previewTrigger.triggerProps}
        type="button"
        className="body-collection__row"
        onClick={(event) => {
          previewTrigger.close();

          if ((event.metaKey || event.ctrlKey) && context.onOpen) {
            context.onOpen(row.refKey, "beside");
          } else {
            row.open();
          }
        }}
      >
        <div className="body-collection__row-head">
          {row.typeName && (
            <span className="body-collection__type">{formatTypeName(row.typeName)}</span>
          )}
          {row.id && <span className="body-collection__id">{row.id}</span>}
          <span className="body-collection__title">{row.title}</span>
          {row.status && (
            <span
              className="body-collection__status widget-enum widget-enum--badge"
              data-value={row.status}
            >
              {row.status}
            </span>
          )}
        </div>
        {row.summary && (
          <div className="body-collection__summary" title={row.summary}>
            {row.summary}
          </div>
        )}
      </button>
      {previewTrigger.preview}
    </>
  );
}

type CollectionRow = {
  key: string;
  refKey: string;
  title: string;
  typeName: string;
  id: string;
  status: string;
  summary: string;
  haystack: string;
  open: () => void;
};

function buildRow(ref: NodeRef, context: BodyRendererProps["context"]): CollectionRow | null {
  const child = context.workspace.nodes?.find(
    (node) => isContentNode(node) && node.ref.nodeId === ref.nodeId,
  );

  if (!child) return null;
  const identity = extractChildIdentity(child, context.workspace);

  const haystack = [
    identity.title,
    identity.id,
    identity.status,
    identity.summary,
    identity.typeName,
  ]
    .join(" ")
    .toLowerCase();

  return {
    key: child.id,
    refKey: publicWorkspaceRef(child.ref),
    title: identity.title || "Untitled",
    typeName: identity.typeName,
    id: identity.id,
    status: identity.status,
    summary: identity.summary,
    haystack,
    open: () => {
      const structural = structuralNodeFromWorkspace(child);

      if (structural) context.onOpenNode?.(structural);
    },
  };
}
