// The typed model the group views render (SPEC-0111). Pure: no React, no I/O.
//
// A group's members are its member roots in rail order, plus any type nested
// under a root (a display child) that the root does not already stand for. An
// interface member stands for its implementing types; its fields are the union
// of theirs.
//
// A member's lifecycle, gap fields, and key text fields come from the type
// profile in its own type documentation (the interface's, for an interface
// member), and each lifecycle value's stage from its enum documentation.
//
// - planMembers(group): members and the concrete types each one stands for.
// - collectionGroup(groups, name): a type or interface as a group of one.
// - pickGuide(plans, docs): the companion document shared by the most members.
// - buildGroupModel(input): the GroupModel.
// - memberLabel, firstSentence, fieldLabel, isEmptyValue, valueText,
//   lifecycleValue, keyText, recordField, reverseField, fileTitle: small
//   readers the views share.
import {
  sharedLabelPrefix,
  typeLabel,
  type DisplayGroup,
  type DisplayGroupMember,
  type EnumValueDoc,
  type TypeDoc,
  type TypeFieldDoc,
  type TypeProfile,
} from "@rhizome/kit";

import type {
  FieldValue,
  GroupView,
  GuideNote,
  LinkedNote,
  ParsedRecord,
  ParsedRecords,
  TypeLabels,
} from "./api.ts";

export type { FieldValue, GroupView, LinkedNote, RecordRef, TypeLabels } from "./api.ts";

export type MemberPlan = {
  name: string;
  kind: "type" | "interface";
  label: string;
  pluralLabel: string;
  description: string;
  count: number;
  issueCount: number;
  /** The concrete types this member stands for that no earlier member claimed. */
  concreteTypes: readonly string[];
  /** Rail names that resolve to this member: its own and nested names adding no types. */
  covers: readonly string[];
};

export type FieldCondition = { field: string; equals: string };

/** A field of a member, merged across the concrete types that declare it. */
export type MemberField = {
  name: string;
  kind: TypeFieldDoc["kind"];
  typeName: string | null;
  list: boolean;
  /** `@display(importance: KEY)`. */
  key: boolean;
  /** Required on every concrete type that declares it. */
  required: boolean;
  /** `@requiresWhen` conditions that make it required. */
  requiredWhen: readonly FieldCondition[];
  policyReason: string | null;
  /** Enum values in declaration order; empty for other kinds. */
  values: readonly EnumValueDoc[];
  declaredBy: readonly string[];
};

/** The profile's lifecycle field, with its staged values in declaration order. */
export type Lifecycle = {
  field: string;
  values: readonly EnumValueDoc[];
  /** The concrete types whose records hold it. */
  declaredBy: readonly string[];
};

/** A forward `@link` field of a member. */
export type MemberLink = {
  field: string;
  /** The declared target type or interface. */
  targetType: string;
  list: boolean;
  /** The member the declared target belongs to; null when it is outside the group. */
  target: string | null;
  /** The display group of the declared target, when it has one. */
  targetGroup: string | null;
  /** The concrete types whose records hold it. */
  declaredBy: readonly string[];
};

export type GroupRecord = ParsedRecord & {
  member: string;
  /** The value of the member's summary field. */
  summary: string | null;
};

export type Member = Omit<MemberPlan, "covers"> & {
  fields: readonly MemberField[];
  summaryField: string | null;
  /** The PARENT-role link field that nests records into a tree. */
  parentField: string | null;
  lifecycle: Lifecycle | null;
  /** The profile's gap fields: KEY fields a record may leave empty. */
  gapFields: readonly string[];
  /** The profile's key text fields, in preference order. */
  keyTextFields: readonly string[];
  /** The member's whole profile, for its other field roles; null when it has none. */
  profile: TypeProfile | null;
  links: readonly MemberLink[];
  companionDocs: readonly string[];
  /** Loaded records, at most `RECORD_CAP` per concrete type. */
  records: readonly GroupRecord[];
  /** Some concrete type had more records than were read. */
  truncated: boolean;
};

export type Guide = GuideNote & { sharedBy: number };

