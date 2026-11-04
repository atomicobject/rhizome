import ELK from "elkjs/lib/elk.bundled.js";
import type { ElkExtendedEdge, ElkPoint } from "elkjs/lib/elk-api";

import type { OntologyAtlasResponse, TypeDoc, TypeFieldDoc } from "../api/types";

type OntologyNodeField = {
  name: string;
  kind: string;
  typeName?: string;
  required: boolean;
  list: boolean;
  isSchemaEdge: boolean;
};

export type OntologyNodeData = {
  name: string;
  count: number;
  issueCount: number;
  isInterface: boolean;
  role: "note" | "embedded" | "section" | "interface";
  description?: string;
  implements: string[];
  schemaTargets: string[];
  fields: OntologyNodeField[];
  scalarFieldCount: number;
  relationCount: number;
  sectionCount: number;
};

export type OntologyNode = {
  id: string;
  type: "ontologyType";
  position: { x: number; y: number };
  data: OntologyNodeData;
  width: number;
  height: number;
};

export type OntologyEdge = {
  id: string;
  source: string;
  target: string;
  sourceHandle?: string;
  targetHandle?: string;
  type: "schema" | "implements";
  kind?: "link" | "reverse" | "neighbor" | "section";
  label?: string;
  data?: { fieldName: string; list: boolean };
  points?: ElkPoint[];
  /**
   * How far the line leaves above or below the card's middle. A card with both
   * relations and implements lines sends them out 10 units apart, so a solid
   * and a dashed trunk never draw over each other.
   */
  sourceOffset?: number;
};

// WHY: the atlas endpoint also returns section docs, which the generated
// response schema does not declare yet.
type OntologyAtlasGraphPayload = OntologyAtlasResponse & {
  sections?: TypeDoc[];
};

export type OntologyGraphModel = {
  nodes: OntologyNode[];
  edges: OntologyEdge[];
};

type VisibleSchemaField = {
  name: string;
  kind: "link" | "reverse" | "neighbor" | "section";
  typeName: string;
  required: boolean;
  list: boolean;
};

/**
 * Atlas cards are compact and all one size: the name on two lines (a
 * multi-word name always breaks before its last word, see `nameLines`) and
 * one line of counts. Fields live on the type page, so a card's size follows
 * its text, not its schema, and the whole ontology fits at a zoom where names
 * read at about 14px. Sizes are in card units; ontology.css sets the same type
 * sizes.
 */
export const NODE_WIDTH = 200;

/** A card's name size and line height, in card units. */
const NAME_SIZE = 20;

const NAME_LINE = NAME_SIZE * 1.25;

/** A type name's two display lines: everything before its last word, then the last word. */
export function nameLines(name: string): [string, string] {
  const words = name.split(/(?<=[a-z0-9])(?=[A-Z])/);

  return words.length > 1 ? [words.slice(0, -1).join(""), words.at(-1) ?? ""] : [name, ""];
}

/** Padding, the gap below the name, and the counts line. */
const CARD_CHROME = 16 + 4 + 18 * 1.3;

const NODE_BORDER_WIDTH = 1;

/** Half the gap between a card's relation and implements trunks, in card units (12px apart at fit zoom). */
const SPLIT_TRUNK = 9;

export const TYPE_TARGET_LEFT_HANDLE = "type-target-left";

export const TYPE_TARGET_RIGHT_HANDLE = "type-target-right";

export const IMPL_SOURCE_LEFT_HANDLE = "impl-source-left";

export const IMPL_SOURCE_RIGHT_HANDLE = "impl-source-right";

const elk = new ELK();

function isSchemaEdgeKind(kind: string): kind is "link" | "reverse" | "neighbor" | "section" {
  return kind === "link" || kind === "reverse" || kind === "neighbor" || kind === "section";
}

