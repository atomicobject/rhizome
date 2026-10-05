import type { ComponentType } from "react";
import type {
  NodeBodyBlock,
  NodeBodyBlockKind,
  NodeWorkspace,
  OntologyEditOp,
  StructuralNode,
  WorkspaceNode,
} from "../../api/types";

/**
 * Render mode: "view" = read-only display, "edit" = interactive / staging
 * mutations via onStageOps. Components may render the same tree in both
 * modes; the registry just picks which component handles which mode.
 */
export type BodyRenderMode = "view" | "edit";

/**
 * Shared context threaded to every body renderer. Holds the workspace graph
 * (so cross-ref lookups resolve), current mode, and the callbacks a renderer
 * needs to stage mutations or open other nodes.
 */
export type BodyRenderContext = {
  workspace: NodeWorkspace;
  mode: BodyRenderMode;
  /** Vault owner used for raw editor draft persistence. */
  vaultKey?: string | null;
  /** Rendered note metadata, including resolved wikilink targets. */
  rendered?: NodeWorkspace["content"]["rendered"] | null;
  /** Stage one or more ops against the active edit session. */
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | void;
  /** Open a note path in the current pane or in a new stacked pane. */
  onOpen?: (path: string, target?: "current" | "stack" | "beside") => void;
  /** Open a child node in a new stacked pane (for PANE-display children). */
  onOpenNode?: (node: StructuralNode) => void;
  /** Register the DOM target for an inline child section by canonical node id. */
  registerSectionTarget?: (nodeId: string, element: HTMLElement | null) => void;
  /** Look up a workspace node by canonical id; returns undefined if missing. */
  lookupNode: (id: string) => WorkspaceNode | undefined;
};

/**
 * The shape every body renderer receives.
 */
export type BodyRendererProps<T extends NodeBodyBlock = NodeBodyBlock> = {
  block: T;
  /** Stable position among body blocks of the same kind. */
  kindOrdinal: number;
  /** The node whose body contains this block — handy for ops targeting the parent. */
  node: WorkspaceNode;
  context: BodyRenderContext;
};

export type BodyRenderer<T extends NodeBodyBlock = NodeBodyBlock> = ComponentType<
  BodyRendererProps<T>
>;

/**
 * One entry in the lookup table. A `typeName` override allows type-specific
 * renderers (e.g. render `UserStory` child-sections differently from generic
 * `NarrativeSection` child-sections) without touching the walker.
 */
export type RegistryEntry = {
  kind: NodeBodyBlockKind;
  typeName?: string; // resolved-type discriminator (optional)
  view: BodyRenderer;
  /** Defaults to `view` when omitted. */
  edit?: BodyRenderer;
};

/**
 * Build a lookup function from a registry table. Exact (kind, typeName)
 * matches win; otherwise falls back to kind-only matches, then to the
 * explicit fallback component if provided.
 */
export function buildRegistry(entries: RegistryEntry[], fallback: BodyRenderer) {
  const byKindAndType = new Map<string, RegistryEntry>();
  const byKind = new Map<string, RegistryEntry>();

  for (const entry of entries) {
    if (entry.typeName) {
      byKindAndType.set(`${entry.kind}:${entry.typeName}`, entry);
    } else {
      byKind.set(entry.kind, entry);
    }
  }

  return function pickRenderer(
    block: NodeBodyBlock,
    typeName: string | undefined,
    mode: BodyRenderMode,
  ): BodyRenderer {
    if (typeName) {
      const specialized = byKindAndType.get(`${block.kind}:${typeName}`);

      if (specialized) return rendererForMode(specialized, mode);
    }

    const generic = byKind.get(block.kind);

    if (generic) return rendererForMode(generic, mode);

    return fallback;
  };
}

function rendererForMode(entry: RegistryEntry, mode: BodyRenderMode) {
  if (mode === "edit" && entry.edit) return entry.edit;

  return entry.view;
}
