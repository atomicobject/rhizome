import { publicTypeName } from "../lib/typeNames";
import type { GraphNode } from "../api/types";

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

export const EDGE_COLORS = {
  faint: "#e8e5e0",
};

export const NODE_SIZE = {
  base: { normal: 6, module: 9 },
};

const COMMUNITY_PALETTE = [
  "#6366f1",
  "#8b5cf6",
  "#ec4899",
  "#f43f5e",
  "#f97316",
  "#eab308",
  "#22c55e",
  "#14b8a6",
  "#06b6d4",
  "#3b82f6",
  "#a855f7",
  "#d946ef",
  "#84cc16",
  "#10b981",
  "#0ea5e9",
  "#6d28d9",
];

const MARKDOWN_COLOR = "#3b82f6";

const LANG_COLORS = new Map<string, string>([
  ["markdown", MARKDOWN_COLOR],
  ["go", "#06b6d4"],
  ["python", "#eab308"],
  ["typescript", MARKDOWN_COLOR],
  ["javascript", "#eab308"],
  ["csharp", "#a855f7"],
  ["rust", "#f97316"],
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

function hashString(input: string) {
  let hash = 0;

  for (let i = 0; i < input.length; i += 1) {
    hash = (hash << 5) - hash + input.charCodeAt(i);
    hash |= 0;
  }

  return hash;
}

export function communityColor(id: string) {
  const index = Math.abs(hashString(id)) % COMMUNITY_PALETTE.length;

  return COMMUNITY_PALETTE[index];
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

  if (node.kind === "module") return "#f59e0b";

  if (node.kind === "embedded") return "#0f766e";

  if (node.community && communityColors.has(node.community)) {
    return communityColors.get(node.community) || MARKDOWN_COLOR;
  }

  if (node.kind === "code") {
    return LANG_COLORS.get(node.lang || "") || "#64748b";
  }

  return MARKDOWN_COLOR;
}

// TYPE_PALETTE is a stable, distinguishable set of colors used to assign a
// deterministic color per ontology type name. Types that aren't present are
// rendered in a neutral fallback. It holds no red: red marks problems and
// selection, so a large type must not paint the graph in the accent color.
const TYPE_PALETTE = [
  "#6366f1", // indigo
  "#ec4899", // pink
  "#22c55e", // green
  "#f97316", // orange
  "#06b6d4", // cyan
  "#a855f7", // purple
  "#eab308", // yellow
  "#14b8a6", // teal
  "#b45309", // amber brown
  "#3b82f6", // blue
  "#84cc16", // lime
  "#d946ef", // fuchsia
  "#0ea5e9", // sky
  "#8b5cf6", // violet
];

/**
 * Build a deterministic resolvedType → color map for a set of type names.
 * Types are sorted alphabetically so the assignment is stable across renders
 * and between sessions. Untyped notes fall back to a neutral slate color.
 */
export function buildTypeColors(typeNames: string[]): Map<string, string> {
  const unique = Array.from(new Set(typeNames.filter(Boolean))).sort((a, b) => a.localeCompare(b));
  const colors = new Map<string, string>();
  unique.forEach((name, index) => {
    colors.set(name, TYPE_PALETTE[index % TYPE_PALETTE.length]);
  });
  colors.set("__untyped__", "#94a3b8");

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
