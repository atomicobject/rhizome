// What an Overview scope holds and how its members link (SPEC-0117). Pure.
//
// A group scope's members are its member roots and the types nested under
// them, as `planMembers` finds them, without embedded types. All notes holds
// each named display group (collapsed into one node until expanded), each type
// no named group lists, and untyped notes as one member.
//
// - scopeOf(context): the scope a view was opened for.
// - scopeModel(input): the scope's nodes, its table sections, and which nodes
//   each concrete type counts toward.
// - linkModel(model, pairs, labels): member edges, links among a node's own
//   records, and the types outside a group that link in.
// - declaredRelations(model, docs, pairs): a group's relation fields toward
//   other members, each marked used or unused.
import {
  sharedLabelPrefix,
  typeLabel,
  type DisplayGroup,
  type TypeDoc,
  type ViewContext,
} from "@rhizome/kit";

import { planMembers, type MemberPlan } from "./model.ts";
import {
  UNGROUPED,
  UNTYPED,
  type FieldCount,
  type LinkPair,
  type MemberFigures,
  type MemberStats,
  type TypeSummary,
} from "./aggregate.ts";

export type Scope = { kind: "group"; group: string } | { kind: "workspace" };

export function scopeOf(context: ViewContext): Scope | null {
  if (context.kind === "workspace") return { kind: "workspace" };

  return context.kind === "group" ? { kind: "group", group: context.group } : null;
}

export type NodeKind = "type" | "interface" | "group" | "untyped";

export type ScopeNode = {
  /** The type or interface name, `group:<name>` for a collapsed group, or `UNTYPED`. */
  id: string;
  kind: NodeKind;
  /** The type, interface, or group name. */
  name: string;
  label: string;
  description: string;
  /** Concrete note types the node stands for. */
  types: readonly string[];
  /** Records, each counted once. */
  count: number;
  issueCount: number;
  lastChanged: number | null;
  /** The named display group that colors the node; null when it has none. */
  group: string | null;
  /** A collapsed group's members, empty otherwise. */
  members: readonly ScopeNode[];
  /** The type's or interface's own figures from the shape; null for groups and untyped notes. */
  stats: MemberStats | null;
};

/** At All notes, one named group and its members, or the ungrouped types (`group: null`). */
export type ScopeSection = { group: ScopeNode | null; members: readonly ScopeNode[] };

export type ScopeModel = {
  scope: Scope;
  /** The map's nodes, most records first. */
  nodes: readonly ScopeNode[];
  nodeIndex: ReadonlyMap<string, ScopeNode>;
  /** The node ids each concrete type counts toward; several only for a type in two named groups. */
  nodesOf: ReadonlyMap<string, readonly string[]>;
  /** Every concrete type inside the scope. */
  types: ReadonlySet<string>;
  /** Named groups in API order, then the ungrouped types; empty for a group scope. */
  sections: readonly ScopeSection[];
  /** Named groups, for colors and the legend. */
  groupNames: readonly string[];
  untyped: ScopeNode | null;
};

export type ScopeInput = {
  scope: Scope;
  groups: readonly DisplayGroup[];
  members: MemberFigures;
  summaries: ReadonlyMap<string, TypeSummary>;
  /** Named groups expanded on the All notes map. */
  expanded: ReadonlySet<string>;
};

export const groupNodeId = (group: string) => `group:${group}`;

export const byCount = (a: ScopeNode, b: ScopeNode) =>
  b.count - a.count || a.label.localeCompare(b.label);

const sum = (values: readonly number[]) => values.reduce((total, value) => total + value, 0);

const latest = (values: readonly (number | null)[]) => {
  const times = values.filter((value): value is number => value !== null);

  return times.length ? Math.max(...times) : null;
};