function fieldRow(f: TypeFieldDoc): OntologyNodeField | null {
  if (!f.name) return null;
  const kind = f.kind ?? "scalar";

  return {
    name: f.name,
    kind,
    typeName: f.typeName,
    required: Boolean(f.required),
    list: Boolean(f.list),
    isSchemaEdge: isSchemaEdgeKind(kind),
  };
}

function visibleFieldRow(f: VisibleSchemaField): OntologyNodeField {
  return {
    name: f.name,
    kind: f.kind,
    typeName: f.typeName,
    required: f.required,
    list: f.list,
    isSchemaEdge: true,
  };
}

function dedupeNodeFields(fields: OntologyNodeField[]): OntologyNodeField[] {
  const seen = new Set<string>();

  return fields.filter((field) => {
    const key = [
      field.name,
      field.kind,
      field.typeName ?? "",
      field.required ? "required" : "optional",
      field.list ? "list" : "single",
    ].join("|");

    if (seen.has(key)) return false;
    seen.add(key);

    return true;
  });
}

/** A card's height: two name lines and the counts line, the same for every card. */
export function cardHeight(): number {
  // Even, so the middle where edges attach is a whole pixel.
  return 2 * Math.ceil((NODE_BORDER_WIDTH * 2 + CARD_CHROME + 2 * NAME_LINE) / 2);
}

function nodeSurveyData(fields: OntologyNodeField[]) {
  let scalarFieldCount = 0;
  let relationCount = 0;
  let sectionCount = 0;

  for (const field of fields) {
    if (!field.isSchemaEdge) {
      scalarFieldCount += 1;
    }

    if (field.kind === "link" || field.kind === "reverse" || field.kind === "neighbor") {
      relationCount += 1;
    }

    if (field.kind === "section") {
      sectionCount += 1;
    }
  }

  return {
    scalarFieldCount,
    relationCount,
    sectionCount,
  };
}

export function relationSourceHandle(fieldName: string, side: "left" | "right"): string {
  return `field-${fieldName}-${side}`;
}

function nodeMidpointY(node: OntologyNode): number {
  return node.height / 2;
}

function typeTargetHandle(side: "left" | "right"): string {
  return side === "left" ? TYPE_TARGET_LEFT_HANDLE : TYPE_TARGET_RIGHT_HANDLE;
}

function implSourceHandle(side: "left" | "right"): string {
  return side === "left" ? IMPL_SOURCE_LEFT_HANDLE : IMPL_SOURCE_RIGHT_HANDLE;
}

function fieldSignature(
  field: Pick<TypeFieldDoc, "name" | "kind" | "typeName" | "list" | "required">,
): string {
  return [
    field.name ?? "",
    field.kind ?? "",
    field.typeName ?? "",
    field.list ? "list" : "single",
    field.required ? "required" : "optional",
  ].join("|");
}

function isEmbeddedRole(doc: TypeDoc): boolean {
  return doc.locator === "EMBEDDED" || doc.role === "EMBEDDED";
}

