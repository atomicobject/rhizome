// Decodes the kit's own schema reads at the boundary, so a view never receives
// a mistyped value. Go omits empty strings, lists, and labels, so absent lists
// become empty and absent labels fall back to the name. A response that does
// not match its contract throws, naming the first part that did not.
import {
  isBoolean,
  isFiniteNumber,
  isString,
  type JsonObject,
  type JsonValue,
} from "../src/api/parse";
import type {
  DisplayGroup,
  DisplayGroupMember,
  DisplayGroupsResponse,
  EnumDoc,
  EnumValueDoc,
  FieldDisplayDoc,
  TypeDoc,
  TypeFieldDoc,
  TypeProfile,
} from "./types";

function isObject(value: JsonValue | undefined): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function fail(where: string): never {
  throw new Error(`Rhizome sent an unexpected ${where}`);
}

function record(value: JsonValue | undefined, where: string) {
  return isObject(value) ? value : fail(where);
}

function text(object: JsonObject, key: string, where: string) {
  const value = object[key];

  return isString(value) ? value : fail(`${where}.${key}`);
}

function optionalText(object: JsonObject, key: string, where: string) {
  const value = object[key];

  return value === undefined || isString(value) ? value : fail(`${where}.${key}`);
}

function optionalNumber(object: JsonObject, key: string, where: string) {
  const value = object[key];

  return value === undefined || isFiniteNumber(value) ? value : fail(`${where}.${key}`);
}

function optionalFlag(object: JsonObject, key: string, where: string) {
  const value = object[key];

  return value === undefined || isBoolean(value) ? value : fail(`${where}.${key}`);
}

function oneOf<T extends string>(
  values: readonly T[],
  value: JsonValue | undefined,
  where: string,
) {
  return values.find((candidate) => candidate === value) ?? fail(where);
}

/** A list, empty when absent. */
function list<T>(
  object: JsonObject,
  key: string,
  where: string,
  item: (value: JsonValue, where: string) => T,
) {
  const value = object[key] ?? [];

  if (!Array.isArray(value)) fail(`${where}.${key}`);

  return value.map((entry, index) => item(entry, `${where}.${key}[${index}]`));
}

const STAGES = ["open", "active", "done", "dropped"] as const;

const asText = (value: JsonValue, where: string) => (isString(value) ? value : fail(where));

function enumValueDoc(value: JsonValue, where: string): EnumValueDoc {
  const doc = record(value, where);

  return {
    name: text(doc, "name", where),
    summary: optionalText(doc, "summary", where),
    label: optionalText(doc, "label", where),
    order: optionalNumber(doc, "order", where),
    tone: optionalText(doc, "tone", where),
    collapsed: optionalFlag(doc, "collapsed", where),
    stage: doc.stage === undefined ? undefined : oneOf(STAGES, doc.stage, `${where}.stage`),
    stageDeclared: optionalFlag(doc, "stageDeclared", where),
  };
}

function enumDoc(value: JsonValue, where: string): EnumDoc {
  const doc = record(value, where);

  return {
    name: text(doc, "name", where),
    summary: optionalText(doc, "summary", where),
    values: list(doc, "values", where, enumValueDoc),
  };
}

const FIELD_KINDS = ["scalar", "enum", "link", "reverse", "neighbor", "section"] as const;

function fieldDisplay(value: JsonValue | undefined, where: string): FieldDisplayDoc | undefined {
  if (value === undefined) return undefined;
  const display = record(value, where);

  return {
    role:
      display.role === undefined
        ? undefined
        : oneOf(["SUMMARY", "PARENT", "RANK"] as const, display.role, `${where}.role`),
    importance:
      display.importance === undefined
        ? undefined
        : oneOf(["KEY", "NORMAL", "DETAIL"] as const, display.importance, `${where}.importance`),
  };
}

function fieldDoc(value: JsonValue, where: string): TypeFieldDoc {
  const doc = record(value, where);
  const policy = doc.policy === undefined ? undefined : record(doc.policy, `${where}.policy`);
  const values = doc.enum === undefined ? undefined : record(doc.enum, `${where}.enum`);

  return {
    name: text(doc, "name", where),
    kind: oneOf(FIELD_KINDS, doc.kind, `${where}.kind`),
    typeName: optionalText(doc, "typeName", where),
    required: optionalFlag(doc, "required", where),
    list: optionalFlag(doc, "list", where),
    summary: optionalText(doc, "summary", where),
    display: fieldDisplay(doc.display, `${where}.display`),
    policy: policy && { reason: optionalText(policy, "reason", `${where}.policy`) },
    enum: values && { values: list(values, "values", `${where}.enum`, enumValueDoc) },
    requiredWhen:
      doc.requiredWhen === undefined
        ? undefined
        : list(doc, "requiredWhen", where, (entry, at) => {
            const condition = record(entry, at);

            return { field: text(condition, "field", at), equals: text(condition, "equals", at) };
          }),
  };
}