/** A group's members without embedded types; an interface keeps only its note implementors. */
function notePlans(group: DisplayGroup, input: ScopeInput): MemberPlan[] {
  const isNote = (type: string) => !input.summaries.get(type)?.embedded;

  return planMembers(group).flatMap((plan) => {
    const concreteTypes = plan.concreteTypes.filter(isNote);

    if (plan.kind === "type" && !isNote(plan.name)) return [];

    if (plan.kind === "interface" && !concreteTypes.length && plan.concreteTypes.length) return [];

    return [{ ...plan, concreteTypes }];
  });
}

function planNode(
  plan: MemberPlan,
  input: ScopeInput,
  group: string | null,
  prefix: string,
): ScopeNode {
  const stats =
    plan.kind === "interface"
      ? (input.members.interfaces.get(plan.name) ?? null)
      : (input.members.types.get(plan.name) ?? null);

  const typeStats = plan.concreteTypes.flatMap((type) => input.members.types.get(type) ?? []);

  return {
    id: plan.name,
    kind: plan.kind,
    name: plan.name,
    label: typeLabel(plan, { plural: true, prefix }),
    description: plan.description || input.summaries.get(plan.name)?.description || "",
    types: plan.concreteTypes,
    count: stats?.count ?? sum(typeStats.map((type) => type.count)),
    issueCount: stats?.issueCount ?? sum(typeStats.map((type) => type.issueCount)),
    lastChanged: stats ? stats.lastChanged : latest(typeStats.map((type) => type.lastChanged)),
    group,
    members: [],
    stats,
  };
}

function typeNode(name: string, input: ScopeInput): ScopeNode {
  const summary = input.summaries.get(name);
  const stats = input.members.types.get(name) ?? null;

  return {
    id: name,
    kind: "type",
    name,
    label: summary?.pluralLabel ?? name,
    description: summary?.description ?? "",
    types: [name],
    count: stats?.count ?? 0,
    issueCount: stats?.issueCount ?? 0,
    lastChanged: stats?.lastChanged ?? null,
    group: null,
    members: [],
    stats,
  };
}

function groupNode(name: string, members: readonly ScopeNode[], input: ScopeInput): ScopeNode {
  const types = [...new Set(members.flatMap((member) => member.types))];

  return {
    id: groupNodeId(name),
    kind: "group",
    name,
    label: name,
    description: "",
    types,
    count: sum(types.map((type) => input.members.types.get(type)?.count ?? 0)),
    issueCount: sum(members.map((member) => member.issueCount)),
    lastChanged: latest(members.map((member) => member.lastChanged)),
    group: name,
    members,
    stats: null,
  };
}

function untypedNode(input: ScopeInput): ScopeNode {
  return {
    id: UNTYPED,
    kind: "untyped",
    name: UNTYPED,
    label: "Untyped notes",
    description: "",
    types: [UNTYPED],
    count: input.members.untyped.count,
    issueCount: 0,
    lastChanged: null,
    group: null,
    members: [],
    stats: null,
  };
}

function indexNodes(nodes: readonly ScopeNode[]) {
  const nodesOf = new Map<string, string[]>();

  for (const node of nodes) {
    for (const type of node.types) {
      const ids = nodesOf.get(type) ?? [];

      if (!ids.includes(node.id)) ids.push(node.id);
      nodesOf.set(type, ids);
    }
  }

  return nodesOf;
}

function groupScope(input: ScopeInput, name: string): ScopeModel | null {
  const group = input.groups.find((candidate) => candidate.name === name);

  if (!group) return null;
  const plans = notePlans(group, input);
  const prefix = sharedLabelPrefix(plans.map((plan) => plan.pluralLabel));
  const color = name === UNGROUPED ? null : name;
  const nodes = plans.map((plan) => planNode(plan, input, color, prefix)).sort(byCount);

  return {
    scope: input.scope,
    nodes,
    nodeIndex: new Map(nodes.map((node) => [node.id, node])),
    nodesOf: indexNodes(nodes),
    types: new Set(nodes.flatMap((node) => node.types)),
    sections: [],
    groupNames: color ? [color] : [],
    untyped: null,
  };
}