export type GroupModel = {
  name: string;
  members: readonly Member[];
  memberIndex: ReadonlyMap<string, Member>;
  /** Member roots as the rail lists them; the default view follows this count. */
  rootCount: number;
  /** Every loaded record by key, in member order. */
  records: ReadonlyMap<string, GroupRecord>;
  /** Note records by path, for resolving link targets. */
  byPath: ReadonlyMap<string, GroupRecord>;
  /** A first word every member's plural label shares, dropped inside the group page. */
  labelPrefix: string;
  guide: Guide | null;
  /** Authored views the switcher offers, besides Overview, bundled views, and generated layouts. */
  views: readonly GroupView[];
  /** For a collection, its standard Table choice's id; null for a group. */
  tableChoice: string | null;
  /**
   * Labels and groups of every ontology type, for notes outside the group,
   * and of every interface navigation lists, for link targets typed by one.
   */
  typeLabels: ReadonlyMap<string, TypeLabels>;
  truncated: boolean;
};

export type GroupModelInput = {
  group: DisplayGroup;
  /** Every display group, for naming the group an outside link points into. */
  groups: readonly DisplayGroup[];
  /** Documentation for every member and every concrete type in `planMembers(group)`. */
  docs: Readonly<Record<string, TypeDoc>>;
  records: ParsedRecords;
  views: readonly GroupView[];
  /** A collection's standard Table choice id; omitted for a group. */
  tableChoice?: string | null;
  typeLabels: ReadonlyMap<string, TypeLabels>;
};

type PlanDraft = MemberPlan & { concreteTypes: string[]; covers: string[] };

/** The group's members in rail order, each with the concrete types it stands for. */
export function planMembers(group: DisplayGroup): MemberPlan[] {
  const claimed = new Set<string>();
  const plans: PlanDraft[] = [];

  const visit = (node: DisplayGroupMember, owner: PlanDraft | null) => {
    const types = node.kind === "interface" ? node.implementors : [node.name];
    const fresh = types.filter((type) => !claimed.has(type));
    let current = owner;

    if (owner === null || fresh.length > 0) {
      current = {
        name: node.name,
        kind: node.kind,
        label: node.label,
        pluralLabel: node.pluralLabel,
        description: node.description ?? "",
        count: node.count,
        issueCount: node.issueCount,
        concreteTypes: fresh,
        covers: [node.name],
      };
      plans.push(current);
    } else {
      owner.covers.push(node.name);
    }

    for (const type of fresh) claimed.add(type);

    for (const child of node.children) visit(child, current);
  };

  for (const root of group.members) visit(root, null);

  return plans;
}

function findNode(nodes: readonly DisplayGroupMember[], name: string): DisplayGroupMember | null {
  for (const node of nodes) {
    if (node.name === name) return node;
    const found = findNode(node.children, name);

    if (found) return found;
  }

  return null;
}

/**
 * A type or interface collection as a group of one member: its node in the
 * display-groups tree, without the display children the rail nests under it,
 * named by its plural label. Null when navigation does not list it.
 */
export function collectionGroup(
  groups: readonly DisplayGroup[],
  name: string,
): DisplayGroup | null {
  for (const group of groups) {
    const node = findNode(group.members, name);

    if (node) return { name: node.pluralLabel, members: [{ ...node, children: [] }] };
  }

  return null;
}

const docsOf = (plan: MemberPlan, docs: Readonly<Record<string, TypeDoc>>) =>
  plan.concreteTypes.flatMap((type) => docs[type] ?? []);

const companionPaths = (docs: readonly TypeDoc[]) => [
  ...new Set(docs.flatMap((doc) => (doc.companionDocs ?? []).map((companion) => companion.path))),
];

/**
 * A member's companion documents, best first: its own documentation's (an
 * interface's own), else its concrete types', the one most of them share first.
 */
function memberCompanions(plan: MemberPlan, docs: Readonly<Record<string, TypeDoc>>) {
  const own = companionPaths(docs[plan.name] ? [docs[plan.name]] : []);

  if (own.length) return own;
  const shared = new Map<string, number>();

  for (const doc of docsOf(plan, docs)) {
    for (const path of companionPaths([doc])) shared.set(path, (shared.get(path) ?? 0) + 1);
  }

  return [...shared].sort((a, b) => b[1] - a[1]).map(([path]) => path);
}

