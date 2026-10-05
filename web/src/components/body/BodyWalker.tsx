import type { WorkspaceNode } from "../../api/types";
import { ChildSectionRouter } from "./ChildSectionRouter";
import { CollectionView } from "./CollectionView";
import { NarrativeEditor } from "./NarrativeEditor";
import { NarrativeView } from "./NarrativeView";
import {
  type BodyRenderContext,
  type BodyRenderer,
  type BodyRendererProps,
  buildRegistry,
} from "./registry";

/**
 * Fallback renderer for body blocks whose kind isn't in the registry. We
 * log a warning in development so schema additions without matching
 * renderers surface during dev — the production build stays silent.
 */
const UnknownBlockView: BodyRenderer = ({ block }) => {
  // eslint-disable-next-line no-console
  console.warn(`[BodyWalker] no renderer registered for block kind "${block.kind}"`);

  return null;
};

/**
 * Default registry for the structural view. Additional type-specific entries
 * (e.g. a UserStory-flavored collection row renderer) can be layered in later
 * via a second registry or via a wrapper component — this base set covers
 * the four kinds the server currently emits.
 */
const pickRenderer = buildRegistry(
  [
    { kind: "narrative", view: NarrativeView, edit: NarrativeEditor },
    { kind: "child_section", view: ChildSectionRouter },
    { kind: "collection", view: CollectionView },
  ],
  UnknownBlockView,
);

type Props = {
  /** The node whose body should be walked. */
  node: WorkspaceNode;
  /** Shared render context; threaded to every renderer. */
  context: BodyRenderContext;
};

/**
 * Iterates the node's body blocks in order and renders each one via the
 * registry-picked component. Recursive by design: `ChildSectionInline`
 * reaches back into `BodyWalker` for the nested child, producing the full
 * tree from a flat-list-per-node graph.
 */
export function BodyWalker({ node, context }: Props) {
  const blocks = (node.body || []).filter((block) => block.kind !== "inline_field");

  if (blocks.length === 0) return null;
  const typeName = resolveTypeName(node);
  const kindOrdinals = new Map<string, number>();

  return (
    <>
      {blocks.map((block, index) => {
        const kindOrdinal = kindOrdinals.get(block.kind) ?? 0;
        kindOrdinals.set(block.kind, kindOrdinal + 1);
        const Renderer = pickRenderer(block, typeName, context.mode);
        const props: BodyRendererProps = { block, kindOrdinal, node, context };
        // Fragment key falls back to index when byte-range repeats (e.g.
        // two adjacent inline fields on the same line rarely happens but
        // shouldn't break React reconciliation).
        const key = bodyBlockKey(node, block, index, kindOrdinal);

        return <Renderer key={key} {...props} />;
      })}
    </>
  );
}

function bodyBlockKey(
  node: WorkspaceNode,
  block: NonNullable<WorkspaceNode["body"]>[number],
  index: number,
  kindOrdinal: number,
) {
  if (block.kind === "narrative") return `${node.id}:narrative:${kindOrdinal}`;

  if (block.childRef?.nodeId) return `${block.kind}:${block.childRef.nodeId}`;

  if (block.fieldName) return `${block.kind}:${block.fieldName}`;

  return `${block.kind}:${index}`;
}

function resolveTypeName(node: WorkspaceNode): string | undefined {
  if (!("data" in node) || !node.data) return undefined;

  return node.data.binding?.typeName || node.data.resolvedType || undefined;
}