export function buildOntologyGraph(atlas: OntologyAtlasGraphPayload): OntologyGraphModel {
  const typeEntries = (atlas.types ?? []).flatMap((e) => {
    const name = e.type?.name;

    if (!name || !e.type) return [];

    return [
      {
        type: e.type,
        name,
        count: e.count,
        issueCount: e.issueCount ?? 0,
      },
    ];
  });

  const sections = (atlas.sections ?? []).flatMap((section) => {
    const name = section.name;

    return name ? [{ section, name }] : [];
  });

  const interfaces = (atlas.interfaces ?? []).flatMap((t) => {
    const name = t.name;

    return name ? [{ iface: t, name }] : [];
  });

  const docsByName = new Map<string, TypeDoc>();
  const sectionNames = new Set<string>();

  for (const { type, name } of typeEntries) docsByName.set(name, type);

  for (const { section, name } of sections) {
    docsByName.set(name, section);
    sectionNames.add(name);
  }

  for (const { iface, name } of interfaces) docsByName.set(name, iface);

  const implementorsByInterface = new Map<string, TypeDoc[]>();

  for (const { type } of typeEntries) {
    if (isEmbeddedRole(type)) continue;

    for (const ifaceName of type.implements ?? []) {
      const implementors = implementorsByInterface.get(ifaceName) ?? [];
      implementors.push(type);
      implementorsByInterface.set(ifaceName, implementors);
    }
  }

  const known = new Set<string>();

  for (const e of typeEntries) known.add(e.name);

  for (const s of sections) known.add(s.name);

  for (const i of interfaces) known.add(i.name);

  const nodes: OntologyNode[] = [];
  const width = NODE_WIDTH;

  function schemaTargets(fields: OntologyNodeField[]): string[] {
    const targets = new Set<string>();

    for (const field of fields) {
      if (field.isSchemaEdge && field.typeName) targets.add(field.typeName);
    }

    return Array.from(targets).sort();
  }

  function inheritedInterfaceFieldSignatures(
    typeDoc: TypeDoc,
    seen: Set<string> = new Set(),
  ): Set<string> {
    const signatures = new Set<string>();

    for (const ifaceName of typeDoc.implements ?? []) {
      if (seen.has(ifaceName)) continue;
      seen.add(ifaceName);
      const iface = docsByName.get(ifaceName);

      if (!iface) continue;

      for (const field of iface.fields ?? []) {
        if (!field.name || !field.kind) continue;
        signatures.add(fieldSignature(field));
      }

      for (const nested of inheritedInterfaceFieldSignatures(iface, seen)) {
        signatures.add(nested);
      }
    }

    return signatures;
  }

  function sharedImplementerFields(interfaceName: string): TypeFieldDoc[] {
    const implementors = implementorsByInterface.get(interfaceName) ?? [];

    if (implementors.length < 2) return [];

    const fieldMaps = implementors.map((doc) => {
      const map = new Map<string, TypeFieldDoc>();

      for (const field of doc.fields ?? []) {
        if (!field.name || !field.kind || !isSchemaEdgeKind(field.kind)) continue;
        map.set(fieldSignature(field), field);
      }

      return map;
    });

    const [first, ...rest] = fieldMaps;
    const shared: TypeFieldDoc[] = [];

    for (const [signature, field] of first.entries()) {
      if (rest.every((map) => map.has(signature))) {
        shared.push(field);
      }
    }

    return shared.sort((a, b) => (a.name ?? "").localeCompare(b.name ?? ""));
  }

  const hoistedFieldSignaturesByInterface = new Map<string, Set<string>>();

  for (const { name } of interfaces) {
    hoistedFieldSignaturesByInterface.set(
      name,
      new Set(sharedImplementerFields(name).map((field) => fieldSignature(field))),
    );
  }

  function ownFields(doc: TypeDoc): OntologyNodeField[] {
    const inherited = inheritedInterfaceFieldSignatures(doc);

    for (const ifaceName of doc.implements ?? []) {
      const hoisted = hoistedFieldSignaturesByInterface.get(ifaceName);

      if (!hoisted) continue;

      for (const signature of hoisted) inherited.add(signature);
    }

    return (doc.fields ?? []).flatMap((field) => {
      if (field.name && field.kind && inherited.has(fieldSignature(field))) {
        return [];
      }

      const row = fieldRow(field);

      return row ? [row] : [];
    });
  }

  function expandVisibleSchemaFields(
    field: TypeFieldDoc,
    seenSections: Set<string> = new Set(),
  ): VisibleSchemaField[] {
    if (!field.kind || !isSchemaEdgeKind(field.kind) || !field.typeName || !field.name) {
      return [];
    }

    if (!sectionNames.has(field.typeName)) {
      return [
        {
          name: field.name,
          kind: field.kind,
          typeName: field.typeName,
          required: Boolean(field.required),
          list: Boolean(field.list),
        },
      ];
    }

    if (seenSections.has(field.typeName)) {
      return [];
    }

    const sectionDoc = docsByName.get(field.typeName);

    if (!sectionDoc) {
      return [];
    }

    const nextSeen = new Set(seenSections);
    nextSeen.add(field.typeName);

    const expanded = (sectionDoc.fields ?? []).flatMap((child) =>
      expandVisibleSchemaFields(child, nextSeen).map((visible) => ({
        ...visible,
        name: `${field.name}.${visible.name}`,
        required: Boolean(field.required) || visible.required,
        list: Boolean(field.list) || visible.list,
      })),
    );

    return expanded;
  }

  function visibleSchemaFields(doc: TypeDoc): VisibleSchemaField[] {
    return ownFields(doc).flatMap((field) => expandVisibleSchemaFields(field));
  }

  for (const entry of typeEntries) {
    const { type: t, name } = entry;

    const fields = dedupeNodeFields([
      ...ownFields(t).flatMap((field) => (sectionNames.has(field.typeName ?? "") ? [] : [field])),
      ...visibleSchemaFields(t).map(visibleFieldRow),
    ]);

    nodes.push({
      id: name,
      type: "ontologyType",
      position: { x: 0, y: 0 },
      width,
      height: cardHeight(),
      data: {
        name,
        count: entry.count,
        issueCount: entry.issueCount,
        isInterface: false,
        role: t.locator === "EMBEDDED" ? "embedded" : "note",
        description: t.description,
        implements: (t.implements ?? []).filter((i) => known.has(i)),
        schemaTargets: schemaTargets(fields),
        fields,
        ...nodeSurveyData(fields),
      },
    });
  }

  for (const { iface, name } of interfaces) {
    const hoisted = sharedImplementerFields(name).flatMap((field) =>
      expandVisibleSchemaFields(field).map(visibleFieldRow),
    );

    const visibleInterfaceFieldRows = visibleSchemaFields(iface).map(visibleFieldRow);

    const visibleInterfaceFieldKeys = new Set(
      visibleInterfaceFieldRows.map((field) => `${field.name}:${field.kind}:${field.typeName}`),
    );

    const fields = dedupeNodeFields([
      ...ownFields(iface).flatMap((field) =>
        sectionNames.has(field.typeName ?? "") ? [] : [field],
      ),
      ...visibleInterfaceFieldRows,
      ...hoisted.filter(
        (field) => !visibleInterfaceFieldKeys.has(`${field.name}:${field.kind}:${field.typeName}`),
      ),
    ]);

    nodes.push({
      id: name,
      type: "ontologyType",
      position: { x: 0, y: 0 },
      width,
      height: cardHeight(),
      data: {
        name,
        count: 0,
        issueCount: 0,
        isInterface: true,
        role: "interface",
        description: iface.description,
        implements: [],
        schemaTargets: schemaTargets(fields),
        fields,
        ...nodeSurveyData(fields),
      },
    });
  }

  const edges: OntologyEdge[] = [];
  const seenEdges = new Set<string>();

  function pushSchemaEdge(
    source: string,
    fieldName: string,
    target: string,
    kind: "link" | "reverse" | "neighbor" | "section",
    list: boolean,
  ) {
    const id = `rel:${source}.${fieldName}->${target}`;

    if (seenEdges.has(id)) return;
    seenEdges.add(id);
    edges.push({
      id,
      source,
      target,
      sourceHandle: relationSourceHandle(fieldName, "right"),
      targetHandle: typeTargetHandle("left"),
      type: "schema",
      kind,
      label: list ? `${fieldName}*` : fieldName,
      data: { fieldName, list },
    });
  }

  function addEdgesForDoc(sourceName: string, doc: TypeDoc) {
    for (const field of visibleSchemaFields(doc)) {
      if (!known.has(field.typeName)) continue;
      pushSchemaEdge(sourceName, field.name, field.typeName, field.kind, field.list);
    }
  }

  for (const { type: t, name } of typeEntries) {
    addEdgesForDoc(name, t);
  }

  for (const { iface, name } of interfaces) {
    addEdgesForDoc(name, iface);

    for (const field of sharedImplementerFields(name)) {
      for (const visible of expandVisibleSchemaFields(field)) {
        if (!known.has(visible.typeName)) continue;
        pushSchemaEdge(name, visible.name, visible.typeName, visible.kind, visible.list);
      }
    }
  }

  for (const { type: t, name } of typeEntries) {
    for (const iface of t.implements ?? []) {
      if (!known.has(iface)) continue;
      edges.push({
        id: `impl:${iface}->${name}`,
        source: iface,
        target: name,
        sourceHandle: implSourceHandle("right"),
        targetHandle: typeTargetHandle("left"),
        type: "implements",
      });
    }
  }

  return { nodes, edges };
}