/**
 * The companion document the most members share, each member offering its
 * own first; ties go to the first one found.
 */
export function pickGuide(
  plans: readonly MemberPlan[],
  docs: Readonly<Record<string, TypeDoc>>,
): { path: string; sharedBy: number } | null {
  const counts = new Map<string, number>();

  for (const plan of plans) {
    for (const path of memberCompanions(plan, docs)) counts.set(path, (counts.get(path) ?? 0) + 1);
  }

  let best: { path: string; sharedBy: number } | null = null;

  for (const [path, sharedBy] of counts)
    if (!best || sharedBy > best.sharedBy) best = { path, sharedBy };

  return best;
}

type FieldDraft = MemberField & { requiredWhen: FieldCondition[]; declaredBy: string[] };

function fieldFromDoc(field: TypeFieldDoc, type: string): FieldDraft {
  return {
    name: field.name,
    kind: field.kind,
    typeName: field.typeName ?? null,
    list: field.list === true,
    key: field.display?.importance === "KEY",
    required: field.required === true,
    requiredWhen: [...(field.requiredWhen ?? [])],
    policyReason: field.policy?.reason ?? null,
    values: field.enum?.values ?? [],
    declaredBy: [type],
  };
}

const sameCondition = (a: FieldCondition, b: FieldCondition) =>
  a.field === b.field && a.equals === b.equals;

// Implementors can declare one name differently (text on one, a link on
// another); each declaration stays its own field, read only on its own types.
const declaration = (field: TypeFieldDoc) =>
  [field.name, field.kind, field.typeName ?? "", field.list === true].join("\u0000");

function mergeFields(docs: readonly TypeDoc[]): MemberField[] {
  const fields = new Map<string, FieldDraft>();

  for (const doc of docs) {
    for (const field of doc.fields) {
      const existing = fields.get(declaration(field));

      if (!existing) {
        fields.set(declaration(field), fieldFromDoc(field, doc.name));
        continue;
      }

      existing.declaredBy.push(doc.name);
      existing.required &&= field.required === true;
      existing.key ||= field.display?.importance === "KEY";
      existing.policyReason ??= field.policy?.reason ?? null;

      for (const condition of field.requiredWhen ?? []) {
        if (!existing.requiredWhen.some((known) => sameCondition(known, condition))) {
          existing.requiredWhen.push(condition);
        }
      }
    }
  }

  return [...fields.values()];
}

const hasStages = (values: readonly EnumValueDoc[]) =>
  values.some((value) => value.stage !== undefined);

/**
 * One lifecycle for the member's profile lifecycle field: every declaration
 * of it whose enum has stages, so implementors with their own status enums
 * keep their values. Values join in declaration order, first one winning.
 */
function memberLifecycle(fields: readonly MemberField[], name: string): Lifecycle | null {
  const declarations = fields.filter(
    (field) => field.name === name && field.kind === "enum" && hasStages(field.values),
  );

  if (!declarations.length) return null;

  const values = new Map<string, EnumValueDoc>();

  for (const field of declarations) {
    for (const value of field.values) if (!values.has(value.name)) values.set(value.name, value);
  }

  return {
    field: name,
    values: [...values.values()],
    declaredBy: declarations.flatMap((field) => field.declaredBy),
  };
}

/**
 * Each rail name of every group, mapped to its group, then each implementor
 * not listed anywhere by name, so a type's own group wins over the group of
 * an interface it implements.
 */
function groupsByType(groups: readonly DisplayGroup[]) {
  const byType = new Map<string, string>();
  const nodes: Array<{ node: DisplayGroupMember; group: string }> = [];

  const visit = (node: DisplayGroupMember, group: string) => {
    nodes.push({ node, group });

    for (const child of node.children) visit(child, group);
  };

  for (const group of groups) for (const root of group.members) visit(root, group.name);

  for (const { node, group } of nodes) if (!byType.has(node.name)) byType.set(node.name, group);

  for (const { node, group } of nodes) {
    for (const name of node.implementors) if (!byType.has(name)) byType.set(name, group);
  }

  return byType;
}

