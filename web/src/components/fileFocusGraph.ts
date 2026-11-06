import type { FileView, GraphEdge, GraphNode, GraphResponse } from "../api/types";
import type { GraphViewNode } from "./graphViewShared";
import { fileFocusLabels, fileFocusRowHeight } from "./fileFocusLabels";

/** File details include code-anchor matches that the graph projection does not. */
export function mergeFileNotes(graph: GraphResponse, file?: FileView): GraphResponse {
  if (file?.kind !== "code") return graph;
  const focused = graph.nodes.find((node) => node.path === file.path);

  if (!focused) return graph;
  const nodes = [...graph.nodes];
  const edges = [...graph.edges];

  for (const note of file.relatedNotes ?? []) {
    let node = nodes.find((n) => (n.notePath || n.path) === note.path && n.kind === "note");

    if (!node) {
      node = {
        id: `note:${note.path}`,
        kind: "note",
        path: note.path,
        label: note.title || note.path.split("/").pop() || note.path,
      };
      nodes.push(node);
    }

    if (
      !edges.some(
        (edge) =>
          [edge.source, edge.target].includes(node.id) &&
          [edge.source, edge.target].includes(focused.id),
      )
    ) {
      edges.push({ source: node.id, target: focused.id, kind: note.reason || "related" });
    }
  }

  return { ...graph, nodes, edges };
}

export const FILE_NOTE_RELATIONS = ["Documented by", "Mentioned in", "Related in graph"];

const GROUP_LIMIT = 8;

const GROUP_GAP = 80;

const HEADING_GAP = 120;

const isNote = (node: GraphNode) => node.kind === "note" || node.kind === "embedded";

const isTest = (node: GraphNode) =>
  /(^|[/_.-])(tests?|spec)([/_.-]|$)/i.test(node.path || node.label);

export function fileFocusGraph(
  input: GraphResponse,
  path: string,
  file?: FileView,
  expanded: ReadonlySet<string> = new Set(),
): Omit<GraphResponse, "nodes"> & { nodes: GraphViewNode[] } {
  const graph = mergeFileNotes(input, file);
  const focused = graph.nodes.find((node) => node.path === path || node.notePath === path);

  if (!focused)
    return {
      ...graph,
      nodes: graph.nodes.map((node, index) => ({ ...node, x: index * 100, y: 0 })),
    };
  const groups = new Map<string, GraphNode[]>();

  for (const node of graph.nodes) {
    if (node === focused) continue;

    const reasons =
      file?.relatedNotes?.filter((note) => note.path === (node.notePath || node.path)) ?? [];

    const called = graph.edges.some(
      (edge) =>
        [edge.source, edge.target].includes(focused.id) &&
        [edge.source, edge.target].includes(node.id) &&
        /call|import|depend|reference/.test(edge.kind),
    );

    const tested =
      isTest(node) ||
      graph.edges.some(
        (edge) =>
          edge.kind === "tests" &&
          [edge.source, edge.target].includes(focused.id) &&
          [edge.source, edge.target].includes(node.id),
      );

    const label = isNote(node)
      ? reasons.some((note) => note.reason === "coderef")
        ? "Documented by"
        : reasons.some((note) => note.reason === "codeanchor")
          ? "Mentioned in"
          : file?.kind === "code"
            ? "Related in graph"
            : "Linked notes"
      : tested
        ? "Tests"
        : called
          ? "Calls / imports"
          : "Other code";

    const members = groups.get(label) ?? [];
    members.push(node);
    groups.set(label, members);
  }

  const nodes: GraphViewNode[] = [
    { ...focused, center: true, label: path.split("/").pop() || focused.label, x: 0, y: 0 },
  ];

  const edges: GraphEdge[] = [];

  for (const side of ["notes", "code"]) {
    const entries = [...groups]
      .filter(([, members]) => isNote(members[0]) === (side === "notes"))
      .sort(([a], [b]) => FILE_NOTE_RELATIONS.indexOf(a) - FILE_NOTE_RELATIONS.indexOf(b));

    const visible = (members: GraphNode[], label: string): GraphViewNode[] =>
      expanded.has(label) || members.length <= GROUP_LIMIT
        ? members
        : [
            ...members.slice(0, GROUP_LIMIT),
            {
              id: `file-more:${label}`,
              kind: side === "notes" ? "note" : "code",
              label: `Show ${members.length - GROUP_LIMIT} more`,
              childCount: members.length - GROUP_LIMIT,
              aggregate: true,
            },
          ];

    const height = (members: GraphNode[], label: string, collapsed = false) => {
      const shown = fileFocusLabels(
        collapsed
          ? members.slice(0, members.length > GROUP_LIMIT ? GROUP_LIMIT + 1 : GROUP_LIMIT)
          : visible(members, label),
      );

      const rows = shown.length > 20 ? Math.ceil(shown.length / 2) : shown.length;

      return (
        HEADING_GAP +
        Math.max(
          ...[shown.slice(0, rows), shown.slice(rows)].map((column) =>
            column.reduce((sum, node) => sum + fileFocusRowHeight(node), 0),
          ),
        ) +
        GROUP_GAP
      );
    };

    const totalHeight = entries.reduce(
      (sum, [label, members]) => sum + height(members, label, true),
      0,
    );

    let top = totalHeight / 2 - (side === "code" ? 60 : 0);

    for (const [label, members] of entries) {
      members.sort((a, b) => (a.path || a.label).localeCompare(b.path || b.label));
      const groupID = `file-group:${label}`;

      const kind =
        side === "notes"
          ? "explorer-documents"
          : label === "Tests"
            ? "explorer-tests"
            : "explorer-code";

      const direction = side === "notes" ? -1 : 1;
      const groupY = top;
      nodes.push({
        id: groupID,
        kind: side === "notes" ? "note" : "code",
        label,
        labelDetail: `${members.length} ${side === "notes" ? "notes" : "files"}`,
        aggregate: true,
        x: direction * 210,
        y: groupY,
      });
      edges.push({ source: focused.id, target: groupID, kind });

      const shown = fileFocusLabels(visible(members, label));

      // Grow sideways when another group occupies the space below this one.
      const rows =
        entries.at(-1)?.[0] !== label
          ? Math.min(members.length, GROUP_LIMIT + 1)
          : shown.length > 20
            ? Math.ceil(shown.length / 2)
            : shown.length;

      let itemY = groupY - HEADING_GAP;
      shown.forEach((node, index) => {
        if (index % rows === 0) itemY = groupY - HEADING_GAP;
        nodes.push({ ...node, x: direction * (280 + Math.floor(index / rows) * 1000), y: itemY });
        itemY -= fileFocusRowHeight(node);
        edges.push({ source: groupID, target: node.id, kind });
      });
      top -= height(members, label, true);
    }
  }

  // Preserve neighbour-to-neighbour links, including links to aggregated members.
  const drawnIDs = new Set(nodes.map((node) => node.id));

  const aggregateID = (id: string) => {
    if (drawnIDs.has(id)) return id;
    const entry = [...groups].find(([, members]) => members.some((node) => node.id === id));

    return entry ? `file-more:${entry[0]}` : id;
  };

  for (const edge of graph.edges) {
    if (edge.source === focused.id || edge.target === focused.id) continue;

    const source = aggregateID(edge.source),
      target = aggregateID(edge.target);

    if (source !== target && drawnIDs.has(source) && drawnIDs.has(target))
      edges.push({ ...edge, source, target });
  }

  return { ...graph, nodes, edges };
}
