import type { WorkspaceNode } from "../../api/types";
import { publicWorkspaceRef } from "../../api/client";
import { formatTypeName } from "../../lib/typeNames";
import { useNotePreviewTrigger } from "../notePreview/NoteLinkPreview";
import { extractChildIdentity, structuralNodeFromWorkspace } from "./childIdentity";
import type { BodyRendererProps } from "./registry";

/**
 * One-row open-pane affordance for a PANE-display child section. Schema
 * picked this path via `@contains(display: PANE)` (the default) on the
 * parent's field. Clicking opens the child node in a new stacked pane;
 * the body walker does not recurse inline.
 *
 * Shows the type badge, ID, title, status chip, and summary ellipsis so
 * scanning the row gives the same information the target pane would.
 */
export function ChildSectionLink({ block, context }: BodyRendererProps) {
  const child = resolveChild(block.childRef?.nodeId, context);
  const target = child ? publicWorkspaceRef(child.ref) : "";

  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target,
    from: context.workspace.node.notePath,
    open: (path, mode) => context.onOpen?.(path, mode),
  });

  if (!child) return null;
  const identity = extractChildIdentity(child, context.workspace);

  const handleOpen = (beside: boolean) => {
    previewTrigger.close();

    if (beside && context.onOpen) {
      context.onOpen(target, "beside");

      return;
    }

    const structural = structuralNodeFromWorkspace(child);

    if (structural) context.onOpenNode?.(structural);
  };

  return (
    <>
      <button
        {...previewTrigger.triggerProps}
        type="button"
        className="body-child-link"
        onClick={(event) => handleOpen(event.metaKey || event.ctrlKey)}
        data-field={block.fieldName}
        ref={(element) => {
          previewTrigger.triggerProps.ref.current = element;

          if (child.ref.nodeId) context.registerSectionTarget?.(child.ref.nodeId, element);
        }}
      >
        {identity.typeName && (
          <span className="body-child-link__type">{formatTypeName(identity.typeName)}</span>
        )}
        {identity.id && <span className="body-child-link__id">{identity.id}</span>}
        <span className="body-child-link__title">{identity.title}</span>
        {identity.status && (
          <span
            className="body-child-link__status widget-enum widget-enum--badge"
            data-value={identity.status}
          >
            {identity.status}
          </span>
        )}
        {identity.summary && (
          <span className="body-child-link__summary" title={identity.summary}>
            {identity.summary}
          </span>
        )}
        <span className="body-child-link__chevron" aria-hidden="true">
          ›
        </span>
      </button>
      {previewTrigger.preview}
    </>
  );
}

function resolveChild(
  nodeId: string | undefined,
  context: BodyRendererProps["context"],
): WorkspaceNode | undefined {
  if (!nodeId) return undefined;

  return context.workspace.nodes?.find((node) => node.ref.nodeId === nodeId);
}
