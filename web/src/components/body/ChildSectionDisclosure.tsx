import { createElement, type MouseEvent } from "react";
import { publicWorkspaceRef } from "../../api/client";
import { ChildSectionBody, hasBodyBlocks, headingTag, resolveChild } from "./ChildSectionInline";
import { extractChildIdentity, structuralNodeFromWorkspace } from "./childIdentity";
import type { BodyRendererProps } from "./registry";

/**
 * A PANE-display child section (`@contains` default) as a collapsed
 * disclosure that reads like the note's own headings. The heading carries the
 * identity a reader scans (ID, title, status, summary); expanding reads and
 * edits the body in place, and Open drills into the section as its own node.
 *
 * The body stays mounted while collapsed so nested sections register as
 * outline targets; outline navigation opens the enclosing `<details>`.
 */
export function ChildSectionDisclosure({ block, context }: BodyRendererProps) {
  const child = resolveChild(block.childRef?.nodeId, context);

  if (!child) return null;
  const identity = extractChildIdentity(child, context.workspace);
  const structural = context.onOpenNode ? structuralNodeFromWorkspace(child) : null;
  // Past the server's body cap the section reads from markdown; edit it as a node.
  const editElsewhere = context.mode === "edit" && !hasBodyBlocks(child);

  const open = (event: MouseEvent) => {
    // The button sits inside <summary>; don't also toggle the disclosure.
    event.preventDefault();

    if ((event.metaKey || event.ctrlKey) && context.onOpen) {
      context.onOpen(publicWorkspaceRef(child.ref), "beside");

      return;
    }

    if (structural) context.onOpenNode?.(structural);
  };

  return (
    <details
      className="body-disclosure"
      data-field={block.fieldName}
      ref={(element) => {
        if (child.ref.nodeId) context.registerSectionTarget?.(child.ref.nodeId, element);
      }}
    >
      <summary className="body-disclosure__summary">
        {createElement(
          headingTag(child),
          { className: "body-disclosure__heading" },
          <span className="body-disclosure__chevron" aria-hidden="true" />,
          identity.id && <span className="body-disclosure__id">{identity.id}</span>,
          <span className="body-disclosure__title">{identity.title}</span>,
          identity.status && <span className="body-disclosure__meta">{identity.status}</span>,
          identity.summary && (
            <span className="body-disclosure__excerpt" title={identity.summary}>
              {identity.summary}
            </span>
          ),
          structural && (
            <button
              type="button"
              className={`body-section__open${editElsewhere ? " is-edit" : ""}`}
              aria-label={`${editElsewhere ? "Edit" : "Open"} section ${identity.title}`}
              title={editElsewhere ? "Open this section to edit it" : "Open section"}
              onClick={open}
            >
              {editElsewhere ? "Edit" : "Open"}
            </button>
          ),
        )}
      </summary>
      <div className="body-disclosure__body">
        <ChildSectionBody child={child} context={context} />
      </div>
    </details>
  );
}
