import { publicTypeName } from "../lib/typeNames";
import type { GraphNode } from "../api/types";
import { typeAccentColor } from "../lib/typeAccent";

/**
 * A graph node that a local/neighborhood view marked as one of its centers.
 * The API never sends `center`; view code adds it before handing nodes to the
 * renderer.
 */
export type GraphViewNode = GraphNode & { center?: boolean };

export type HoverInfo = {
  label: string;
  kind: string;
  path?: string;
  connectionCount: number;
};

export type GraphScope = {
  mode: "global" | "local" | "module";
  label?: string;
};

export function graphNodeNavigationTarget(node: GraphNode): string {
  if (node.kind === "embedded") {
    const locator = (node.sourceLocator || "").trim();

    if (locator) return locator;
    const ref = node.nodeRef;

    if (ref?.notePath && ref.fragment) {
      return `${ref.notePath}#${ref.fragment}`.trim();
    }

    if (ref?.notePath && ref.nodeId?.includes("#")) {
      return `${ref.notePath}#${ref.nodeId.split("#").pop() || ""}`.trim();
    }

    if (node.nodeId?.includes("#")) {
      return node.nodeId.trim();
    }

    const path = (node.path || "").trim();

    if (path.includes("#")) return path;

    return (node.notePath || path).trim();
  }

  return (node.notePath || node.path || "").trim();
}

export const CAMERA = {
  minRatio: 0.05,
  maxRatio: 4,
  zoomStep: 0.8,
};

export const NODE_SIZE = {
  base: { normal: 6, module: 9 },
};

// Graph colors are CSS values on the --type-* palette; useSigmaGraph resolves
// them to concrete colors for WebGL and again when the theme changes.
const MARKDOWN_COLOR = "var(--type-1)";

const NEUTRAL_COLOR = "var(--type-8)";

const LANG_COLORS = new Map<string, string>([
  ["markdown", MARKDOWN_COLOR],
  ["go", "var(--type-4)"],
  ["python", "var(--type-3)"],
  ["typescript", MARKDOWN_COLOR],
  ["javascript", "var(--type-3)"],
  ["csharp", "var(--type-7)"],
  ["rust", "var(--type-6)"],
]);

export const MIXED_EDGE_LABEL = "Mixed";

const EDGE_KIND_LABELS = new Map<string, string>([
  ["wikilink", "Link"],
  ["coderef", "Coderef"],
  ["mentions", "Code anchor"],
  ["calls", "Calls"],
  ["links_to", "Links"],
  ["ontology", "Ontology"],
  ["embeds", "Embeds"],
  ["mixed", MIXED_EDGE_LABEL],
]);

/** Human-readable label for an edge kind, falling back to the raw kind. */
export function edgeKindLabel(kind: string): string {
  return EDGE_KIND_LABELS.get(kind) || kind;
}

const KIND_LABELS = new Map<string, string>([
  ["note", "Note"],
  ["embedded", "Embedded"],
  ["code", "Code"],
  ["module", "Folder"],
]);

/** Human-readable label for a node kind, falling back to the raw kind. */
export function kindLabel(kind: string): string {
  return KIND_LABELS.get(kind) || kind;
}

const ICON_SVGS = {
  note: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 30 30" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><g transform="translate(4.5 4.5) scale(0.85)"><path d="M6 3h8l4 4v14H6z"/><path d="M14 3v5h5"/></g></svg>`,
  embedded: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 30 30" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><g transform="translate(4.5 4.5) scale(0.85)"><rect x="5" y="5" width="14" height="14" rx="2"/><path d="M9 9h6M9 13h6"/><path d="M17 15l3 3M20 15l-3 3"/></g></svg>`,
  code: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 30 30" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><g transform="translate(4.5 4.5) scale(0.85)"><path d="M8 8l-4 4 4 4"/><path d="M16 8l4 4-4 4"/><path d="M14 6l-4 12"/></g></svg>`,
  module: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 30 30" fill="none" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><g transform="translate(4.5 4.5) scale(0.85)"><path d="M3 7h7l2 2h9v10H3z"/><path d="M3 7v-2h6l2 2"/></g></svg>`,
};

function svgDataUri(svg: string) {
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

const CODE_ICON = svgDataUri(ICON_SVGS.code);

const NODE_ICONS = new Map<string, string>([
  ["note", svgDataUri(ICON_SVGS.note)],
  ["embedded", svgDataUri(ICON_SVGS.embedded)],
  ["code", CODE_ICON],
  ["module", svgDataUri(ICON_SVGS.module)],
]);

/** Clusters share the type palette, so the graph never introduces new hues. */
export function communityColor(id: string) {
  return typeAccentColor(id);
}

export function nodeColor(
  node: GraphNode,
  communityColors: Map<string, string>,
  typeColors?: Map<string, string>,
) {
  // When a type-color map is provided, prefer it for notes so ontology
  // visualizations are colored by resolved type rather than community.
  if (typeColors && (node.kind === "note" || node.kind === "embedded")) {
    const resolved = publicTypeName(node.resolvedType);
    const color = typeColors.get(resolved) || typeColors.get("__untyped__");

    if (color) return color;
  }

  if (node.kind === "module") return "var(--type-3)";

  if (node.kind === "embedded") return "var(--type-4)";

  if (node.community && communityColors.has(node.community)) {
    return communityColors.get(node.community) || MARKDOWN_COLOR;
  }

  if (node.kind === "code") {
    return LANG_COLORS.get(node.lang || "") || NEUTRAL_COLOR;
  }

  return MARKDOWN_COLOR;
}

/**
 * Map each resolved type to its palette color (the same slot it has in the
 * Atlas and type pages). Untyped notes use a faint neutral.
 */
export function buildTypeColors(typeNames: string[]): Map<string, string> {
  const colors = new Map<string, string>();

  for (const name of typeNames) {
    if (name) colors.set(name, typeAccentColor(name));
  }

  colors.set("__untyped__", "var(--text-faint)");

  return colors;
}

export function nodeIcon(kind: string): string {
  return NODE_ICONS.get(kind) || CODE_ICON;
}

export function truncatePath(path: string, maxLen: number = 40) {
  if (!path || path.length <= maxLen) return path;
  const parts = path.split("/");

  if (parts.length <= 2) return `...${path.slice(-maxLen + 3)}`;

  return `.../${parts.slice(-2).join("/")}`;
}
