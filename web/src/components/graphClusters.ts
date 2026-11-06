import type { GraphViewNode } from "./graphViewShared";

export function graphClusters(nodes: GraphViewNode[], communityColors: Map<string, string>) {
  const byCommunity = new Map<string, { size: number; topLabel: string; topAuthority: number }>();
  nodes.forEach((node) => {
    if (!node.community) return;

    const current = byCommunity.get(node.community) || {
      size: 0,
      topLabel: "",
      topAuthority: -1,
    };

    current.size += 1;

    if (node.kind === "note" && (node.authority ?? -1) > current.topAuthority) {
      current.topAuthority = node.authority ?? -1;
      current.topLabel = node.label;
    }

    byCommunity.set(node.community, current);
  });

  return Array.from(byCommunity.entries())
    .map(([id, data]) => ({
      id,
      size: data.size,
      label: data.topLabel || `Cluster ${id}`,
      color: communityColors.get(id) || "var(--type-8)",
    }))
    .sort((a, b) => b.size - a.size);
}
