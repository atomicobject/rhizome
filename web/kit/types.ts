// Kit-local declarations of the public API contracts group views read
// (SPEC-0111). They mirror the server's JSON so the kit's public types do not
// depend on the first-party UI's generated client.

/** Where a view's source came from. */
export type ViewOrigin = "bundled" | "repository" | "generated";

/** A lifecycle stage (SPEC-0112). */
export type LifecycleStage = "open" | "active" | "done" | "dropped";

/** An enum value's `@view` presentation metadata. */
export type EnumValueDoc = {
  name: string;
  summary?: string;
  label?: string;
  order?: number;
  tone?: string;
  collapsed?: boolean;
  /** Declared or inferred from authored tone or `collapsed`; absent when the enum has no stages. */
  stage?: LifecycleStage;
  /** True when the schema declares the stage; absent when it was inferred. */
  stageDeclared?: boolean;
};

export type EnumDoc = { name: string; summary?: string; values: EnumValueDoc[] };

export type FieldDisplayDoc = {
  role?: "SUMMARY" | "PARENT";
  importance?: "KEY" | "NORMAL" | "DETAIL";
};

export type TypeFieldDoc = {
  name: string;
  kind: "scalar" | "enum" | "link" | "reverse" | "neighbor" | "section";
  typeName?: string;
  required?: boolean;
  list?: boolean;
  summary?: string;
  display?: FieldDisplayDoc;
  policy?: { reason?: string };
  enum?: { values: EnumValueDoc[] };
  /** The field is required whenever any listed field holds the listed value. */
  requiredWhen?: Array<{ field: string; equals: string }>;
};

export type CompanionDocRef = { path: string; purpose?: string };

/**
 * What views need to know about a type or interface, derived on the server
 * from the compiled schema (SPEC-0112). Field lists name schema fields in the
 * order views should prefer them and are empty when nothing qualifies.
 */
export type TypeProfile = {
  /** How the type's records are best viewed by default. */
  // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
  shape: "workflow" | "contract" | "dated" | "catalog" | "reference";
  /** The enum field records move through; its values carry stages. */
  lifecycleField?: string;
  /** Other enum fields whose enum has stages. */
  orderedFields: string[];
  /** Enum fields with no stages. */
  categoryFields: string[];
  summaryField?: string;
  primaryDateField?: string;
  /** Links to the core identity type. */
  peopleFields: string[];
  /** KEY String fields other than the title, identifier, and summary. */
  keyTextFields: string[];
  /** Other forward link fields, KEY fields first. */
  relationFields: string[];
  /** `@reverse` fields and inbound `@neighbors` list fields. */
  reverseFields: string[];
  /** KEY fields a record may leave empty: not required, the lifecycle, or `@requiresWhen`. */
  gapFields: string[];
};

/** Type documentation from `GET /api/v1/ontology/types/{name}?notes=none`. */
export type TypeDoc = {
  name: string;
  /** Note types have GraphQL neighbor fields; embedded node types do not. */
  role?: "NOTE" | "SECTION" | "EMBEDDED_NODE" | "INTERFACE";
  label: string;
  pluralLabel: string;
  summary?: string;
  description?: string;
  implements?: string[];
  fields: TypeFieldDoc[];
  enums?: EnumDoc[];
  companionDocs?: CompanionDocRef[];
  summaryField?: string;
  parentField?: string;
  /** Absent on section and embedded types. */
  profile?: TypeProfile;
};

export type DisplayGroupMember = {
  name: string;
  kind: "type" | "interface";
  label: string;
  pluralLabel: string;
  description?: string;
  count: number;
  /** Published validation issues in the member's type or interface scope. */
  issueCount: number;
  implementors: string[];
  children: DisplayGroupMember[];
};

export type DisplayGroup = { name: string; members: DisplayGroupMember[] };

/** `GET /api/v1/display-groups`, sorted by name with the rail's `Other` group last. */
export type DisplayGroupsResponse = { groups: DisplayGroup[] };