function workspaceScope(input: ScopeInput): ScopeModel {
  const named = input.groups.filter((group) => group.name !== UNGROUPED);
  const seen = new Set<string>();
  const sections: ScopeSection[] = [];
  const nodes: ScopeNode[] = [];

  for (const group of named) {
    const members = notePlans(group, input)
      .map((plan) => planNode(plan, input, group.name, ""))
      .sort(byCount);

    const node = groupNode(group.name, members, input);
    sections.push({ group: node, members });

    // A member an earlier expanded group already drew keeps that group's node.
    const drawn = input.expanded.has(group.name)
      ? members.filter((member) => !seen.has(member.id))
      : [node];

    for (const member of drawn) seen.add(member.id);
    nodes.push(...drawn);
  }

  const grouped = new Set(sections.flatMap((section) => section.group?.types ?? []));

  const ungrouped = [...input.members.types.keys()]
    .flatMap((type) =>
      grouped.has(type) || input.summaries.get(type)?.embedded ? [] : [typeNode(type, input)],
    )
    .sort(byCount);

  if (ungrouped.length) sections.push({ group: null, members: ungrouped });
  const untyped = untypedNode(input);
  const all = [...nodes, ...ungrouped, untyped].sort(byCount);

  return {
    scope: input.scope,
    nodes: all,
    nodeIndex: new Map(
      [...all, ...sections.flatMap((section) => section.group ?? [])].map((node) => [
        node.id,
        node,
      ]),
    ),
    nodesOf: indexNodes(all),
    types: new Set(
      [...input.members.types.keys()].filter((type) => !input.summaries.get(type)?.embedded),
    ),
    sections,
    groupNames: named.map((group) => group.name),
    untyped,
  };
}

/** The scope's nodes and sections; null when the group no longer exists. */
export function scopeModel(input: ScopeInput): ScopeModel | null {
  return input.scope.kind === "workspace"
    ? workspaceScope(input)
    : groupScope(input, input.scope.group);
}

export type MapEdge = {
  key: string;
  a: string;
  b: string;
  links: number;
  relationLinks: number;
  plainLinks: number;
  /** Relation links by declaring type and field. */
  fields: readonly FieldCount[];
  /** Relation links are at least half of its links. */
  relation: boolean;
};

export type OutsideNeighbor = {
  /** A type outside the group, or `UNTYPED`. */
  id: string;
  label: string;
  links: number;
  /** Links by member node. */
  byMember: ReadonlyMap<string, number>;
};

export type LinkModel = {
  /** Edges between two distinct nodes, most links first. */
  edges: readonly MapEdge[];
  edgeIndex: ReadonlyMap<string, MapEdge>;
  /** Links among a node's own records. */
  self: ReadonlyMap<string, number>;
  /** A group's outside neighbors, most links first; empty at All notes. */
  outside: readonly OutsideNeighbor[];
};

export const edgeKey = (a: string, b: string) => (a < b ? `${a}\u0000${b}` : `${b}\u0000${a}`);

function mergeFields(into: FieldCount[], fields: readonly FieldCount[]) {
  for (const field of fields) {
    const known = into.find((entry) => entry.type === field.type && entry.field === field.field);

    if (known) known.count += field.count;
    else into.push({ ...field });
  }
}

/** Label of a type outside the scope, or of untyped notes. */
export const outsideLabel = (id: string, summaries: ReadonlyMap<string, TypeSummary>) =>
  id === UNTYPED ? "Untyped notes" : (summaries.get(id)?.pluralLabel ?? id);

/**
 * Type-pair links rolled up to the scope's nodes. Links among one type's own
 * records stay on its nodes, and so does a link whose two ends two groups both
 * hold, so groups sharing records get no edge from them. A pair joining the
 * same node from both ends counts once there.
 */