function memberOwners(plans: readonly MemberPlan[]) {
  const owners = new Map<string, string>();

  for (const plan of plans) {
    for (const name of [...plan.covers, ...plan.concreteTypes]) {
      if (!owners.has(name)) owners.set(name, plan.name);
    }
  }

  return owners;
}

const isText = (value: FieldValue | undefined): value is string => typeof value === "string";

const stringValue = (value: FieldValue | undefined) =>
  isText(value) && value !== "" ? value : null;

function buildMember(
  plan: MemberPlan,
  input: GroupModelInput,
  resolve: { owners: ReadonlyMap<string, string>; groups: ReadonlyMap<string, string> },
): Member {
  const docs = docsOf(plan, input.docs);
  const fields = mergeFields(docs);
  const summaryField = docs.find((doc) => doc.summaryField)?.summaryField ?? null;
  const profile = input.docs[plan.name]?.profile;
  const lifecycleField = profile?.lifecycleField;
  const pages = plan.concreteTypes.flatMap((type) => input.records.pages.get(type) ?? []);

  const links = fields.flatMap((field): MemberLink[] => {
    if (field.kind !== "link" || field.typeName === null) return [];
    const target = resolve.owners.get(field.typeName) ?? null;

    return [
      {
        field: field.name,
        targetType: field.typeName,
        list: field.list,
        target,
        targetGroup:
          target !== null
            ? input.group.name
            : (resolve.groups.get(field.typeName) ??
              input.typeLabels.get(field.typeName)?.group ??
              null),
        declaredBy: field.declaredBy,
      },
    ];
  });

  return {
    name: plan.name,
    kind: plan.kind,
    label: plan.label,
    pluralLabel: plan.pluralLabel,
    description: plan.description,
    count: plan.count,
    issueCount: plan.issueCount,
    concreteTypes: plan.concreteTypes,
    fields,
    summaryField,
    parentField: docs.find((doc) => doc.parentField)?.parentField ?? null,
    lifecycle: lifecycleField ? memberLifecycle(fields, lifecycleField) : null,
    gapFields: profile?.gapFields ?? [],
    keyTextFields: profile?.keyTextFields ?? [],
    profile: profile ?? null,
    links,
    companionDocs: companionPaths(docs),
    records: pages.flatMap((page) =>
      page.records.map((record) => {
        const field = input.docs[record.type]?.summaryField;

        return {
          ...record,
          member: plan.name,
          summary: field ? stringValue(record.values.get(field)) : null,
        };
      }),
    ),
    truncated: pages.some((page) => page.truncated),
  };
}

/** Type labels plus the labels of navigation nodes they lack, such as interfaces. */
function withNavigationLabels(
  labels: ReadonlyMap<string, TypeLabels>,
  groups: readonly DisplayGroup[],
): ReadonlyMap<string, TypeLabels> {
  const merged = new Map(labels);

  const visit = (node: DisplayGroupMember, group: string) => {
    if (!merged.has(node.name))
      merged.set(node.name, { label: node.label, pluralLabel: node.pluralLabel, group });

    for (const child of node.children) visit(child, group);
  };

  for (const group of groups) for (const root of group.members) visit(root, group.name);

  return merged;
}

/** Turn the loaded API responses for one group into its model. */
export function buildGroupModel(input: GroupModelInput): GroupModel {
  const plans = planMembers(input.group);
  const resolve = { owners: memberOwners(plans), groups: groupsByType(input.groups) };
  const members = plans.map((plan) => buildMember(plan, input, resolve));
  const records = new Map<string, GroupRecord>();
  const byPath = new Map<string, GroupRecord>();

  for (const member of members) {
    for (const record of member.records) {
      records.set(record.key, record);

      if (record.key === record.path) byPath.set(record.path, record);
    }
  }

  const guidePath = pickGuide(plans, input.docs);
  const guideNote = input.records.guide;

  return {
    name: input.group.name,
    members,
    memberIndex: new Map(members.map((member) => [member.name, member])),
    rootCount: input.group.members.length,
    records,
    byPath,
    labelPrefix: sharedLabelPrefix(members.map((member) => member.pluralLabel)),
    guide:
      guidePath === null
        ? null
        : {
            path: guidePath.path,
            title: guideNote?.path === guidePath.path ? guideNote.title : fileTitle(guidePath.path),
            summary: guideNote?.path === guidePath.path ? guideNote.summary : null,
            sharedBy: guidePath.sharedBy,
          },
    views: input.views,
    tableChoice: input.tableChoice ?? null,
    typeLabels: withNavigationLabels(input.typeLabels, input.groups),
    truncated: members.some((member) => member.truncated),
  };
}