/** A self-relation's hook under its card's right end, in card units. */
const LOOP = { inset: 52, width: 30, depth: 16 };

/** A relation to the card's own type: a short hook down from its bottom edge and back up. */
function selfLoopPoints(node: OntologyNode | undefined): ElkPoint[] {
  if (!node) return [];
  const bottom = node.position.y + node.height;
  const start = node.position.x + node.width - LOOP.inset;
  const end = start + LOOP.width;

  return [
    { x: start, y: bottom },
    { x: start, y: bottom + LOOP.depth },
    { x: end, y: bottom + LOOP.depth },
    { x: end, y: bottom },
  ];
}

export async function layoutOntologyGraph(graph: OntologyGraphModel): Promise<OntologyGraphModel> {
  const { children = [], edges: laidOutEdges = [] } = await elk.layout({
    id: "ontology",
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "RIGHT",
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
      "elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
      "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
      "elk.layered.spacing.edgeNodeBetweenLayers": "24",
      "elk.layered.spacing.nodeNodeBetweenLayers": "44",
      "elk.aspectRatio": "1.7",
      "elk.spacing.nodeNode": "28",
      // Parallel lanes stay at least 12px apart at the Atlas's fit zoom.
      "elk.spacing.edgeEdge": "18",
      "elk.layered.spacing.edgeEdgeBetweenLayers": "18",
      "elk.padding": "[top=20,left=20,bottom=20,right=20]",
    },
    children: graph.nodes.map((n) => {
      return {
        id: n.id,
        width: n.width,
        height: n.height,
        layoutOptions: {
          "elk.portConstraints": "FIXED_POS",
        },
        ports: [
          {
            id: `${n.id}:${TYPE_TARGET_LEFT_HANDLE}`,
            x: 0,
            y: nodeMidpointY(n),
            width: 0,
            height: 0,
          },
          {
            id: `${n.id}:${TYPE_TARGET_RIGHT_HANDLE}`,
            x: n.width,
            y: nodeMidpointY(n),
            width: 0,
            height: 0,
          },
          {
            id: `${n.id}:${IMPL_SOURCE_LEFT_HANDLE}`,
            x: 0,
            y: nodeMidpointY(n),
            width: 0,
            height: 0,
          },
          {
            id: `${n.id}:${IMPL_SOURCE_RIGHT_HANDLE}`,
            x: n.width,
            y: nodeMidpointY(n),
            width: 0,
            height: 0,
          },
          ...n.data.fields.flatMap((f) => {
            if (!f.isSchemaEdge) return [];
            // Every field's relation leaves from the card's middle.
            const y = nodeMidpointY(n);

            return [
              {
                id: `${n.id}:${relationSourceHandle(f.name, "left")}`,
                x: 0,
                y,
                width: 0,
                height: 0,
              },
              {
                id: `${n.id}:${relationSourceHandle(f.name, "right")}`,
                x: n.width,
                y,
                width: 0,
                height: 0,
              },
            ];
          }),
        ],
      };
    }),
    // A self-relation is drawn as a short hook on its card (OntologyRoutedEdge), not routed around it.
    edges: graph.edges
      .filter((e) => e.source !== e.target)
      .map((e) => {
        return {
          id: e.id,
          sources: [`${e.source}:${e.sourceHandle ?? implSourceHandle("right")}`],
          targets: [`${e.target}:${e.targetHandle ?? typeTargetHandle("left")}`],
        };
      }),
  });

  const positioned = graph.nodes.map((n) => {
    const laidOut = children.find((child) => child.id === n.id);

    return {
      ...n,
      position: {
        x: laidOut?.x ?? 0,
        y: laidOut?.y ?? 0,
      },
    };
  });

  const laidOutEdgesById = new Map(laidOutEdges.map((edge) => [edge.id, edge]));

  function edgeRoutePoints(edge: ElkExtendedEdge | undefined): ElkPoint[] {
    const section = edge?.sections?.[0];

    if (!section) return [];

    return [section.startPoint, ...(section.bendPoints ?? []), section.endPoint].map((point) => ({
      x: point.x,
      y: point.y,
    }));
  }

  const implementing = new Set(
    graph.edges.flatMap((edge) => (edge.type === "implements" ? [edge.source] : [])),
  );

  const relating = new Set(
    graph.edges.flatMap((edge) => (edge.type === "implements" ? [] : [edge.source])),
  );

  const routedEdges = graph.edges.map((edge) => ({
    ...edge,
    points:
      edge.source === edge.target
        ? selfLoopPoints(positioned.find((node) => node.id === edge.source))
        : edgeRoutePoints(laidOutEdgesById.get(edge.id)),
    sourceOffset:
      implementing.has(edge.source) && relating.has(edge.source)
        ? edge.type === "implements"
          ? SPLIT_TRUNK
          : -SPLIT_TRUNK
        : 0,
  }));

  return packComponents({ nodes: positioned, edges: routedEdges });
}