const PROFILE_KINDS = ["workflow", "contract", "dated", "catalog", "reference"] as const;

/**
 * The profile, or undefined when absent or of a shape this kit does not know:
 * a newer server may add shapes, and the rest of the type's documentation
 * still serves the view.
 */
function typeProfile(value: JsonValue | undefined, where: string): TypeProfile | undefined {
  if (value === undefined) return undefined;
  const profile = record(value, where);
  const names = (key: string) => list(profile, key, where, asText);
  const kind = PROFILE_KINDS.find((candidate) => candidate === profile.shape);

  if (kind === undefined) return undefined;

  return {
    // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
    shape: kind,
    lifecycleField: optionalText(profile, "lifecycleField", where),
    orderedFields: names("orderedFields"),
    categoryFields: names("categoryFields"),
    summaryField: optionalText(profile, "summaryField", where),
    rankField: optionalText(profile, "rankField", where),
    primaryDateField: optionalText(profile, "primaryDateField", where),
    peopleFields: names("peopleFields"),
    keyTextFields: names("keyTextFields"),
    relationFields: names("relationFields"),
    reverseFields: names("reverseFields"),
    gapFields: names("gapFields"),
  };
}

const TYPE_ROLES = ["NOTE", "SECTION", "EMBEDDED_NODE", "INTERFACE"] as const;

/** `GET /api/v1/ontology/types/{name}?notes=none`; null when Rhizome has no such type. */
export function parseTypeDocResponse(value: JsonValue, name: string): TypeDoc | null {
  const where = `type ${name}`;
  const response = record(value, where);

  if (response.type === undefined || response.type === null) return null;
  const doc = record(response.type, where);
  const typeName = text(doc, "name", where);
  const label = optionalText(doc, "label", where) || typeName;

  return {
    name: typeName,
    role: doc.role === undefined ? undefined : oneOf(TYPE_ROLES, doc.role, `${where}.role`),
    label,
    pluralLabel: optionalText(doc, "pluralLabel", where) || label,
    summary: optionalText(doc, "summary", where),
    description: optionalText(doc, "description", where),
    implements: list(doc, "implements", where, asText),
    fields: list(doc, "fields", where, fieldDoc),
    enums: list(doc, "enums", where, enumDoc),
    companionDocs: list(doc, "companionDocs", where, (entry, at) => {
      const companion = record(entry, at);

      return { path: text(companion, "path", at), purpose: optionalText(companion, "purpose", at) };
    }),
    summaryField: optionalText(doc, "summaryField", where),
    parentField: optionalText(doc, "parentField", where),
    profile: typeProfile(doc.profile, `${where}.profile`),
  };
}

function groupMember(value: JsonValue, where: string): DisplayGroupMember {
  const member = record(value, where);
  const count = member.count;
  const issueCount = member.issueCount;

  return {
    name: text(member, "name", where),
    kind: oneOf(["type", "interface"] as const, member.kind, `${where}.kind`),
    label: text(member, "label", where),
    pluralLabel: text(member, "pluralLabel", where),
    description: optionalText(member, "description", where),
    count: isFiniteNumber(count) ? count : fail(`${where}.count`),
    issueCount: isFiniteNumber(issueCount) ? issueCount : fail(`${where}.issueCount`),
    implementors: list(member, "implementors", where, asText),
    children: list(member, "children", where, groupMember),
  };
}

/** `GET /api/v1/display-groups`. */
export function parseDisplayGroups(value: JsonValue): DisplayGroupsResponse {
  const where = "display groups response";

  const groups = list(record(value, where), "groups", where, (entry, at): DisplayGroup => {
    const group = record(entry, at);

    if (!Array.isArray(group.members)) fail(`${at}.members`);

    return { name: text(group, "name", at), members: list(group, "members", at, groupMember) };
  });

  return { groups };
}
