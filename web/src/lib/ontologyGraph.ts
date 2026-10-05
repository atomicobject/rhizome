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

export const NODE_WIDTH = 280;

export const ROW_HEIGHT = 22;

export const HEADER_HEIGHT = 104;

export const FIELD_LIST_TOP_PADDING = 4;

const NODE_BORDER_WIDTH = 1;

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

function nodeHeight(fields: OntologyNodeField[]): number {
  const schemaEdgeRows = fields.filter((f) => f.isSchemaEdge).length;
  const summaryRow = fields.length - schemaEdgeRows > 0 ? 1 : 0;

  return (
    NODE_BORDER_WIDTH * 2 +
    HEADER_HEIGHT +
    FIELD_LIST_TOP_PADDING * 2 +
    (schemaEdgeRows + summaryRow) * ROW_HEIGHT
  );
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

export function fieldRowMidpointY(rowIndex: number): number {
  return (
    NODE_BORDER_WIDTH +
    HEADER_HEIGHT +
    FIELD_LIST_TOP_PADDING +
    rowIndex * ROW_HEIGHT +
    ROW_HEIGHT / 2
  );
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
      width: NODE_WIDTH,
      height: nodeHeight(fields),
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
      width: NODE_WIDTH,
      height: nodeHeight(fields),
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
      "elk.layered.spacing.nodeNodeBetweenLayers": "64",
      "elk.aspectRatio": "1.5",
      "elk.spacing.nodeNode": "28",
      "elk.padding": "[top=20,left=20,bottom=20,right=20]",
    },
    children: graph.nodes.map((n) => {
      let schemaEdgeIndex = 0;

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
            const y = fieldRowMidpointY(schemaEdgeIndex);
            schemaEdgeIndex += 1;

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
    edges: graph.edges.map((e) => {
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

  const routedEdges = graph.edges.map((edge) => ({
    ...edge,
    points: edgeRoutePoints(laidOutEdgesById.get(edge.id)),
  }));

  return { nodes: positioned, edges: routedEdges };
}