type Rect = { x: number; y: number; w: number; h: number };

/** ELK's padding around the drawing, in card units. */
const LAYOUT_PADDING = 20;

/** The least gap between disconnected groups of types after packing, in card units. */
const COMPONENT_GAP = 40;

/** The packing grid, in card units. */
const PACK_STEP = 10;

/** The canvas's usual width over height; packing keeps the drawing near it, so it fits large. */
const VIEW_ASPECT = 1.6;

function boundsOf(rects: readonly Rect[]): Rect {
  const x = Math.min(...rects.map((r) => r.x));
  const y = Math.min(...rects.map((r) => r.y));

  return {
    x,
    y,
    w: Math.max(...rects.map((r) => r.x + r.w)) - x,
    h: Math.max(...rects.map((r) => r.y + r.h)) - y,
  };
}

const intersects = (a: Rect, b: Rect, gap: number) =>
  a.x - gap < b.x + b.w && a.x + a.w + gap > b.x && a.y - gap < b.y + b.h && a.y + a.h + gap > b.y;

/**
 * ELK stacks disconnected groups of types in a row, which can leave a void
 * beside the largest one. This moves each smaller group with more than one
 * type, largest first, to the free spot at least COMPONENT_GAP clear of every
 * card and line already placed that keeps the drawing smallest at the
 * canvas's aspect. Lone types stay in ELK's packed grid.
 */
