import { createElement } from "react";
import { isContentNode, type NodeBodyBlock, type WorkspaceNode } from "../../api/types";
import { BodyWalker } from "./BodyWalker";
import { structuralNodeFromWorkspace } from "./childIdentity";
import { NarrativeView } from "./NarrativeView";
import type { BodyRendererProps } from "./registry";

/**
 * Render a child section inline within the parent's body.
 *
 * This is the recursion point: we look up the child node in the workspace
 * graph and delegate to a nested `<BodyWalker>`. Declared `INLINE` sections
 * and undeclared headings both land here. The section reads as the document
 * the author wrote: its heading at the authored level, then its body.
 */
export function ChildSectionInline({ block, context }: BodyRendererProps) {
  const child = resolveChild(block.childRef?.nodeId, context);

  if (!child) return null;
  const title = childTitle(child);
  const childNodeID = child.ref.nodeId;
  const projected = hasBodyBlocks(child);
  const structural = context.onOpenNode ? structuralNodeFromWorkspace(child) : null;
  // A section without projected blocks (beyond the server's body cap) reads
  // from markdown and is not editable here; say where editing is.
  const editElsewhere = context.mode === "edit" && !projected;

  return (
    <section
      className="body-section"
      data-field={block.fieldName}
      ref={(element) => {
        if (childNodeID) {
          context.registerSectionTarget?.(childNodeID, element);
        }
      }}
    >
      {title &&
        createElement(
          headingTag(child),
          { className: "body-section__title" },
          title,
          structural && (
            <button
              type="button"
              className={`body-section__open${editElsewhere ? " is-edit" : ""}`}
              aria-label={`${editElsewhere ? "Edit" : "Open"} section ${title}`}
              title={editElsewhere ? "Open this section to edit it" : "Open section"}
              onClick={() => context.onOpenNode?.(structural)}
            >
              {editElsewhere ? "Edit" : "Open"}
            </button>
          ),
        )}
      {projected ? (
        <BodyWalker node={child} context={context} />
      ) : (
        // The server projects blocks for every inline section up to its
        // body cap. Past it, sections still carry their markdown, so they
        // read from that; editing happens when the section opens as a node.
        markdownBody(child, context).map((part, index) =>
          part.kind === "narrative" ? (
            <NarrativeView
              key="narrative"
              block={part}
              kindOrdinal={0}
              node={child}
              context={context}
            />
          ) : (
            <ChildSectionInline
              key={part.childRef?.nodeId || index}
              block={part}
              kindOrdinal={index}
              node={child}
              context={context}
            />
          ),
        )
      )}
    </section>
  );
}

function hasBodyBlocks(node: WorkspaceNode): boolean {
  return (node.body || []).some((block) => block.kind !== "inline_field");
}

/**
 * Rebuild a section's body from its markdown: the prose before its first
 * subsection, then each direct subsection in document order. Subsections are
 * found by where their markdown sits in this section's markdown, because a
 * section past the body cap has no blocks naming its children.
 */
function markdownBody(
  node: WorkspaceNode,
  context: Pick<BodyRendererProps["context"], "workspace">,
): NodeBodyBlock[] {
  const markdown = isContentNode(node) ? node.data.markdown || "" : "";
  const body = markdown.replace(/^ {0,3}#{1,6}(?:[ \t][^\n]*)?(?:\n|$)/, "");

  const contained = (context.workspace.nodes || [])
    .flatMap((candidate) => {
      if (candidate === node || !isContentNode(candidate) || candidate.kind === "note") return [];

      if (candidate.notePath !== node.notePath) return [];
      const text = candidate.data.markdown || "";
      const start = text ? body.indexOf(text) : -1;

      return start >= 0 ? [{ child: candidate, start, end: start + text.length }] : [];
    })
    .sort((left, right) => left.start - right.start || right.end - left.end);

  // Keep only direct subsections: skip anything inside one already taken.
  const children: typeof contained = [];

  for (const entry of contained) {
    const last = children.at(-1);

    if (!last || entry.start >= last.end) children.push(entry);
  }

  const prose = body.slice(0, children[0]?.start ?? body.length);
  const blocks: NodeBodyBlock[] = [];

  if (prose.trim()) {
    blocks.push({ kind: "narrative", range: { start: 0, end: 0 }, markdown: prose });
  }

  for (const { child } of children) {
    blocks.push({ kind: "child_section", range: { start: 0, end: 0 }, childRef: child.ref });
  }

  return blocks;
}

function headingTag(node: WorkspaceNode): string {
  const level = isContentNode(node) ? node.data.level : undefined;
  const depth = Number(level?.match(/^H([1-6])$/i)?.[1] ?? 2);

  return `h${depth}`;
}

function resolveChild(
  nodeId: string | undefined,
  context: BodyRendererProps["context"],
): WorkspaceNode | undefined {
  if (!nodeId) return undefined;

  return (
    context.lookupNode(nodeId) ||
    context.workspace.nodes?.find((node) => node.id === nodeId || node.ref.nodeId === nodeId)
  );
}

function childTitle(node: WorkspaceNode): string {
  if (isContentNode(node)) return node.data.title || "";

  return "";
}
