import type {
  OntologyTypeSummary,
  OntologyInterfaceSummary,
  OntologySummaryResponse,
} from "../api/types";

export type RailNode =
  | { kind: "type"; type: OntologyTypeSummary; children: RailNode[] }
  | { kind: "interface"; interface: OntologyInterfaceSummary; children: RailNode[] };

export type RailGroup = { name: string; children: RailNode[] };

type DisplayPresentation = { group: string; parent: string };

export function label(item: { name: string; label?: string; pluralLabel?: string }): string {
  return item.pluralLabel || item.label || item.name;
}

function presentation(item: OntologyTypeSummary | OntologyInterfaceSummary): DisplayPresentation {
  return {
    group: item.displayGroup?.trim() ?? "",
    parent: item.displayParent?.trim() ?? "",
  };
}

function compareCodeUnits(left: string, right: string): number {
  if (left === right) return 0;

  return left < right ? -1 : 1;
}

function railNodeLabel(node: RailNode): string {
  return label(node.kind === "type" ? node.type : node.interface);
}

function sortRailNodes(nodes: RailNode[]): RailNode[] {
  return nodes
    .map((node) => ({ ...node, children: sortRailNodes(node.children) }))
    .sort((left, right) => railNodeLabel(left).localeCompare(railNodeLabel(right)));
}

export function buildRailGroups(summary: OntologySummaryResponse | null): RailGroup[] {
  const types = summary?.types ?? [];
  const interfaces = summary?.interfaces ?? [];
  const nodes = new Map<string, RailNode>();

  for (const type of types) nodes.set(type.name, { kind: "type", type, children: [] });

  for (const iface of interfaces)
    nodes.set(iface.name, { kind: "interface", interface: iface, children: [] });
  const parents = new Map<string, string>();

  const setParent = (child: string, parent: string) => {
    if (child === parent || !nodes.has(child) || !nodes.has(parent) || parents.has(child)) return;

    for (let ancestor = parent; ancestor; ancestor = parents.get(ancestor) || "") {
      if (ancestor === child) return;
    }

    parents.set(child, parent);
  };

  // Claim order must match displayTree in pkg/ontology/viewconfig/display_groups.go
  // (pinned by testdata/display-groups/rail-groups.json): explicit parents by
  // name, then interfaces by label with a name tie-break, in code-unit order.
  for (const name of [...nodes.keys()].sort(compareCodeUnits)) {
    const node = nodes.get(name)!;
    const item = node.kind === "type" ? node.type : node.interface;
    const { parent } = presentation(item);

    if (parent) setParent(name, parent);
  }

  for (const iface of [...interfaces].sort(
    (left, right) =>
      compareCodeUnits(label(left), label(right)) || compareCodeUnits(left.name, right.name),
  )) {
    for (const implementor of iface.implementors) setParent(implementor, iface.name);
  }

  for (const [child, parent] of parents) nodes.get(parent)?.children.push(nodes.get(child)!);

  const groupFor = (name: string): string | null => {
    for (let current = name; current; current = parents.get(current) || "") {
      const node = nodes.get(current);

      if (!node) continue;
      const item = node.kind === "type" ? node.type : node.interface;
      const { group } = presentation(item);

      if (group) return group;
    }

    return null;
  };

  const grouped = new Map<string, RailNode[]>();

  for (const [name, node] of nodes) {
    if (parents.has(name)) continue;

    if (node.kind === "type" && node.type.role === "embedded") continue;
    const group = groupFor(name) ?? "Other";
    grouped.set(group, [...(grouped.get(group) ?? []), node]);
  }

  return [...grouped.entries()]
    .map(([name, children]) => ({ name, children: sortRailNodes(children) }))
    .sort((left, right) => {
      if (left.name === "Other") return 1;

      if (right.name === "Other") return -1;

      return left.name.localeCompare(right.name);
    });
}

export function flattenRailNodes(nodes: RailNode[]): RailNode[] {
  return nodes.flatMap((node) => [node, ...flattenRailNodes(node.children)]);
}