export function linkModel(
  model: ScopeModel,
  pairs: readonly LinkPair[],
  summaries: ReadonlyMap<string, TypeSummary>,
): LinkModel {
  const edges = new Map<string, Omit<MapEdge, "relation"> & { fields: FieldCount[] }>();
  const self = new Map<string, number>();
  const outside = new Map<string, { links: number; byMember: Map<string, number> }>();

  const addSelf = (node: string, links: number) => self.set(node, (self.get(node) ?? 0) + links);

  for (const pair of pairs) {
    const from = model.nodesOf.get(pair.a) ?? [];
    const to = model.nodesOf.get(pair.b) ?? [];

    if (pair.a === pair.b) {
      for (const node of from) addSelf(node, pair.links);
      continue;
    }

    if (from.length && to.length) {
      const counted = new Set<string>();

      for (const a of from) {
        for (const b of to) {
          const key = edgeKey(a, b);

          // Two groups that both hold both ends hold the link inside each.
          if (counted.has(key) || (a !== b && to.includes(a) && from.includes(b))) continue;
          counted.add(key);

          if (a === b) {
            addSelf(a, pair.links);
            continue;
          }

          const edge = edges.get(key) ?? {
            key,
            a: a < b ? a : b,
            b: a < b ? b : a,
            links: 0,
            relationLinks: 0,
            plainLinks: 0,
            fields: [],
          };

          edge.links += pair.links;
          edge.relationLinks += pair.relationLinks;
          edge.plainLinks += pair.plainLinks;
          mergeFields(edge.fields, pair.fields);
          edges.set(key, edge);
        }
      }

      continue;
    }

    if (model.scope.kind !== "group" || (!from.length && !to.length)) continue;
    const [members, other] = from.length ? [from, pair.b] : [to, pair.a];
    const entry = outside.get(other) ?? { links: 0, byMember: new Map<string, number>() };
    entry.links += pair.links;

    for (const member of members)
      entry.byMember.set(member, (entry.byMember.get(member) ?? 0) + pair.links);
    outside.set(other, entry);
  }

  const edgeList = [...edges.values()]
    .map((edge) => ({ ...edge, relation: edge.relationLinks * 2 >= edge.links }))
    .sort((a, b) => b.links - a.links || a.key.localeCompare(b.key));

  return {
    edges: edgeList,
    edgeIndex: new Map(edgeList.map((edge) => [edge.key, edge])),
    self,
    outside: [...outside]
      .map(([id, entry]) => ({ id, label: outsideLabel(id, summaries), ...entry }))
      .sort((a, b) => b.links - a.links || a.id.localeCompare(b.id)),
  };
}

export type DeclaredRelation = {
  /** The declaring member node. */
  member: string;
  field: string;
  /** The member node the field's declared target belongs to. */
  target: string;
  /** Record links through the field, across the member's types declaring it. */
  count: number;
};

/** The member node a declared target type or interface belongs to. */
function ownerOf(model: ScopeModel, typeName: string) {
  if (model.nodeIndex.has(typeName)) return typeName;
  const owners = model.nodesOf.get(typeName) ?? [];

  return owners.length === 1 ? owners[0] : null;
}

/**
 * Each member's relation fields toward another member, merged across the
 * concrete types that declare them, with the record links they carry. Read
 * from each concrete type's documentation.
 */
export function declaredRelations(
  model: ScopeModel,
  docs: Readonly<Record<string, TypeDoc>>,
  pairs: readonly LinkPair[],
): DeclaredRelation[] {
  const used = new Map<string, number>();

  for (const pair of pairs) {
    for (const field of pair.fields) {
      const key = `${field.type}\u0000${field.field}`;
      used.set(key, (used.get(key) ?? 0) + field.count);
    }
  }

  const found = new Map<string, DeclaredRelation>();

  for (const node of model.nodes) {
    for (const type of node.types) {
      for (const field of docs[type]?.fields ?? []) {
        if (field.kind !== "link" || !field.typeName) continue;
        const target = ownerOf(model, field.typeName);

        if (target === null || target === node.id) continue;
        const key = `${node.id}\u0000${field.name}\u0000${target}`;
        const relation = found.get(key) ?? { member: node.id, field: field.name, target, count: 0 };
        relation.count += used.get(`${type}\u0000${field.name}`) ?? 0;
        found.set(key, relation);
      }
    }
  }

  return [...found.values()];
}