/** A note's file name without its extension, for a note whose title was not read. */
export const fileTitle = (path: string) =>
  path
    .split("/")
    .at(-1)
    ?.replace(/\.[^.]+$/, "") ?? path;

/** A member's label inside the group page, dropping the shared prefix. */
export function memberLabel(
  model: GroupModel,
  member: Member,
  options: { count?: number; plural?: boolean } = {},
) {
  return typeLabel(member, { ...options, prefix: model.labelPrefix });
}

/** The first sentence of a description, for one-line summaries. */
export function firstSentence(text: string) {
  return (text.split(/(?<=[.!?])\s/)[0] ?? "").replace(/\s+/g, " ").trim();
}

export function isEmptyValue(value: FieldValue | readonly LinkedNote[] | undefined) {
  return (
    value === undefined ||
    value === null ||
    value === "" ||
    (Array.isArray(value) && value.length === 0)
  );
}

/** A field value as display text; lists join with commas. */
export function valueText(value: FieldValue | undefined) {
  if (value === undefined || value === null) return "";

  return Array.isArray(value) ? value.join(", ") : String(value);
}

/** A field's name as a column label: "governingSpecs" reads "Governing specs". */
export function fieldLabel(name: string) {
  const words = name
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .toLowerCase();

  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** The record's lifecycle value name, or null when the member has none or it is empty. */
export function lifecycleValue(member: Member, record: GroupRecord) {
  return member.lifecycle?.declaredBy.includes(record.type)
    ? stringValue(record.values.get(member.lifecycle.field))
    : null;
}

/**
 * A linked note lies outside the group: it is no loaded record, holds none,
 * and is not of a member type, which a record past the record cap would be.
 */
export function isOutside(model: GroupModel, note: LinkedNote) {
  // A section's path carries its fragment; the note before it holds the section.
  const holder = note.path.split("#", 1)[0];

  return (
    !model.records.has(note.key) &&
    !model.byPath.has(note.path) &&
    !model.byPath.has(holder) &&
    !model.members.some((member) => note.type !== null && member.concreteTypes.includes(note.type))
  );
}

/**
 * A reverse or neighbor field across a member's concrete types, which can
 * declare one name with different source types: every type declaring it, and
 * the first declaration's source type. Null when no type declares it.
 */
export function reverseField(member: Member, name: string) {
  const declarations = member.fields.filter(
    (field) => field.name === name && (field.kind === "reverse" || field.kind === "neighbor"),
  );

  if (!declarations.length) return null;

  return {
    declaredBy: declarations.flatMap((field) => field.declaredBy),
    targetType: declarations[0].typeName ?? name,
  };
}

/** The value of a link field or a scalar field on a record whose type declares it. */
export function recordField(record: GroupRecord, field: MemberField) {
  if (field.kind === "link")
    return linkTargets(record, { field: field.name, declaredBy: field.declaredBy });

  return fieldValue(record, field);
}

/** The record's first filled profile key text field, labeled. */
export function keyText(member: Member, record: GroupRecord) {
  for (const name of member.keyTextFields) {
    for (const field of member.fields) {
      if (field.name !== name || field.kind !== "scalar") continue;
      const value = fieldValue(record, field);

      if (!isEmptyValue(value)) return { label: fieldLabel(name), text: valueText(value) };
    }
  }

  return null;
}

/** A scalar or enum field's value on a record whose type declares it. */
export function fieldValue(record: GroupRecord, field: MemberField) {
  return field.declaredBy.includes(record.type) ? record.values.get(field.name) : undefined;
}

/** A record's targets for a link its type declares. */
export function linkTargets(
  record: GroupRecord,
  link: Pick<MemberLink, "field" | "declaredBy">,
): readonly LinkedNote[] {
  return link.declaredBy.includes(record.type) ? (record.links.get(link.field) ?? []) : [];
}