export function packComponents(graph: OntologyGraphModel): OntologyGraphModel {
  const root = new Map(graph.nodes.map((node) => [node.id, node.id]));

  const find = (id: string): string => {
    const parent = root.get(id) ?? id;

    return parent === id ? id : find(parent);
  };

  for (const edge of graph.edges)
    if (root.has(edge.source) && root.has(edge.target))
      root.set(find(edge.source), find(edge.target));

  const groups = new Map<string, OntologyNode[]>();

  for (const node of graph.nodes)
    groups.set(find(node.id), [...(groups.get(find(node.id)) ?? []), node]);

  const ordered = [...groups.values()].sort((a, b) => b.length - a.length);
  const moving = ordered.slice(1).filter((group) => group.length > 1);

  if (moving.length === 0) return graph;
  const movingIds = new Set(moving.flat().map((node) => node.id));
  const rect = (node: OntologyNode): Rect => ({ ...node.position, w: node.width, h: node.height });

  const segments = (edge: OntologyEdge): Rect[] =>
    (edge.points ?? []).slice(1).map((point, index) => {
      const from = (edge.points ?? [])[index];

      return {
        x: Math.min(from.x, point.x),
        y: Math.min(from.y, point.y),
        w: Math.abs(point.x - from.x),
        h: Math.abs(point.y - from.y),
      };
    });

  const obstacles: Rect[] = [
    ...graph.nodes.filter((node) => !movingIds.has(node.id)).map(rect),
    ...graph.edges.filter((edge) => !movingIds.has(edge.source)).flatMap(segments),
  ];

  const shifts = new Map<string, { dx: number; dy: number }>();

  for (const group of moving) {
    const ids = new Set(group.map((node) => node.id));

    const own = [
      ...group.map(rect),
      ...graph.edges.filter((e) => ids.has(e.source)).flatMap(segments),
    ];

    const box = boundsOf(own);
    const placed = boundsOf(obstacles);

    let best: { dx: number; dy: number; cost: number } | null = null;

    // A grid over the placed drawing and its margin, plus the spots flush with its edges.
    const span = (from: number, to: number, ...flush: number[]) => [
      ...Array.from(
        { length: Math.floor((to - from) / PACK_STEP) + 1 },
        (_, i) => from + i * PACK_STEP,
      ),
      ...flush,
    ];

    const xs = span(
      placed.x - box.w - COMPONENT_GAP,
      placed.x + placed.w + COMPONENT_GAP,
      placed.x,
      placed.x + placed.w - box.w,
    );

    const ys = span(
      placed.y - box.h - COMPONENT_GAP,
      placed.y + placed.h + COMPONENT_GAP,
      placed.y,
      placed.y + placed.h - box.h,
    );

    for (const y of ys) {
      for (const x of xs) {
        const dx = x - box.x;
        const dy = y - box.y;
        const moved = own.map((r) => ({ ...r, x: r.x + dx, y: r.y + dy }));

        if (moved.some((r) => obstacles.some((o) => intersects(r, o, COMPONENT_GAP)))) continue;
        const all = boundsOf([placed, ...moved]);
        // The side that limits the zoom, then the area, so ties pick the tightest spot.
        const cost = Math.max(all.w, all.h * VIEW_ASPECT) + (all.w * all.h) / 1e6;

        if (!best || cost < best.cost) best = { dx, dy, cost };
      }
    }

    if (!best) continue;

    for (const r of own) obstacles.push({ ...r, x: r.x + best.dx, y: r.y + best.dy });

    for (const id of ids) shifts.set(id, best);
  }

  // Moving a group can leave the old origin empty; start the drawing at ELK's padding again.
  const moved = graph.nodes.map((node) => {
    const { dx, dy } = shifts.get(node.id) ?? { dx: 0, dy: 0 };

    return { x: node.position.x + dx, y: node.position.y + dy };
  });

  const originX = Math.min(...moved.map((point) => point.x)) - LAYOUT_PADDING;
  const originY = Math.min(...moved.map((point) => point.y)) - LAYOUT_PADDING;

  const shift = (id: string) => {
    const { dx, dy } = shifts.get(id) ?? { dx: 0, dy: 0 };

    return { dx: dx - originX, dy: dy - originY };
  };

  return {
    nodes: graph.nodes.map((node) => ({
      ...node,
      position: { x: node.position.x + shift(node.id).dx, y: node.position.y + shift(node.id).dy },
    })),
    edges: graph.edges.map((edge) => ({
      ...edge,
      points: edge.points?.map((point) => ({
        x: point.x + shift(edge.source).dx,
        y: point.y + shift(edge.source).dy,
      })),
    })),
  };
}
